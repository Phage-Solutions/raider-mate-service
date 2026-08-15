package comp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// fakeCompStore stands in for Postgres for both Locker and Manual.
type fakeCompStore struct {
	pool     []Raider
	tmpl     Template
	mode     Mode
	modes    map[string]db.CompMode
	written  []ReplaceComp
	modeSets []db.CompMode
}

func newFakeCompStore() *fakeCompStore {
	return &fakeCompStore{modes: map[string]db.CompMode{}}
}

func (s *fakeCompStore) AssignmentPool(context.Context, uuid.UUID) ([]Raider, Template, Mode, error) {
	return s.pool, s.tmpl, s.mode, nil
}

func (s *fakeCompStore) CompMode(_ context.Context, _ uuid.UUID, compName string) (db.CompMode, bool, error) {
	m, ok := s.modes[compName]
	return m, ok, nil
}

func (s *fakeCompStore) SetCompMode(_ context.Context, _ uuid.UUID, compName string, mode db.CompMode) error {
	s.modes[compName] = mode
	s.modeSets = append(s.modeSets, mode)
	return nil
}

func (s *fakeCompStore) ReplaceComp(_ context.Context, arg ReplaceComp) error {
	s.written = append(s.written, arg)
	if _, ok := s.modes[arg.CompName]; !ok {
		s.modes[arg.CompName] = arg.Mode
	}
	return nil
}

func TestLockRefusesAManualComp(t *testing.T) {
	store := newFakeCompStore()
	store.modes["prog"] = db.CompModeMANUAL

	_, err := NewLocker(store).Lock(context.Background(), uuid.New(), "prog")

	if !errors.Is(err, ErrCompIsManual) {
		t.Fatalf("err = %v, want ErrCompIsManual", err)
	}
	if len(store.written) != 0 {
		t.Errorf("wrote %d comps, want none: the raid lead's board must survive", len(store.written))
	}
}

func TestLockCreatesAnAutoComp(t *testing.T) {
	store := newFakeCompStore()

	if _, err := NewLocker(store).Lock(context.Background(), uuid.New(), "prog"); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	if len(store.written) != 1 {
		t.Fatalf("wrote %d comps, want 1", len(store.written))
	}
	if store.written[0].Mode != db.CompModeAUTO {
		t.Errorf("mode = %s, want AUTO", store.written[0].Mode)
	}
}

