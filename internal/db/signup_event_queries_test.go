//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func seedUserAndCharacter(ctx context.Context, t *testing.T, q *Queries, discordID int64, name string) (User, Character) {
	t.Helper()

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: discordID, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: name, Realm: "Area-52", Region: "us", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character %s: %v", name, err)
	}
	return user, character
}

func TestUpdateEventPartiallyUpdatesLeavingOtherFieldsAlone(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 50)

	messageID := int64(123456789)
	channelID := int64(987654321)
	updated, err := q.UpdateEvent(ctx, UpdateEventParams{
		ID:        event.ID,
		MessageID: &messageID,
		ChannelID: &channelID,
	})
	if err != nil {
		t.Fatalf("updating event with message/channel id: %v", err)
	}
	if updated.MessageID == nil || *updated.MessageID != messageID {
		t.Errorf("message_id = %v, want %d", updated.MessageID, messageID)
	}
	if updated.ChannelID == nil || *updated.ChannelID != channelID {
		t.Errorf("channel_id = %v, want %d", updated.ChannelID, channelID)
	}
	if updated.Title != event.Title {
		t.Errorf("title = %q, want it unchanged at %q", updated.Title, event.Title)
	}

	newStart := pgtype.Timestamptz{Time: time.Now().Add(3 * time.Hour), Valid: true}
	rescheduled, err := q.UpdateEvent(ctx, UpdateEventParams{ID: event.ID, StartsAt: newStart})
	if err != nil {
		t.Fatalf("updating starts_at: %v", err)
	}
	if !rescheduled.StartsAt.Time.Equal(newStart.Time) {
		t.Errorf("starts_at = %v, want %v", rescheduled.StartsAt.Time, newStart.Time)
	}
	if rescheduled.MessageID == nil || *rescheduled.MessageID != messageID {
		t.Errorf("message_id = %v, want it to survive the starts_at-only update", rescheduled.MessageID)
	}
}

func TestDeleteEventCascadesToSignups(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 51)
	_, character := seedUserAndCharacter(ctx, t, q, 52, "Danthrax")

	if _, err := q.UpsertSignup(ctx, UpsertSignupParams{
		EventID: event.ID, CharacterID: character.ID, Status: SignupStatusCONFIRMED,
	}); err != nil {
		t.Fatalf("signing up: %v", err)
	}

	if err := q.DeleteEvent(ctx, event.ID); err != nil {
		t.Fatalf("deleting event: %v", err)
	}

	if _, err := q.GetEvent(ctx, event.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("getting deleted event: got %v, want pgx.ErrNoRows", err)
	}
	signups, err := q.ListSignupsForEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("listing signups for deleted event: %v", err)
	}
	if len(signups) != 0 {
		t.Errorf("signups after event delete = %d, want 0 (cascade)", len(signups))
	}
}

func TestDeleteSignupRemovesOnlyThatRow(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 53)
	_, charA := seedUserAndCharacter(ctx, t, q, 54, "Alice")
	_, charB := seedUserAndCharacter(ctx, t, q, 55, "Bob")

	for _, c := range []Character{charA, charB} {
		if _, err := q.UpsertSignup(ctx, UpsertSignupParams{
			EventID: event.ID, CharacterID: c.ID, Status: SignupStatusCONFIRMED,
		}); err != nil {
			t.Fatalf("signing up %s: %v", c.Name, err)
		}
	}

	if err := q.DeleteSignup(ctx, DeleteSignupParams{EventID: event.ID, CharacterID: charA.ID}); err != nil {
		t.Fatalf("deleting signup: %v", err)
	}

	remaining, err := q.ListSignupsForEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("listing signups: %v", err)
	}
	if len(remaining) != 1 || remaining[0].CharacterID != charB.ID {
		t.Fatalf("remaining signups = %+v, want only Bob's", remaining)
	}
}

func TestUpsertSignupLateUntilRoundTripsAndClearsOnStatusChange(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 56)
	_, character := seedUserAndCharacter(ctx, t, q, 57, "Latecomer")

	lateUntil := pgtype.Timestamptz{Time: time.Now().Add(20 * time.Minute), Valid: true}
	signup, err := q.UpsertSignup(ctx, UpsertSignupParams{
		EventID: event.ID, CharacterID: character.ID, Status: SignupStatusLATE, LateUntil: lateUntil,
	})
	if err != nil {
		t.Fatalf("upserting LATE signup: %v", err)
	}
	if !signup.LateUntil.Valid || !signup.LateUntil.Time.Equal(lateUntil.Time) {
		t.Fatalf("late_until = %+v, want %v", signup.LateUntil, lateUntil.Time)
	}

	// A status change to CONFIRMED with no late_until clears it, same as the
	// existing rule for assigned_role.
	changed, err := q.UpsertSignup(ctx, UpsertSignupParams{
		EventID: event.ID, CharacterID: character.ID, Status: SignupStatusCONFIRMED,
	})
	if err != nil {
		t.Fatalf("upserting CONFIRMED over LATE: %v", err)
	}
	if changed.LateUntil.Valid {
		t.Errorf("late_until = %+v, want cleared after the status changed away from LATE", changed.LateUntil)
	}
}

