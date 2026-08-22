package billing

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Raider-Mate/raider-mate-service/internal/db"
)

// Store implements tierStore over Postgres.
type Store struct {
	queries *db.Queries
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: db.New(pool)}
}

func (s *Store) Subscription(ctx context.Context, discordGuildID int64) (Subscription, bool, error) {
	row, err := s.queries.GetSubscription(ctx, discordGuildID)
	// No row is the common case and the correct answer, not a failure to look.
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, false, nil
	}
	if err != nil {
		return Subscription{}, false, err
	}

	sub := Subscription{
		Tier:   Tier(row.Tier),
		Status: Status(row.Status),
	}
	if row.CurrentPeriodEnd.Valid {
		end := row.CurrentPeriodEnd.Time
		sub.CurrentPeriodEnd = &end
	}
	return sub, true, nil
}
