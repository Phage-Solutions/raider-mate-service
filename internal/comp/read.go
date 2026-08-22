package comp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/db"
)

// CompInfo is one named comp's mode, without its slots.
type CompInfo struct {
	Name string
	Mode db.CompMode
}

// Board is one named comp's mode and full slot list.
type Board struct {
	Name  string
	Mode  db.CompMode
	Slots []Assignment
}

// readerStore is the persistence Reader needs. Declared here, by the consumer.
type readerStore interface {
	ListComps(ctx context.Context, eventID uuid.UUID) ([]CompInfo, error)
	CompMode(ctx context.Context, eventID uuid.UUID, compName string) (db.CompMode, bool, error)
	ListCompSlots(ctx context.Context, eventID uuid.UUID, compName string) ([]Assignment, error)
}

// Reader is read-only access to comps and their slots.
type Reader struct {
	store readerStore
}

// NewReader builds a Reader.
func NewReader(store readerStore) *Reader {
	return &Reader{store: store}
}

// List returns every named comp for an event.
func (r *Reader) List(ctx context.Context, eventID uuid.UUID) ([]CompInfo, error) {
	infos, err := r.store.ListComps(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("listing comps: %w", err)
	}
	return infos, nil
}

// Get returns one named comp's mode and slots. found is false when no comp by that
// name has ever been created for the event.
func (r *Reader) Get(ctx context.Context, eventID uuid.UUID, name string) (board Board, found bool, err error) {
	mode, found, err := r.store.CompMode(ctx, eventID, name)
	if err != nil {
		return Board{}, false, fmt.Errorf("reading comp mode: %w", err)
	}
	if !found {
		return Board{}, false, nil
	}

	slots, err := r.store.ListCompSlots(ctx, eventID, name)
	if err != nil {
		return Board{}, false, fmt.Errorf("listing comp slots: %w", err)
	}

	return Board{Name: name, Mode: mode, Slots: slots}, true, nil
}
