// Package signup implements event creation, the multi-role signup flow, its deadline
// gate, and the late-request queue a player's write falls into once that deadline has
// passed. Roles live on the character (internal/roster), never here: a signup means
// "I am coming, here is my role menu," and assignment (internal/comp) happens later.
package signup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// selfReported are the statuses a raider may write about their own character. ABSENT
// is one of them: it is a planned absence ("I am out for a while"), which only the
// raider knows, as against DECLINED, which answers this one event. NO_SHOW is the
// exception, because it is a raid lead's judgement about what happened on the night,
// not something anyone reports about themselves.
var selfReported = []db.SignupStatus{
	db.SignupStatusCONFIRMED, db.SignupStatusTENTATIVE, db.SignupStatusDECLINED,
	db.SignupStatusLATE, db.SignupStatusABSENT,
}

// AllowedStatuses returns the statuses this caller may write. Write enforces the same
// list, so the API cannot advertise a status the write path then refuses.
func AllowedStatuses(isRaidLead bool) []db.SignupStatus {
	if isRaidLead {
		return AllStatuses()
	}
	return slices.Clone(selfReported)
}

// AllStatuses is every value of the enum, for input validation.
func AllStatuses() []db.SignupStatus {
	return append(slices.Clone(selfReported), db.SignupStatusNOSHOW)
}

// ErrSignupsClosed means a player wrote to an event whose signup_deadline has passed.
// A raid lead write never returns it. The caller (the API handler) is expected to
// catch this and file a late_signup_requests row instead of surfacing a bare error the
// bot has to invent a message for.
var ErrSignupsClosed = errors.New("signups closed")

// ErrStatusRequiresRaidLead means a player tried to write NO_SHOW. design.md section 3
// makes it raid-lead-controlled regardless of who owns the character or where the
// deadline stands.
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
	UpsertSignup(ctx context.Context, in SignupWrite) (Signup, []string, error)
	DeleteSignup(ctx context.Context, eventID, characterID uuid.UUID) ([]string, error)
	ListSignupsForEvent(ctx context.Context, eventID uuid.UUID) ([]Signup, error)
	RaidLeadRoleIDs(ctx context.Context, discordGuildID int64) ([]int64, error)
	InsertNotification(ctx context.Context, n Notification) error
	TransactSignups(ctx context.Context, fn func(ctx context.Context, tx signupStore) error) error
}

// Signups writes and reads signups, gated by the signup deadline and by who is
// allowed to write which status.
type Signups struct {
	store  signupStore
	logger *slog.Logger
}

// NewSignups builds a Signups. The logger is here for the same case LateRequests has
// one: an event with no channel_id cannot be notified about, and that has to be
// visible to whoever is wondering why the raid leads never heard about a drop.
func NewSignups(store signupStore, logger *slog.Logger) *Signups {
	return &Signups{store: store, logger: logger}
}

