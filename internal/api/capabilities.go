package api

import (
	"log/slog"
	"net/http"
	"strconv"
)

type guildCapabilitiesResponse struct {
	IsRaidLead   bool  `json:"is_raid_lead"`
	IsGuildAdmin bool  `json:"is_guild_admin"`
	Links        Links `json:"_links"`
}

// getGuildCapabilitiesHandler answers "what may I do in this guild".
//
// It exists so a client can decide what to put in front of someone without keeping its
// own copy of the rules. Raid-lead capability is resolved here, from the guild's mapped
// roles, and a dashboard that recomputed it from role ids would be a second
// implementation of the one rule that decides who can run a raid.
//
// Open to anyone in the guild: it reports only what the caller themselves may do, which
// they find out anyway the moment they try.
func getGuildCapabilitiesHandler(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		href := "/api/guilds/" + strconv.FormatInt(guildID, 10)
		links := Links{}
		links.add(true, "self", href+"/capabilities", "")
		// The transition a client hangs a Configuration entry off. Absent means there is
		// nothing there for this caller, so nothing should be offered.
		links.add(actor.MayConfigureGuild(), "configure", href+"/settings", "")
		// Separate from configure on purpose. Running a raid is the raid-lead roles'
		// job alone, so a guild admin who holds none of them gets the Configuration
		// entry and no way to create an event, which is exactly the rule POST enforces.
		links.add(actor.IsRaidLead, "create-event", href+"/events", "POST")

		writeJSON(w, logger, http.StatusOK, guildCapabilitiesResponse{
			IsRaidLead:   actor.IsRaidLead,
			IsGuildAdmin: actor.IsGuildAdmin,
			Links:        links,
		})
	}
}