func TestManualSaveWritesTheBoardAsManual(t *testing.T) {
	store := newFakeCompStore()
	tank, healer, benched := uuid.New(), uuid.New(), uuid.New()

	err := NewManual(store).Save(context.Background(), uuid.New(), "hand", []Placement{
		{CharacterID: tank, Role: db.RoleEnumTANK},
		{CharacterID: healer, Role: db.RoleEnumHEALER},
		{CharacterID: benched, Role: db.RoleEnumRDPS, IsBench: true},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if len(store.written) != 1 {
		t.Fatalf("wrote %d comps, want 1", len(store.written))
	}
	got := store.written[0]
	if got.Mode != db.CompModeMANUAL {
		t.Errorf("mode = %s, want MANUAL", got.Mode)
	}

	// slot_index is a position within its is_bench partition, so both partitions
	// number from zero.
	want := []Assignment{
		{CharacterID: tank, Role: db.RoleEnumTANK, SlotIndex: 0, IsBench: false},
		{CharacterID: healer, Role: db.RoleEnumHEALER, SlotIndex: 1, IsBench: false},
		{CharacterID: benched, Role: db.RoleEnumRDPS, SlotIndex: 0, IsBench: true},
	}
	if len(got.Result.Assignments) != len(want) {
		t.Fatalf("assignments = %+v, want %d", got.Result.Assignments, len(want))
	}
	for i, w := range want {
		a := got.Result.Assignments[i]
		if a.CharacterID != w.CharacterID || a.Role != w.Role || a.SlotIndex != w.SlotIndex || a.IsBench != w.IsBench {
			t.Errorf("assignment %d = %+v, want %+v", i, a, w)
		}
		if a.Reason == "" {
			t.Errorf("assignment %d has no reason; comp_slots.reason is NOT NULL", i)
		}
	}
}

func TestManualSaveRefusesAnAutoComp(t *testing.T) {
	store := newFakeCompStore()
	store.modes["prog"] = db.CompModeAUTO

	err := NewManual(store).Save(context.Background(), uuid.New(), "prog", []Placement{
		{CharacterID: uuid.New(), Role: db.RoleEnumTANK},
	})

	if !errors.Is(err, ErrCompIsAuto) {
		t.Fatalf("err = %v, want ErrCompIsAuto", err)
	}
	if len(store.written) != 0 {
		t.Errorf("wrote %d comps, want none", len(store.written))
	}
}

func TestManualSaveHonoursPlacementsTheAssignerWouldNeverMake(t *testing.T) {
	// A healer-only character placed as a tank, and the pool's best tank benched.
	// Raid lead judgement is not second-guessed (design.md section 5).
	store := newFakeCompStore()
	healer := uuid.New()

	err := NewManual(store).Save(context.Background(), uuid.New(), "hand", []Placement{
		{CharacterID: healer, Role: db.RoleEnumTANK},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := store.written[0].Result.Assignments[0].Role; got != db.RoleEnumTANK {
		t.Errorf("role = %s, want TANK exactly as the raid lead asked", got)
	}
	if len(store.written[0].Result.Advisories) != 0 {
		t.Errorf("advisories = %+v, want none: a manual board is not second-guessed", store.written[0].Result.Advisories)
	}
}

func TestManualSaveRejectsADuplicateCharacter(t *testing.T) {
	store := newFakeCompStore()
	dup := uuid.New()

	err := NewManual(store).Save(context.Background(), uuid.New(), "hand", []Placement{
		{CharacterID: dup, Role: db.RoleEnumTANK},
		{CharacterID: dup, Role: db.RoleEnumMDPS},
	})

	if err == nil {
		t.Fatalf("Save = nil, want an error naming the duplicate")
	}
	if len(store.written) != 0 {
		t.Errorf("wrote %d comps, want none", len(store.written))
	}
}

func TestManualSaveRejectsAnEmptyRole(t *testing.T) {
	store := newFakeCompStore()

	err := NewManual(store).Save(context.Background(), uuid.New(), "hand", []Placement{
		{CharacterID: uuid.New()},
	})

	if err == nil {
		t.Fatalf("Save = nil, want an error: comp_slots.role is NOT NULL")
	}
}

func TestManualSaveThenLockLeavesTheBoardAlone(t *testing.T) {
	store := newFakeCompStore()
	eventID := uuid.New()
	tank := uuid.New()

	if err := NewManual(store).Save(context.Background(), eventID, "hand", []Placement{
		{CharacterID: tank, Role: db.RoleEnumTANK},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Three more people sign up and a raid lead re-locks.
	store.pool = []Raider{{
		CharacterID: uuid.New(),
		Roles:       []RoleChoice{{Role: db.RoleEnumHEALER, Priority: 1}},
	}}
	_, err := NewLocker(store).Lock(context.Background(), eventID, "hand")

	if !errors.Is(err, ErrCompIsManual) {
		t.Fatalf("err = %v, want ErrCompIsManual", err)
	}
	if len(store.written) != 1 {
		t.Errorf("wrote %d comps, want only the manual save", len(store.written))
	}
}

func TestSetModeConvertsWithoutTouchingSlots(t *testing.T) {
	store := newFakeCompStore()
	store.modes["prog"] = db.CompModeAUTO

	if err := NewManual(store).SetMode(context.Background(), uuid.New(), "prog", db.CompModeMANUAL); err != nil {
		t.Fatalf("SetMode: %v", err)
	}

	if store.modes["prog"] != db.CompModeMANUAL {
		t.Errorf("mode = %s, want MANUAL", store.modes["prog"])
	}
	if len(store.written) != 0 {
		t.Errorf("wrote %d comps, want none: converting keeps the assigner's last output", len(store.written))
	}
}
