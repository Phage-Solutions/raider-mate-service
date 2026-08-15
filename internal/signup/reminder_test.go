package signup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

type failedMark struct {
	id     uuid.UUID
	status db.JobStatus
}

// fakeReminderStore stands in for Postgres for Runner. Transact just invokes fn
// against itself: no real transaction, but the same call shape a caller sees.
type fakeReminderStore struct {
	event       Event
	jobs        []db.ScheduledJob
	undecided   []int64
	confirmed   []ConfirmedSignup
	compSlots   int64
	signups     []Signup
	roleIDs     []int64
	getEventErr error

	notified []Notification
	sentIDs  []uuid.UUID
	failed   []failedMark
}

func (s *fakeReminderStore) Transact(ctx context.Context, fn func(context.Context, reminderStore) error) error {
	return fn(ctx, s)
}

func (s *fakeReminderStore) ClaimDueJobs(context.Context, int32) ([]db.ScheduledJob, error) {
	return s.jobs, nil
}

func (s *fakeReminderStore) MarkJobSent(_ context.Context, id uuid.UUID) error {
	s.sentIDs = append(s.sentIDs, id)
	return nil
}

func (s *fakeReminderStore) MarkJobFailed(_ context.Context, id uuid.UUID, status db.JobStatus) error {
	s.failed = append(s.failed, failedMark{id: id, status: status})
	return nil
}

func (s *fakeReminderStore) GetEvent(context.Context, uuid.UUID) (Event, error) {
	if s.getEventErr != nil {
		return Event{}, s.getEventErr
	}
	return s.event, nil
}

func (s *fakeReminderStore) ListSignupsForEvent(context.Context, uuid.UUID) ([]Signup, error) {
	return s.signups, nil
}

func (s *fakeReminderStore) ListUndecidedForEvent(context.Context, uuid.UUID) ([]int64, error) {
	return s.undecided, nil
}

func (s *fakeReminderStore) ListConfirmedWithRole(context.Context, uuid.UUID) ([]ConfirmedSignup, error) {
	return s.confirmed, nil
}

func (s *fakeReminderStore) CountCompSlotsForEvent(context.Context, uuid.UUID) (int64, error) {
	return s.compSlots, nil
}

func (s *fakeReminderStore) RaidLeadRoleIDs(context.Context, int64) ([]int64, error) {
	return s.roleIDs, nil
}

