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

// dbTimestamptz rounds a Go time down to what timestamptz can hold. Postgres stores
// microseconds and time.Now() offers nanoseconds, so a raw time.Now() comes back from
// a round trip a few hundred nanoseconds earlier than it went in, and an equality
// assertion against it fails. Only on a machine whose clock is that fine: darwin/arm64
// hands out microsecond-granular values, which is why this passed locally and failed
// everywhere else. Truncating at the seed keeps the assertions exact.
func dbTimestamptz(at time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: at.Truncate(time.Microsecond), Valid: true}
}

func TestUpsertUserIsIdempotentPerGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	userInGuildA, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 1, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user in guild A: %v", err)
	}

	userInGuildB, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 1, DiscordGuildID: 200})
	if err != nil {
		t.Fatalf("upserting user in guild B: %v", err)
	}
	if userInGuildA.ID == userInGuildB.ID {
		t.Fatalf("same discord id in two guilds got one row, want two")
	}

	userInGuildAAgain, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 1, DiscordGuildID: 100})
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

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 2, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
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

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 3, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
		UserID: user.ID, Name: "Bob", Realm: "Area-52", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}
	event, err := q.CreateEvent(ctx, CreateEventParams{
		ID:             NewID(),
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

	// A fresh id per attempt, so the violation below is attributable to
	// UNIQUE (event_id, character_id) and not to the primary key.
	const insertSignup = `INSERT INTO signups (id, event_id, character_id, status) VALUES ($1, $2, $3, 'CONFIRMED')`
	if _, err := tx.Exec(ctx, insertSignup, NewID(), event.ID, character.ID); err != nil {
		t.Fatalf("first signup insert: %v", err)
	}

	_, err = tx.Exec(ctx, insertSignup, NewID(), event.ID, character.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second signup insert: got %v, want unique_violation", err)
	}
	if pgErr.ConstraintName != "signups_event_id_character_id_key" {
		t.Errorf("violated constraint = %s, want signups_event_id_character_id_key", pgErr.ConstraintName)
	}
}

func TestGetCharacterInGuildRejectsForeignGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 4, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
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

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 5, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}

	first, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
		UserID: user.ID, Name: "First", Realm: "Area-52", IsMain: false,
	})
	if err != nil {
		t.Fatalf("creating first character: %v", err)
	}
	second, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
		UserID: user.ID, Name: "Second", Realm: "Area-52", IsMain: false,
	})
	if err != nil {
		t.Fatalf("creating second character: %v", err)
	}

	if bytes.Compare(first.ID[:], second.ID[:]) >= 0 {
		t.Fatalf("second insert's id did not sort after the first: %s then %s", first.ID, second.ID)
	}
}

func TestDeleteCharacterIgnoresAForeignGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 20, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
		UserID: user.ID, Name: "Doomed", Realm: "Area-52", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}

	rows, err := q.DeleteCharacter(ctx, DeleteCharacterParams{ID: character.ID, DiscordGuildID: 999})
	if err != nil {
		t.Fatalf("deleting from a foreign guild: %v", err)
	}
	if rows != 0 {
		t.Fatalf("rows = %d, want 0: a character id alone must not delete another guild's row", rows)
	}

	rows, err = q.DeleteCharacter(ctx, DeleteCharacterParams{ID: character.ID, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("deleting from the owning guild: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}

	if _, err := q.GetCharacterInGuild(ctx, GetCharacterInGuildParams{
		ID: character.ID, DiscordGuildID: 100,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("loading a deleted character: got %v, want pgx.ErrNoRows", err)
	}
}

func TestDeleteCharacterCascadesToItsRoles(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 21, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
		UserID: user.ID, Name: "Rolled", Realm: "Area-52", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}
	if err := q.SetCharacterRole(ctx, SetCharacterRoleParams{
		CharacterID: character.ID, Role: RoleEnumTANK, Priority: 1,
	}); err != nil {
		t.Fatalf("setting role: %v", err)
	}

	if _, err := q.DeleteCharacter(ctx, DeleteCharacterParams{
		ID: character.ID, DiscordGuildID: 100,
	}); err != nil {
		t.Fatalf("deleting character: %v", err)
	}

	// The API promises a delete takes the raider's history with it; that is the FK
	// cascade doing the work, not application code, so it is worth pinning here.
	roles, err := q.ListCharacterRoles(ctx, character.ID)
	if err != nil {
		t.Fatalf("listing roles: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("roles = %d, want 0 after the character was deleted", len(roles))
	}
}

func TestSetCharacterMainIgnoresAForeignGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 22, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
		UserID: user.ID, Name: "Alt", Realm: "Area-52", IsMain: false,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}

	rows, err := q.SetCharacterMain(ctx, SetCharacterMainParams{
		ID: character.ID, DiscordGuildID: 999, IsMain: true,
	})
	if err != nil {
		t.Fatalf("setting main from a foreign guild: %v", err)
	}
	if rows != 0 {
		t.Fatalf("rows = %d, want 0", rows)
	}

	if _, err := q.SetCharacterMain(ctx, SetCharacterMainParams{
		ID: character.ID, DiscordGuildID: 100, IsMain: true,
	}); err != nil {
		t.Fatalf("setting main: %v", err)
	}
	updated, err := q.GetCharacterInGuild(ctx, GetCharacterInGuildParams{
		ID: character.ID, DiscordGuildID: 100,
	})
	if err != nil {
		t.Fatalf("loading character: %v", err)
	}
	if !updated.IsMain {
		t.Error("is_main = false, want the flag flipped by the owning guild's write")
	}
}

func TestCreateCharacterGrantsMainOnlyToFirst(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 30, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}

	// Every registration asks for main, which is what raider-mate-bot does on every
	// /character add. Only the first may have it: the alts must not take the flag off
	// the character holding it, and must not trip characters_one_main_per_user either.
	names := []string{"Danthrax", "Kelthuz", "Zugzug"}
	want := []bool{true, false, false}

	for i, name := range names {
		character, err := q.CreateCharacter(ctx, CreateCharacterParams{
			ID:     NewID(),
			UserID: user.ID, Name: name, Realm: "Draenor", Region: "eu", IsMain: true,
		})
		if err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		if character.IsMain != want[i] {
			t.Errorf("%s is_main = %v, want %v", name, character.IsMain, want[i])
		}
	}
}

