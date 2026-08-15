// Package signup implements event creation, the multi-role signup flow, its deadline
// gate, and the late-request queue a player's write falls into once that deadline has
// passed. Roles live on the character (internal/roster), never here: a signup means
// "I am coming, here is my role menu," and assignment (internal/comp) happens later.
package signup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// ErrSignupsClosed means a player wrote to an event whose signup_deadline has passed.
// A raid lead write never returns it. The caller (the API handler) is expected to
// catch this and file a late_signup_requests row instead of surfacing a bare error the
// bot has to invent a message for.
var ErrSignupsClosed = errors.New("signups closed")

// ErrStatusRequiresRaidLead means a player tried to write ABSENT or NO_SHOW.
// design.md section 3 makes both raid-lead-controlled regardless of who owns the
// character or where the deadline stands.
var ErrStatusRequiresRaidLead = errors.New("status requires raid lead")

// Signup is a character's response to an event, translated out of pgtype into plain
// Go types.
type Signup struct {
	ID           uuid.UUID
	EventID      uuid.UUID
	CharacterID  uuid.UUID
	Status       db.SignupStatus
	AssignedRole *db.RoleEnum
	LateUntil    *time.Time
	Note         *string
	CreatedAt    time.Time
}

// SignupWrite is a signup create-or-update. LateUntil is a plain write-through field:
// Write nils it out unless Status is LATE, so "I'll be 20 minutes late" only sticks
// when it is the actionable status design.md:240 describes.
type SignupWrite struct {
	EventID     uuid.UUID
	CharacterID uuid.UUID
	Status      db.SignupStatus
	Note        *string
	LateUntil   *time.Time
}

// signupStore is the persistence Signups needs. Declared here, by the consumer.
type signupStore interface {
	GetEvent(ctx context.Context, id uuid.UUID) (Event, error)
	UpsertSignup(ctx context.Context, in SignupWrite) (Signup, error)
	DeleteSignup(ctx context.Context, eventID, characterID uuid.UUID) error
	ListSignupsForEvent(ctx context.Context, eventID uuid.UUID) ([]Signup, error)
}

// Signups writes and reads signups, gated by the signup deadline and by who is
// allowed to write which status.
type Signups struct {
	store signupStore
}

// NewSignups builds a Signups.
func NewSignups(store signupStore) *Signups {
	return &Signups{store: store}
}

// Write creates or updates a signup. isRaidLead governs two independent checks: the
// deadline gate (a raid lead can always write) and status authority (ABSENT/NO_SHOW
// are raid-lead-only regardless of the deadline).
func (s *Signups) Write(ctx context.Context, in SignupWrite, isRaidLead bool) (Signup, error) {
	if !isRaidLead && isAssignedStatus(in.Status) {
		return Signup{}, ErrStatusRequiresRaidLead
	}

	event, err := s.store.GetEvent(ctx, in.EventID)
	if err != nil {
		return Signup{}, fmt.Errorf("loading event: %w", err)
	}
	if err := checkDeadline(event.SignupDeadline, isRaidLead, time.Now()); err != nil {
		return Signup{}, err
	}

	if in.Status != db.SignupStatusLATE {
		in.LateUntil = nil
	}

	signup, err := s.store.UpsertSignup(ctx, in)
	if err != nil {
		return Signup{}, fmt.Errorf("writing signup: %w", err)
	}
	return signup, nil
}

// Withdraw deletes a signup. Past the deadline this closes for players the same way a
// new signup would: the gate treats "I can no longer come" as a write like any other,
// so a late withdrawal is a late request carrying DECLINED, not a silent delete.
func (s *Signups) Withdraw(ctx context.Context, eventID, characterID uuid.UUID, isRaidLead bool) error {
	event, err := s.store.GetEvent(ctx, eventID)
	if err != nil {
		return fmt.Errorf("loading event: %w", err)
	}
	if err := checkDeadline(event.SignupDeadline, isRaidLead, time.Now()); err != nil {
		return err
	}

	if err := s.store.DeleteSignup(ctx, eventID, characterID); err != nil {
		return fmt.Errorf("withdrawing signup: %w", err)
	}
	return nil
}

// List returns every signup for an event, in signup order.
func (s *Signups) List(ctx context.Context, eventID uuid.UUID) ([]Signup, error) {
	signups, err := s.store.ListSignupsForEvent(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("listing signups: %w", err)
	}
	return signups, nil
}

// checkDeadline is the deadline gate: a pure function of the event's deadline, who is
// writing, and the current time.
func checkDeadline(deadline time.Time, isRaidLead bool, now time.Time) error {
	if isRaidLead {
		return nil
	}
	if now.After(deadline) {
		return ErrSignupsClosed
	}
	return nil
}

// isAssignedStatus reports whether status is one only a raid lead may write.
func isAssignedStatus(status db.SignupStatus) bool {
	return status == db.SignupStatusABSENT || status == db.SignupStatusNOSHOW
}
