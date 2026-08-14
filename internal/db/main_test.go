//go:build integration

package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Phage-Solutions/raider-mate-service/migrations"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	// os.Exit skips deferred calls, so the container teardown lives in a function
	// that returns the exit code instead.
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("raidermate_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "starting postgres container:", err)
		return 1
	}
	defer pgContainer.Terminate(ctx) //nolint:errcheck

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "getting connection string:", err)
		return 1
	}

	if err := migrate(connStr); err != nil {
		fmt.Fprintln(os.Stderr, "migrating:", err)
		return 1
	}

	pool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connecting pool:", err)
		return 1
	}
	defer pool.Close()

	return m.Run()
}

func migrate(connStr string) error {
	sqlDB, err := sql.Open("pgx", connStr)
	if err != nil {
		return fmt.Errorf("opening migration connection: %w", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}
