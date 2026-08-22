package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/signup"
)

const (
	homeGuild    = int64(100)
	foreignGuild = int64(200)
)

// fakeEvents returns one event, in whichever guild the test says.
type fakeEvents struct {
	guildID int64
	err     error
	calls   int
}

func (f *fakeEvents) Get(_ context.Context, id uuid.UUID) (signup.Event, error) {
	f.calls++
	if f.err != nil {
		return signup.Event{}, f.err
	}
	return signup.Event{ID: id, DiscordGuildID: f.guildID}, nil
}

// requestAs builds a request carrying a resolved actor, as the auth middleware would.
func requestAs(method, target string, actor Actor, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	return r.WithContext(withActor(context.Background(), actor))
}

func homeActor(raidLead bool) Actor {
	return Actor{DiscordID: 1, GuildID: uint64(homeGuild), IsRaidLead: raidLead} //nolint:gosec
}

func TestRequireEventInGuildAcceptsTheActorsOwnGuild(t *testing.T) {
	events := &fakeEvents{guildID: homeGuild}
	w := httptest.NewRecorder()
	r := requestAs(http.MethodGet, "/api/events/x", homeActor(false), "")

	event, ok := requireEventInGuild(w, r, events, testLogger(), uuid.New())

	if !ok {
		t.Fatalf("ok = false, want the actor's own guild accepted")
	}
	if event.DiscordGuildID != homeGuild {
		t.Errorf("guild = %d, want %d", event.DiscordGuildID, homeGuild)
	}
}

func TestRequireEventInGuildHidesAForeignGuildsEvent(t *testing.T) {
	events := &fakeEvents{guildID: foreignGuild}
	w := httptest.NewRecorder()
	r := requestAs(http.MethodGet, "/api/events/x", homeActor(true), "")

	if _, ok := requireEventInGuild(w, r, events, testLogger(), uuid.New()); ok {
		t.Fatalf("ok = true, want a foreign guild's event refused even for a raid lead")
	}
	// 404 rather than 403: 403 would confirm the id names a real event.
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 so the event's existence is not confirmed", w.Code)
	}
}