func TestListUndecidedForEventGroupsByUserNotCharacter(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 58)
	user, _ := seedUserAndCharacter(ctx, t, q, 59, "Main")
	for i, altName := range []string{"Alt1", "Alt2", "Alt3"} {
		if _, err := q.CreateCharacter(ctx, CreateCharacterParams{
			UserID: user.ID, Name: altName, Realm: "Area-52", Region: "us", IsMain: false,
		}); err != nil {
			t.Fatalf("creating alt %d: %v", i, err)
		}
	}
	_, otherUserChar := seedUserAndCharacter(ctx, t, q, 60, "Decided")
	if _, err := q.UpsertSignup(ctx, UpsertSignupParams{
		EventID: event.ID, CharacterID: otherUserChar.ID, Status: SignupStatusCONFIRMED,
	}); err != nil {
		t.Fatalf("signing up the decided character: %v", err)
	}

	undecided, err := q.ListUndecidedForEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("listing undecided: %v", err)
	}
	if len(undecided) != 1 || undecided[0] != user.DiscordID {
		t.Fatalf("undecided = %v, want exactly [%d] (one row for four alts, none for the decided user)", undecided, user.DiscordID)
	}
}

func TestListConfirmedWithRoleOnlyReturnsAssignedSignups(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 61)
	user, assigned := seedUserAndCharacter(ctx, t, q, 62, "Assigned")
	_, unassigned := seedUserAndCharacter(ctx, t, q, 63, "Unassigned")

	for _, c := range []Character{assigned, unassigned} {
		if _, err := q.UpsertSignup(ctx, UpsertSignupParams{
			EventID: event.ID, CharacterID: c.ID, Status: SignupStatusCONFIRMED,
		}); err != nil {
			t.Fatalf("signing up %s: %v", c.Name, err)
		}
	}
	tank := RoleEnumTANK
	if err := q.SetSignupAssignedRole(ctx, SetSignupAssignedRoleParams{
		EventID: event.ID, CharacterID: assigned.ID, AssignedRole: &tank,
	}); err != nil {
		t.Fatalf("assigning role: %v", err)
	}

	rows, err := q.ListConfirmedWithRole(ctx, event.ID)
	if err != nil {
		t.Fatalf("listing confirmed with role: %v", err)
	}
	if len(rows) != 1 || rows[0].DiscordID != user.DiscordID {
		t.Fatalf("rows = %+v, want exactly the assigned character's user", rows)
	}
	if rows[0].AssignedRole == nil || *rows[0].AssignedRole != RoleEnumTANK {
		t.Errorf("assigned_role = %v, want TANK", rows[0].AssignedRole)
	}
}

func TestCountCompSlotsForEventReflectsLockState(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event, character := seedEventForComp(ctx, t, q, 64)

	before, err := q.CountCompSlotsForEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("counting slots before lock: %v", err)
	}
	if before != 0 {
		t.Fatalf("count before lock = %d, want 0", before)
	}

	if err := q.InsertCompSlot(ctx, InsertCompSlotParams{
		EventID: event.ID, CompName: "prog", CharacterID: character.ID,
		Role: RoleEnumTANK, SlotIndex: 0, IsBench: false, Reason: "locked",
	}); err != nil {
		t.Fatalf("inserting comp slot: %v", err)
	}

	after, err := q.CountCompSlotsForEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("counting slots after lock: %v", err)
	}
	if after != 1 {
		t.Errorf("count after lock = %d, want 1", after)
	}
}

func TestLateSignupRequestReRequestUpsertsRatherThanDuplicating(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 65)
	_, character := seedUserAndCharacter(ctx, t, q, 66, "Latecomer")

	first, err := q.UpsertLateRequest(ctx, UpsertLateRequestParams{
		EventID: event.ID, CharacterID: character.ID, Status: SignupStatusLATE,
	})
	if err != nil {
		t.Fatalf("filing first late request: %v", err)
	}

	if err := q.DecideLateRequest(ctx, DecideLateRequestParams{
		ID: first.ID, State: RequestStateAPPROVED,
	}); err != nil {
		t.Fatalf("approving request: %v", err)
	}

	got, err := q.GetLateRequest(ctx, first.ID)
	if err != nil {
		t.Fatalf("getting late request: %v", err)
	}
	if got.State != RequestStateAPPROVED {
		t.Errorf("state = %s, want APPROVED", got.State)
	}
	if !got.DecidedAt.Valid {
		t.Errorf("decided_at = %+v, want set after approval", got.DecidedAt)
	}

	// Filing again for the same event/character upserts: same row, reset to PENDING.
	second, err := q.UpsertLateRequest(ctx, UpsertLateRequestParams{
		EventID: event.ID, CharacterID: character.ID, Status: SignupStatusDECLINED,
	})
	if err != nil {
		t.Fatalf("filing second late request: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-request created a new row (%s), want the same row (%s)", second.ID, first.ID)
	}
	if second.State != RequestStatePENDING {
		t.Errorf("state = %s, want PENDING reset by the re-request", second.State)
	}
	if second.DecidedAt.Valid {
		t.Errorf("decided_at = %+v, want cleared by the re-request", second.DecidedAt)
	}

	all, err := q.ListLateRequests(ctx, event.ID)
	if err != nil {
		t.Fatalf("listing late requests: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("late requests = %d, want 1 (upserted, not duplicated)", len(all))
	}
	if all[0].Status != SignupStatusDECLINED {
		t.Errorf("status = %s, want DECLINED from the re-request", all[0].Status)
	}
}