func (s *fakeReminderStore) InsertNotification(_ context.Context, n Notification) error {
	s.notified = append(s.notified, n)
	return nil
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunDueReminder24hEmitsOneRowPerUndecidedUser(t *testing.T) {
	jobID := uuid.New()
	store := &fakeReminderStore{
		event:     Event{Title: "Prog Night"},
		jobs:      []db.ScheduledJob{{ID: jobID, JobType: db.JobEnumREMINDER24H}},
		undecided: []int64{111, 222, 333, 444}, // one raider's four alts already collapsed to one entry each
	}

	if err := NewRunner(store, newTestLogger()).RunDue(context.Background(), 10); err != nil {
		t.Fatalf("RunDue: %v", err)
	}

	if len(store.notified) != 4 {
		t.Fatalf("notified = %d, want 4 (one per undecided discord id)", len(store.notified))
	}
	for _, n := range store.notified {
		if n.Kind != db.NotificationKindREMINDER24H || n.TargetKind != db.NotificationTargetUSER {
			t.Errorf("notification = %+v, want a REMINDER_24H USER row", n)
		}
	}
	if len(store.sentIDs) != 1 || store.sentIDs[0] != jobID {
		t.Errorf("sent = %v, want [%s]", store.sentIDs, jobID)
	}
}

func TestRunDueReminder1hUsesTheAssignedRolePerSignup(t *testing.T) {
	tank := db.RoleEnumTANK
	store := &fakeReminderStore{
		event: Event{Title: "Prog Night"},
		jobs:  []db.ScheduledJob{{ID: uuid.New(), JobType: db.JobEnumREMINDER1H}},
		confirmed: []ConfirmedSignup{
			{DiscordID: 1, AssignedRole: &tank},
		},
	}

	if err := NewRunner(store, newTestLogger()).RunDue(context.Background(), 10); err != nil {
		t.Fatalf("RunDue: %v", err)
	}

	if len(store.notified) != 1 {
		t.Fatalf("notified = %d, want 1", len(store.notified))
	}
	if store.notified[0].Kind != db.NotificationKindREMINDER1H {
		t.Errorf("kind = %s, want REMINDER_1H", store.notified[0].Kind)
	}
	if store.notified[0].DiscordID == nil || *store.notified[0].DiscordID != 1 {
		t.Errorf("discord_id = %v, want 1", store.notified[0].DiscordID)
	}
}

func TestRunDueCompNagStaysSilentOnALockedComp(t *testing.T) {
	channelID := int64(555)
	jobID := uuid.New()
	store := &fakeReminderStore{
		event:     Event{Title: "Prog Night", ChannelID: &channelID},
		jobs:      []db.ScheduledJob{{ID: jobID, JobType: db.JobEnumCOMPNAG}},
		compSlots: 1, // locked: at least one slot exists
	}

	if err := NewRunner(store, newTestLogger()).RunDue(context.Background(), 10); err != nil {
		t.Fatalf("RunDue: %v", err)
	}

	if len(store.notified) != 0 {
		t.Errorf("notified = %d, want none: comp is locked", len(store.notified))
	}
	if len(store.sentIDs) != 1 || store.sentIDs[0] != jobID {
		t.Errorf("sent = %v, want [%s]: the job still completes", store.sentIDs, jobID)
	}
}

func TestRunDueCompNagFiresWhenNothingIsLocked(t *testing.T) {
	channelID := int64(555)
	store := &fakeReminderStore{
		event:     Event{Title: "Prog Night", ChannelID: &channelID, DiscordGuildID: 100},
		jobs:      []db.ScheduledJob{{ID: uuid.New(), JobType: db.JobEnumCOMPNAG}},
		compSlots: 0,
		roleIDs:   []int64{781, 799},
	}

	if err := NewRunner(store, newTestLogger()).RunDue(context.Background(), 10); err != nil {
		t.Fatalf("RunDue: %v", err)
	}

	if len(store.notified) != 1 {
		t.Fatalf("notified = %d, want 1", len(store.notified))
	}
	n := store.notified[0]
	if n.Kind != db.NotificationKindCOMPNAG || n.TargetKind != db.NotificationTargetROLE {
		t.Errorf("notification = %+v, want a COMP_NAG ROLE row", n)
	}
	if len(n.RoleIDs) != 2 {
		t.Errorf("role_ids = %v, want the mapped raid lead roles", n.RoleIDs)
	}
}

func TestRunDueRoleJobWithNoChannelIsMarkedSentNotRetried(t *testing.T) {
	jobID := uuid.New()
	store := &fakeReminderStore{
		event: Event{Title: "Prog Night", ChannelID: nil, DiscordGuildID: 100},
		jobs:  []db.ScheduledJob{{ID: jobID, JobType: db.JobEnumSIGNUPDEADLINE}},
	}

	if err := NewRunner(store, newTestLogger()).RunDue(context.Background(), 10); err != nil {
		t.Fatalf("RunDue: %v", err)
	}

	if len(store.notified) != 0 {
		t.Errorf("notified = %d, want none: no channel to post in", len(store.notified))
	}
	if len(store.failed) != 0 {
		t.Errorf("failed = %v, want none: a missing channel is not retried", store.failed)
	}
	if len(store.sentIDs) != 1 || store.sentIDs[0] != jobID {
		t.Errorf("sent = %v, want [%s]", store.sentIDs, jobID)
	}
}

func TestRunDueRetryPolicyFailsOnTheThirdAttempt(t *testing.T) {
	tests := []struct {
		attempts   int16
		wantStatus db.JobStatus
	}{
		{attempts: 0, wantStatus: db.JobStatusPENDING},
		{attempts: 1, wantStatus: db.JobStatusPENDING},
		{attempts: 2, wantStatus: db.JobStatusFAILED},
	}

	for _, tt := range tests {
		jobID := uuid.New()
		store := &fakeReminderStore{
			jobs:        []db.ScheduledJob{{ID: jobID, JobType: db.JobEnumREMINDER24H, Attempts: tt.attempts}},
			getEventErr: errors.New("boom"),
		}

		if err := NewRunner(store, newTestLogger()).RunDue(context.Background(), 10); err != nil {
			t.Fatalf("attempts=%d: RunDue: %v", tt.attempts, err)
		}
		if len(store.sentIDs) != 0 {
			t.Errorf("attempts=%d: sent = %v, want none: resolution failed", tt.attempts, store.sentIDs)
		}
		if len(store.failed) != 1 || store.failed[0].status != tt.wantStatus {
			t.Errorf("attempts=%d: failed = %+v, want status %s", tt.attempts, store.failed, tt.wantStatus)
		}
	}
}
