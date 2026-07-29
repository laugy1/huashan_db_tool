// Package pipeline executes Migration definitions: read from SQL Server,
// optionally group consecutive rows into embedded documents, then bulk upsert to MongoDB.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/tommyliu/huashan_db_tool/internal/config"
	"github.com/tommyliu/huashan_db_tool/internal/sink"
	"github.com/tommyliu/huashan_db_tool/internal/source"
)

// Stats 描述一次 migration 的執行結果。
type Stats struct {
	Name        string
	RowsRead    int64
	DocsWritten int64
	Duration    time.Duration
}

// Run 依序執行 cfg 中所有 migration。任何一個失敗即中斷並回傳已完成的統計資訊。
func Run(ctx context.Context, cfg *config.Config, src *source.Source, dst *sink.Sink, logger *slog.Logger) ([]Stats, error) {
	results := make([]Stats, 0, len(cfg.Migrations))
	for _, m := range cfg.Migrations {
		s, err := runOne(ctx, m, src, dst, logger)
		results = append(results, s)
		if err != nil {
			return results, fmt.Errorf("migration %q: %w", m.Name, err)
		}
	}
	return results, nil
}

func runOne(ctx context.Context, m config.Migration, src *source.Source, dst *sink.Sink, logger *slog.Logger) (Stats, error) {
	start := time.Now()
	st := Stats{Name: m.Name}

	log := logger.With("migration", m.Name, "collection", m.TargetCollection)
	log.Info("starting migration")

	if m.Truncate {
		n, err := dst.Truncate(ctx, m.TargetCollection)
		if err != nil {
			return st, err
		}
		log.Info("truncated target collection", "deleted", n)
	}

	writer := dst.NewBulkUpserter(m.TargetCollection, m.UpsertKey, m.BatchSize)

	agg := newAggregator(m, writer, log)

	rows, err := src.Stream(ctx, m.Query, func(row source.Row) error {
		return agg.handle(ctx, row)
	})
	if err != nil {
		return st, err
	}
	st.RowsRead = rows

	if err := agg.finalize(ctx); err != nil {
		return st, err
	}
	if err := writer.Flush(ctx); err != nil {
		return st, err
	}

	st.DocsWritten = agg.written
	st.Duration = time.Since(start)
	log.Info("migration finished",
		"rows_read", st.RowsRead,
		"docs_written", st.DocsWritten,
		"duration", st.Duration.String(),
	)
	return st, nil
}

// aggregator 負責把連續、group_by 鍵相同的 row 合併成單一文件，
// 並把 group_by 之外的欄位放進 EmbedAs 子陣列。
//
// 若 EmbedAs 為空但 GroupBy 不為空：仍會去重，僅保留每組第一筆的非 group_by 欄位。
// 若 GroupBy 也為空：1:1 寫入。
type aggregator struct {
	m       config.Migration
	writer  *sink.BulkUpserter
	log     *slog.Logger
	written int64

	currentKey []any
	currentDoc map[string]any
	currentArr []map[string]any
}

func newAggregator(m config.Migration, writer *sink.BulkUpserter, log *slog.Logger) *aggregator {
	return &aggregator{m: m, writer: writer, log: log}
}

func (a *aggregator) handle(ctx context.Context, row source.Row) error {
	// 1:1 直接寫入
	if len(a.m.GroupBy) == 0 {
		doc := renameKeys(row, a.m.Rename)
		if err := a.writer.Add(ctx, doc); err != nil {
			return err
		}
		a.written++
		return nil
	}

	key := extractKey(row, a.m.GroupBy)

	if a.currentDoc == nil {
		a.startGroup(row, key)
		return nil
	}
	if !sameKey(key, a.currentKey) {
		if err := a.flushGroup(ctx); err != nil {
			return err
		}
		a.startGroup(row, key)
		return nil
	}

	// 同一組：追加 embed
	if a.m.EmbedAs != "" {
		if child := childPart(row, a.m.GroupBy, a.m.Rename); child != nil {
			a.currentArr = append(a.currentArr, child)
		}
	}
	return nil
}

func (a *aggregator) startGroup(row source.Row, key []any) {
	a.currentKey = key
	a.currentDoc = parentPart(row, a.m.GroupBy, a.m.Rename, a.m.EmbedAs)
	a.currentArr = a.currentArr[:0]
	if a.m.EmbedAs != "" {
		if child := childPart(row, a.m.GroupBy, a.m.Rename); child != nil {
			a.currentArr = append(a.currentArr, child)
		}
	}
}

func (a *aggregator) flushGroup(ctx context.Context) error {
	if a.currentDoc == nil {
		return nil
	}
	if a.m.EmbedAs != "" {
		// 把累積的子文件複製一份，避免共用底層陣列。
		out := make([]map[string]any, len(a.currentArr))
		copy(out, a.currentArr)
		a.currentDoc[a.m.EmbedAs] = out
	}
	if err := a.writer.Add(ctx, a.currentDoc); err != nil {
		return err
	}
	a.written++
	a.currentDoc = nil
	a.currentArr = a.currentArr[:0]
	a.currentKey = nil
	return nil
}

func (a *aggregator) finalize(ctx context.Context) error {
	if len(a.m.GroupBy) == 0 {
		return nil
	}
	return a.flushGroup(ctx)
}

// extractKey 依 group_by 欄位順序抓出值。
func extractKey(row source.Row, groupBy []string) []any {
	out := make([]any, len(groupBy))
	for i, c := range groupBy {
		out[i] = row[c]
	}
	return out
}

func sameKey(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// parentPart 取出父文件的欄位（含 group_by），會排除掉準備放進 EmbedAs 的欄位。
// 若 EmbedAs 為空，則父文件包含所有欄位（取每組第一筆）。
func parentPart(row source.Row, groupBy []string, rename map[string]string, embedAs string) map[string]any {
	gb := toSet(groupBy)
	out := make(map[string]any, len(row))
	for k, v := range row {
		if embedAs != "" && !gb[k] {
			continue
		}
		out[renameKey(k, rename)] = v
	}
	return out
}

// childPart 回傳「不在 group_by 內」的欄位作為子文件。
// 若所有欄位都在 group_by（亦即沒有真正的子欄位，或 LEFT JOIN 沒有對到），回傳 nil。
func childPart(row source.Row, groupBy []string, rename map[string]string) map[string]any {
	gb := toSet(groupBy)
	out := make(map[string]any)
	allNil := true
	for k, v := range row {
		if gb[k] {
			continue
		}
		out[renameKey(k, rename)] = v
		if v != nil {
			allNil = false
		}
	}
	if len(out) == 0 {
		return nil
	}
	// LEFT JOIN 未匹配時，所有子欄位都是 NULL，視為無子文件。
	if allNil {
		return nil
	}
	return out
}

func renameKeys(row source.Row, rename map[string]string) map[string]any {
	if len(rename) == 0 {
		out := make(map[string]any, len(row))
		for k, v := range row {
			out[k] = v
		}
		return out
	}
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[renameKey(k, rename)] = v
	}
	return out
}

func renameKey(k string, rename map[string]string) string {
	if v, ok := rename[k]; ok && v != "" {
		return v
	}
	return k
}

func toSet(ss []string) map[string]bool {
	out := make(map[string]bool, len(ss))
	for _, s := range ss {
		out[s] = true
	}
	return out
}
