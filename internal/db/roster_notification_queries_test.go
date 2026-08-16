//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// queueRosterRedraws is the fan-out roster.Store.ApplySync performs: list the events
// needing a redraw, then insert one notification apiece. It returns how many rows
// actually landed, so a suppressed insert is visible to the assertions.
func queueRosterRedraws(ctx context.Context, t *testing.T, q *Queries, characterID uuid.UUID) int64 {
	t.Helper()

	events, err := q.ListEventsNeedingRosterRedraw(ctx, characterID)
	if err != nil {
		t.Fatalf("listing events needing redraw: %v", err)
	}

	var queued int64
	for _, e := range events {
		rows, err := q.InsertNotification(ctx, InsertNotificationParams{
			ID:             NewID(),
			DiscordGuildID: e.DiscordGuildID,
			EventID:        e.ID,
			Kind:           NotificationKindROSTERUPDATED,
			TargetKind:     NotificationTargetMESSAGE,
			ChannelID:      e.ChannelID,
			Payload:        []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("queueing redraw for event %s: %v", e.ID, err)
		}
		queued += rows
	}
	return queued
}

// seedSignedUpCharacter creates a raider signed up to one event, which is the shape
// the roster redraw fan-out runs over.
func seedSignedUpCharacter(ctx context.Context, t *testing.T, q *Queries, discordID int64) uuid.UUID {
	t.Helper()

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: discordID, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}

	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
		UserID: user.ID, Name: "Thrallmain", Realm: "silvermoon", Region: "eu", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}
	return character.ID
}

// seedPostedEvent creates an event the bot has already posted, so there is a message
// to redraw.
func seedPostedEvent(ctx context.Context, t *testing.T, q *Queries, startsAt time.Time, messageID, channelID *int64) Event {
	t.Helper()

	event, err := q.CreateEvent(ctx, CreateEventParams{
		ID:             NewID(),
		DiscordGuildID: 100,
		Type:           EventTypeRAID,
		Title:          "Prog Night",
		StartsAt:       pgtype.Timestamptz{Time: startsAt, Valid: true},
		SignupDeadline: pgtype.Timestamptz{Time: startsAt.Add(-time.Hour), Valid: true},
		CompTemplate:   []byte(`{}`),
		MessageID:      messageID,
		ChannelID:      channelID,
	})
	if err != nil {
		t.Fatalf("creating event: %v", err)
	}
	return event
}

func signUp(ctx context.Context, t *testing.T, q *Queries, eventID, characterID uuid.UUID) {
	t.Helper()

	if _, err := q.UpsertSignup(ctx, UpsertSignupParams{
		ID:      NewID(),
		EventID: eventID, CharacterID: characterID, Status: SignupStatusCONFIRMED,
	}); err != nil {
		t.Fatalf("signing up: %v", err)
	}
}

func TestRosterRedrawOnlyQueuesPostedUpcomingEvents(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	characterID := seedSignedUpCharacter(ctx, t, q, 8001)
	messageID, channelID := int64(555), int64(777)

	upcoming := seedPostedEvent(ctx, t, q, time.Now().Add(24*time.Hour), &messageID, &channelID)
	past := seedPostedEvent(ctx, t, q, time.Now().Add(-24*time.Hour), &messageID, &channelID)
	unposted := seedPostedEvent(ctx, t, q, time.Now().Add(48*time.Hour), nil, nil)
	for _, event := range []Event{upcoming, past, unposted} {
		signUp(ctx, t, q, event.ID, characterID)
	}

	if rows := queueRosterRedraws(ctx, t, q, characterID); rows != 1 {
		t.Fatalf("queued %d redraws, want 1", rows)
	}

	queued, err := q.ClaimNotifications(ctx, claimParams(10, nil))
	if err != nil {
		t.Fatalf("claiming: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("claimed %d, want 1", len(queued))
	}
	if queued[0].EventID != upcoming.ID {
		t.Errorf("event_id = %s, want the upcoming event %s", queued[0].EventID, upcoming.ID)
	}
	if queued[0].Kind != NotificationKindROSTERUPDATED {
		t.Errorf("kind = %s, want ROSTER_UPDATED", queued[0].Kind)
	}
	if queued[0].TargetKind != NotificationTargetMESSAGE {
		t.Errorf("target_kind = %s, want MESSAGE", queued[0].TargetKind)
	}
	if queued[0].ChannelID == nil || *queued[0].ChannelID != channelID {
		t.Errorf("channel_id = %v, want %d", queued[0].ChannelID, channelID)
	}
}

func TestRosterRedrawCoalescesPerEvent(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	messageID, channelID := int64(556), int64(778)
	event := seedPostedEvent(ctx, t, q, time.Now().Add(24*time.Hour), &messageID, &channelID)

	first := seedSignedUpCharacter(ctx, t, q, 8002)
	second := seedSignedUpCharacter(ctx, t, q, 8003)
	signUp(ctx, t, q, event.ID, first)
	signUp(ctx, t, q, event.ID, second)

	queueRosterRedraws(ctx, t, q, first)

	// The second raider on the same raid finds a redraw already pending. One message
	// edit covers both.
	if rows := queueRosterRedraws(ctx, t, q, second); rows != 0 {
		t.Errorf("queued %d redraws for an event that already had one, want 0", rows)
	}
}

func TestRosterRedrawQueuesAgainAfterClaim(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	messageID, channelID := int64(557), int64(779)
	event := seedPostedEvent(ctx, t, q, time.Now().Add(24*time.Hour), &messageID, &channelID)
	characterID := seedSignedUpCharacter(ctx, t, q, 8004)
	signUp(ctx, t, q, event.ID, characterID)

	queueRosterRedraws(ctx, t, q, characterID)
	if _, err := q.ClaimNotifications(ctx, claimParams(10, nil)); err != nil {
		t.Fatalf("claiming: %v", err)
	}

	// A change arriving while the bot renders cannot be in the message it is building,
	// so the claimed row must not suppress a fresh redraw.
	if rows := queueRosterRedraws(ctx, t, q, characterID); rows != 1 {
		t.Errorf("queued %d redraws behind a claimed one, want 1", rows)
	}
}
