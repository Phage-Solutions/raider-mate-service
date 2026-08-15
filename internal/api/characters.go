package api

import (
	"log/slog"
	"net/http"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
	"github.com/Phage-Solutions/raider-mate-service/internal/roster"
)

type characterResponse struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Realm      string   `json:"realm"`
	Region     string   `json:"region"`
	Class      *string  `json:"class,omitempty"`
	Spec       *string  `json:"spec,omitempty"`
	Ilvl       *float64 `json:"ilvl,omitempty"`
	MplusScore *float64 `json:"mplus_score,omitempty"`
	IsMain     bool     `json:"is_main"`
	Synced     bool     `json:"synced"`
	Links      Links    `json:"_links"`
}

func characterToResponse(c roster.Character) characterResponse {
	links := Links{}
	links.add(true, "self", "/api/characters/"+c.ID.String(), "")
	links.add(true, "roles", "/api/characters/"+c.ID.String()+"/roles", "PUT")

	return characterResponse{
		ID:         c.ID.String(),
		Name:       c.Name,
		Realm:      c.Realm,
		Region:     c.Region,
		Class:      c.Class,
		Spec:       c.Spec,
		Ilvl:       c.Ilvl,
		MplusScore: c.MplusScore,
		IsMain:     c.IsMain,
		Synced:     c.Synced,
		Links:      links,
	}
}

type createCharacterRequest struct {
	Name   string `json:"name"`
	Realm  string `json:"realm"`
	Region string `json:"region"`
	IsMain bool   `json:"is_main"`
}

// createCharacterHandler registers a character for the calling actor. A new
// character has no ilvl until the next worker sync tick: hard rule 5 forbids
// calling Raider.IO from this handler, so Character.Synced tells the bot the
// truth rather than showing a stale placeholder.
func createCharacterHandler(characters *roster.Characters, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		guildID, err := pathSnowflake(r, "gid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		if guildID != int64(actor.GuildID) { //nolint:gosec
			writeError(w, logger, http.StatusForbidden, "guild mismatch")
			return
		}

		var body createCharacterRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		character, err := characters.Register(r.Context(), roster.RegisterInput{
			DiscordID:      int64(actor.DiscordID), //nolint:gosec
			DiscordGuildID: guildID,
			Name:           body.Name,
			Realm:          body.Realm,
			Region:         body.Region,
			IsMain:         body.IsMain,
		})
		if err != nil {
			logger.ErrorContext(r.Context(), "registering character", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusCreated, characterToResponse(character))
	}
}

// listGuildCharactersHandler returns every character registered in a guild.
func listGuildCharactersHandler(characters *roster.Characters, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		guildID, err := pathSnowflake(r, "gid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		if guildID != int64(actor.GuildID) { //nolint:gosec
			writeError(w, logger, http.StatusForbidden, "guild mismatch")
			return
		}

		list, err := characters.ListForGuild(r.Context(), guildID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing characters", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		out := make([]characterResponse, len(list))
		for i, c := range list {
			out[i] = characterToResponse(c)
		}
		writeJSON(w, logger, http.StatusOK, out)
	}
}

// listUserCharactersHandler returns one Discord user's characters within the
// actor's guild. The guild comes from the actor's headers, not the path: a bot
// process serves one guild's request at a time, and {did} alone cannot say which
// guild's roster to search.
func listUserCharactersHandler(characters *roster.Characters, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		discordID, err := pathSnowflake(r, "did")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		list, err := characters.ListForUser(r.Context(), discordID, int64(actor.GuildID)) //nolint:gosec
		if err != nil {
			logger.ErrorContext(r.Context(), "listing characters", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		out := make([]characterResponse, len(list))
		for i, c := range list {
			out[i] = characterToResponse(c)
		}
		writeJSON(w, logger, http.StatusOK, out)
	}
}

type roleChoiceRequest struct {
	Role     string `json:"role"`
	Priority int16  `json:"priority"`
}

type putCharacterRolesRequest struct {
	Roles []roleChoiceRequest `json:"roles"`
}

// putCharacterRolesHandler replaces a character's whole role menu. Self only: this
// is the ephemeral role select from design.md section 4, entirely separate from the
// signup write it precedes (hard rule 2).
func putCharacterRolesHandler(characters *roster.Characters, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		characterID, err := pathUUID(r, "cid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		owned, err := characters.OwnedByDiscord(r.Context(), characterID, int64(actor.GuildID), int64(actor.DiscordID)) //nolint:gosec
		if err != nil {
			logger.ErrorContext(r.Context(), "checking character ownership", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}
		if !owned {
			writeError(w, logger, http.StatusForbidden, "not your character")
			return
		}

		var body putCharacterRolesRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		roles := make([]roster.RoleChoice, len(body.Roles))
		for i, rc := range body.Roles {
			roles[i] = roster.RoleChoice{Role: db.RoleEnum(rc.Role), Priority: rc.Priority}
		}

		if err := characters.SetRoles(r.Context(), characterID, roles); err != nil {
			logger.ErrorContext(r.Context(), "setting character roles", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
