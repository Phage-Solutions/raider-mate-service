package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
	"github.com/Phage-Solutions/raider-mate-service/internal/roster"
)

// fakeCharacterStore satisfies the unexported interface roster.NewCharacters takes.
// Only the methods these tests reach are given behaviour; the rest exist to satisfy
// the interface and fail loudly if a handler starts calling them.
type fakeCharacterStore struct {
	guildID   int64
	ownerID   int64
	character roster.Character
	roles     []roster.RoleChoice
	inGuild   bool

	deleted  bool
	mainSet  *bool
	notFound bool
}

func (f *fakeCharacterStore) UpsertUser(context.Context, int64, int64) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (f *fakeCharacterStore) CreateCharacter(context.Context, uuid.UUID, roster.RegisterInput) (roster.Character, error) {
	return f.character, nil
}

func (f *fakeCharacterStore) GetCharacterInGuild(_ context.Context, _ uuid.UUID, discordGuildID int64) (roster.Character, error) {
	if !f.inGuild || discordGuildID != f.guildID {
		return roster.Character{}, pgx.ErrNoRows
	}
	return f.character, nil
}

func (f *fakeCharacterStore) GetCharacterOwner(_ context.Context, _ uuid.UUID, discordGuildID int64) (int64, error) {
	if !f.inGuild || discordGuildID != f.guildID {
		return 0, pgx.ErrNoRows
	}
	return f.ownerID, nil
}

func (f *fakeCharacterStore) DeleteCharacter(context.Context, uuid.UUID, int64) (bool, error) {
	if f.notFound {
		return false, nil
	}
	f.deleted = true
	return true, nil
}

func (f *fakeCharacterStore) SetCharacterMain(_ context.Context, _ uuid.UUID, _ int64, isMain bool) (bool, error) {
	if f.notFound {
		return false, nil
	}
	f.mainSet = &isMain
	return true, nil
}

func (f *fakeCharacterStore) ListCharactersInGuild(context.Context, int64) ([]roster.Character, error) {
	return []roster.Character{f.character}, nil
}

func (f *fakeCharacterStore) ListCharactersByDiscord(context.Context, int64, int64) ([]roster.Character, error) {
	return []roster.Character{f.character}, nil
}

func (f *fakeCharacterStore) ReplaceCharacterRoles(context.Context, uuid.UUID, []roster.RoleChoice) error {
	return nil
}

func (f *fakeCharacterStore) ListCharacterRoles(context.Context, uuid.UUID) ([]roster.RoleChoice, error) {
	return f.roles, nil
}

// newCharacterFixture wires a Characters over a fake holding one character owned by
// ownerDiscordID, living in homeGuild.
func newCharacterFixture(ownerDiscordID int64) (*fakeCharacterStore, *roster.Characters, uuid.UUID) {
	id := uuid.New()
	store := &fakeCharacterStore{
		guildID: homeGuild,
		ownerID: ownerDiscordID,
		inGuild: true,
		character: roster.Character{
			ID: id, Name: "Thrall", Realm: "Draenor", Region: "eu", IsMain: true,
		},
		roles: []roster.RoleChoice{
			{Role: db.RoleEnumTANK, Priority: 1},
			{Role: db.RoleEnumMDPS, Priority: 2},
		},
	}
	return store, roster.NewCharacters(store), id
}

// characterRequest builds a request with {cid} already resolved, as the mux would.
func characterRequest(method, target string, actor Actor, id uuid.UUID, body string) *http.Request {
	r := requestAs(method, target, actor, body)
	r.SetPathValue("cid", id.String())
	return r
}

