//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestNotificationRoundTripsAndClearsFromUndelivered(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 40)
	discordID := int64(9001)

	if err := q.InsertNotification(ctx, InsertNotificationParams{
		DiscordGuildID: 100,
		EventID:        event.ID,
		Kind:           NotificationKindREMINDER24H,
		TargetKind:     NotificationTargetUSER,
		DiscordID:      &discordID,
		Payload:        []byte(`{"title":"Prog Night"}`),
	}); err != nil {
		t.Fatalf("inserting notification: %v", err)
	}

	undelivered, err := q.ListUndeliveredNotifications(ctx, ListUndeliveredNotificationsParams{RowLimit: 10})
	if err != nil {
		t.Fatalf("listing undelivered: %v", err)
	}
	if len(undelivered) != 1 {
		t.Fatalf("undelivered = %d, want 1", len(undelivered))
	}
	if undelivered[0].DiscordID == nil || *undelivered[0].DiscordID != discordID {
		t.Errorf("discord_id = %v, want %d", undelivered[0].DiscordID, discordID)
	}

	if err := q.MarkNotificationDelivered(ctx, undelivered[0].ID); err != nil {
		t.Fatalf("marking delivered: %v", err)
	}

	after, err := q.ListUndeliveredNotifications(ctx, ListUndeliveredNotificationsParams{RowLimit: 10})
	if err != nil {
		t.Fatalf("re-listing undelivered: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("undelivered after ack = %d, want 0", len(after))
	}
}

func TestListUndeliveredNotificationsFiltersByGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	eventA := seedEventForJobs(ctx, t, q, 41)
	eventB, err := q.CreateEvent(ctx, CreateEventParams{
		DiscordGuildID: 200,
		Type:           EventTypeRAID,
		Title:          "Other Guild Night",
		StartsAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		SignupDeadline: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CompTemplate:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("creating event for guild 200: %v", err)
	}

	channelID := int64(555)
	if err := q.InsertNotification(ctx, InsertNotificationParams{
		DiscordGuildID: 100, EventID: eventA.ID, Kind: NotificationKindCOMPNAG,
		TargetKind: NotificationTargetROLE, ChannelID: &channelID, Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("inserting notification for guild 100: %v", err)
	}
	if err := q.InsertNotification(ctx, InsertNotificationParams{
		DiscordGuildID: 200, EventID: eventB.ID, Kind: NotificationKindCOMPNAG,
		TargetKind: NotificationTargetROLE, ChannelID: &channelID, Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("inserting notification for guild 200: %v", err)
	}

	guild := int64(100)
	filtered, err := q.ListUndeliveredNotifications(ctx, ListUndeliveredNotificationsParams{
		GuildID: &guild, RowLimit: 10,
	})
	if err != nil {
		t.Fatalf("listing filtered notifications: %v", err)
	}
	if len(filtered) != 1 || filtered[0].DiscordGuildID != 100 {
		t.Fatalf("filtered = %+v, want exactly the guild-100 row", filtered)
	}
}

func TestNotificationCheckConstraintRejectsUserRowWithNoDiscordID(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 42)

	err := q.InsertNotification(ctx, InsertNotificationParams{
		DiscordGuildID: 100, EventID: event.ID, Kind: NotificationKindREMINDER1H,
		TargetKind: NotificationTargetUSER, Payload: []byte(`{}`),
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("inserting USER notification with no discord_id: got %v, want a check_violation", err)
	}
}
