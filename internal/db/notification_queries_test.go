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

	if _, err := q.InsertNotification(ctx, InsertNotificationParams{
		ID:             NewID(),
		DiscordGuildID: 100,
		EventID:        event.ID,
		Kind:           NotificationKindREMINDER24H,
		TargetKind:     NotificationTargetUSER,
		DiscordID:      &discordID,
		Payload:        []byte(`{"title":"Prog Night"}`),
	}); err != nil {
		t.Fatalf("inserting notification: %v", err)
	}

	undelivered, err := q.ClaimNotifications(ctx, claimParams(10, nil))
	if err != nil {
		t.Fatalf("claiming: %v", err)
	}
	if len(undelivered) != 1 {
		t.Fatalf("claimed = %d, want 1", len(undelivered))
	}
	if undelivered[0].DiscordID == nil || *undelivered[0].DiscordID != discordID {
		t.Errorf("discord_id = %v, want %d", undelivered[0].DiscordID, discordID)
	}

	guild := int64(100)
	rows, err := q.MarkNotificationDelivered(ctx, MarkNotificationDeliveredParams{
		ID: undelivered[0].ID, GuildID: &guild,
	})
	if err != nil {
		t.Fatalf("marking delivered: %v", err)
	}
	if rows != 1 {
		t.Fatalf("acked %d rows, want 1", rows)
	}

	// Lease wide open, so only delivered_at can be keeping it out of the result.
	after, err := q.ClaimNotifications(ctx, claimParamsAt(time.Now().Add(time.Hour), 10, nil))
	if err != nil {
		t.Fatalf("re-claiming: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("claimable after ack = %d, want 0", len(after))
	}
}

// testClaimLease mirrors the lease signup.Outbox applies. Passing a bare time.Now()
// instead would mean a zero-length lease, under which every claimed row is instantly
// reclaimable and the claim proves nothing.
const testClaimLease = 5 * time.Minute

// claimParams claims unclaimed rows and any whose lease has lapsed.
func claimParams(limit int32, guildID *int64) ClaimNotificationsParams {
	return claimParamsAt(time.Now().Add(-testClaimLease), limit, guildID)
}

func claimParamsAt(claimedBefore time.Time, limit int32, guildID *int64) ClaimNotificationsParams {
	return ClaimNotificationsParams{
		ClaimedBefore: pgtype.Timestamptz{Time: claimedBefore, Valid: true},
		GuildID:       guildID,
		RowLimit:      limit,
	}
}

func TestClaimNotificationsHandsARowToOnePollerOnly(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 45)
	discordID := int64(9002)
	if _, err := q.InsertNotification(ctx, InsertNotificationParams{
		ID:             NewID(),
		DiscordGuildID: 100, EventID: event.ID, Kind: NotificationKindREMINDER24H,
		TargetKind: NotificationTargetUSER, DiscordID: &discordID, Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("inserting notification: %v", err)
	}

	first, err := q.ClaimNotifications(ctx, claimParams(10, nil))
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first claim = %d rows, want 1", len(first))
	}

	// The second poller's turn. The ack arrives in a later HTTP request, so nothing
	// has been delivered yet; only the claim stops this row being sent twice.
	second, err := q.ClaimNotifications(ctx, claimParams(10, nil))
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second claim = %d rows, want 0: a claimed row must not be sent twice", len(second))
	}
}

func TestClaimNotificationsRedeliversAfterTheLeaseExpires(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 46)
	discordID := int64(9003)
	if _, err := q.InsertNotification(ctx, InsertNotificationParams{
		ID:             NewID(),
		DiscordGuildID: 100, EventID: event.ID, Kind: NotificationKindREMINDER1H,
		TargetKind: NotificationTargetUSER, DiscordID: &discordID, Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("inserting notification: %v", err)
	}

	if _, err := q.ClaimNotifications(ctx, claimParams(10, nil)); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// A bot that claimed and died. Delivery stays at-least-once: the work must come
	// back once its lease lapses, or the reminder is silently lost.
	again, err := q.ClaimNotifications(ctx, claimParamsAt(time.Now().Add(time.Hour), 10, nil))
	if err != nil {
		t.Fatalf("claim after lease: %v", err)
	}
	if len(again) != 1 {
		t.Errorf("claim after lease = %d rows, want 1 redelivered", len(again))
	}
}

