package roster

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Store implements syncStore over Postgres.
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: db.New(pool)}
}

func (s *Store) DueForSync(ctx context.Context, staleBefore time.Time, limit int32) ([]db.Character, error) {
	return s.queries.ListCharactersDueForSync(ctx, db.ListCharactersDueForSyncParams{
		StaleBefore: pgtype.Timestamptz{Time: staleBefore, Valid: true},
		RowLimit:    limit,
	})
}

func (s *Store) LatestSnapshotGear(ctx context.Context, characterID uuid.UUID) (gear []byte, ilvl, mplusScore float64, found bool, err error) {
	snap, err := s.queries.GetLatestCharacterSnapshot(ctx, characterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, 0, false, nil
	}
	if err != nil {
		return nil, 0, 0, false, err
	}

	// A NULL number is not the same as zero and there is nothing to compare against,
	// so report no usable snapshot and let the caller write a fresh one.
	if !snap.Ilvl.Valid || !snap.MplusScore.Valid {
		return nil, 0, 0, false, nil
	}

	ilvlF, err := numericToFloat64(snap.Ilvl)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("converting stored ilvl: %w", err)
	}
	scoreF, err := numericToFloat64(snap.MplusScore)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("converting stored mplus_score: %w", err)
	}

	return snap.Gear, ilvlF, scoreF, true, nil
}

func (s *Store) ApplySync(ctx context.Context, arg applySyncParams) error {
	var ilvl, score pgtype.Numeric
	if err := ilvl.Scan(numeric(arg.profile.ItemLevel)); err != nil {
		return fmt.Errorf("encoding ilvl: %w", err)
	}
	if err := score.Scan(numeric(arg.profile.MythicPlusScore)); err != nil {
		return fmt.Errorf("encoding mplus_score: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := s.queries.WithTx(tx)

	if err := q.UpdateCharacterFromSync(ctx, db.UpdateCharacterFromSyncParams{
		ID:         arg.characterID,
		Class:      nilIfEmpty(arg.profile.Class),
		Spec:       nilIfEmpty(arg.profile.Spec),
		Ilvl:       ilvl,
		MplusScore: score,
	}); err != nil {
		return fmt.Errorf("updating character: %w", err)
	}

	if _, err := q.InsertCharacterSnapshot(ctx, db.InsertCharacterSnapshotParams{
		CharacterID: arg.characterID,
		Ilvl:        ilvl,
		MplusScore:  score,
		Gear:        arg.gearJSON,
	}); err != nil {
		return fmt.Errorf("inserting snapshot: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing tx: %w", err)
	}

	return nil
}

func (s *Store) TouchSynced(ctx context.Context, characterID uuid.UUID) error {
	return s.queries.TouchCharacterSynced(ctx, characterID)
}

func (s *Store) MarkSyncAttempted(ctx context.Context, characterID uuid.UUID) error {
	return s.queries.MarkCharacterSyncAttempted(ctx, characterID)
}

// nilIfEmpty maps a missing field to SQL NULL, which UpdateCharacterFromSync
// COALESCEs back to the stored value rather than blanking the column.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func numericToFloat64(n pgtype.Numeric) (float64, error) {
	f, err := n.Float64Value()
	if err != nil {
		return 0, err
	}
	return f.Float64, nil
}