func TestRequireEventInGuildHidesALoadFailure(t *testing.T) {
	events := &fakeEvents{err: errors.New("no rows")}
	w := httptest.NewRecorder()
	r := requestAs(http.MethodGet, "/api/events/x", homeActor(false), "")

	if _, ok := requireEventInGuild(w, r, events, testLogger(), uuid.New()); ok {
		t.Fatalf("ok = true, want a failed load refused")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestEventScopedHandlersRefuseAForeignGuild drives each event-scoped handler with a
// foreign guild's event and nil domain dependencies. The nils are the point: if a
// handler reached its store before scoping, it would panic instead of returning 404.
func TestEventScopedHandlersRefuseAForeignGuild(t *testing.T) {
	eventID := uuid.New()
	characterID := uuid.New()
	requestID := uuid.New()
	logger := testLogger()

	cases := []struct {
		name    string
		method  string
		pattern string
		target  string
		body    string
		actor   Actor
		handler func(events eventLookup) http.HandlerFunc
	}{
		{
			name: "list signups", method: http.MethodGet,
			pattern: "GET /api/events/{id}/signups",
			target:  "/api/events/" + eventID.String() + "/signups",
			actor:   homeActor(false),
			handler: func(e eventLookup) http.HandlerFunc { return listSignupsHandler(nil, nil, e, logger) },
		},
		{
			name: "put signup", method: http.MethodPut,
			pattern: "PUT /api/events/{id}/signups/{cid}",
			target:  "/api/events/" + eventID.String() + "/signups/" + characterID.String(),
			body:    `{"status":"CONFIRMED"}`,
			actor:   homeActor(true),
			handler: func(e eventLookup) http.HandlerFunc { return putSignupHandler(nil, nil, nil, e, logger) },
		},
		{
			name: "delete signup", method: http.MethodDelete,
			pattern: "DELETE /api/events/{id}/signups/{cid}",
			target:  "/api/events/" + eventID.String() + "/signups/" + characterID.String(),
			actor:   homeActor(true),
			handler: func(e eventLookup) http.HandlerFunc { return deleteSignupHandler(nil, nil, nil, e, logger) },
		},
		{
			name: "list late requests", method: http.MethodGet,
			pattern: "GET /api/events/{id}/late-requests",
			target:  "/api/events/" + eventID.String() + "/late-requests",
			actor:   homeActor(true),
			handler: func(e eventLookup) http.HandlerFunc { return listLateRequestsHandler(nil, e, logger) },
		},
		{
			name: "approve late request", method: http.MethodPost,
			pattern: "POST /api/events/{id}/late-requests/{rid}/approve",
			target:  "/api/events/" + eventID.String() + "/late-requests/" + requestID.String() + "/approve",
			actor:   homeActor(true),
			handler: func(e eventLookup) http.HandlerFunc { return approveLateRequestHandler(nil, e, logger) },
		},
		{
			name: "reject late request", method: http.MethodPost,
			pattern: "POST /api/events/{id}/late-requests/{rid}/reject",
			target:  "/api/events/" + eventID.String() + "/late-requests/" + requestID.String() + "/reject",
			actor:   homeActor(true),
			handler: func(e eventLookup) http.HandlerFunc { return rejectLateRequestHandler(nil, e, logger) },
		},
		{
			name: "list comps", method: http.MethodGet,
			pattern: "GET /api/events/{id}/comps",
			target:  "/api/events/" + eventID.String() + "/comps",
			actor:   homeActor(false),
			handler: func(e eventLookup) http.HandlerFunc { return listCompsHandler(nil, e, logger) },
		},
		{
			name: "get comp", method: http.MethodGet,
			pattern: "GET /api/events/{id}/comps/{name}",
			target:  "/api/events/" + eventID.String() + "/comps/prog",
			actor:   homeActor(false),
			handler: func(e eventLookup) http.HandlerFunc { return getCompHandler(nil, nil, e, logger) },
		},
		{
			name: "lock comp", method: http.MethodPost,
			pattern: "POST /api/events/{id}/comps/{name}/lock",
			target:  "/api/events/" + eventID.String() + "/comps/prog/lock",
			actor:   homeActor(true),
			handler: func(e eventLookup) http.HandlerFunc { return lockCompHandler(nil, nil, e, logger) },
		},
		{
			name: "save comp", method: http.MethodPut,
			pattern: "PUT /api/events/{id}/comps/{name}",
			target:  "/api/events/" + eventID.String() + "/comps/prog",
			body:    `{"slots":[]}`,
			actor:   homeActor(true),
			handler: func(e eventLookup) http.HandlerFunc { return saveCompHandler(nil, nil, e, logger) },
		},
		{
			name: "set comp mode", method: http.MethodPut,
			pattern: "PUT /api/events/{id}/comps/{name}/mode",
			target:  "/api/events/" + eventID.String() + "/comps/prog/mode",
			body:    `{"mode":"MANUAL"}`,
			actor:   homeActor(true),
			handler: func(e eventLookup) http.HandlerFunc { return setCompModeHandler(nil, e, logger) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := &fakeEvents{guildID: foreignGuild}

			// A ServeMux so r.PathValue resolves the same way it does in production.
			mux := http.NewServeMux()
			mux.HandleFunc(tc.pattern, tc.handler(events))

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, requestAs(tc.method, tc.target, tc.actor, tc.body))

			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404 for another guild's event", w.Code)
			}
			if events.calls != 1 {
				t.Errorf("event lookups = %d, want exactly 1", events.calls)
			}
		})
	}
}

func TestCompWriteHandlersRequireARaidLead(t *testing.T) {
	eventID := uuid.New()
	logger := testLogger()

	cases := []struct {
		name    string
		method  string
		pattern string
		target  string
		body    string
		handler func(events eventLookup) http.HandlerFunc
	}{
		{
			"save comp", http.MethodPut,
			"PUT /api/events/{id}/comps/{name}",
			"/api/events/" + eventID.String() + "/comps/prog",
			`{"slots":[]}`,
			func(e eventLookup) http.HandlerFunc { return saveCompHandler(nil, nil, e, logger) },
		},
		{
			"set comp mode", http.MethodPut,
			"PUT /api/events/{id}/comps/{name}/mode",
			"/api/events/" + eventID.String() + "/comps/prog/mode",
			`{"mode":"MANUAL"}`,
			func(e eventLookup) http.HandlerFunc { return setCompModeHandler(nil, e, logger) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Own guild, so only the raid-lead check can refuse this.
			events := &fakeEvents{guildID: homeGuild}

			mux := http.NewServeMux()
			mux.HandleFunc(tc.pattern, tc.handler(events))

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, requestAs(tc.method, tc.target, homeActor(false), tc.body))

			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403 for a plain raider", w.Code)
			}
			if events.calls != 0 {
				t.Errorf("event lookups = %d, want 0: refuse before loading anything", events.calls)
			}
		})
	}
}
