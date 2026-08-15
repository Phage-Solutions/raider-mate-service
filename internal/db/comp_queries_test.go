//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func seedEventForComp(ctx context.Context, t *testing.T, q *Queries, discordID int64) (Event, Character) {
	t.Helper()

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: discordID, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "Danthrax", Realm: "Area-52", Region: "us", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}
	event, err := q.CreateEvent(ctx, CreateEventParams{
		DiscordGuildID: 100,
		Type:           EventTypeRAID,
		Title:          "Prog Night",
		StartsAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		SignupDeadline: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CompTemplate:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("creating event: %v", err)
	}
	if _, err := q.UpsertSignup(ctx, UpsertSignupParams{
		EventID: event.ID, CharacterID: character.ID, Status: SignupStatusCONFIRMED,
	}); err != nil {
		t.Fatalf("signing up character: %v", err)
	}

	// comp_slots carries an FK to comps, so the comps these tests write slots into
	// have to exist first.
	for _, name := range []string{"prog", "farm"} {
		if _, err := q.UpsertComp(ctx, UpsertCompParams{
			EventID: event.ID, Name: name, Mode: CompModeAUTO,
		}); err != nil {
			t.Fatalf("creating comp %q: %v", name, err)
		}
	}

	return event, character
}

func TestCreateEventPersistsDifficulty(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	mythic := RaidDifficultyMYTHIC
	event, err := q.CreateEvent(ctx, CreateEventParams{
		DiscordGuildID: 100,
		Type:           EventTypeRAID,
		Title:          "Mythic Prog",
		StartsAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		SignupDeadline: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CompTemplate:   []byte(`{}`),
		Difficulty:     &mythic,
	})
	if err != nil {
		t.Fatalf("creating mythic event: %v", err)
	}

	// The assigner reads difficulty back through GetEvent to pick its size rule, so a
	// value that does not survive the round trip silently turns Mythic into flex.
	got, err := q.GetEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("reading event back: %v", err)
	}
	if got.Difficulty == nil || *got.Difficulty != RaidDifficultyMYTHIC {
		t.Errorf("difficulty = %v, want MYTHIC", got.Difficulty)
	}
}

func TestListAssignmentPoolAndRolesFeedTheAssigner(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, character := seedEventForComp(ctx, t, q, 21)

	if err := q.SetCharacterRole(ctx, SetCharacterRoleParams{
		CharacterID: character.ID, Role: RoleEnumTANK, Priority: 1,
	}); err != nil {
		t.Fatalf("setting tank role: %v", err)
	}
	if err := q.SetCharacterRole(ctx, SetCharacterRoleParams{
		CharacterID: character.ID, Role: RoleEnumMDPS, Priority: 2,
	}); err != nil {
		t.Fatalf("setting mdps role: %v", err)
	}

	pool, err := q.ListAssignmentPoolForEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("listing assignment pool: %v", err)
	}
	if len(pool) != 1 || pool[0].CharacterID != character.ID {
		t.Fatalf("pool = %+v, want the one confirmed signup", pool)
	}

	roles, err := q.ListRolesForCharacters(ctx, []uuid.UUID{character.ID})
	if err != nil {
		t.Fatalf("listing roles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("roles = %+v, want both choices", roles)
	}
	if roles[0].Role != RoleEnumTANK || roles[0].Priority != 1 {
		t.Errorf("first role = %+v, want TANK at priority 1", roles[0])
	}
}

func TestUpsertCompKeepsTheModeOfAnExistingComp(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, _ := seedEventForComp(ctx, t, q, 22)

	if _, err := q.UpsertComp(ctx, UpsertCompParams{
		EventID: event.ID, Name: "hand", Mode: CompModeMANUAL,
	}); err != nil {
		t.Fatalf("creating manual comp: %v", err)
	}

	// The assigner's write path upserts with AUTO. It must not convert a raid lead's
	// comp back to assigner-owned as a side effect.
	if _, err := q.UpsertComp(ctx, UpsertCompParams{
		EventID: event.ID, Name: "hand", Mode: CompModeAUTO,
	}); err != nil {
		t.Fatalf("re-upserting comp: %v", err)
	}

	got, err := q.GetComp(ctx, GetCompParams{EventID: event.ID, Name: "hand"})
	if err != nil {
		t.Fatalf("reading comp back: %v", err)
	}
	if got.Mode != CompModeMANUAL {
		t.Errorf("mode = %s, want MANUAL preserved", got.Mode)
	}
}

