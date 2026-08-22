package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/signup"
)

// eventLookup is enough to answer "which guild owns this event". Declared here by the
// consumer; signup.Events already satisfies it.
type eventLookup interface {
	Get(ctx context.Context, id uuid.UUID) (signup.Event, error)
}

// requireEventInGuild loads the event and confirms it belongs to the actor's guild,
// writing the response and returning false when it does not.
//
// Actor.GuildID is the only thing binding a request to a tenant, so every handler
// keyed on a bare event UUID has to pass through here: without it, knowing an event's
// id is enough to read or write another guild's data. A foreign guild's event reads
// as 404 rather than 403, so its existence is not confirmed either way.
func requireEventInGuild(w http.ResponseWriter, r *http.Request, events eventLookup, logger *slog.Logger, eventID uuid.UUID) (signup.Event, bool) {
	actor, _ := actorFromContext(r.Context())

	// A load failure is 404 for the same reason: distinguishing "no such event" from
	// "the database is down" would leak which ids exist.
	event, err := events.Get(r.Context(), eventID)
	if err != nil {
		writeError(w, logger, http.StatusNotFound, "event not found")
		return signup.Event{}, false
	}
	if event.DiscordGuildID != int64(actor.GuildID) { //nolint:gosec
		writeError(w, logger, http.StatusNotFound, "event not found")
		return signup.Event{}, false
	}

	return event, true
}

// requireGuildPath confirms a {gid} path segment matches the actor's guild. Unlike an
// event id, the caller named the guild here, so a mismatch is a plain 403: there is
// nothing to conceal about a guild whose id they already hold.
func requireGuildPath(w http.ResponseWriter, r *http.Request, logger *slog.Logger, param string) (int64, bool) {
	actor, _ := actorFromContext(r.Context())

	guildID, err := pathSnowflake(r, param)
	if err != nil {
		writeError(w, logger, http.StatusBadRequest, err.Error())
		return 0, false
	}
	if guildID != int64(actor.GuildID) { //nolint:gosec
		writeError(w, logger, http.StatusForbidden, "guild mismatch")
		return 0, false
	}

	return guildID, true
}
