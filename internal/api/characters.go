package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

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

// characterToResponse renders one character. The two flags gate different links,
// because the two writes have different owners: a role menu is the raider's own
// ephemeral select and stays self-only even for a raid lead, while deleting a
// mistyped registration is roster hygiene a raid lead is expected to do. Offering a
// link that would 403 is worse than omitting it; the absence is the answer.
func characterToResponse(c roster.Character, owned, isRaidLead bool) characterResponse {
	href := "/api/characters/" + c.ID.String()

	links := Links{}
	links.add(true, "self", href, "")
	links.add(true, "role-menu", href+"/roles", "")
	links.add(owned, "roles", href+"/roles", "PUT")
	links.add(owned || isRaidLead, "edit", href, "PATCH")
	links.add(owned || isRaidLead, "delete", href, "DELETE")

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

// characterSummary is the part of a character needed to render it in someone else's
// resource: a signup row, a comp slot. Enough to draw the line without a second
// request, and no links of its own, since the full character resource carries those.
type characterSummary struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Realm  string   `json:"realm"`
	Class  *string  `json:"class,omitempty"`
	Spec   *string  `json:"spec,omitempty"`
	Ilvl   *float64 `json:"ilvl,omitempty"`
	IsMain bool     `json:"is_main"`
}

// characterSummaries indexes a guild roster by character id. Signup lists and comp
// boards both carry bare character ids, and every client rendering one needs names:
// one roster read here beats the same join repeated in the bot and the dashboard.
func characterSummaries(list []roster.Character) map[uuid.UUID]characterSummary {
	out := make(map[uuid.UUID]characterSummary, len(list))
	for _, c := range list {
		out[c.ID] = characterSummary{
			ID: c.ID.String(), Name: c.Name, Realm: c.Realm,
			Class: c.Class, Spec: c.Spec, Ilvl: c.Ilvl, IsMain: c.IsMain,
		}
	}
	return out
}

// lookupCharacter returns the summary for id, or nil when the roster read did not
// include it. A signup whose character was deleted mid-request is the honest case;
// omitting the field beats inventing a placeholder name.
func lookupCharacter(byID map[uuid.UUID]characterSummary, id uuid.UUID) *characterSummary {
	summary, ok := byID[id]
	if !ok {
		return nil
	}
	return &summary
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

		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
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

		// Registered for the actor themselves, so they can always edit its roles.
		writeJSON(w, logger, http.StatusCreated, characterToResponse(character, true, actor.IsRaidLead))
	}
}