func TestCreateCharacterRejectsDuplicateIdentity(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 31, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}

	create := func() error {
		_, err := q.CreateCharacter(ctx, CreateCharacterParams{
			ID:     NewID(),
			UserID: user.ID, Name: "Danthrax", Realm: "Draenor", Region: "eu", IsMain: true,
		})
		return err
	}

	if err := create(); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	// The constraint name is asserted because the store maps this one, and only this
	// one, to roster.ErrCharacterExists and a 409.
	err = create()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second registration: got %v, want unique_violation", err)
	}
	if pgErr.ConstraintName != "characters_user_id_name_realm_region_key" {
		t.Errorf("violated constraint = %s, want characters_user_id_name_realm_region_key", pgErr.ConstraintName)
	}
}

func TestSetCharacterMainSwitchesEitherDirection(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 32, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}

	create := func(name string) Character {
		t.Helper()
		c, err := q.CreateCharacter(ctx, CreateCharacterParams{
			ID:     NewID(),
			UserID: user.ID, Name: name, Realm: "Draenor", Region: "eu", IsMain: true,
		})
		if err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		return c
	}

	// first holds main by registration order; second is the alt.
	first := create("Danthrax")
	second := create("Kelthuz")

	// The pair the store runs inside one transaction. Both directions are exercised
	// because a single-statement demote-and-promote passes one way and violates
	// characters_one_main_per_user the other, depending on heap scan order.
	promote := func(target Character) {
		t.Helper()
		if err := q.ClearMainForCharacterOwner(ctx, ClearMainForCharacterOwnerParams{
			ID: target.ID, DiscordGuildID: 100,
		}); err != nil {
			t.Fatalf("clearing main before promoting %s: %v", target.Name, err)
		}
		rows, err := q.SetCharacterMain(ctx, SetCharacterMainParams{
			ID: target.ID, DiscordGuildID: 100, IsMain: true,
		})
		if err != nil {
			t.Fatalf("promoting %s: %v", target.Name, err)
		}
		if rows != 1 {
			t.Fatalf("promoting %s affected %d rows, want 1", target.Name, rows)
		}
	}

	assertMain := func(want Character, other Character) {
		t.Helper()
		for _, c := range []Character{want, other} {
			got, err := q.GetCharacterInGuild(ctx, GetCharacterInGuildParams{ID: c.ID, DiscordGuildID: 100})
			if err != nil {
				t.Fatalf("reading %s: %v", c.Name, err)
			}
			if wantMain := c.ID == want.ID; got.IsMain != wantMain {
				t.Errorf("%s is_main = %v, want %v", c.Name, got.IsMain, wantMain)
			}
		}
	}

	promote(second)
	assertMain(second, first)

	promote(first)
	assertMain(first, second)
}

func TestClearMainForCharacterOwnerIgnoresForeignGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, err := q.UpsertUser(ctx, UpsertUserParams{ID: NewID(), DiscordID: 33, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("upserting user: %v", err)
	}
	character, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID:     NewID(),
		UserID: user.ID, Name: "Danthrax", Realm: "Draenor", Region: "eu", IsMain: true,
	})
	if err != nil {
		t.Fatalf("creating character: %v", err)
	}

	// The subquery finds nothing for another guild, so the demote must be a no-op
	// rather than clearing the flag on a roster the caller cannot see.
	if err := q.ClearMainForCharacterOwner(ctx, ClearMainForCharacterOwnerParams{
		ID: character.ID, DiscordGuildID: 999,
	}); err != nil {
		t.Fatalf("clearing main from a foreign guild: %v", err)
	}

	got, err := q.GetCharacterInGuild(ctx, GetCharacterInGuildParams{ID: character.ID, DiscordGuildID: 100})
	if err != nil {
		t.Fatalf("reading character: %v", err)
	}
	if !got.IsMain {
		t.Error("is_main = false, want the flag untouched by a foreign guild's write")
	}
}