func TestCompSlotRequiresItsComp(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, character := seedEventForComp(ctx, t, q, 23)

	err := q.InsertCompSlot(ctx, InsertCompSlotParams{
		EventID: event.ID, CompName: "ghost", CharacterID: character.ID,
		Role: RoleEnumTANK, SlotIndex: 0, IsBench: false, Reason: "MANUAL: placed by a raid lead",
	})
	if err == nil {
		t.Fatalf("inserting a slot for a comp that does not exist succeeded, want an FK violation")
	}
}

func TestDeleteCompCascadesToItsSlots(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, character := seedEventForComp(ctx, t, q, 24)

	if _, err := q.UpsertComp(ctx, UpsertCompParams{
		EventID: event.ID, Name: "hand", Mode: CompModeMANUAL,
	}); err != nil {
		t.Fatalf("creating comp: %v", err)
	}
	if err := q.InsertCompSlot(ctx, InsertCompSlotParams{
		EventID: event.ID, CompName: "hand", CharacterID: character.ID,
		Role: RoleEnumTANK, SlotIndex: 0, IsBench: false, Reason: "MANUAL: placed by a raid lead",
	}); err != nil {
		t.Fatalf("inserting comp slot: %v", err)
	}

	if err := q.DeleteComp(ctx, DeleteCompParams{EventID: event.ID, Name: "hand"}); err != nil {
		t.Fatalf("deleting comp: %v", err)
	}

	slots, err := q.ListCompSlots(ctx, ListCompSlotsParams{EventID: event.ID, CompName: "hand"})
	if err != nil {
		t.Fatalf("listing slots: %v", err)
	}
	if len(slots) != 0 {
		t.Errorf("slots = %+v, want none after the comp was deleted", slots)
	}
}

