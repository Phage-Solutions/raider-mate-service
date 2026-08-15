package comp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// ReplaceComp is what a comp write is: the full set of assignments for one event and
// comp name, replacing whatever that name held before. Mode is the mode to create the
// comp with when it does not exist yet; it never changes an existing comp's mode.
type ReplaceComp struct {
	EventID  uuid.UUID
	CompName string
	Mode     db.CompMode
	Result   Result
}

// compStore is the persistence Locker needs. Declared here, by the consumer.
type compStore interface {
	AssignmentPool(ctx context.Context, eventID uuid.UUID) ([]Raider, Template, Mode, error)
	CompMode(ctx context.Context, eventID uuid.UUID, compName string) (db.CompMode, bool, error)
	ReplaceComp(ctx context.Context, arg ReplaceComp) error
}

// Locker computes and persists comp locks.
type Locker struct {
	store compStore
}

// NewLocker builds a Locker.
func NewLocker(store compStore) *Locker {
	return &Locker{store: store}
}

// Lock computes a comp for eventID from its current confirmed and late signups, and
// persists it under compName, replacing any existing slots for that name.
//
// comp_slots can hold several named drafts side by side ("prog" and "farm" on the
// same event), but signups.assigned_role is a single column: locking any comp_name
// overwrites it for the whole event, reflecting whichever comp was locked most
// recently.
// A comp a raid lead owns is never recomputed: Lock reports ErrCompIsManual and writes
// nothing, so the hand-built board survives any number of later locks.
func (l *Locker) Lock(ctx context.Context, eventID uuid.UUID, compName string) (Result, error) {
	compMode, found, err := l.store.CompMode(ctx, eventID, compName)
	if err != nil {
		return Result{}, fmt.Errorf("reading comp mode: %w", err)
	}
	if found && compMode == db.CompModeMANUAL {
		return Result{}, fmt.Errorf("locking comp %q: %w", compName, ErrCompIsManual)
	}

	raiders, tmpl, mode, err := l.store.AssignmentPool(ctx, eventID)
	if err != nil {
		return Result{}, fmt.Errorf("loading assignment pool: %w", err)
	}

	result := Assign(raiders, tmpl, mode)

	if err := l.store.ReplaceComp(ctx, ReplaceComp{
		EventID:  eventID,
		CompName: compName,
		Mode:     db.CompModeAUTO,
		Result:   result,
	}); err != nil {
		return Result{}, fmt.Errorf("persisting comp: %w", err)
	}

	return result, nil
}