func TestGetCharacterRolesReturnsTheMenuInPriorityOrder(t *testing.T) {
	_, characters, id := newCharacterFixture(1)
	w := httptest.NewRecorder()

	getCharacterRolesHandler(characters, testLogger())(w, characterRequest(
		http.MethodGet, "/api/characters/"+id.String()+"/roles", homeActor(false), id, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var got []roleChoiceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got) != 2 || got[0].Role != string(db.RoleEnumTANK) {
		t.Errorf("roles = %v, want TANK first: the bot pre-ticks the select from this", got)
	}
}

func TestGetCharacterRolesHidesAForeignGuildsCharacter(t *testing.T) {
	store, characters, id := newCharacterFixture(1)
	store.inGuild = false
	w := httptest.NewRecorder()

	getCharacterRolesHandler(characters, testLogger())(w, characterRequest(
		http.MethodGet, "/api/characters/"+id.String()+"/roles", homeActor(false), id, ""))

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestGetCharacterRolesAllowsAGuildmate(t *testing.T) {
	_, characters, id := newCharacterFixture(999) // owned by somebody else
	w := httptest.NewRecorder()

	getCharacterRolesHandler(characters, testLogger())(w, characterRequest(
		http.MethodGet, "/api/characters/"+id.String()+"/roles", homeActor(false), id, ""))

	// A guildmate's role menu is already on every signup list and comp board; hiding
	// it here would only stop the bot rendering what it can already see.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestDeleteCharacterRefusesSomebodyElsesCharacter(t *testing.T) {
	store, characters, id := newCharacterFixture(999)
	w := httptest.NewRecorder()

	deleteCharacterHandler(characters, testLogger())(w, characterRequest(
		http.MethodDelete, "/api/characters/"+id.String(), homeActor(false), id, ""))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if store.deleted {
		t.Error("deleted = true, want the delete refused before it reached the store")
	}
}

func TestDeleteCharacterAllowsARaidLeadCleaningUpTheRoster(t *testing.T) {
	store, characters, id := newCharacterFixture(999)
	w := httptest.NewRecorder()

	deleteCharacterHandler(characters, testLogger())(w, characterRequest(
		http.MethodDelete, "/api/characters/"+id.String(), homeActor(true), id, ""))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if !store.deleted {
		t.Error("deleted = false, want a raid lead able to remove a mistyped registration")
	}
}

func TestDeleteCharacterHidesAForeignGuildsCharacter(t *testing.T) {
	store, characters, id := newCharacterFixture(1)
	store.inGuild = false
	w := httptest.NewRecorder()

	deleteCharacterHandler(characters, testLogger())(w, characterRequest(
		http.MethodDelete, "/api/characters/"+id.String(), homeActor(true), id, ""))

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if store.deleted {
		t.Error("deleted = true, want a raid lead confined to their own guild's roster")
	}
}

func TestPatchCharacterSetsIsMain(t *testing.T) {
	store, characters, id := newCharacterFixture(1)
	w := httptest.NewRecorder()

	patchCharacterHandler(characters, testLogger())(w, characterRequest(
		http.MethodPatch, "/api/characters/"+id.String(), homeActor(false), id, `{"is_main":false}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %s", w.Code, http.StatusOK, w.Body)
	}
	if store.mainSet == nil || *store.mainSet {
		t.Errorf("mainSet = %v, want false written through", store.mainSet)
	}
}

func TestPatchCharacterRejectsAnEmptyBody(t *testing.T) {
	store, characters, id := newCharacterFixture(1)
	w := httptest.NewRecorder()

	patchCharacterHandler(characters, testLogger())(w, characterRequest(
		http.MethodPatch, "/api/characters/"+id.String(), homeActor(false), id, `{}`))

	// is_main is a pointer so that an absent field is distinguishable from false;
	// without that, {} would silently demote the raider's main.
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if store.mainSet != nil {
		t.Errorf("mainSet = %v, want nothing written for an absent field", *store.mainSet)
	}
}

func TestCharacterLinksSeparateRoleEditingFromRosterHygiene(t *testing.T) {
	character := roster.Character{ID: uuid.New(), Name: "Thrall"}

	raidLead := characterToResponse(character, false, true)
	if _, ok := raidLead.Links["roles"]; ok {
		t.Error("links has roles, want a role menu to stay self-only even for a raid lead")
	}
	if _, ok := raidLead.Links["delete"]; !ok {
		t.Error("links has no delete, want a raid lead able to clear up the roster")
	}

	owner := characterToResponse(character, true, false)
	if _, ok := owner.Links["roles"]; !ok {
		t.Error("links has no roles, want the owner able to edit their own menu")
	}

	stranger := characterToResponse(character, false, false)
	if _, ok := stranger.Links["delete"]; ok {
		t.Error("links has delete, want the absence of a link to be the answer")
	}
	if _, ok := stranger.Links["role-menu"]; !ok {
		t.Error("links has no role-menu, want a guildmate able to read the menu")
	}
}

func TestLookupCharacterOmitsAnUnknownID(t *testing.T) {
	byID := characterSummaries([]roster.Character{{ID: uuid.New(), Name: "Thrall"}})

	if got := lookupCharacter(byID, uuid.New()); got != nil {
		t.Errorf("summary = %v, want nil rather than an invented name", got)
	}
}
