//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// notifyGuild keeps this file's rows out of everyone else's way. The cases here commit,
// because pg_notify delivers at commit and the rolled back transaction newTxQueries
// hands out never gets there, so a guild of their own is what stops a committed row
// showing up in another test's guild-scoped count.
const notifyGuild = int64(900)

// listenForQueued opens a connection outside the pool and puts it in LISTEN. The
// connection has to stay out of the pool: one inside LISTEN cannot serve a query.
func listenForQueued(ctx context.Context, t *testing.T) func(timeout time.Duration) bool {
	t.Helper()

	acquired, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring listener connection: %v", err)
	}
	conn := acquired.Hijack()
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	if _, err := conn.Exec(ctx, "LISTEN notification_queued"); err != nil {
		t.Fatalf("listening: %v", err)
	}

	return func(timeout time.Duration) bool {
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		_, err := conn.WaitForNotification(waitCtx)
		return err == nil
	}
}

// seedCommittedEvent writes a posted, upcoming event and removes it afterwards. The
// delete cascades to signups and notifications, so the cleanup is one statement.
func seedCommittedEvent(ctx context.Context, t *testing.T, q *Queries) Event {
	t.Helper()

	messageID, channelID := int64(9001), int64(9002)
	event, err := q.CreateEvent(ctx, CreateEventParams{
		DiscordGuildID: notifyGuild,
		Type:           EventTypeRAID,
		Title:          "Signal Night",
		StartsAt:       pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
		SignupDeadline: pgtype.Timestamptz{Time: time.Now().Add(23 * time.Hour), Valid: true},
		CompTemplate:   []byte(`{}`),
		MessageID:      &messageID,
		ChannelID:      &channelID,
	})
	if err != nil {
		t.Fatalf("creating event: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteEvent(context.Background(), event.ID) })
	return event
}

// seedCommittedCharacter writes a raider and removes them and their user afterwards.
func seedCommittedCharacter(ctx context.Context, t *testing.T, q *Queries, discordID int64) Character {
	t.Helper()

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: discordID, DiscordGuildID: notifyGuild})
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "Signaller", Realm: "silvermoon", Region: "eu", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}
	t.Cleanup(func() {
		// Characters cascade from the user, so one delete covers both.
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})
	return character
}

func TestInsertingANotificationRaisesTheQueuedSignal(t *testing.T) {
	ctx := context.Background()
	q := New(pool)
	waitForSignal := listenForQueued(ctx, t)
	event := seedCommittedEvent(ctx, t, q)

	discordID := int64(9100)
	if err := q.InsertNotification(ctx, InsertNotificationParams{
		DiscordGuildID: notifyGuild,
		EventID:        event.ID,
		Kind:           NotificationKindREMINDER24H,
		TargetKind:     NotificationTargetUSER,
		DiscordID:      &discordID,
		Payload:        []byte(`{"title":"Signal Night"}`),
	}); err != nil {
		t.Fatalf("inserting notification: %v", err)
	}

	if !waitForSignal(5 * time.Second) {
		t.Fatal("no notification_queued signal after an insert")
	}
}

func TestOneStatementRaisesOneSignalHoweverManyRows(t *testing.T) {
	ctx := context.Background()
	q := New(pool)
	waitForSignal := listenForQueued(ctx, t)
	event := seedCommittedEvent(ctx, t, q)
	character := seedCommittedCharacter(ctx, t, q, 9101)

	if _, err := q.UpsertSignup(ctx, UpsertSignupParams{
		EventID: event.ID, CharacterID: character.ID, Status: SignupStatusCONFIRMED,
	}); err != nil {
		t.Fatalf("signing up: %v", err)
	}

	// The fan-out insert writes one row per event this character is signed up to. The
	// bot's answer to all of them is a single claim, which is why the trigger fires per
	// statement rather than per row.
	rows, err := q.InsertRosterUpdatedNotifications(ctx, character.ID)
	if err != nil {
		t.Fatalf("queueing redraws: %v", err)
	}
	if rows != 1 {
		t.Fatalf("queued %d redraws, want 1", rows)
	}

	if !waitForSignal(5 * time.Second) {
		t.Fatal("no notification_queued signal after the fan-out insert")
	}
	if waitForSignal(500 * time.Millisecond) {
		t.Error("a second signal arrived for a single statement")
	}
}
