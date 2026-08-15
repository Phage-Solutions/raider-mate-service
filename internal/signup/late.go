package signup

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Notification is one outbox row. Both LateRequests (LATE_REQUEST_FILED, filed the
// moment a player requests one) and the worker's Runner (commit 3, the scheduled
// kinds) produce these; neither talks to Discord directly.
type Notification struct {
	DiscordGuildID int64
	EventID        uuid.UUID
	Kind           db.NotificationKind
	TargetKind     db.NotificationTarget
	DiscordID      *int64
	RoleIDs        []int64
	ChannelID      *int64
	Payload        []byte
}

// LateRequest is a late_signup_requests row, translated out of pgtype.
type LateRequest struct {
	ID          uuid.UUID
	EventID     uuid.UUID
	CharacterID uuid.UUID
	Status      db.SignupStatus
	Note        *string
	State       db.RequestState
	CreatedAt   time.Time
	DecidedAt   *time.Time
}

// LateRequestWrite is what a player files: the status they were trying to write when
// the deadline gate closed on them. A withdrawal past the deadline files DECLINED;
// the table is named for when this happens, not for the status it carries.
type LateRequestWrite struct {
	EventID     uuid.UUID
	CharacterID uuid.UUID
	Status      db.SignupStatus
	Note        *string
}

// lateRequestPayload is what a bot needs to render a LATE_REQUEST_FILED notification.
type lateRequestPayload struct {
	EventTitle  string          `json:"event_title"`
	CharacterID uuid.UUID       `json:"character_id"`
	Status      db.SignupStatus `json:"status"`
}

// lateStore is the persistence LateRequests needs. Declared here, by the consumer.
type lateStore interface {
	GetEvent(ctx context.Context, id uuid.UUID) (Event, error)
	UpsertSignup(ctx context.Context, in SignupWrite) (Signup, error)
	UpsertLateRequest(ctx context.Context, in LateRequestWrite) (LateRequest, error)
	GetLateRequest(ctx context.Context, id uuid.UUID) (LateRequest, error)
	ListLateRequests(ctx context.Context, eventID uuid.UUID) ([]LateRequest, error)
	DecideLateRequest(ctx context.Context, id uuid.UUID, state db.RequestState) error
	RaidLeadRoleIDs(ctx context.Context, discordGuildID int64) ([]int64, error)
	InsertNotification(ctx context.Context, n Notification) error
}

// LateRequests files, lists, and decides late signup requests: what a player's write
// becomes once Signups.Write has returned ErrSignupsClosed.
type LateRequests struct {
	store lateStore
}

// NewLateRequests builds a LateRequests.
func NewLateRequests(store lateStore) *LateRequests {
	return &LateRequests{store: store}
}

// File records a request and notifies the raid lead. Filing writes a
// LATE_REQUEST_FILED notification with no scheduled_jobs row behind it: it fires the
// instant a player asks, not on a poll.
func (l *LateRequests) File(ctx context.Context, in LateRequestWrite) (LateRequest, error) {
	req, err := l.store.UpsertLateRequest(ctx, in)
	if err != nil {
		return LateRequest{}, fmt.Errorf("filing late request: %w", err)
	}

	event, err := l.store.GetEvent(ctx, in.EventID)
	if err != nil {
		return LateRequest{}, fmt.Errorf("loading event: %w", err)
	}

	// A ROLE notification with no channel to post in is worse than none; the bot
	// has not learned this event's channel yet (see events.channel_id).
	if event.ChannelID == nil {
		return req, nil
	}

	roleIDs, err := l.store.RaidLeadRoleIDs(ctx, event.DiscordGuildID)
	if err != nil {
		return LateRequest{}, fmt.Errorf("loading raid lead roles: %w", err)
	}
	payload, err := json.Marshal(lateRequestPayload{
		EventTitle: event.Title, CharacterID: in.CharacterID, Status: in.Status,
	})
	if err != nil {
		return LateRequest{}, fmt.Errorf("encoding notification payload: %w", err)
	}
	if err := l.store.InsertNotification(ctx, Notification{
		DiscordGuildID: event.DiscordGuildID,
		EventID:        event.ID,
		Kind:           db.NotificationKindLATEREQUESTFILED,
		TargetKind:     db.NotificationTargetROLE,
		RoleIDs:        roleIDs,
		ChannelID:      event.ChannelID,
		Payload:        payload,
	}); err != nil {
		return LateRequest{}, fmt.Errorf("writing late-request notification: %w", err)
	}

	return req, nil
}

// List returns every late request for an event, most recent first.
func (l *LateRequests) List(ctx context.Context, eventID uuid.UUID) ([]LateRequest, error) {
	reqs, err := l.store.ListLateRequests(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("listing late requests: %w", err)
	}
	return reqs, nil
}

// Approve upserts the signup with the requested status and marks the request decided.
func (l *LateRequests) Approve(ctx context.Context, id uuid.UUID) error {
	req, err := l.store.GetLateRequest(ctx, id)
	if err != nil {
		return fmt.Errorf("loading late request: %w", err)
	}
	if _, err := l.store.UpsertSignup(ctx, SignupWrite{
		EventID: req.EventID, CharacterID: req.CharacterID, Status: req.Status, Note: req.Note,
	}); err != nil {
		return fmt.Errorf("writing approved signup: %w", err)
	}
	if err := l.store.DecideLateRequest(ctx, id, db.RequestStateAPPROVED); err != nil {
		return fmt.Errorf("marking late request approved: %w", err)
	}
	return nil
}

// Reject marks the request decided without touching the signup.
func (l *LateRequests) Reject(ctx context.Context, id uuid.UUID) error {
	if err := l.store.DecideLateRequest(ctx, id, db.RequestStateREJECTED); err != nil {
		return fmt.Errorf("marking late request rejected: %w", err)
	}
	return nil
}
