package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// raidLeadRoleLister is the one piece of persistence auth needs: a guild's mapped
// raid-lead role IDs, to resolve Actor.IsRaidLead.
type raidLeadRoleLister interface {
	RaidLeadRoleIDs(ctx context.Context, discordGuildID int64) ([]int64, error)
}

// requireAuth checks the shared client key, parses the actor headers, resolves
// IsRaidLead against guild_raid_lead_roles, and stores the result on the request
// context for handlers to read via actorFromContext. One raid-lead lookup per
// request; no cache until something proves it is needed.
func requireAuth(next http.Handler, apiKey string, roles raidLeadRoleLister, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validAPIKey(r.Header.Get("Authorization"), apiKey) {
			writeError(w, logger, http.StatusUnauthorized, "invalid or missing API key")
			return
		}

		actor, err := actorFromHeaders(r.Header)
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		// Snowflakes stay uint64 on Actor since they are Discord's identifiers, not
		// this service's; the DB layer stores them signed (bigint), so the guild id
		// crosses that boundary here.
		raidLeadRoleIDs, err := roles.RaidLeadRoleIDs(r.Context(), int64(actor.GuildID)) //nolint:gosec
		if err != nil {
			logger.ErrorContext(r.Context(), "loading raid lead roles", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}
		actor.IsRaidLead = resolveIsRaidLead(actor, int64sToUint64s(raidLeadRoleIDs))

		next.ServeHTTP(w, r.WithContext(withActor(r.Context(), actor)))
	})
}

// requireGuildlessAuth is requireAuth for the one route that is asked before a guild
// has been chosen: GET /api/users/{did}/guilds, which exists precisely to find out
// which guild to work in. requireAuth cannot serve it, because it insists on a real
// guild snowflake and 0 is not one, so every caller of that route was answered 400
// before its handler ever ran.
//
// The actor it puts on the context carries the Discord ID and nothing else: no guild,
// no roles, and IsRaidLead false. That is not a downgrade to work around, it is the
// truth at this point in the flow, and it is why nothing behind this middleware may
// read anything guild-scoped. The handler's own check that {did} is the caller is the
// whole authorization story here.
func requireGuildlessAuth(next http.Handler, apiKey string, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validAPIKey(r.Header.Get("Authorization"), apiKey) {
			writeError(w, logger, http.StatusUnauthorized, "invalid or missing API key")
			return
		}

		discordID, err := parseSnowflake(r.Header.Get("X-Actor-Discord-Id"))
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, fmt.Sprintf("X-Actor-Discord-Id: %v", err))
			return
		}

		next.ServeHTTP(w, r.WithContext(withActor(r.Context(), Actor{DiscordID: discordID})))
	})
}

// requireServiceKey checks the shared client key and nothing else. It guards the
// routes the bot process calls on its own behalf rather than on a raider's, where
// there is no interaction to take actor headers from and no single guild to scope to.
func requireServiceKey(next http.Handler, apiKey string, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validAPIKey(r.Header.Get("Authorization"), apiKey) {
			writeError(w, logger, http.StatusUnauthorized, "invalid or missing API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validAPIKey(header, want string) bool {
	got, ok := strings.CutPrefix(header, bearerPrefix)
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func int64sToUint64s(ids []int64) []uint64 {
	out := make([]uint64, len(ids))
	for i, id := range ids {
		out[i] = uint64(id) //nolint:gosec
	}
	return out
}
