package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

type raidLeadRolesResponse struct {
	RoleIDs []string `json:"role_ids"`
	Links   Links    `json:"_links"`
}

type raidLeadRolesRequest struct {
	RoleIDs []string `json:"role_ids"`
}

func raidLeadRolesLinks(guildID int64, isAdmin bool) Links {
	href := "/api/guilds/" + strconv.FormatInt(guildID, 10) + "/raid-lead-roles"
	links := Links{}
	links.add(isAdmin, "self", href, "")
	links.add(isAdmin, "replace", href, "PUT")
	return links
}

// listRaidLeadRolesHandler returns a guild's mapped raid-lead role IDs. Admin only,
// since this is the policy that decides who else gets raid-lead actions.
func listRaidLeadRolesHandler(raidLeads *signup.RaidLeads, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsGuildAdmin {
			writeError(w, logger, http.StatusForbidden, "guild admin required")
			return
		}

		guildID, err := pathSnowflake(r, "gid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		if guildID != int64(actor.GuildID) { //nolint:gosec
			writeError(w, logger, http.StatusForbidden, "guild mismatch")
			return
		}

		roleIDs, err := raidLeads.List(r.Context(), guildID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing raid lead roles", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusOK, raidLeadRolesResponse{
			RoleIDs: snowflakesToStrings(roleIDs),
			Links:   raidLeadRolesLinks(guildID, true),
		})
	}
}

// putRaidLeadRolesHandler replaces a guild's whole raid-lead role mapping. Admin
// only. Bootstrap (empty mapping) is legal: it is what a fresh install starts from.
func putRaidLeadRolesHandler(raidLeads *signup.RaidLeads, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsGuildAdmin {
			writeError(w, logger, http.StatusForbidden, "guild admin required")
			return
		}

		guildID, err := pathSnowflake(r, "gid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		if guildID != int64(actor.GuildID) { //nolint:gosec
			writeError(w, logger, http.StatusForbidden, "guild mismatch")
			return
		}

		var body raidLeadRolesRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		roleIDs, err := stringsToSnowflakes(body.RoleIDs)
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		if err := raidLeads.Replace(r.Context(), guildID, roleIDs); err != nil {
			logger.ErrorContext(r.Context(), "replacing raid lead roles", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusOK, raidLeadRolesResponse{
			RoleIDs: body.RoleIDs,
			Links:   raidLeadRolesLinks(guildID, true),
		})
	}
}

func snowflakesToStrings(ids []int64) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatInt(id, 10)
	}
	return out
}

func stringsToSnowflakes(ids []string) ([]int64, error) {
	out := make([]int64, len(ids))
	for i, s := range ids {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}
