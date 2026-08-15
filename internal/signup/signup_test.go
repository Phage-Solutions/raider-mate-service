package signup

import (
	"context"
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

	lateWritten []LateRequestWrite
	lateReqs    map[uuid.UUID]LateRequest
	decided     map[uuid.UUID]db.RequestState
	roleIDs     []int64
	notified    []Notification
}

func newFakeSignupStore() *fakeSignupStore {
	return &fakeSignupStore{
		lateReqs: map[uuid.UUID]LateRequest{},
		decided:  map[uuid.UUID]db.RequestState{},
	}
}

func (s *fakeSignupStore) GetEvent(context.Context, uuid.UUID) (Event, error) {
	return s.event, nil
}

func (s *fakeSignupStore) UpsertSignup(_ context.Context, in SignupWrite) (Signup, error) {
	s.written = append(s.written, in)
	return Signup{EventID: in.EventID, CharacterID: in.CharacterID, Status: in.Status, Note: in.Note, LateUntil: in.LateUntil}, nil
}

func (s *fakeSignupStore) DeleteSignup(_ context.Context, _, characterID uuid.UUID) error {
	s.deleted = append(s.deleted, characterID)
	return nil
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
	s.notified = append(s.notified, n)
	return nil
}

func TestWritePassesBeforeTheDeadlineForAPlayer(t *testing.T) {
	now := time.Now()
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: now.Add(time.Hour)}

	_, err := NewSignups(store).Write(context.Background(), SignupWrite{Status: db.SignupStatusCONFIRMED}, false)
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

	_, err := NewSignups(store).Write(context.Background(), SignupWrite{Status: db.SignupStatusCONFIRMED}, false)
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

	_, err := NewSignups(store).Write(context.Background(), SignupWrite{Status: db.SignupStatusCONFIRMED}, true)
	if err != nil {
		t.Fatalf("Write past deadline as raid lead: %v", err)
	}
	if len(store.written) != 1 {
		t.Fatalf("wrote %d signups, want 1", len(store.written))
	}
}

func TestWriteRejectsAbsentFromAPlayer(t *testing.T) {
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour)}

	_, err := NewSignups(store).Write(context.Background(), SignupWrite{Status: db.SignupStatusABSENT}, false)
	if !errors.Is(err, ErrStatusRequiresRaidLead) {
		t.Fatalf("err = %v, want ErrStatusRequiresRaidLead", err)
	}
	if len(store.written) != 0 {
		t.Errorf("wrote %d signups, want none", len(store.written))
	}
}

func TestWriteAllowsAbsentFromARaidLead(t *testing.T) {
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour)}

	_, err := NewSignups(store).Write(context.Background(), SignupWrite{Status: db.SignupStatusABSENT}, true)
	if err != nil {
		t.Fatalf("Write ABSENT as raid lead: %v", err)
	}
	if len(store.written) != 1 {
		t.Fatalf("wrote %d signups, want 1", len(store.written))
	}
}

func TestWriteClearsLateUntilWhenStatusIsNotLate(t *testing.T) {
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(time.Hour)}
	lateUntil := time.Now().Add(20 * time.Minute)

	_, err := NewSignups(store).Write(context.Background(), SignupWrite{
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

	_, err := NewSignups(store).Write(context.Background(), SignupWrite{
		Status: db.SignupStatusLATE, LateUntil: &lateUntil,
	}, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if store.written[0].LateUntil == nil || !store.written[0].LateUntil.Equal(lateUntil) {
		t.Errorf("late_until = %v, want %v", store.written[0].LateUntil, lateUntil)
	}
}

func TestWithdrawRejectsAPlayerPastTheDeadline(t *testing.T) {
	store := newFakeSignupStore()
	store.event = Event{SignupDeadline: time.Now().Add(-time.Hour)}

	err := NewSignups(store).Withdraw(context.Background(), uuid.New(), uuid.New(), false)
	if !errors.Is(err, ErrSignupsClosed) {
		t.Fatalf("err = %v, want ErrSignupsClosed", err)
	}
	if len(store.deleted) != 0 {
		t.Errorf("deleted %d signups, want none", len(store.deleted))
	}
}
