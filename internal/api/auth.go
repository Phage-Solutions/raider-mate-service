package api

import (
	"context"
	"crypto/subtle"
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
