package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func capabilitiesFor(t *testing.T, actor Actor) guildCapabilitiesResponse {
	t.Helper()

	w := httptest.NewRecorder()
	r := requestAs(http.MethodGet, "/api/guilds/100/capabilities", actor, "")
	r.SetPathValue("gid", "100")

	getGuildCapabilitiesHandler(testLogger()).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got guildCapabilitiesResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	return got
}

func TestCapabilitiesOffersConfigureToRaidLeadsAndAdmins(t *testing.T) {
	lead := homeActor(true)
	if _, ok := capabilitiesFor(t, lead).Links["configure"]; !ok {
		t.Error("want a configure link for a raid lead")
	}

	admin := Actor{DiscordID: 1, GuildID: uint64(homeGuild), IsGuildAdmin: true} //nolint:gosec
	if _, ok := capabilitiesFor(t, admin).Links["configure"]; !ok {
		t.Error("want a configure link for a Discord admin, which keeps the bootstrap open")
	}
}

func TestCapabilitiesOffersNothingToAPlainRaider(t *testing.T) {
	got := capabilitiesFor(t, homeActor(false))

	if _, ok := got.Links["configure"]; ok {
		t.Errorf("links = %v, want no configure link for a raider", got.Links)
	}
	if got.IsRaidLead || got.IsGuildAdmin {
		t.Errorf("capabilities = %+v, want both false", got)
	}
}

func TestCapabilitiesOffersCreateEventToRaidLeadsOnly(t *testing.T) {
	if _, ok := capabilitiesFor(t, homeActor(true)).Links["create-event"]; !ok {
		t.Error("want a create-event link for a raid lead")
	}

	// The rule POST /api/guilds/{gid}/events enforces: the admin flag does not make
	// someone a raid lead, so a client must not be told it can offer the control.
	admin := Actor{DiscordID: 1, GuildID: uint64(homeGuild), IsGuildAdmin: true} //nolint:gosec
	if _, ok := capabilitiesFor(t, admin).Links["create-event"]; ok {
		t.Error("want no create-event link for an admin who holds no raid lead role")
	}

	if _, ok := capabilitiesFor(t, homeActor(false)).Links["create-event"]; ok {
		t.Error("want no create-event link for a raider")
	}
}
