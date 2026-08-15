package api

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phage-Solutions/raider-mate-service/internal/comp"
	"github.com/Phage-Solutions/raider-mate-service/internal/roster"
	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

// NewRouter builds the HTTP handler tree for the service. Wiring is by hand: each
// domain package's Store is constructed once and handed to the use cases that need
// it, the same shape cmd/worker uses for roster.Syncer and signup.Runner.
func NewRouter(pool *pgxpool.Pool, apiKey string, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler(pool, logger))

	rosterStore := roster.NewStore(pool)
	characters := roster.NewCharacters(rosterStore)

	compStore := comp.NewStore(pool)
	locker := comp.NewLocker(compStore)
	reader := comp.NewReader(compStore)
	manual := comp.NewManual(compStore)

	signupStore := signup.NewStore(pool)
	events := signup.NewEvents(signupStore)
	signups := signup.NewSignups(signupStore)
	lateRequests := signup.NewLateRequests(signupStore, logger)
	raidLeads := signup.NewRaidLeads(signupStore)
	outbox := signup.NewOutbox(signupStore)

	apiMux := http.NewServeMux()

	apiMux.HandleFunc("GET /api/guilds/{gid}/raid-lead-roles", listRaidLeadRolesHandler(raidLeads, logger))
	apiMux.HandleFunc("PUT /api/guilds/{gid}/raid-lead-roles", putRaidLeadRolesHandler(raidLeads, logger))

	apiMux.HandleFunc("POST /api/guilds/{gid}/characters", createCharacterHandler(characters, logger))
	apiMux.HandleFunc("GET /api/guilds/{gid}/characters", listGuildCharactersHandler(characters, logger))
	apiMux.HandleFunc("GET /api/users/{did}/characters", listUserCharactersHandler(characters, logger))
	apiMux.HandleFunc("PATCH /api/characters/{cid}", patchCharacterHandler(characters, logger))
	apiMux.HandleFunc("DELETE /api/characters/{cid}", deleteCharacterHandler(characters, logger))
	apiMux.HandleFunc("GET /api/characters/{cid}/roles", getCharacterRolesHandler(characters, logger))
	apiMux.HandleFunc("PUT /api/characters/{cid}/roles", putCharacterRolesHandler(characters, logger))

	apiMux.HandleFunc("POST /api/guilds/{gid}/events", createEventHandler(events, logger))
	apiMux.HandleFunc("GET /api/guilds/{gid}/events", listGuildEventsHandler(events, logger))
	apiMux.HandleFunc("GET /api/events/{id}", getEventHandler(events, logger))
	apiMux.HandleFunc("PATCH /api/events/{id}", patchEventHandler(events, logger))
	apiMux.HandleFunc("DELETE /api/events/{id}", deleteEventHandler(events, logger))

	apiMux.HandleFunc("PUT /api/events/{id}/signups/{cid}", putSignupHandler(signups, lateRequests, characters, events, logger))
	apiMux.HandleFunc("DELETE /api/events/{id}/signups/{cid}", deleteSignupHandler(signups, lateRequests, characters, events, logger))
	apiMux.HandleFunc("GET /api/events/{id}/signups", listSignupsHandler(signups, characters, events, logger))

	apiMux.HandleFunc("GET /api/events/{id}/late-requests", listLateRequestsHandler(lateRequests, events, logger))
	apiMux.HandleFunc("POST /api/events/{id}/late-requests/{rid}/approve", approveLateRequestHandler(lateRequests, events, logger))
	apiMux.HandleFunc("POST /api/events/{id}/late-requests/{rid}/reject", rejectLateRequestHandler(lateRequests, events, logger))

	apiMux.HandleFunc("GET /api/events/{id}/comps", listCompsHandler(reader, events, logger))
	apiMux.HandleFunc("GET /api/events/{id}/comps/{name}", getCompHandler(reader, characters, events, logger))
	apiMux.HandleFunc("PUT /api/events/{id}/comps/{name}", saveCompHandler(manual, characters, events, logger))
	apiMux.HandleFunc("PUT /api/events/{id}/comps/{name}/mode", setCompModeHandler(manual, events, logger))
	apiMux.HandleFunc("POST /api/events/{id}/comps/{name}/lock", lockCompHandler(locker, characters, events, logger))

	apiMux.HandleFunc("GET /api/notifications", listNotificationsHandler(outbox, logger))
	apiMux.HandleFunc("POST /api/notifications/{id}/delivered", markNotificationDeliveredHandler(outbox, logger))

	mux.Handle("/api/", requireAuth(apiMux, apiKey, signupStore, logger))

	return recoverPanic(logRequests(mux, logger), logger)
}
