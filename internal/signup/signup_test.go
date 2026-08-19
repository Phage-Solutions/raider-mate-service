package signup

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// fakeSignupStore stands in for Postgres for both Signups and LateRequests: the two
// share GetEvent and UpsertSignup, same precedent as fakeCompStore in internal/comp.
type fakeSignupStore struct {
	event Event

	written []SignupWrite
	deleted []uuid.UUID
	listed  []Signup
	// dropFrom is what UpsertSignup and DeleteSignup report having emptied. The pool
	// rule that decides this lives in the real store, so the fake answers what the
	// test set.
	dropFrom []string

	lateWritten []LateRequestWrite
	lateReqs    map[uuid.UUID]LateRequest
	decided     map[uuid.UUID]db.RequestState
	roleIDs     []int64
	notified    []Notification
	notifyErr   error
}

func newFakeSignupStore() *fakeSignupStore {
	return &fakeSignupStore{
		lateReqs: map[uuid.UUID]LateRequest{},
		decided:  map[uuid.UUID]db.RequestState{},
	}
}

// The two Transact methods just invoke fn against the fake, same shape as
// fakeReminderStore.Transact: no real transaction, but the call the caller makes.
func (s *fakeSignupStore) TransactSignups(ctx context.Context, fn func(context.Context, signupStore) error) error {
	return fn(ctx, s)
}

func (s *fakeSignupStore) TransactLate(ctx context.Context, fn func(context.Context, lateStore) error) error {
	return fn(ctx, s)
}

func (s *fakeSignupStore) GetEvent(context.Context, uuid.UUID) (Event, error) {
	return s.event, nil
}

func (s *fakeSignupStore) UpsertSignup(_ context.Context, in SignupWrite) (Signup, []string, error) {
	s.written = append(s.written, in)
	return Signup{EventID: in.EventID, CharacterID: in.CharacterID, Status: in.Status, Note: in.Note, LateUntil: in.LateUntil}, s.dropFrom, nil
}

func (s *fakeSignupStore) DeleteSignup(_ context.Context, _, characterID uuid.UUID) ([]string, error) {
	s.deleted = append(s.deleted, characterID)
	return s.dropFrom, nil
}

func (s *fakeSignupStore) ListSignupsForEvent(context.Context, uuid.UUID) ([]Signup, error) {
	return s.listed, nil
}

func (s *fakeSignupStore) UpsertLateRequest(_ context.Context, in LateRequestWrite) (LateRequest, error) {
	s.lateWritten = append(s.lateWritten, in)
	req := LateRequest{
		ID: uuid.New(), EventID: in.EventID, CharacterID: in.CharacterID,
		Status: in.Status, Note: in.Note, LateUntil: in.LateUntil, State: db.RequestStatePENDING,
	}
	s.lateReqs[req.ID] = req
	return req, nil
}

func (s *fakeSignupStore) GetLateRequest(_ context.Context, id uuid.UUID) (LateRequest, error) {
	return s.lateReqs[id], nil
}

func (s *fakeSignupStore) ListLateRequests(context.Context, uuid.UUID) ([]LateRequest, error) {
	reqs := make([]LateRequest, 0, len(s.lateReqs))
	for _, r := range s.lateReqs {
		reqs = append(reqs, r)
	}
	return reqs, nil
}

// DecideLateRequest updates the stored row too, not just the audit map: the state
// guard in Approve/Reject reads it back, so a fake that only recorded the call would
// make a decided request look pending forever.
func (s *fakeSignupStore) DecideLateRequest(_ context.Context, id uuid.UUID, state db.RequestState) error {
	s.decided[id] = state
	if req, ok := s.lateReqs[id]; ok {
		req.State = state
		s.lateReqs[id] = req
	}
	return nil
}

func (s *fakeSignupStore) RaidLeadRoleIDs(context.Context, int64) ([]int64, error) {
	return s.roleIDs, nil
}

func (s *fakeSignupStore) InsertNotification(_ context.Context, n Notification) error {
	if s.notifyErr != nil {
		return s.notifyErr
	}
	s.notified = append(s.notified, n)
	return nil
}

func TestWritePassesBeforeTheDeadlineForAPlayer(t *testing.T) {
	now := time.Now()
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: now.Add(time.Hour)}

	_, err := NewSignups(store, newTestLogger()).Write(context.Background(), SignupWrite{Status: db.SignupStatusCONFIRMED}, false)
	if err != nil {
		t.Fatalf("Write before deadline: %v", err)
	}
	if len(store.written) != 1 {
		t.Fatalf("wrote %d signups, want 1", len(store.written))
	}
}

