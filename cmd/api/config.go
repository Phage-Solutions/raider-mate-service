package main

import (
	"fmt"
	"os"
)

// Config holds the values the service reads from the environment at startup.
type Config struct {
	DatabaseURL string
	Addr        string
	LogLevel    string
}

func loadConfig() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	return Config{
		DatabaseURL: databaseURL,
		Addr:        addr,
		LogLevel:    logLevel,
	}, nil
}
