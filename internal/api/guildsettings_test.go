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

// fakeSettingsStore satisfies the unexported interface signup.NewSettings takes.
type fakeSettingsStore struct {
	stored  signup.GuildSettings
	written *signup.GuildSettings
}

func (f *fakeSettingsStore) GuildSettings(context.Context, int64) (signup.GuildSettings, error) {
	return f.stored, nil
}

func (f *fakeSettingsStore) UpsertGuildSettings(_ context.Context, s signup.GuildSettings) (signup.GuildSettings, error) {
	f.written = &s
	return s, nil
}

func settingsRequest(method string, actor Actor, body string) *http.Request {
	r := requestAs(method, "/api/guilds/100/settings", actor, body)
	r.SetPathValue("gid", strconv.FormatInt(homeGuild, 10))
	return r
}

func adminActor() Actor {
	actor := homeActor(true)
	actor.IsGuildAdmin = true
	return actor
}

func TestGuildSettingsAreReadableByAnyMember(t *testing.T) {
	channelID := int64(555)
	settings := signup.NewSettings(&fakeSettingsStore{
		stored: signup.GuildSettings{DiscordGuildID: 100, EventsChannelID: &channelID},
	})

	w := httptest.NewRecorder()
	getGuildSettingsHandler(settings, testLogger())(w, settingsRequest(http.MethodGet, homeActor(false), ""))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var got guildSettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.EventsChannelID == nil || *got.EventsChannelID != "555" {
		t.Errorf("events_channel_id = %v, want the snowflake as a string", got.EventsChannelID)
	}
	// A non-admin may see where events go, but not change it.
	if _, ok := got.Links["replace"]; ok {
		t.Error("links has replace for a non-admin, want the absence of a link to be the answer")
	}
}

func TestWritingGuildSettingsNeedsAnAdmin(t *testing.T) {
	store := &fakeSettingsStore{}
	settings := signup.NewSettings(store)

	w := httptest.NewRecorder()
	putGuildSettingsHandler(settings, testLogger())(w,
		settingsRequest(http.MethodPut, homeActor(true), `{"events_channel_id":"555"}`))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: a raid lead is not an admin", w.Code)
	}
	if store.written != nil {
		t.Error("settings were written, want the request refused before persistence")
	}
}

