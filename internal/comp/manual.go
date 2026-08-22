package comp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/db"
)

var (
	// ErrCompIsManual means the assigner was pointed at a comp a raid lead owns. The
	// raid lead's placements are the comp; recomputing them would throw their work away.
	ErrCompIsManual = errors.New("comp is manual")
	// ErrCompIsAuto means a hand-built board was saved over a comp the assigner owns.
	// Converting is deliberate (SetMode), never a side effect of saving.
	ErrCompIsAuto = errors.New("comp is auto")
	// ErrInvalidBoard means the board could not be written as given. This covers only
	// what the schema itself would reject, never a judgement about the composition.
	ErrInvalidBoard = errors.New("invalid board")
	// ErrCompNameTaken means the event already has a comp under the requested name.
	// A comp is keyed (event_id, name), so the two would be the same comp.
	ErrCompNameTaken = errors.New("comp name taken")
	// ErrCompNotFound means there is no comp on this event under that name.
	ErrCompNotFound = errors.New("comp not found")
	// ErrInvalidName means the requested name is not one a comp can be keyed by.
	ErrInvalidName = errors.New("invalid comp name")
)

// ManualReason is the reason recorded against every hand-placed slot. comp_slots.reason
// is NOT NULL, and "a raid lead put them there" is the honest answer.
const ManualReason = "MANUAL: placed by a raid lead"

// Placement is one seat a raid lead decided. There is no priority, score, or reason:
// the raid lead's judgement is the reason, and design.md section 5 is explicit that an
// assigner which overrides raid lead judgement gets switched off. Nothing here checks
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
	RenameComp(ctx context.Context, eventID uuid.UUID, from, to string) error
}

// Manual is the raid-lead-driven half of comp building: whole-board saves for comps the
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
// This is a whole-board write, not a per-slot edit: the raid lead's screen holds the
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

// Rename moves a comp to a new name, slots and all. The mode is irrelevant: a name is
// a label a raid lead chose, not a claim on who owns the board, so an auto comp
// renames exactly like a manual one.
//
// Trimmed but not otherwise policed. The name is part of the comp's key and appears in
// Discord, so an empty one is refused; what a raid lead calls their second Mythic group
// is their business.
// It answers with the comp's mode, which the rename leaves alone. The caller needs it
// to describe the comp it just moved, and reading it is already part of checking the
// comp is there at all.
func (m *Manual) Rename(ctx context.Context, eventID uuid.UUID, from, to string) (db.CompMode, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return "", fmt.Errorf("renaming comp %q: %w", from, ErrInvalidName)
	}

	mode, found, err := m.store.CompMode(ctx, eventID, from)
	if err != nil {
		return "", fmt.Errorf("reading comp mode: %w", err)
	}
	if !found {
		return "", fmt.Errorf("renaming comp %q: %w", from, ErrCompNotFound)
	}

	// Opening the field and closing it again is not a write.
	if to == from {
		return mode, nil
	}

	if err := m.store.RenameComp(ctx, eventID, from, to); err != nil {
		return "", err
	}
	return mode, nil
}

// SetMode converts a comp between raid-lead-owned and assigner-owned. Its slots are
// left alone: converting an auto comp to manual hands the raid lead the assigner's last
// output as a starting point, which is the usual reason to convert at all.
func (m *Manual) SetMode(ctx context.Context, eventID uuid.UUID, compName string, mode db.CompMode) error {
	if err := m.store.SetCompMode(ctx, eventID, compName, mode); err != nil {
		return fmt.Errorf("setting comp mode: %w", err)
	}
	return nil
}

// manualAssignments turns a raid lead's board into rows. slot_index is a position
// within its is_bench partition (see migration 00004), so each partition numbers from
// zero in submitted order.
func manualAssignments(placements []Placement) ([]Assignment, error) {
	seen := make(map[uuid.UUID]struct{}, len(placements))
	var seated, benched int16

	out := make([]Assignment, 0, len(placements))
	for _, p := range placements {
		if _, dup := seen[p.CharacterID]; dup {
			// The schema would reject this anyway; saying so beats a raw SQLSTATE.
			return nil, fmt.Errorf("%w: character %s appears twice", ErrInvalidBoard, p.CharacterID)
		}
		seen[p.CharacterID] = struct{}{}

		if p.Role == "" {
			return nil, fmt.Errorf("%w: character %s has no role", ErrInvalidBoard, p.CharacterID)
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
			Reason:      ManualReason,
		})
	}

	return out, nil
}
