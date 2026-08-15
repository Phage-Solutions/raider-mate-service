package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the values the worker reads from the environment at startup.
type Config struct {
	DatabaseURL         string
	LogLevel            string
	SyncInterval        time.Duration
	SyncStaleAfter      time.Duration
	SyncBatch           int32
	RaiderIOBaseURL     string
	RaiderIOMinInterval time.Duration
	JobPollInterval     time.Duration
	JobBatch            int32
}

func loadConfig() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	// Non-positive values here do not fail loudly at the point of use: a zero
	// interval panics time.NewTicker, and a zero batch turns into LIMIT 0, which
	// syncs nothing while the worker looks healthy.
	syncInterval, err := envDuration("SYNC_INTERVAL", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	if syncInterval <= 0 {
		return Config{}, fmt.Errorf("SYNC_INTERVAL must be positive, got %s", syncInterval)
	}
	syncStaleAfter, err := envDuration("SYNC_STALE_AFTER", 6*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if syncStaleAfter <= 0 {
		return Config{}, fmt.Errorf("SYNC_STALE_AFTER must be positive, got %s", syncStaleAfter)
	}
	syncBatch, err := envInt32("SYNC_BATCH", 50)
	if err != nil {
		return Config{}, err
	}
	if syncBatch <= 0 {
		return Config{}, fmt.Errorf("SYNC_BATCH must be positive, got %d", syncBatch)
	}
	raiderIOMinInterval, err := envDuration("RAIDERIO_MIN_INTERVAL", 250*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	// Zero is legal here and means "no gating".
	if raiderIOMinInterval < 0 {
		return Config{}, fmt.Errorf("RAIDERIO_MIN_INTERVAL must not be negative, got %s", raiderIOMinInterval)
	}

	raiderIOBaseURL := os.Getenv("RAIDERIO_BASE_URL")

	jobPollInterval, err := envDuration("JOB_POLL_INTERVAL", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	if jobPollInterval <= 0 {
		return Config{}, fmt.Errorf("JOB_POLL_INTERVAL must be positive, got %s", jobPollInterval)
	}
	jobBatch, err := envInt32("JOB_BATCH", 100)
	if err != nil {
		return Config{}, err
	}
	if jobBatch <= 0 {
		return Config{}, fmt.Errorf("JOB_BATCH must be positive, got %d", jobBatch)
	}

	return Config{
		DatabaseURL:         databaseURL,
		LogLevel:            logLevel,
		SyncInterval:        syncInterval,
		SyncStaleAfter:      syncStaleAfter,
		SyncBatch:           syncBatch,
		RaiderIOBaseURL:     raiderIOBaseURL,
		RaiderIOMinInterval: raiderIOMinInterval,
		JobPollInterval:     jobPollInterval,
		JobBatch:            jobBatch,
	}, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", name, err)
	}
	return d, nil
}

func envInt32(name string, fallback int32) (int32, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", name, err)
	}
	return int32(n), nil
}
