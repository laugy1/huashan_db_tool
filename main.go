// Command huashan_db_tool migrates data from MS SQL Server into MongoDB
// according to a YAML configuration file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tommyliu/huashan_db_tool/internal/cmsapi"
	"github.com/tommyliu/huashan_db_tool/internal/config"
	"github.com/tommyliu/huashan_db_tool/internal/migrate/events"
	"github.com/tommyliu/huashan_db_tool/internal/pipeline"
	"github.com/tommyliu/huashan_db_tool/internal/sink"
	"github.com/tommyliu/huashan_db_tool/internal/source"
)

func main() {
	var (
		cfgPath  = flag.String("c", "config.yaml", "path to YAML config file")
		task     = flag.String("task", "events", "migration task: generic | events | events-images")
		jsonLog  = flag.Bool("json", false, "emit logs as JSON")
		dryRun   = flag.Bool("dry-run", false, "validate config & connections, then exit without writing")
		logLevel = flag.String("log-level", "info", "log level: debug | info | warn | error")
	)
	flag.Parse()

	logger := newLogger(*logLevel, *jsonLog)

	if err := run(*cfgPath, *task, *dryRun, logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfgPath, task string, dryRun bool, logger *slog.Logger) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	logger.Info("config loaded", "path", cfgPath, "task", task)

	switch task {
	case "generic":
		if len(cfg.Migrations) == 0 {
			return fmt.Errorf("task=generic requires at least one migration in config")
		}
	case "events", "events-images":
	default:
		return fmt.Errorf("unknown task %q (use generic, events, or events-images)", task)
	}

	if task == "events-images" {
		if err := cfg.ValidateCMS(); err != nil {
			return err
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	connectCtx, connectCancel := context.WithTimeout(ctx, 30*time.Second)
	defer connectCancel()

	if task == "events-images" {
		return runEventsImages(ctx, connectCtx, cfg, dryRun, logger)
	}

	src, err := source.Open(connectCtx, cfg.MSSQLDSN())
	if err != nil {
		return fmt.Errorf("connect mssql: %w", err)
	}
	defer src.Close()
	logger.Info("connected to MS SQL Server", "server", cfg.MSSQL.Server, "database", cfg.MSSQL.Database)

	dst, err := sink.Open(connectCtx, cfg.MongoURI(), cfg.MongoDB.Database)
	if err != nil {
		return fmt.Errorf("connect mongo: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = dst.Close(closeCtx)
	}()
	logger.Info("connected to MongoDB", "database", cfg.MongoDB.Database)

	if dryRun {
		logger.Info("dry-run complete: config valid & both databases reachable")
		return nil
	}

	start := time.Now()
	switch task {
	case "events":
		stats, err := events.RunPhase1(ctx, cfg.Events, src, dst, logger)
		reportEvents(logger, stats, time.Since(start))
		return err
	default:
		stats, err := pipeline.Run(ctx, cfg, src, dst, logger)
		reportGeneric(logger, stats, time.Since(start))
		return err
	}
}

func runEventsImages(ctx, connectCtx context.Context, cfg *config.Config, dryRun bool, logger *slog.Logger) error {
	lookupMode := cfg.EventsImages.ModeIsLookup()

	var src *source.Source
	if !lookupMode {
		var err error
		src, err = source.Open(connectCtx, cfg.MSSQLDSN())
		if err != nil {
			return fmt.Errorf("connect mssql: %w", err)
		}
		defer src.Close()
		logger.Info("connected to MS SQL Server", "server", cfg.MSSQL.Server, "database", cfg.MSSQL.Database)
	}

	dst, err := sink.Open(connectCtx, cfg.MongoURI(), cfg.MongoDB.Database)
	if err != nil {
		return fmt.Errorf("connect mongo: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = dst.Close(closeCtx)
	}()
	logger.Info("connected to MongoDB", "database", cfg.MongoDB.Database)

	var cms *cmsapi.Client
	if !lookupMode {
		cms = cmsapi.NewClient(cfg.CMS.BaseURL, cfg.CMS.Username, cfg.CMS.Password)
	}

	if dryRun {
		return events.DryRunPhase2(ctx, cfg.EventsImages, cms, dst, logger)
	}

	if !lookupMode {
		if err := cms.Login(ctx); err != nil {
			return fmt.Errorf("cms login: %w", err)
		}
		logger.Info("cms login ok", "base_url", cfg.CMS.BaseURL)
	} else {
		logger.Info("events-images lookup mode: skipping download/upload, linking existing media")
	}

	start := time.Now()
	stats, err := events.RunPhase2(ctx, cfg.EventsImages, cms, src, dst, logger)
	reportEventsImages(logger, stats, time.Since(start))
	return err
}

func reportGeneric(logger *slog.Logger, stats []pipeline.Stats, total time.Duration) {
	var totalRows, totalDocs int64
	for _, s := range stats {
		totalRows += s.RowsRead
		totalDocs += s.DocsWritten
	}
	logger.Info("migration summary",
		"migrations", len(stats),
		"rows_read", totalRows,
		"docs_written", totalDocs,
		"total_duration", total.String(),
	)
}

func reportEvents(logger *slog.Logger, stats []events.Stats, total time.Duration) {
	var totalRows, totalDocs int64
	for _, s := range stats {
		totalRows += s.RowsRead
		totalDocs += s.DocsWritten
		logger.Info("site migration done",
			"site_id", s.SiteID,
			"collection", s.Collection,
			"rows_read", s.RowsRead,
			"docs_written", s.DocsWritten,
			"duration", s.Duration.String(),
		)
	}
	logger.Info("events phase1 summary",
		"sites", len(stats),
		"rows_read", totalRows,
		"docs_written", totalDocs,
		"total_duration", total.String(),
	)
}

func reportEventsImages(logger *slog.Logger, stats []events.ImagesStats, total time.Duration) {
	var scanned, updated, uploaded, failed int64
	for _, s := range stats {
		scanned += s.DocsScanned
		updated += s.DocsUpdated
		uploaded += s.ImagesUploaded
		failed += s.ImagesFailed
		logger.Info("collection image migration done",
			"collection", s.Collection,
			"docs_scanned", s.DocsScanned,
			"docs_updated", s.DocsUpdated,
			"images_uploaded", s.ImagesUploaded,
			"images_failed", s.ImagesFailed,
			"duration", s.Duration.String(),
		)
	}
	logger.Info("events phase2 summary",
		"collections", len(stats),
		"docs_scanned", scanned,
		"docs_updated", updated,
		"images_uploaded", uploaded,
		"images_failed", failed,
		"total_duration", total.String(),
	)
}

func newLogger(level string, asJSON bool) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if asJSON {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}
