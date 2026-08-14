package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phage-Solutions/raider-mate-service/internal/raiderio"
	"github.com/Phage-Solutions/raider-mate-service/internal/roster"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	client := raiderio.NewClient(cfg.RaiderIOBaseURL, cfg.RaiderIOMinInterval)
	store := roster.NewStore(pool)
	syncer := roster.NewSyncer(client, store, logger)

	logger.Info("starting worker",
		"sync_interval", cfg.SyncInterval,
		"sync_stale_after", cfg.SyncStaleAfter,
		"sync_batch", cfg.SyncBatch,
	)

	tick := func() {
		if err := syncer.SyncDue(ctx, cfg.SyncStaleAfter, cfg.SyncBatch); err != nil {
			logger.ErrorContext(ctx, "sync tick failed", "error", err)
		}
	}

	// Sync once up front. A worker restarted more often than SyncInterval would
	// otherwise never reach its first tick.
	tick()

	ticker := time.NewTicker(cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tick()
		case <-ctx.Done():
			logger.Info("shutting down")
			return nil
		}
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}
