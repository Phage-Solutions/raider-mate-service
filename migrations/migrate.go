package migrations

import (
	"context"
	"database/sql"
	"fmt"

	// goose talks database/sql, so pgx registers its stdlib driver under "pgx".
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Up applies every pending migration from the embedded FS. It opens its own
// database/sql connection because goose does not speak pgx natively, and closes
// it again before returning.
func Up(ctx context.Context, databaseURL string) error {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("opening migration connection: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	goose.SetBaseFS(FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}
