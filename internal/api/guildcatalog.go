package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

type discordChannelDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type discordChannelsResponse struct {
	Channels []discordChannelDTO `json:"channels"`
	Links    Links               `json:"_links"`
}

type putDiscordChannelsRequest struct {
	Channels []discordChannelDTO `json:"channels"`
}

// discordChannelsLinks gates on admin. There is no PUT to offer here even to an
// admin: the write side is the bot pushing as itself through requireServiceKey, not
// a transition this caller can reach, so the absence of a replace link is the answer
// (rule 7).
func discordChannelsLinks(guildID int64, isAdmin bool) Links {
	href := "/api/guilds/" + strconv.FormatInt(guildID, 10) + "/discord-channels"
	links := Links{}
	links.add(isAdmin, "self", href, "")
	return links
}

func channelsToDTO(channels []signup.Channel) []discordChannelDTO {
	out := make([]discordChannelDTO, len(channels))
	for i, c := range channels {
		out[i] = discordChannelDTO{ID: strconv.FormatInt(c.DiscordChannelID, 10), Name: c.Name, Type: string(c.Type)}
	}
	return out
}

// listGuildChannelsHandler returns a guild's Discord channel catalog, as the bot last
// pushed it. Admin only: this exists to back the dashboard's settings picker, and a
// raid lead is not necessarily an admin.
func listGuildChannelsHandler(catalog *signup.GuildCatalog, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsGuildAdmin {
			writeError(w, logger, http.StatusForbidden, "guild admin required")
			return
		}

		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		channels, err := catalog.Channels(r.Context(), guildID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing guild channels", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusOK, discordChannelsResponse{
			Channels: channelsToDTO(channels),
			Links:    discordChannelsLinks(guildID, true),
		})
	}
}

// putGuildChannelsHandler replaces a guild's whole channel catalog. This is the bot
// reporting its own view of the guild rather than a raider's request, so it sits
// behind requireServiceKey with no actor to scope against: the service cannot ask
// Discord itself (hard rule 6), so the bot decides what is worth pushing and this
// service only stores it.
func putGuildChannelsHandler(catalog *signup.GuildCatalog, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		guildID, err := pathSnowflake(r, "gid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		var body putDiscordChannelsRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		channels := make([]signup.Channel, len(body.Channels))
		for i, c := range body.Channels {
			id, err := strconv.ParseInt(c.ID, 10, 64)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, "channels["+strconv.Itoa(i)+"].id: "+err.Error())
				return
			}
			channelType, err := parseChannelType(c.Type)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, "channels["+strconv.Itoa(i)+"].type: "+err.Error())
				return
			}
			channels[i] = signup.Channel{DiscordChannelID: id, Name: c.Name, Type: channelType}
		}

		if err := catalog.ReplaceChannels(r.Context(), guildID, channels); err != nil {
			logger.ErrorContext(r.Context(), "replacing guild channels", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

type discordRoleDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    int32  `json:"color"`
	Position int32  `json:"position"`
}

type discordRolesResponse struct {
	Roles []discordRoleDTO `json:"roles"`
	Links Links            `json:"_links"`
}

type putDiscordRolesRequest struct {
	Roles []discordRoleDTO `json:"roles"`
}

func discordRolesLinks(guildID int64, isAdmin bool) Links {
	href := "/api/guilds/" + strconv.FormatInt(guildID, 10) + "/discord-roles"
	links := Links{}
	links.add(isAdmin, "self", href, "")
	return links
}

func rolesToDTO(roles []signup.Role) []discordRoleDTO {
	out := make([]discordRoleDTO, len(roles))
	for i, r := range roles {
		out[i] = discordRoleDTO{
			ID: strconv.FormatInt(r.DiscordRoleID, 10), Name: r.Name, Color: r.Color, Position: r.Position,
		}
	}
	return out
}

// listGuildRolesHandler returns a guild's Discord role catalog, as the bot last
// pushed it. Admin only, same reasoning as listGuildChannelsHandler.
func listGuildRolesHandler(catalog *signup.GuildCatalog, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsGuildAdmin {
			writeError(w, logger, http.StatusForbidden, "guild admin required")
			return
		}

		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		roles, err := catalog.Roles(r.Context(), guildID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing guild roles", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusOK, discordRolesResponse{
			Roles: rolesToDTO(roles),
			Links: discordRolesLinks(guildID, true),
		})
	}
}

// putGuildRolesHandler replaces a guild's whole role catalog. Same shape and
// reasoning as putGuildChannelsHandler.
func putGuildRolesHandler(catalog *signup.GuildCatalog, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		guildID, err := pathSnowflake(r, "gid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		var body putDiscordRolesRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		roles := make([]signup.Role, len(body.Roles))
		for i, role := range body.Roles {
			id, err := strconv.ParseInt(role.ID, 10, 64)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, "roles["+strconv.Itoa(i)+"].id: "+err.Error())
				return
			}
			roles[i] = signup.Role{DiscordRoleID: id, Name: role.Name, Color: role.Color, Position: role.Position}
		}

		if err := catalog.ReplaceRoles(r.Context(), guildID, roles); err != nil {
			logger.ErrorContext(r.Context(), "replacing guild roles", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