// listGuildCharactersHandler returns every character registered in a guild.
func listGuildCharactersHandler(characters *roster.Characters, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		list, err := characters.ListForGuild(r.Context(), guildID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing characters", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		// A guild roster is everyone's characters, so the roles link has to be decided
		// per row rather than per response.
		mine, err := characters.ListForUser(r.Context(), int64(actor.DiscordID), guildID) //nolint:gosec
		if err != nil {
			logger.ErrorContext(r.Context(), "listing actor characters", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}
		owned := make(map[uuid.UUID]bool, len(mine))
		for _, c := range mine {
			owned[c.ID] = true
		}

		out := make([]characterResponse, len(list))
		for i, c := range list {
			out[i] = characterToResponse(c, owned[c.ID], actor.IsRaidLead)
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

		// Every row belongs to {did}, so one comparison covers the whole list.
		self := discordID == int64(actor.DiscordID) //nolint:gosec

		out := make([]characterResponse, len(list))
		for i, c := range list {
			out[i] = characterToResponse(c, self, actor.IsRaidLead)
		}
		writeJSON(w, logger, http.StatusOK, out)
	}
}

// requireCharacterInGuild resolves a {cid} path segment to a character in the actor's
// guild. Reading a guildmate's character is allowed: their name and role menu are
// already visible on every signup list and comp board in the guild.
func requireCharacterInGuild(w http.ResponseWriter, r *http.Request, characters *roster.Characters, logger *slog.Logger) (uuid.UUID, bool) {
	actor, _ := actorFromContext(r.Context())

	characterID, err := pathUUID(r, "cid")
	if err != nil {
		writeError(w, logger, http.StatusBadRequest, err.Error())
		return uuid.Nil, false
	}

	inGuild, err := characters.InGuild(r.Context(), characterID, int64(actor.GuildID)) //nolint:gosec
	if err != nil {
		logger.ErrorContext(r.Context(), "checking character guild", "error", err)
		writeError(w, logger, http.StatusInternalServerError, "internal error")
		return uuid.Nil, false
	}
	if !inGuild {
		writeError(w, logger, http.StatusNotFound, "character not found")
		return uuid.Nil, false
	}

	return characterID, true
}

// requireWritableCharacter is requireCharacterInGuild plus "and you may write to it":
// the owner, or a raid lead clearing up their own guild's roster. owned reports which
// of the two it was, because it is also the answer for the roles link: editing a role
// menu stays self-only even for a raid lead (design.md section 4, the raider's own
// ephemeral select).
func requireWritableCharacter(w http.ResponseWriter, r *http.Request, characters *roster.Characters, logger *slog.Logger) (characterID uuid.UUID, owned, ok bool) {
	actor, _ := actorFromContext(r.Context())

	characterID, ok = requireCharacterInGuild(w, r, characters, logger)
	if !ok {
		return uuid.Nil, false, false
	}

	owned, err := characters.OwnedByDiscord(r.Context(), characterID, int64(actor.GuildID), int64(actor.DiscordID)) //nolint:gosec
	if err != nil {
		logger.ErrorContext(r.Context(), "checking character ownership", "error", err)
		writeError(w, logger, http.StatusInternalServerError, "internal error")
		return uuid.Nil, false, false
	}
	if !owned && !actor.IsRaidLead {
		writeError(w, logger, http.StatusForbidden, "not your character")
		return uuid.Nil, false, false
	}

	return characterID, owned, true
}

type roleChoiceRequest struct {
	Role     string `json:"role"`
	Priority int16  `json:"priority"`
}

type roleChoiceResponse struct {
	Role     string `json:"role"`
	Priority int16  `json:"priority"`
}

// getCharacterRolesHandler returns a character's current role menu. The bot needs
// this to render the role select with the raider's existing picks already ticked:
// the PUT replaces the whole menu, so a select that opens blank silently drops
// every role the raider chose last time.
func getCharacterRolesHandler(characters *roster.Characters, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		characterID, ok := requireCharacterInGuild(w, r, characters, logger)
		if !ok {
			return
		}

		roles, err := characters.ListRoles(r.Context(), characterID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing character roles", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		out := make([]roleChoiceResponse, len(roles))
		for i, rc := range roles {
			out[i] = roleChoiceResponse{Role: string(rc.Role), Priority: rc.Priority}
		}
		writeJSON(w, logger, http.StatusOK, out)
	}
}

type patchCharacterRequest struct {
	IsMain *bool `json:"is_main,omitempty"`
}

// patchCharacterHandler edits a character. is_main is the only field: name, realm and
// region are the Raider.IO identity the sync job keys on, so correcting a typo there
// is a delete and a re-register rather than an edit that would silently repoint the
// character at a different armoury profile.
func patchCharacterHandler(characters *roster.Characters, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		characterID, owned, ok := requireWritableCharacter(w, r, characters, logger)
		if !ok {
			return
		}

		var body patchCharacterRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		if body.IsMain == nil {
			writeError(w, logger, http.StatusBadRequest, "is_main: required")
			return
		}

		actor, _ := actorFromContext(r.Context())
		if err := characters.SetMain(r.Context(), characterID, int64(actor.GuildID), *body.IsMain); err != nil { //nolint:gosec
			if errors.Is(err, roster.ErrCharacterNotFound) {
				writeError(w, logger, http.StatusNotFound, "character not found")
				return
			}
			logger.ErrorContext(r.Context(), "setting character main", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		character, err := characters.GetInGuild(r.Context(), characterID, int64(actor.GuildID)) //nolint:gosec
		if err != nil {
			logger.ErrorContext(r.Context(), "loading character", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusOK, characterToResponse(character, owned, actor.IsRaidLead))
	}
}

// deleteCharacterHandler removes a character and everything hanging off it. Every
// child table cascades, so this drops the raider's signups, comp slots and gear
// snapshots too. That is right for the case it exists for, a mistyped registration,
// and wrong for a raider leaving the guild, which is not a delete at all.
func deleteCharacterHandler(characters *roster.Characters, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		characterID, _, ok := requireWritableCharacter(w, r, characters, logger)
		if !ok {
			return
		}

		actor, _ := actorFromContext(r.Context())
		if err := characters.Delete(r.Context(), characterID, int64(actor.GuildID)); err != nil { //nolint:gosec
			if errors.Is(err, roster.ErrCharacterNotFound) {
				writeError(w, logger, http.StatusNotFound, "character not found")
				return
			}
			logger.ErrorContext(r.Context(), "deleting character", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
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
			role, err := parseRole(rc.Role)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, "roles["+strconv.Itoa(i)+"]."+err.Error())
				return
			}
			roles[i] = roster.RoleChoice{Role: role, Priority: rc.Priority}
		}

		if err := characters.SetRoles(r.Context(), characterID, roles); err != nil {
			logger.ErrorContext(r.Context(), "setting character roles", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
