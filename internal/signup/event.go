package signup

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Event is an event, translated out of pgtype into plain Go types.
type Event struct {
	ID             uuid.UUID
	DiscordGuildID int64
	Type           db.EventType
	Title          string
	StartsAt       time.Time
	SignupDeadline time.Time
	CompTemplate   []byte
	MessageID      *int64
	ChannelID      *int64
	Difficulty     *db.RaidDifficulty
}

// CreateEventInput is what a raid lead POSTs to create an event.
type CreateEventInput struct {
	DiscordGuildID int64
	Type           db.EventType
	Title          string
	StartsAt       time.Time
	SignupDeadline time.Time
	CompTemplate   []byte
	Difficulty     *db.RaidDifficulty
}

// UpdateEventInput is a partial edit: a nil field leaves the stored value alone. A
// message_id/channel_id-only edit (the bot learning its own post) is a legal,
// common case, and must not disturb the schedule.
type UpdateEventInput struct {
	ID             uuid.UUID
	Title          *string
	StartsAt       *time.Time
	SignupDeadline *time.Time
	CompTemplate   []byte
	Difficulty     *db.RaidDifficulty
	MessageID      *int64
	ChannelID      *int64
}

// eventStore is the persistence Events needs. Declared here, by the consumer.
type eventStore interface {
	CreateEvent(ctx context.Context, in CreateEventInput) (Event, error)
	GetEvent(ctx context.Context, id uuid.UUID) (Event, error)
	ListUpcomingEvents(ctx context.Context, discordGuildID int64) ([]Event, error)
	UpdateEvent(ctx context.Context, in UpdateEventInput) (Event, error)
	DeleteEvent(ctx context.Context, id uuid.UUID) error
}

// Events creates, edits, and deletes events, keeping scheduled_jobs in step with
// them. The schedule math (jobsFor) and the cancel-and-reschedule transaction live in
// the store rather than here, so the event and the jobs that reference it commit
// together: a second, separate round trip risks an event with no reminders if it
// fails.
type Events struct {
	store eventStore
}

// NewEvents builds an Events.
func NewEvents(store eventStore) *Events {
	return &Events{store: store}
}

func (e *Events) Create(ctx context.Context, in CreateEventInput) (Event, error) {
	event, err := e.store.CreateEvent(ctx, in)
	if err != nil {
		return Event{}, fmt.Errorf("creating event: %w", err)
	}
	return event, nil
}

func (e *Events) Get(ctx context.Context, id uuid.UUID) (Event, error) {
	event, err := e.store.GetEvent(ctx, id)
	if err != nil {
		return Event{}, fmt.Errorf("loading event: %w", err)
	}
	return event, nil
}

// ListUpcoming returns a guild's events that have not started yet, soonest first.
func (e *Events) ListUpcoming(ctx context.Context, discordGuildID int64) ([]Event, error) {
	events, err := e.store.ListUpcomingEvents(ctx, discordGuildID)
	if err != nil {
		return nil, fmt.Errorf("listing events: %w", err)
	}
	return events, nil
}

// Update applies a partial edit. Reschedule-on-edit (design.md section 6) happens
// inside the store transaction whenever StartsAt or SignupDeadline is part of the
// edit: every PENDING job for the event is canceled and the schedule recomputed from
// the new times.
func (e *Events) Update(ctx context.Context, in UpdateEventInput) (Event, error) {
	event, err := e.store.UpdateEvent(ctx, in)
	if err != nil {
		return Event{}, fmt.Errorf("updating event: %w", err)
	}
	return event, nil
}

// Delete removes an event. Its signups, scheduled_jobs, comps, and notifications all
// carry ON DELETE CASCADE, so nothing further is needed here.
func (e *Events) Delete(ctx context.Context, id uuid.UUID) error {
	if err := e.store.DeleteEvent(ctx, id); err != nil {
		return fmt.Errorf("deleting event: %w", err)
	}
	return nil
}
