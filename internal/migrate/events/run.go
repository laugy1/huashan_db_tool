package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tommyliu/huashan_db_tool/internal/config"
	"github.com/tommyliu/huashan_db_tool/internal/sink"
	"github.com/tommyliu/huashan_db_tool/internal/source"
)

// Stats 描述 events 階段一遷移結果。
type Stats struct {
	SiteID       int
	Collection   string
	RowsRead     int64
	DocsWritten  int64
	Duration     time.Duration
}

// RunPhase1 將 SiteID 1（華山）與 2（烏梅）活動寫入對應 MongoDB collection。
func RunPhase1(ctx context.Context, cfg *config.EventsConfig, src *source.Source, dst *sink.Sink, logger *slog.Logger) ([]Stats, error) {
	catIdx, err := LoadCategoryIndex(ctx, dst.Collection(categoryCollection))
	if err != nil {
		return nil, fmt.Errorf("load category index: %w", err)
	}
	logger.Info("loaded category index", "entries", catIdx.Len())

	for _, siteID := range []int{1, 2} {
		profile := siteProfiles[siteID]
		if err := catIdx.RequireCategory(categoryIDEventCategory, profile.CategoryRelated, categoryNameHistorical); err != nil {
			return nil, err
		}
	}

	sites := []int{1, 2}
	results := make([]Stats, 0, len(sites))

	for _, siteID := range sites {
		profile, ok := siteProfiles[siteID]
		if !ok {
			return results, fmt.Errorf("unsupported site_id %d", siteID)
		}

		st, err := runSite(ctx, cfg, src, dst, profile, catIdx, logger)
		results = append(results, st)
		if err != nil {
			return results, fmt.Errorf("site_id=%d: %w", siteID, err)
		}
	}
	return results, nil
}

func runSite(ctx context.Context, cfg *config.EventsConfig, src *source.Source, dst *sink.Sink, profile siteProfile, catIdx *CategoryIndex, logger *slog.Logger) (Stats, error) {
	start := time.Now()
	st := Stats{SiteID: profile.SiteID, Collection: profile.Collection}

	log := logger.With("site_id", profile.SiteID, "collection", profile.Collection)
	log.Info("starting events phase1 migration")

	if cfg.Truncate {
		n, err := dst.Truncate(ctx, profile.Collection)
		if err != nil {
			return st, err
		}
		log.Info("truncated target collection", "deleted", n)
	}

	inserter := dst.NewBulkInserter(profile.Collection, cfg.BatchSize)

	rows, err := src.StreamParams(ctx, eventsQuery, []any{profile.SiteID}, func(row source.Row) error {
		doc, err := buildDocument(row, profile, catIdx)
		if err != nil {
			return fmt.Errorf("event_id=%v: %w", row["event_id"], err)
		}
		if err := inserter.Add(ctx, doc); err != nil {
			return err
		}
		st.DocsWritten++
		return nil
	})
	if err != nil {
		return st, err
	}
	st.RowsRead = rows

	if err := inserter.Flush(ctx); err != nil {
		return st, err
	}

	st.Duration = time.Since(start)
	log.Info("events phase1 migration finished",
		"rows_read", st.RowsRead,
		"docs_written", st.DocsWritten,
		"duration", st.Duration.String(),
	)
	return st, nil
}