func TestCompSlotRoundTripsWithReason(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, character := seedEventForComp(ctx, t, q, 20)

	if err := q.InsertCompSlot(ctx, InsertCompSlotParams{
		EventID: event.ID, CompName: "prog", CharacterID: character.ID,
		Role: RoleEnumTANK, SlotIndex: 0, IsBench: false,
		Reason: "TANK: priority 1, main, first signup",
	}); err != nil {
		t.Fatalf("inserting comp slot: %v", err)
	}

	slots, err := q.ListCompSlots(ctx, ListCompSlotsParams{EventID: event.ID, CompName: "prog"})
	if err != nil {
		t.Fatalf("listing comp slots: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("got %d slots, want 1", len(slots))
	}
	if slots[0].Reason != "TANK: priority 1, main, first signup" {
		t.Errorf("reason = %q, want it to round-trip", slots[0].Reason)
	}
	if slots[0].Role != RoleEnumTANK || slots[0].IsBench {
		t.Errorf("slot = %+v, want TANK, not benched", slots[0])
	}
}

func TestRelockingReplacesRatherThanColliding(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, character := seedEventForComp(ctx, t, q, 21)

	insert := func(role RoleEnum, reason string) error {
		return q.InsertCompSlot(ctx, InsertCompSlotParams{
			EventID: event.ID, CompName: "prog", CharacterID: character.ID,
			Role: role, SlotIndex: 0, IsBench: false, Reason: reason,
		})
	}

	if err := insert(RoleEnumTANK, "first lock"); err != nil {
		t.Fatalf("first lock insert: %v", err)
	}

	if err := q.DeleteCompSlots(ctx, DeleteCompSlotsParams{EventID: event.ID, CompName: "prog"}); err != nil {
		t.Fatalf("clearing before relock: %v", err)
	}
	if err := insert(RoleEnumHEALER, "second lock"); err != nil {
		t.Fatalf("second lock insert: %v", err)
	}

	slots, err := q.ListCompSlots(ctx, ListCompSlotsParams{EventID: event.ID, CompName: "prog"})
	if err != nil {
		t.Fatalf("listing comp slots: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("got %d slots after relock, want 1 (replaced, not accumulated)", len(slots))
	}
	if slots[0].Role != RoleEnumHEALER || slots[0].Reason != "second lock" {
		t.Errorf("slot = %+v, want the second lock's HEALER assignment", slots[0])
	}
}

func TestTwoCompNamesCoexistOnOneEvent(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, character := seedEventForComp(ctx, t, q, 22)

	if err := q.InsertCompSlot(ctx, InsertCompSlotParams{
		EventID: event.ID, CompName: "prog", CharacterID: character.ID,
		Role: RoleEnumTANK, SlotIndex: 0, IsBench: false, Reason: "prog comp",
	}); err != nil {
		t.Fatalf("inserting prog slot: %v", err)
	}
	if err := q.InsertCompSlot(ctx, InsertCompSlotParams{
		EventID: event.ID, CompName: "farm", CharacterID: character.ID,
		Role: RoleEnumHEALER, SlotIndex: 0, IsBench: false, Reason: "farm comp",
	}); err != nil {
		t.Fatalf("inserting farm slot: %v", err)
	}

	prog, err := q.ListCompSlots(ctx, ListCompSlotsParams{EventID: event.ID, CompName: "prog"})
	if err != nil {
		t.Fatalf("listing prog slots: %v", err)
	}
	farm, err := q.ListCompSlots(ctx, ListCompSlotsParams{EventID: event.ID, CompName: "farm"})
	if err != nil {
		t.Fatalf("listing farm slots: %v", err)
	}

	if len(prog) != 1 || prog[0].Role != RoleEnumTANK {
		t.Errorf("prog slots = %+v, want one TANK slot", prog)
	}
	if len(farm) != 1 || farm[0].Role != RoleEnumHEALER {
		t.Errorf("farm slots = %+v, want one HEALER slot", farm)
	}
}

func TestLockingSetsAssignedRoleAndLeavesStatusAlone(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, character := seedEventForComp(ctx, t, q, 23)

	if err := q.InsertCompSlot(ctx, InsertCompSlotParams{
		EventID: event.ID, CompName: "prog", CharacterID: character.ID,
		Role: RoleEnumTANK, SlotIndex: 0, IsBench: false, Reason: "TANK: priority 1, main, first signup",
	}); err != nil {
		t.Fatalf("inserting comp slot: %v", err)
	}

	if err := q.ClearAssignedRoles(ctx, event.ID); err != nil {
		t.Fatalf("clearing assigned roles: %v", err)
	}
	tank := RoleEnumTANK
	if err := q.SetSignupAssignedRole(ctx, SetSignupAssignedRoleParams{
		EventID: event.ID, CharacterID: character.ID, AssignedRole: &tank,
	}); err != nil {
		t.Fatalf("setting assigned role: %v", err)
	}

	signups, err := q.ListSignupsForEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("listing signups: %v", err)
	}
	if len(signups) != 1 {
		t.Fatalf("got %d signups, want 1", len(signups))
	}
	if signups[0].AssignedRole == nil || *signups[0].AssignedRole != RoleEnumTANK {
		t.Errorf("assigned_role = %v, want TANK", signups[0].AssignedRole)
	}
	if signups[0].Status != SignupStatusCONFIRMED {
		t.Errorf("status = %s, want CONFIRMED untouched by the lock", signups[0].Status)
	}
}

func TestClearAssignedRolesNullsOutBenchedSignups(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, character := seedEventForComp(ctx, t, q, 24)

	tank := RoleEnumTANK
	if err := q.SetSignupAssignedRole(ctx, SetSignupAssignedRoleParams{
		EventID: event.ID, CharacterID: character.ID, AssignedRole: &tank,
	}); err != nil {
		t.Fatalf("setting assigned role from a prior lock: %v", err)
	}

	// A relock where this character ends up benched: ClearAssignedRoles runs, and
	// nothing sets it again for this character.
	if err := q.ClearAssignedRoles(ctx, event.ID); err != nil {
		t.Fatalf("clearing assigned roles: %v", err)
	}

	signups, err := q.ListSignupsForEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("listing signups: %v", err)
	}
	if len(signups) != 1 {
		t.Fatalf("got %d signups, want 1", len(signups))
	}
	if signups[0].AssignedRole != nil {
		t.Errorf("assigned_role = %v, want NULL after a relock that benched this character", signups[0].AssignedRole)
	}
	if signups[0].Status != SignupStatusCONFIRMED {
		t.Errorf("status = %s, want CONFIRMED untouched", signups[0].Status)
	}
}
