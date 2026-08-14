package comp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

var (
	// ErrCompIsManual means the assigner was pointed at a comp an officer owns. The
	// officer's placements are the comp; recomputing them would throw their work away.
	ErrCompIsManual = errors.New("comp is manual")
	// ErrCompIsAuto means a hand-built board was saved over a comp the assigner owns.
	// Converting is deliberate (SetMode), never a side effect of saving.
	ErrCompIsAuto = errors.New("comp is auto")
)

// Placement is one seat an officer decided. There is no priority, score, or reason:
// the officer's judgement is the reason, and design.md section 5 is explicit that an
// assigner which overrides officer judgement gets switched off. Nothing here checks
// the character's role menu, whether they signed up, or whether the board is a legal
// raid composition.
type Placement struct {
	CharacterID uuid.UUID
	Role        db.RoleEnum
	IsBench     bool
}

// manualStore is the persistence Manual needs. Declared here, by the consumer.
type manualStore interface {
	CompMode(ctx context.Context, eventID uuid.UUID, compName string) (db.CompMode, bool, error)
	SetCompMode(ctx context.Context, eventID uuid.UUID, compName string, mode db.CompMode) error
	ReplaceComp(ctx context.Context, arg ReplaceComp) error
}

// Manual is the officer-driven half of comp building: whole-board saves for comps the
// assigner does not touch.
type Manual struct {
	store manualStore
}

// NewManual builds a Manual.
func NewManual(store manualStore) *Manual {
	return &Manual{store: store}
}

// Save writes placements as the complete contents of compName, replacing whatever it
// held before. The comp is created as MANUAL if it does not exist yet; an existing
// AUTO comp is refused rather than silently converted.
//
// This is a whole-board write, not a per-slot edit: the officer's screen holds the
// board and submits it entire, so slot_index falls out of the submitted order and
// there is no partial state to reconcile.
func (m *Manual) Save(ctx context.Context, eventID uuid.UUID, compName string, placements []Placement) error {
	mode, found, err := m.store.CompMode(ctx, eventID, compName)
	if err != nil {
		return fmt.Errorf("reading comp mode: %w", err)
	}
	if found && mode != db.CompModeMANUAL {
		return fmt.Errorf("saving manual comp %q: %w", compName, ErrCompIsAuto)
	}

	assignments, err := manualAssignments(placements)
	if err != nil {
		return err
	}

	if err := m.store.ReplaceComp(ctx, ReplaceComp{
		EventID:  eventID,
		CompName: compName,
		Mode:     db.CompModeMANUAL,
		Result:   Result{Assignments: assignments},
	}); err != nil {
		return fmt.Errorf("persisting manual comp: %w", err)
	}

	return nil
}

// SetMode converts a comp between officer-owned and assigner-owned. Its slots are
// left alone: converting an auto comp to manual hands the officer the assigner's last
// output as a starting point, which is the usual reason to convert at all.
func (m *Manual) SetMode(ctx context.Context, eventID uuid.UUID, compName string, mode db.CompMode) error {
	if err := m.store.SetCompMode(ctx, eventID, compName, mode); err != nil {
		return fmt.Errorf("setting comp mode: %w", err)
	}
	return nil
}

// manualAssignments turns an officer's board into rows. slot_index is a position
// within its is_bench partition (see migration 00004), so each partition numbers from
// zero in submitted order.
func manualAssignments(placements []Placement) ([]Assignment, error) {
	seen := make(map[uuid.UUID]struct{}, len(placements))
	var seated, benched int16

	out := make([]Assignment, 0, len(placements))
	for _, p := range placements {
		if _, dup := seen[p.CharacterID]; dup {
			// The schema would reject this anyway; saying so beats a raw SQLSTATE.
			return nil, fmt.Errorf("character %s appears twice in the board", p.CharacterID)
		}
		seen[p.CharacterID] = struct{}{}

		if p.Role == "" {
			return nil, fmt.Errorf("character %s has no role; comp_slots.role is NOT NULL", p.CharacterID)
		}

		index := seated
		if p.IsBench {
			index = benched
			benched++
		} else {
			seated++
		}

		out = append(out, Assignment{
			CharacterID: p.CharacterID,
			Role:        p.Role,
			SlotIndex:   index,
			IsBench:     p.IsBench,
			Reason:      "MANUAL: placed by an officer",
		})
	}

	return out, nil
}
