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

func (s *Store) UpsertUser(ctx context.Context, discordID, discordGuildID int64) (uuid.UUID, error) {
	user, err := s.queries.UpsertUser(ctx, db.UpsertUserParams{DiscordID: discordID, DiscordGuildID: discordGuildID})
	if err != nil {
		return uuid.Nil, err
	}
	return user.ID, nil
}

func (s *Store) CreateCharacter(ctx context.Context, userID uuid.UUID, in RegisterInput) (Character, error) {
	row, err := s.queries.CreateCharacter(ctx, db.CreateCharacterParams{
		UserID: userID, Name: in.Name, Realm: in.Realm, Region: in.Region, IsMain: in.IsMain,
	})
	if err != nil {
		return Character{}, err
	}
	return characterFromRow(row)
}

func (s *Store) GetCharacterInGuild(ctx context.Context, characterID uuid.UUID, discordGuildID int64) (Character, error) {
	row, err := s.queries.GetCharacterInGuild(ctx, db.GetCharacterInGuildParams{
		ID: characterID, DiscordGuildID: discordGuildID,
	})
	if err != nil {
		return Character{}, err
	}
	return characterFromRow(row)
}

func (s *Store) GetCharacterOwner(ctx context.Context, characterID uuid.UUID, discordGuildID int64) (int64, error) {
	return s.queries.GetCharacterOwner(ctx, db.GetCharacterOwnerParams{
		ID: characterID, DiscordGuildID: discordGuildID,
	})
}

func (s *Store) DeleteCharacter(ctx context.Context, characterID uuid.UUID, discordGuildID int64) (bool, error) {
	rows, err := s.queries.DeleteCharacter(ctx, db.DeleteCharacterParams{
		ID: characterID, DiscordGuildID: discordGuildID,
	})
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *Store) SetCharacterMain(ctx context.Context, characterID uuid.UUID, discordGuildID int64, isMain bool) (bool, error) {
	rows, err := s.queries.SetCharacterMain(ctx, db.SetCharacterMainParams{
		ID: characterID, DiscordGuildID: discordGuildID, IsMain: isMain,
	})
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *Store) ListCharactersInGuild(ctx context.Context, discordGuildID int64) ([]Character, error) {
	rows, err := s.queries.ListCharactersInGuild(ctx, discordGuildID)
	if err != nil {
		return nil, err
	}
	return charactersFromRows(rows)
}

func (s *Store) ListCharactersByDiscord(ctx context.Context, discordID, discordGuildID int64) ([]Character, error) {
	rows, err := s.queries.ListCharactersByDiscord(ctx, db.ListCharactersByDiscordParams{
		DiscordID: discordID, DiscordGuildID: discordGuildID,
	})
	if err != nil {
		return nil, err
	}
	return charactersFromRows(rows)
}

// ReplaceCharacterRoles clears and rewrites the whole role menu in one transaction,
// matching the ephemeral select's whole-set submit.
func (s *Store) ReplaceCharacterRoles(ctx context.Context, characterID uuid.UUID, roles []RoleChoice) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := s.queries.WithTx(tx)

	if err := q.DeleteCharacterRoles(ctx, characterID); err != nil {
		return fmt.Errorf("clearing existing roles: %w", err)
	}
	for _, r := range roles {
		if err := q.SetCharacterRole(ctx, db.SetCharacterRoleParams{
			CharacterID: characterID, Role: r.Role, Priority: r.Priority,
		}); err != nil {
			return fmt.Errorf("setting role %s: %w", r.Role, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing tx: %w", err)
	}
	return nil
}

func (s *Store) ListCharacterRoles(ctx context.Context, characterID uuid.UUID) ([]RoleChoice, error) {
	rows, err := s.queries.ListCharacterRoles(ctx, characterID)
	if err != nil {
		return nil, err
	}
	out := make([]RoleChoice, len(rows))
	for i, r := range rows {
		out[i] = RoleChoice{Role: r.Role, Priority: r.Priority}
	}
	return out, nil
}

// ListRolesForCharacters reads several role menus at once, keyed by character id.
// Rendering a signup list or a comp board needs every raider's menu, and one query
// per raider would be twenty-five round trips to draw one embed.
func (s *Store) ListRolesForCharacters(ctx context.Context, characterIDs []uuid.UUID) (map[uuid.UUID][]RoleChoice, error) {
	rows, err := s.queries.ListRolesForCharacters(ctx, characterIDs)
	if err != nil {
		return nil, err
	}
	// The query orders by character_id, priority, role, so appending preserves the
	// priority order the select menu renders in.
	out := make(map[uuid.UUID][]RoleChoice, len(characterIDs))
	for _, r := range rows {
		out[r.CharacterID] = append(out[r.CharacterID], RoleChoice{Role: r.Role, Priority: r.Priority})
	}
	return out, nil
}

func characterFromRow(row db.Character) (Character, error) {
	ilvl, err := nullableFloat64(row.Ilvl)
	if err != nil {
		return Character{}, fmt.Errorf("converting ilvl: %w", err)
	}
	mplusScore, err := nullableFloat64(row.MplusScore)
	if err != nil {
		return Character{}, fmt.Errorf("converting mplus_score: %w", err)
	}

	return Character{
		ID:         row.ID,
		UserID:     row.UserID,
		Name:       row.Name,
		Realm:      row.Realm,
		Region:     row.Region,
		Class:      row.Class,
		Spec:       row.Spec,
		Ilvl:       ilvl,
		MplusScore: mplusScore,
		IsMain:     row.IsMain,
		Synced:     row.LastSynced.Valid,
	}, nil
}

func charactersFromRows(rows []db.Character) ([]Character, error) {
	out := make([]Character, len(rows))
	for i, row := range rows {
		c, err := characterFromRow(row)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

// nullableFloat64 converts a possibly-NULL numeric column. NULL is a different fact
// than zero here (no ilvl on file versus a genuine 0), so it stays nil rather than
// collapsing to 0.
func nullableFloat64(n pgtype.Numeric) (*float64, error) {
	if !n.Valid {
		return nil, nil
	}
	f, err := numericToFloat64(n)
	if err != nil {
		return nil, err
	}
	return &f, nil
}