func TestWriteRejectsAPlayerPastTheDeadline(t *testing.T) {
	now := time.Now()
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: now.Add(-time.Hour)}

	_, err := NewSignups(store, newTestLogger()).Write(context.Background(), SignupWrite{Status: db.SignupStatusCONFIRMED}, false)
	if !errors.Is(err, ErrSignupsClosed) {
		t.Fatalf("err = %v, want ErrSignupsClosed", err)
	}
	if len(store.written) != 0 {
		t.Errorf("wrote %d signups, want none", len(store.written))
	}
}

func TestWritePassesForARaidLeadPastTheDeadline(t *testing.T) {
	now := time.Now()
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: now.Add(-time.Hour)}

	_, err := NewSignups(store, newTestLogger()).Write(context.Background(), SignupWrite{Status: db.SignupStatusCONFIRMED}, true)
	if err != nil {
		t.Fatalf("Write past deadline as raid lead: %v", err)
	}
	if len(store.written) != 1 {
		t.Fatalf("wrote %d signups, want 1", len(store.written))
	}
}

// Both statuses report what is happening on the night, so the gate for them is the
// pull rather than the signup deadline.
func TestWriteAcceptsLateAndAbsentPastTheDeadline(t *testing.T) {
	now := time.Now()
	for _, status := range []db.SignupStatus{db.SignupStatusLATE, db.SignupStatusABSENT} {
		t.Run(string(status), func(t *testing.T) {
			store := newFakeSignupStore()
			store.event = Event{SignupDeadline: now.Add(-time.Hour), StartsAt: now.Add(time.Hour)}

			_, err := NewSignups(store, newTestLogger()).Write(context.Background(), SignupWrite{Status: status}, false)
			if err != nil {
				t.Fatalf("Write %s past deadline: %v", status, err)
			}
			if len(store.written) != 1 {
				t.Fatalf("wrote %d signups, want 1", len(store.written))
			}
		})
	}
}

func TestWriteRejectsLateOnceTheRaidHasStarted(t *testing.T) {
	now := time.Now()
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: now.Add(-2 * time.Hour), StartsAt: now.Add(-time.Hour)}

	lateUntil := now.Add(time.Hour)
	_, err := NewSignups(store, newTestLogger()).Write(context.Background(),
		SignupWrite{Status: db.SignupStatusLATE, LateUntil: &lateUntil}, false)
	if !errors.Is(err, ErrSignupsClosed) {
		t.Fatalf("err = %v, want ErrSignupsClosed", err)
	}
	if len(store.written) != 0 {
		t.Errorf("wrote %d signups, want none", len(store.written))
	}
}

// TestWriteStatusAuthority walks every status for both callers. ABSENT is a planned
// absence the raider declares, so it belongs to them; NO_SHOW is the raid lead's
// judgement about the night and is the only value they hold alone.
func TestWriteStatusAuthority(t *testing.T) {
	tests := []struct {
		status     db.SignupStatus
		isRaidLead bool
		wantErr    error
	}{
		{db.SignupStatusCONFIRMED, false, nil},
		{db.SignupStatusTENTATIVE, false, nil},
		{db.SignupStatusDECLINED, false, nil},
		{db.SignupStatusLATE, false, nil},
		{db.SignupStatusABSENT, false, nil},
		{db.SignupStatusNOSHOW, false, ErrStatusRequiresRaidLead},
		{db.SignupStatusABSENT, true, nil},
		{db.SignupStatusNOSHOW, true, nil},
	}

	for _, tt := range tests {
		name := string(tt.status) + "/player"
		if tt.isRaidLead {
			name = string(tt.status) + "/raid lead"
		}
		t.Run(name, func(t *testing.T) {
			store := newFakeSignupStore()
			store.event = Event{SignupDeadline: time.Now().Add(time.Hour)}

			_, err := NewSignups(store, newTestLogger()).Write(
				context.Background(), SignupWrite{Status: tt.status}, tt.isRaidLead)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}

			want := 1
			if tt.wantErr != nil {
				want = 0
			}
			if len(store.written) != want {
				t.Errorf("wrote %d signups, want %d", len(store.written), want)
			}
		})
	}
}

func TestWriteNotifiesWhenTheWriteEmptiesALockedComp(t *testing.T) {
	channelID := int64(42)
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour), ChannelID: &channelID}
	store.dropFrom = []string{"prog comp"}

	if _, err := NewSignups(store, newTestLogger()).Write(context.Background(), SignupWrite{
		Status: db.SignupStatusABSENT,
	}, false); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if len(store.notified) != 1 {
		t.Fatalf("queued %d notifications, want 1", len(store.notified))
	}
	if got := store.notified[0].Kind; got != db.NotificationKindCOMPSLOTDROPPED {
		t.Errorf("kind = %v, want COMP_SLOT_DROPPED", got)
	}
}

