package comp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Store implements compStore over Postgres.
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: db.New(pool)}
}

func (s *Store) AssignmentPool(ctx context.Context, eventID uuid.UUID) ([]Raider, Template, Mode, error) {
	event, err := s.queries.GetEvent(ctx, eventID)
	if err != nil {
		return nil, Template{}, 0, fmt.Errorf("loading event: %w", err)
	}

	var tmpl Template
	if err := json.Unmarshal(event.CompTemplate, &tmpl); err != nil {
		return nil, Template{}, 0, fmt.Errorf("parsing comp_template: %w", err)
	}

	mode := modeFor(event)

	rows, err := s.queries.ListAssignmentPoolForEvent(ctx, eventID)
	if err != nil {
		return nil, Template{}, 0, fmt.Errorf("listing signup pool: %w", err)
	}
	if len(rows) == 0 {
		return nil, tmpl, mode, nil
	}

	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.CharacterID
	}
	roleRows, err := s.queries.ListRolesForCharacters(ctx, ids)
	if err != nil {
		return nil, Template{}, 0, fmt.Errorf("listing character roles: %w", err)
	}
	rolesByCharacter := make(map[uuid.UUID][]RoleChoice, len(rows))
	for _, rr := range roleRows {
		rolesByCharacter[rr.CharacterID] = append(rolesByCharacter[rr.CharacterID], RoleChoice{
			Role: rr.Role, Priority: rr.Priority,
		})
	}

	// Raid gear and Mythic+ score are cached on the same character row under
	// different columns; which one ranks candidates depends on the event type.
	useMplus := event.Type == db.EventTypeMYTHICPLUS

	raiders := make([]Raider, len(rows))
	for i, r := range rows {
		score := r.Ilvl
		if useMplus {
			score = r.MplusScore
		}
		scoreF, err := numericToFloat64(score)
		if err != nil {
			return nil, Template{}, 0, fmt.Errorf("converting score for character %s: %w", r.CharacterID, err)
		}
		raiders[i] = Raider{
			CharacterID: r.CharacterID,
			Name:        r.Name,
			Roles:       rolesByCharacter[r.CharacterID],
			IsMain:      r.IsMain,
			Score:       scoreF,
			SignedUpAt:  r.SignedUpAt.Time,
		}
	}

	return raiders, tmpl, mode, nil
}

// modeFor derives the comp sizing rule from the event. Mythic+ events carry no
// difficulty; among raids, only Mythic is fixed-size, so anything else (including a
// NULL difficulty, which should not happen for a raid but is not this package's
// place to reject) is treated as flex.
func modeFor(event db.Event) Mode {
	if event.Type == db.EventTypeMYTHICPLUS {
		return ModeMythicPlus
	}
	if event.Difficulty != nil && *event.Difficulty == db.RaidDifficultyMYTHIC {
		return ModeRaidMythic
	}
	return ModeRaidFlex
}

// CompMode reports the mode of one comp, and whether it exists at all. A comp that
// has never been written has no mode: the caller decides what an absent comp means,
// which for both Lock and Manual.Save is "create it as mine".
func (s *Store) CompMode(ctx context.Context, eventID uuid.UUID, compName string) (db.CompMode, bool, error) {
	comp, err := s.queries.GetComp(ctx, db.GetCompParams{EventID: eventID, Name: compName})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return comp.Mode, true, nil
}

func (s *Store) ListComps(ctx context.Context, eventID uuid.UUID) ([]CompInfo, error) {
	rows, err := s.queries.ListComps(ctx, eventID)
	if err != nil {
		return nil, err
	}
	out := make([]CompInfo, len(rows))
	for i, r := range rows {
		out[i] = CompInfo{Name: r.Name, Mode: r.Mode}
	}
	return out, nil
}

func (s *Store) ListCompSlots(ctx context.Context, eventID uuid.UUID, compName string) ([]Assignment, error) {
	rows, err := s.queries.ListCompSlots(ctx, db.ListCompSlotsParams{EventID: eventID, CompName: compName})
	if err != nil {
		return nil, err
	}
	out := make([]Assignment, len(rows))
	for i, r := range rows {
		out[i] = Assignment{
			CharacterID: r.CharacterID, Role: r.Role, SlotIndex: r.SlotIndex, IsBench: r.IsBench, Reason: r.Reason,
		}
	}
	return out, nil
}

func (s *Store) SetCompMode(ctx context.Context, eventID uuid.UUID, compName string, mode db.CompMode) error {
	return s.queries.SetCompMode(ctx, db.SetCompModeParams{
		EventID: eventID, Name: compName, Mode: mode,
	})
}

func (s *Store) ReplaceComp(ctx context.Context, arg ReplaceComp) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := s.queries.WithTx(tx)

	// comp_slots carries an FK to comps, so the comp has to exist before its slots.
	// UpsertComp leaves an existing comp's mode alone.
	if _, err := q.UpsertComp(ctx, db.UpsertCompParams{
		ID:      db.NewID(),
		EventID: arg.EventID, Name: arg.CompName, Mode: arg.Mode,
	}); err != nil {
		return fmt.Errorf("upserting comp: %w", err)
	}

	if err := q.DeleteCompSlots(ctx, db.DeleteCompSlotsParams{
		EventID: arg.EventID, CompName: arg.CompName,
	}); err != nil {
		return fmt.Errorf("clearing existing comp slots: %w", err)
	}

	for _, a := range arg.Result.Assignments {
		if err := q.InsertCompSlot(ctx, db.InsertCompSlotParams{
			ID:          db.NewID(),
			EventID:     arg.EventID,
			CompName:    arg.CompName,
			CharacterID: a.CharacterID,
			Role:        a.Role,
			SlotIndex:   a.SlotIndex,
			IsBench:     a.IsBench,
			Reason:      a.Reason,
		}); err != nil {
			return fmt.Errorf("inserting comp slot for %s: %w", a.CharacterID, err)
		}
	}

	if err := q.ClearAssignedRoles(ctx, arg.EventID); err != nil {
		return fmt.Errorf("clearing assigned roles: %w", err)
	}
	for _, a := range arg.Result.Assignments {
		if a.IsBench {
			continue
		}
		role := a.Role
		if err := q.SetSignupAssignedRole(ctx, db.SetSignupAssignedRoleParams{
			EventID:      arg.EventID,
			CharacterID:  a.CharacterID,
			AssignedRole: &role,
		}); err != nil {
			return fmt.Errorf("setting assigned role for %s: %w", a.CharacterID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing tx: %w", err)
	}

	return nil
}

func numericToFloat64(n pgtype.Numeric) (float64, error) {
	if !n.Valid {
		return 0, nil
	}
	f, err := n.Float64Value()
	if err != nil {
		return 0, err
	}
	return f.Float64, nil
}