func TestAnAdminSetsTheEventsChannel(t *testing.T) {
	store := &fakeSettingsStore{}
	settings := signup.NewSettings(store)

	w := httptest.NewRecorder()
	putGuildSettingsHandler(settings, testLogger())(w,
		settingsRequest(http.MethodPut, adminActor(), `{"events_channel_id":"555"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if store.written == nil || store.written.EventsChannelID == nil || *store.written.EventsChannelID != 555 {
		t.Fatalf("written = %+v, want the channel stored", store.written)
	}
}

// Omitting the field clears it, returning the guild to posting wherever the create
// command was run. That is a whole-row write behaving as documented, not a bug.
func TestOmittingTheChannelClearsIt(t *testing.T) {
	channelID := int64(555)
	store := &fakeSettingsStore{stored: signup.GuildSettings{DiscordGuildID: 100, EventsChannelID: &channelID}}
	settings := signup.NewSettings(store)

	w := httptest.NewRecorder()
	putGuildSettingsHandler(settings, testLogger())(w, settingsRequest(http.MethodPut, adminActor(), `{}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if store.written == nil || store.written.EventsChannelID != nil {
		t.Errorf("written = %+v, want the channel cleared", store.written)
	}
}

// Zero is never a real snowflake, and storing it would point every event message at a
// channel that cannot exist.
func TestAZeroChannelIsRejected(t *testing.T) {
	store := &fakeSettingsStore{}
	settings := signup.NewSettings(store)

	w := httptest.NewRecorder()
	putGuildSettingsHandler(settings, testLogger())(w,
		settingsRequest(http.MethodPut, adminActor(), `{"events_channel_id":"0"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if store.written != nil {
		t.Error("settings were written, want the request refused")
	}
}

// An unknown zone name has to be refused here. Stored, it would only be found out much
// later by the bot, while parsing a raid time a raid lead had already typed.
func TestAnUnknownTimezoneIsRejected(t *testing.T) {
	store := &fakeSettingsStore{}
	settings := signup.NewSettings(store)

	w := httptest.NewRecorder()
	putGuildSettingsHandler(settings, testLogger())(w,
		settingsRequest(http.MethodPut, adminActor(), `{"timezone":"Europe/Nowhere"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if store.written != nil {
		t.Error("settings were written, want the request refused")
	}
}

func TestAnIANATimezoneIsStored(t *testing.T) {
	store := &fakeSettingsStore{}
	settings := signup.NewSettings(store)

	w := httptest.NewRecorder()
	putGuildSettingsHandler(settings, testLogger())(w,
		settingsRequest(http.MethodPut, adminActor(), `{"timezone":"Europe/Bratislava"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if store.written == nil || store.written.Timezone == nil || *store.written.Timezone != "Europe/Bratislava" {
		t.Fatalf("written = %+v, want the zone stored", store.written)
	}
}

func TestEventMentionRolesRoundTrip(t *testing.T) {
	store := &fakeSettingsStore{}
	settings := signup.NewSettings(store)

	w := httptest.NewRecorder()
	putGuildSettingsHandler(settings, testLogger())(w,
		settingsRequest(http.MethodPut, adminActor(), `{"event_mention_role_ids":["781","799"]}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if store.written == nil || len(store.written.EventMentionRoleIDs) != 2 {
		t.Fatalf("written = %+v, want two role ids", store.written)
	}

	var got guildSettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(got.EventMentionRoleIDs) != 2 || got.EventMentionRoleIDs[0] != "781" {
		t.Errorf("role ids = %v, want them back as strings", got.EventMentionRoleIDs)
	}
}

// "Ping nobody" is a choice a guild makes, so it has to serialise as [] rather than
// null: a client cannot render a setting it has to guess at.
func TestNoMentionRolesSerialisesAsAnEmptyArray(t *testing.T) {
	settings := signup.NewSettings(&fakeSettingsStore{})

	w := httptest.NewRecorder()
	getGuildSettingsHandler(settings, testLogger())(w, settingsRequest(http.MethodGet, homeActor(false), ""))

	if !strings.Contains(w.Body.String(), `"event_mention_role_ids":[]`) {
		t.Errorf("body = %s, want an empty array", w.Body.String())
	}
}

// Discord renders only https images, so anything else would be stored and then produce
// a card with no artwork, which is a failure nobody thinks to look for.
func TestABannerMustBeAnHTTPSURL(t *testing.T) {
	for _, raw := range []string{"http://example.com/a.png", "ftp://example.com/a.png", "not a url", "/local/a.png"} {
		store := &fakeSettingsStore{}
		w := httptest.NewRecorder()
		putGuildSettingsHandler(signup.NewSettings(store), testLogger())(w,
			settingsRequest(http.MethodPut, adminActor(), `{"event_banner_url":"`+raw+`"}`))

		if w.Code != http.StatusBadRequest {
			t.Errorf("%q gave status %d, want 400", raw, w.Code)
		}
		if store.written != nil {
			t.Errorf("%q was stored, want it refused", raw)
		}
	}
}

func TestAnHTTPSBannerIsStored(t *testing.T) {
	store := &fakeSettingsStore{}
	w := httptest.NewRecorder()
	putGuildSettingsHandler(signup.NewSettings(store), testLogger())(w,
		settingsRequest(http.MethodPut, adminActor(), `{"event_banner_url":"https://example.com/nerubar.png"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if store.written == nil || store.written.EventBannerURL == nil {
		t.Fatalf("written = %+v, want the banner stored", store.written)
	}
}