// The write and its notification share a transaction, so a notification that cannot be
// queued has to fail the write rather than leave a raider pulled out of a locked comp
// with nothing telling the raid lead about the hole.
func TestWriteFailsWhenTheDroppedSlotNotificationCannotBeQueued(t *testing.T) {
	channelID := int64(42)
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour), ChannelID: &channelID}
	store.dropFrom = []string{"prog comp"}
	store.notifyErr = errors.New("outbox unavailable")

	_, err := NewSignups(store, newTestLogger()).Write(context.Background(), SignupWrite{
		Status: db.SignupStatusABSENT,
	}, false)
	if !errors.Is(err, store.notifyErr) {
		t.Fatalf("err = %v, want the notification failure surfaced", err)
	}
}

func TestWriteNotifiesNobodyWhenNoCompSlotWasHeld(t *testing.T) {
	channelID := int64(42)
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour), ChannelID: &channelID}

	if _, err := NewSignups(store, newTestLogger()).Write(context.Background(), SignupWrite{
		Status: db.SignupStatusDECLINED,
	}, false); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if len(store.notified) != 0 {
		t.Errorf("queued %d notifications, want none", len(store.notified))
	}
}

func TestWriteClearsLateUntilWhenStatusIsNotLate(t *testing.T) {
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour)}
	lateUntil := time.Now().Add(20 * time.Minute)

	_, err := NewSignups(store, newTestLogger()).Write(context.Background(), SignupWrite{
		Status: db.SignupStatusCONFIRMED, LateUntil: &lateUntil,
	}, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if store.written[0].LateUntil != nil {
		t.Errorf("late_until = %v, want nil: only meaningful alongside LATE", store.written[0].LateUntil)
	}
}

func TestWriteKeepsLateUntilWhenStatusIsLate(t *testing.T) {
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour)}
	lateUntil := time.Now().Add(20 * time.Minute)

	_, err := NewSignups(store, newTestLogger()).Write(context.Background(), SignupWrite{
		Status: db.SignupStatusLATE, LateUntil: &lateUntil,
	}, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if store.written[0].LateUntil == nil || !store.written[0].LateUntil.Equal(lateUntil) {
		t.Errorf("late_until = %v, want %v", store.written[0].LateUntil, lateUntil)
	}
}

// Taking a name off the sheet gives up a seat the same as going absent does, and the
// payload carries no status: "someone withdrew" is not "someone is declined".
func TestWithdrawDropsTheSeatAndNotifiesWithNoStatus(t *testing.T) {
	channelID := int64(42)
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour), ChannelID: &channelID}
	store.dropFrom = []string{"prog comp"}

	if err := NewSignups(store, newTestLogger()).Withdraw(
		context.Background(), uuid.New(), uuid.New(), false); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	if len(store.notified) != 1 {
		t.Fatalf("queued %d notifications, want 1", len(store.notified))
	}
	if got := store.notified[0].Kind; got != db.NotificationKindCOMPSLOTDROPPED {
		t.Fatalf("kind = %v, want COMP_SLOT_DROPPED", got)
	}

	var payload compSlotsDroppedPayload
	if err := json.Unmarshal(store.notified[0].Payload, &payload); err != nil {
		t.Fatalf("decoding payload: %v", err)
	}
	if payload.Status != nil {
		t.Errorf("status = %v, want none on a withdrawal", *payload.Status)
	}
	if len(payload.CompNames) != 1 || payload.CompNames[0] != "prog comp" {
		t.Errorf("comp names = %v, want the comp that lost the seat", payload.CompNames)
	}
}

func TestWithdrawNotifiesNobodyWhenNoSeatWasHeld(t *testing.T) {
	channelID := int64(42)
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour), ChannelID: &channelID}

	if err := NewSignups(store, newTestLogger()).Withdraw(
		context.Background(), uuid.New(), uuid.New(), false); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if len(store.notified) != 0 {
		t.Errorf("queued %d notifications, want none", len(store.notified))
	}
}

func TestWithdrawRejectsAPlayerPastTheDeadline(t *testing.T) {
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(-time.Hour)}

	err := NewSignups(store, newTestLogger()).Withdraw(context.Background(), uuid.New(), uuid.New(), false)
	if !errors.Is(err, ErrSignupsClosed) {
		t.Fatalf("err = %v, want ErrSignupsClosed", err)
	}
	if len(store.deleted) != 0 {
		t.Errorf("deleted %d signups, want none", len(store.deleted))
	}
}
