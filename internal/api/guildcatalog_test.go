package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

// fakeCatalogStore satisfies the unexported interface signup.NewGuildCatalog takes.
type fakeCatalogStore struct {
	channels        []signup.Channel
	roles           []signup.Role
	writtenChannels *[]signup.Channel
	writtenRoles    *[]signup.Role
}

func (f *fakeCatalogStore) GuildChannels(context.Context, int64) ([]signup.Channel, error) {
	return f.channels, nil
}

func (f *fakeCatalogStore) ReplaceGuildChannels(_ context.Context, _ int64, channels []signup.Channel) error {
	f.writtenChannels = &channels
	return nil
}

func (f *fakeCatalogStore) GuildRoles(context.Context, int64) ([]signup.Role, error) {
	return f.roles, nil
}

func (f *fakeCatalogStore) ReplaceGuildRoles(_ context.Context, _ int64, roles []signup.Role) error {
	f.writtenRoles = &roles
	return nil
}

func catalogRequest(method, path string, actor Actor, body string) *http.Request {
	r := requestAs(method, path, actor, body)
	r.SetPathValue("gid", strconv.FormatInt(homeGuild, 10))
	return r
}

func TestGuildChannelsRequireAnAdminToRead(t *testing.T) {
	catalog := signup.NewGuildCatalog(&fakeCatalogStore{})

	w := httptest.NewRecorder()
	listGuildChannelsHandler(catalog, testLogger())(w,
		catalogRequest(http.MethodGet, "/api/guilds/100/discord-channels", homeActor(true), ""))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: a raid lead is not an admin", w.Code)
	}
}

func TestAnAdminListsGuildChannels(t *testing.T) {
	store := &fakeCatalogStore{
		channels: []signup.Channel{{DiscordChannelID: 555, Name: "general", Type: "TEXT"}},
	}
	catalog := signup.NewGuildCatalog(store)

	w := httptest.NewRecorder()
	listGuildChannelsHandler(catalog, testLogger())(w,
		catalogRequest(http.MethodGet, "/api/guilds/100/discord-channels", adminActor(), ""))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var got discordChannelsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(got.Channels) != 1 || got.Channels[0].ID != "555" || got.Channels[0].Type != "TEXT" {
		t.Errorf("channels = %+v, want the one channel back as strings", got.Channels)
	}
}

// The bot pushes the catalog as itself through requireServiceKey, so putGuildChannelsHandler
// never sees an actor. It has to work with none in context.
func TestBotPushesGuildChannelsWithoutAnActor(t *testing.T) {
	store := &fakeCatalogStore{}
	catalog := signup.NewGuildCatalog(store)

	r := httptest.NewRequest(http.MethodPut, "/api/guilds/100/discord-channels",
		strings.NewReader(`{"channels":[{"id":"555","name":"general","type":"TEXT"}]}`))
	r.SetPathValue("gid", "100")

	w := httptest.NewRecorder()
	putGuildChannelsHandler(catalog, testLogger())(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if store.writtenChannels == nil || len(*store.writtenChannels) != 1 {
		t.Fatalf("written = %+v, want one channel stored", store.writtenChannels)
	}
	got := (*store.writtenChannels)[0]
	if got.DiscordChannelID != 555 || got.Name != "general" || got.Type != "TEXT" {
		t.Errorf("written channel = %+v, want id 555, name general, type TEXT", got)
	}
}

func TestAnUnknownChannelTypeIsRejected(t *testing.T) {
	store := &fakeCatalogStore{}
	catalog := signup.NewGuildCatalog(store)

	r := httptest.NewRequest(http.MethodPut, "/api/guilds/100/discord-channels",
		strings.NewReader(`{"channels":[{"id":"555","name":"general","type":"NONSENSE"}]}`))
	r.SetPathValue("gid", "100")

	w := httptest.NewRecorder()
	putGuildChannelsHandler(catalog, testLogger())(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if store.writtenChannels != nil {
		t.Error("channels were written, want the request refused")
	}
}

func TestGuildRolesRequireAnAdminToRead(t *testing.T) {
	catalog := signup.NewGuildCatalog(&fakeCatalogStore{})

	w := httptest.NewRecorder()
	listGuildRolesHandler(catalog, testLogger())(w,
		catalogRequest(http.MethodGet, "/api/guilds/100/discord-roles", homeActor(true), ""))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: a raid lead is not an admin", w.Code)
	}
}

func TestBotPushesGuildRolesWithoutAnActor(t *testing.T) {
	store := &fakeCatalogStore{}
	catalog := signup.NewGuildCatalog(store)

	r := httptest.NewRequest(http.MethodPut, "/api/guilds/100/discord-roles",
		strings.NewReader(`{"roles":[{"id":"781","name":"Raid Lead","color":15158332,"position":5}]}`))
	r.SetPathValue("gid", "100")

	w := httptest.NewRecorder()
	putGuildRolesHandler(catalog, testLogger())(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if store.writtenRoles == nil || len(*store.writtenRoles) != 1 {
		t.Fatalf("written = %+v, want one role stored", store.writtenRoles)
	}
	got := (*store.writtenRoles)[0]
	if got.DiscordRoleID != 781 || got.Name != "Raid Lead" || got.Position != 5 {
		t.Errorf("written role = %+v, want id 781, name Raid Lead, position 5", got)
	}
}