// Write creates or updates a signup. isRaidLead governs two independent checks: the
// deadline gate (a raid lead can always write) and status authority (NO_SHOW is
// raid-lead-only regardless of the deadline).
func (s *Signups) Write(ctx context.Context, in SignupWrite, isRaidLead bool) (Signup, error) {
	if !slices.Contains(AllowedStatuses(isRaidLead), in.Status) {
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

	// The write and the notification that reports it share a transaction. Splitting
	// them means a raider can be pulled out of a locked comp while the message saying
	// so is lost to a failed insert, and the raid lead turns up to a hole.
	var signup Signup
	err = s.store.TransactSignups(ctx, func(ctx context.Context, tx signupStore) error {
		written, droppedFrom, err := tx.UpsertSignup(ctx, in)
		if err != nil {
			return fmt.Errorf("writing signup: %w", err)
		}
		signup = written
		return notifyCompSlotsDropped(ctx, tx, s.logger, event, in.CharacterID, &in.Status, droppedFrom)
	})
	if err != nil {
		return Signup{}, err
	}
	return signup, nil
}

// Withdraw deletes a signup. Past the deadline this closes for players the same way a
// new signup would: the gate treats "I can no longer come" as a write like any other,
// so a late withdrawal is a late request carrying DECLINED, not a silent delete.
//
// Taking a name off the sheet gives up a seat the same as a status change does, so it
// drops comp slots and tells the raid lead on the same terms.
func (s *Signups) Withdraw(ctx context.Context, eventID, characterID uuid.UUID, isRaidLead bool) error {
	event, err := s.store.GetEvent(ctx, eventID)
	if err != nil {
		return fmt.Errorf("loading event: %w", err)
	}
	if err := checkDeadline(event.SignupDeadline, isRaidLead, time.Now()); err != nil {
		return err
	}

	return s.store.TransactSignups(ctx, func(ctx context.Context, tx signupStore) error {
		droppedFrom, err := tx.DeleteSignup(ctx, eventID, characterID)
		if err != nil {
			return fmt.Errorf("withdrawing signup: %w", err)
		}
		return notifyCompSlotsDropped(ctx, tx, s.logger, event, characterID, nil, droppedFrom)
	})
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

// compSlotsDroppedPayload is what a bot needs to render a COMP_SLOT_DROPPED
// notification: who left, what they said, and which comps now have a hole.
//
// Status is absent on a withdrawal, which deletes the signup rather than restating it.
// "Someone took their name off" is a different sentence from "someone is absent", and a
// status invented to fill the field would make the bot write the wrong one.
type compSlotsDroppedPayload struct {
	EventTitle  string           `json:"event_title"`
	CharacterID uuid.UUID        `json:"character_id"`
	Status      *db.SignupStatus `json:"status,omitempty"`
	CompNames   []string         `json:"comp_names"`
}

// compNotifier is the slice of the store the dropped-slot notification needs. Both
// Signups.Write and LateRequests.Approve write signups, so both can empty a locked
// comp and both emit this.
type compNotifier interface {
	RaidLeadRoleIDs(ctx context.Context, discordGuildID int64) ([]int64, error)
	InsertNotification(ctx context.Context, n Notification) error
}

// notifyCompSlotsDropped tells the raid lead that a locked comp just lost someone.
// Doing nothing for an empty comps is the common case: most writes happen before
// anything is locked.
func notifyCompSlotsDropped(ctx context.Context, store compNotifier, logger *slog.Logger, event Event, characterID uuid.UUID, status *db.SignupStatus, comps []string) error {
	if len(comps) == 0 {
		return nil
	}

	// Same trade as LateRequests.File: a ROLE notification with no channel to post in
	// would address nobody, and the write itself has already happened. Logged rather
	// than swallowed, so a bot that never PATCHes channel_id does not look like a
	// working system that quietly notifies no one.
	if event.ChannelID == nil {
		logger.WarnContext(ctx, "comp slots dropped with no channel to notify in",
			"event_id", event.ID, "character_id", characterID)
		return nil
	}

	roleIDs, err := store.RaidLeadRoleIDs(ctx, event.DiscordGuildID)
	if err != nil {
		return fmt.Errorf("loading raid lead roles: %w", err)
	}
	payload, err := json.Marshal(compSlotsDroppedPayload{
		EventTitle: event.Title, CharacterID: characterID, Status: status, CompNames: comps,
	})
	if err != nil {
		return fmt.Errorf("encoding notification payload: %w", err)
	}
	if err := store.InsertNotification(ctx, Notification{
		DiscordGuildID: event.DiscordGuildID,
		EventID:        event.ID,
		Kind:           db.NotificationKindCOMPSLOTDROPPED,
		TargetKind:     db.NotificationTargetROLE,
		RoleIDs:        roleIDs,
		ChannelID:      event.ChannelID,
		Payload:        payload,
	}); err != nil {
		return fmt.Errorf("writing comp-slot-dropped notification: %w", err)
	}
	return nil
}
