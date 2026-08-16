package signup

import (
	"context"
	"errors"
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
	DiscordIDs     []int64
	ChannelID      *int64
	Payload        []byte
	CreatedAt      time.Time
}

// ErrNotificationNotFound means the id does not exist, or belongs to another guild.
// The two are deliberately indistinguishable to the caller.
var ErrNotificationNotFound = errors.New("notification not found")

// claimLease is how long a claimed notification stays off other pollers' lists. Long
// enough that a bot sending a batch of DMs is not raced, short enough that a bot which
// died mid-batch has its work redelivered promptly.
const claimLease = 5 * time.Minute

// outboxStore is the persistence Outbox needs. Declared here, by the consumer.
type outboxStore interface {
	ClaimNotifications(ctx context.Context, guildID *int64, claimedBefore time.Time, limit int32) ([]StoredNotification, error)
	MarkNotificationDelivered(ctx context.Context, id uuid.UUID, discordGuildID *int64) error
}

// Outbox is the bot's read/ack side of the notifications table: it claims undelivered
// rows and acks each after sending. Delivery is at-least-once: a bot that sends a DM
// and dies before acking will send it again once its claim lease expires, acceptable
// for reminders and simpler than a two-phase ack. What the claim removes is the case
// that is not acceptable, two pollers sending the same batch on every tick.
type Outbox struct {
	store outboxStore
}

// NewOutbox builds an Outbox.
func NewOutbox(store outboxStore) *Outbox {
	return &Outbox{store: store}
}

// Claim takes up to limit undelivered notifications for one guild and marks them
// claimed, so a concurrent poller gets a different batch.
func (o *Outbox) Claim(ctx context.Context, guildID *int64, limit int32) ([]StoredNotification, error) {
	notifications, err := o.store.ClaimNotifications(ctx, guildID, time.Now().Add(-claimLease), limit)
	if err != nil {
		return nil, fmt.Errorf("claiming notifications: %w", err)
	}
	return notifications, nil
}

// MarkDelivered acks one notification. A nil discordGuildID acks by id alone, which
// only the bot's service-key route may do; anything a raider can reach passes their
// guild, or they could suppress another guild's reminders.
func (o *Outbox) MarkDelivered(ctx context.Context, id uuid.UUID, discordGuildID *int64) error {
	if err := o.store.MarkNotificationDelivered(ctx, id, discordGuildID); err != nil {
		return fmt.Errorf("marking notification delivered: %w", err)
	}
	return nil
}
