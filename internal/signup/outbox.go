package signup

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// StoredNotification is a notifications row, translated out of pgtype.
type StoredNotification struct {
	ID             uuid.UUID
	DiscordGuildID int64
	EventID        uuid.UUID
	Kind           db.NotificationKind
	TargetKind     db.NotificationTarget
	DiscordID      *int64
	RoleIDs        []int64
	ChannelID      *int64
	Payload        []byte
	CreatedAt      time.Time
}

// outboxStore is the persistence Outbox needs. Declared here, by the consumer.
type outboxStore interface {
	ListUndeliveredNotifications(ctx context.Context, guildID *int64, limit int32) ([]StoredNotification, error)
	MarkNotificationDelivered(ctx context.Context, id uuid.UUID) error
}

// Outbox is the bot's read/ack side of the notifications table: it polls for
// undelivered rows and acks each after sending. Polling is at-least-once: a bot
// that sends a DM and dies before acking will send it again, acceptable for
// reminders and simpler than a two-phase ack.
type Outbox struct {
	store outboxStore
}

// NewOutbox builds an Outbox.
func NewOutbox(store outboxStore) *Outbox {
	return &Outbox{store: store}
}

// ListUndelivered returns undelivered notifications, optionally scoped to one
// guild so a single bot process serving many guilds can page.
func (o *Outbox) ListUndelivered(ctx context.Context, guildID *int64, limit int32) ([]StoredNotification, error) {
	notifications, err := o.store.ListUndeliveredNotifications(ctx, guildID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing undelivered notifications: %w", err)
	}
	return notifications, nil
}

func (o *Outbox) MarkDelivered(ctx context.Context, id uuid.UUID) error {
	if err := o.store.MarkNotificationDelivered(ctx, id); err != nil {
		return fmt.Errorf("marking notification delivered: %w", err)
	}
	return nil
}
