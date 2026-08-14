//go:build integration

package db

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// newTxQueries starts a transaction that is rolled back when the test ends, so
// tests do not see each other's writes.
func newTxQueries(ctx context.Context, t *testing.T) (*Queries, pgx.Tx) {
	t.Helper()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback(context.Background())
	})

	return New(tx), tx
}

func TestUpsertUserIsIdempotentPerGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	userInGuildA, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 1, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user in guild A: %v", err)
	}

	userInGuildB, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 1, DiscordGuildID: 200})
	if err != nil {
		t.Fatalf("upserting user in guild B: %v", err)
	}
	if userInGuildA.ID == userInGuildB.ID {
		t.Fatalf("same discord id in two guilds got one row, want two")
	}

	userInGuildAAgain, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 1, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user in guild A again: %v", err)
	}
	if userInGuildAAgain.ID != userInGuildA.ID {
		t.Fatalf("same discord id and guild id produced a new row, want the same one")
	}
}

func TestCharacterRolesRoundTripInPriorityOrder(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 2, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "Danthrax", Realm: "Area-52", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}

	roles := []SetCharacterRoleParams{
		{CharacterID: character.ID, Role: RoleEnumTANK, Priority: 2},
		{CharacterID: character.ID, Role: RoleEnumHEALER, Priority: 1},
		{CharacterID: character.ID, Role: RoleEnumMDPS, Priority: 3},
	}
	for _, r := range roles {
		if err := q.SetCharacterRole(ctx, r); err != nil {
			t.Fatalf("setting role %s: %v", r.Role, err)
		}
	}

	got, err := q.ListCharacterRoles(ctx, character.ID)
	if err != nil {
		t.Fatalf("listing roles: %v", err)
	}

	want := []RoleEnum{RoleEnumHEALER, RoleEnumTANK, RoleEnumMDPS}
	if len(got) != len(want) {
		t.Fatalf("got %d roles, want %d", len(got), len(want))
	}
	for i, role := range want {
		if got[i].Role != role {
			t.Fatalf("role at position %d: got %s, want %s", i, got[i].Role, role)
		}
	}
}

func TestSignupRejectsDuplicateCharacterPerEvent(t *testing.T) {
	ctx := context.Background()
	q, tx := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 3, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "Bob", Realm: "Area-52", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}
	event, err := q.CreateEvent(ctx, CreateEventParams{
		DiscordGuildID: 100,
		Type:           EventTypeRAID,
		Title:          "Prog Night",
		StartsAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		SignupDeadline: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CompTemplate:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("creating event: %v", err)
	}

	const insertSignup = `INSERT INTO signups (event_id, character_id, status) VALUES ($1, $2, 'CONFIRMED')`
	if _, err := tx.Exec(ctx, insertSignup, event.ID, character.ID); err != nil {
		t.Fatalf("first signup insert: %v", err)
	}

	_, err = tx.Exec(ctx, insertSignup, event.ID, character.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second signup insert: got %v, want unique_violation", err)
	}
}

func TestGetCharacterInGuildRejectsForeignGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 4, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "Sneaky", Realm: "Area-52", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}

	_, err = q.GetCharacterInGuild(ctx, GetCharacterInGuildParams{
		ID: character.ID, DiscordGuildID: 999,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("looking up character from a foreign guild: got %v, want pgx.ErrNoRows", err)
	}
}

func TestPrimaryKeysAreTimeOrdered(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{DiscordID: 5, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}

	first, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "First", Realm: "Area-52", IsMain: false,
	})
	if err != nil {
		t.Fatalf("creating first character: %v", err)
	}
	second, err := q.CreateCharacter(ctx, CreateCharacterParams{
		UserID: user.ID, Name: "Second", Realm: "Area-52", IsMain: false,
	})
	if err != nil {
		t.Fatalf("creating second character: %v", err)
	}

	if bytes.Compare(first.ID[:], second.ID[:]) >= 0 {
		t.Fatalf("second insert's id did not sort after the first: %s then %s", first.ID, second.ID)
	}
}
