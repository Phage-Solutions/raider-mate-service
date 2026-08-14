//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestGetLatestCharacterSnapshotReturnsNewest(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 10, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "Danthrax", Realm: "Area-52", Region: "us", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}

	var older, newer pgtype.Numeric
	if err := older.Scan("400"); err != nil {
		t.Fatalf("scanning older ilvl: %v", err)
	}
	if err := newer.Scan("415"); err != nil {
		t.Fatalf("scanning newer ilvl: %v", err)
	}

	if _, err := q.InsertCharacterSnapshot(ctx, InsertCharacterSnapshotParams{
		CharacterID: character.ID, Ilvl: older, MplusScore: older, Gear: []byte(`[]`),
	}); err != nil {
		t.Fatalf("inserting older snapshot: %v", err)
	}
	// captured_at is transaction start time, so both snapshots tie here; the query
	// breaks ties on id (UUIDv7, insertion-ordered).
	want, err := q.InsertCharacterSnapshot(ctx, InsertCharacterSnapshotParams{
		CharacterID: character.ID, Ilvl: newer, MplusScore: newer, Gear: []byte(`[{"slot":"head"}]`),
	})
	if err != nil {
		t.Fatalf("inserting newer snapshot: %v", err)
	}

	got, err := q.GetLatestCharacterSnapshot(ctx, character.ID)
	if err != nil {
		t.Fatalf("getting latest snapshot: %v", err)
	}
	if got.ID != want.ID {
		t.Fatalf("got snapshot %s, want the newer one %s", got.ID, want.ID)
	}
}

func TestListCharactersDueForSyncOrdersOldestFirstNullsFirst(t *testing.T) {
	ctx := context.Background()
	q, tx := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 11, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}

	neverSynced, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "NeverSynced", Realm: "Area-52", Region: "us",
	})
	if err != nil {
		t.Fatalf("creating never-synced character: %v", err)
	}
	syncedRecently, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "SyncedRecently", Realm: "Area-52", Region: "us",
	})
	if err != nil {
		t.Fatalf("creating recently-synced character: %v", err)
	}
	syncedLongAgo, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "SyncedLongAgo", Realm: "Area-52", Region: "us",
	})
	if err != nil {
		t.Fatalf("creating long-ago-synced character: %v", err)
	}

	if err := q.UpdateCharacterFromSync(ctx, UpdateCharacterFromSyncParams{ID: syncedRecently.ID}); err != nil {
		t.Fatalf("syncing recent character: %v", err)
	}

	// Both columns, so the character sorts behind the never-attempted one instead of
	// tying with it on a NULL sync_attempted_at.
	const backdate = `UPDATE characters SET last_synced = $2, sync_attempted_at = $2 WHERE id = $1`
	if _, err := tx.Exec(ctx, backdate, syncedLongAgo.ID, time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatalf("backdating long-ago character: %v", err)
	}

	got, err := q.ListCharactersDueForSync(ctx, ListCharactersDueForSyncParams{
		StaleBefore: pgtype.Timestamptz{Time: time.Now().Add(-6 * time.Hour), Valid: true},
		RowLimit:    10,
	})
	if err != nil {
		t.Fatalf("listing due characters: %v", err)
	}

	if len(got) < 2 {
		t.Fatalf("got %d due characters, want at least 2 (never-synced and long-ago)", len(got))
	}
	if got[0].ID != neverSynced.ID {
		t.Fatalf("first due character = %s, want the never-synced one %s", got[0].Name, neverSynced.Name)
	}
	for _, c := range got {
		if c.ID == syncedRecently.ID {
			t.Fatalf("recently-synced character %s should not be due", c.Name)
		}
	}
}

func TestMarkCharacterSyncAttemptedDropsCharacterFromDueSet(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 12, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	failing, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "Failing", Realm: "Area-52", Region: "us",
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}

	// A fetch failed: last_synced stays NULL, so the staleness filter alone would
	// hand the character straight back at the head of the next batch.
	if err := q.MarkCharacterSyncAttempted(ctx, failing.ID); err != nil {
		t.Fatalf("marking sync attempt: %v", err)
	}

	got, err := q.ListCharactersDueForSync(ctx, ListCharactersDueForSyncParams{
		StaleBefore: pgtype.Timestamptz{Time: time.Now().Add(-6 * time.Hour), Valid: true},
		RowLimit:    10,
	})
	if err != nil {
		t.Fatalf("listing due characters: %v", err)
	}

	for _, c := range got {
		if c.ID == failing.ID {
			t.Fatalf("character %s is due again immediately after a failed attempt", c.Name)
		}
	}
}

func TestUpdateCharacterFromSyncKeepsClassWhenNull(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 13, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "Danthrax", Realm: "Area-52", Region: "us",
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}

	class, spec := "Warrior", "Protection"
	if err := q.UpdateCharacterFromSync(ctx, UpdateCharacterFromSyncParams{
		ID: character.ID, Class: &class, Spec: &spec,
	}); err != nil {
		t.Fatalf("seeding class and spec: %v", err)
	}

	// Raider.IO omitted both fields this time.
	if err := q.UpdateCharacterFromSync(ctx, UpdateCharacterFromSyncParams{ID: character.ID}); err != nil {
		t.Fatalf("syncing without class or spec: %v", err)
	}

	got, err := q.GetCharacterInGuild(ctx, GetCharacterInGuildParams{ID: character.ID, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("reading character back: %v", err)
	}
	if got.Class == nil || *got.Class != class {
		t.Errorf("class = %v, want %q preserved", got.Class, class)
	}
	if got.Spec == nil || *got.Spec != spec {
		t.Errorf("spec = %v, want %q preserved", got.Spec, spec)
	}
}