func TestMarkNotificationDeliveredIgnoresAnotherGuildsRow(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 47)
	discordID := int64(9004)
	if _, err := q.InsertNotification(ctx, InsertNotificationParams{
		ID:             NewID(),
		DiscordGuildID: 100, EventID: event.ID, Kind: NotificationKindREMINDER24H,
		TargetKind: NotificationTargetUSER, DiscordID: &discordID, Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("inserting notification: %v", err)
	}
	claimed, err := q.ClaimNotifications(ctx, claimParams(10, nil))
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claiming: %v (%d rows)", err, len(claimed))
	}

	// Guild 200 acking guild 100's notification would silently suppress its reminder.
	otherGuild := int64(200)
	rows, err := q.MarkNotificationDelivered(ctx, MarkNotificationDeliveredParams{
		ID: claimed[0].ID, GuildID: &otherGuild,
	})
	if err != nil {
		t.Fatalf("marking delivered: %v", err)
	}
	if rows != 0 {
		t.Errorf("acked %d rows from the wrong guild, want 0", rows)
	}
}

func TestClaimNotificationsFiltersByGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	eventA := seedEventForJobs(ctx, t, q, 41)
	eventB, err := q.CreateEvent(ctx, CreateEventParams{
		ID:             NewID(),
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
	if _, err := q.InsertNotification(ctx, InsertNotificationParams{
		ID:             NewID(),
		DiscordGuildID: 100, EventID: eventA.ID, Kind: NotificationKindCOMPNAG,
		TargetKind: NotificationTargetROLE, ChannelID: &channelID, Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("inserting notification for guild 100: %v", err)
	}
	if _, err := q.InsertNotification(ctx, InsertNotificationParams{
		ID:             NewID(),
		DiscordGuildID: 200, EventID: eventB.ID, Kind: NotificationKindCOMPNAG,
		TargetKind: NotificationTargetROLE, ChannelID: &channelID, Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("inserting notification for guild 200: %v", err)
	}

	guild := int64(100)
	filtered, err := q.ClaimNotifications(ctx, claimParams(10, &guild))
	if err != nil {
		t.Fatalf("claiming filtered notifications: %v", err)
	}
	if len(filtered) != 1 || filtered[0].DiscordGuildID != 100 {
		t.Fatalf("filtered = %+v, want exactly the guild-100 row", filtered)
	}
}

func TestNotificationCheckConstraintRejectsUserRowWithNoDiscordID(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 42)

	_, err := q.InsertNotification(ctx, InsertNotificationParams{
		ID:             NewID(),
		DiscordGuildID: 100, EventID: event.ID, Kind: NotificationKindREMINDER1H,
		TargetKind: NotificationTargetUSER, Payload: []byte(`{}`),
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("inserting USER notification with no discord_id: got %v, want a check_violation", err)
	}
}

// The kind arrived in migration 00002, after the squashed baseline. Nothing else
// exercises the value against a real enum, so a migration that never ran would only
// show up as a failed insert in production.
func TestInsertNotificationAcceptsCompSlotDropped(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 45)
	channelID := int64(555)

	if _, err := q.InsertNotification(ctx, InsertNotificationParams{
		ID:             NewID(),
		DiscordGuildID: 100,
		EventID:        event.ID,
		Kind:           NotificationKindCOMPSLOTDROPPED,
		TargetKind:     NotificationTargetROLE,
		RoleIds:        []int64{777},
		ChannelID:      &channelID,
		Payload:        []byte(`{"event_title":"Prog Night","comp_names":["prog"]}`),
	}); err != nil {
		t.Fatalf("inserting COMP_SLOT_DROPPED notification: %v", err)
	}

	claimed, err := q.ClaimNotifications(ctx, claimParams(10, nil))
	if err != nil {
		t.Fatalf("claiming: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Kind != NotificationKindCOMPSLOTDROPPED {
		t.Fatalf("claimed = %+v, want one COMP_SLOT_DROPPED row", claimed)
	}
}
