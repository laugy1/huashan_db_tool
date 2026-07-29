// Package source connects to MS SQL Server and streams rows as map[string]any.
package source

import (
	"context"
	"database/sql"
	"fmt"

	// 註冊 mssql driver。
	_ "github.com/microsoft/go-mssqldb"
)

// Source wraps a *sql.DB connected to a MS SQL Server instance.
type Source struct {
	db *sql.DB
}

// Open dials SQL Server using the supplied DSN and verifies connectivity.
func Open(ctx context.Context, dsn string) (*Source, error) {
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlserver: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlserver: %w", err)
	}
	return &Source{db: db}, nil
}

// Close releases the underlying connection pool.
func (s *Source) Close() error { return s.db.Close() }

// Row 為 column 名稱對應到值的對應表。
type Row = map[string]any

// RowHandler 會收到每一筆 row；若回傳 error，串流即中斷。
type RowHandler func(Row) error

// Stream 執行 query 並以 streaming 方式呼叫 handler，避免將整個結果集留在記憶體。
// 欄位值會盡量轉換成 Go 原生型別（int64、float64、string、bool、time.Time、[]byte、nil）。
func (s *Source) Stream(ctx context.Context, query string, handler RowHandler) (int64, error) {
	return s.StreamParams(ctx, query, nil, handler)
}

// StreamParams 與 Stream 相同，但支援 SQL 參數（@p1, @p2, ...）。
func (s *Source) StreamParams(ctx context.Context, query string, args []any, handler RowHandler) (int64, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("columns: %w", err)
	}

	var count int64
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return count, fmt.Errorf("scan: %w", err)
		}
		row := make(Row, len(cols))
		for i, c := range cols {
			row[c] = normalize(values[i])
		}
		if err := handler(row); err != nil {
			return count, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("rows: %w", err)
	}
	return count, nil
}

// normalize 把 driver 回傳的 []byte 視情況轉成 string，其他型別維持原樣。
// SQL Server 的 varchar/nvarchar/text/ntext 可能以 []byte 形式回傳，存進 Mongo 之前
// 轉成 string 比較直觀。
func normalize(v any) any {
	switch x := v.(type) {
	case []byte:
		// 對於非 UTF-8 binary，呼叫端可以在 Rename / 後處理階段自行調整。
		return string(x)
	default:
		return x
	}
}
