package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Raider-Mate/raider-mate-service/internal/roster"
)

func TestListUserGuildsRefusesToAnswerAboutSomebodyElse(t *testing.T) {
	store := &fakeCharacterStore{guilds: []int64{10, 20}}
	w := httptest.NewRecorder()
	// Actor 1 asking about user 2, and a raid lead at that.
	r := requestAs(http.MethodGet, "/api/users/2/guilds", homeActor(true), "")
	r.SetPathValue("did", "2")

	listUserGuildsHandler(roster.NewCharacters(store), testLogger()).ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: which guilds somebody belongs to is not visible anywhere else", w.Code)
	}
	if store.askedAbout != 0 {
		t.Errorf("store was queried about %d; the guard must refuse before reaching it", store.askedAbout)
	}
}

func TestListUserGuildsAnswersAboutYourself(t *testing.T) {
	store := &fakeCharacterStore{guilds: []int64{10, 20}}
	w := httptest.NewRecorder()
	r := requestAs(http.MethodGet, "/api/users/1/guilds", homeActor(false), "")
	r.SetPathValue("did", "1")

	listUserGuildsHandler(roster.NewCharacters(store), testLogger()).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var got []userGuildResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	// Snowflakes go out as strings, the same as every other id in this API.
	want := []string{"10", "20"}
	if len(got) != len(want) {
		t.Fatalf("guilds = %v, want %v", got, want)
	}
	for i, expected := range want {
		if got[i].DiscordGuildID != expected {
			t.Errorf("guilds[%d] = %q, want %q", i, got[i].DiscordGuildID, expected)
		}
	}
}
