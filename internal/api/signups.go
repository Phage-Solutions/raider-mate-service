package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
	"github.com/Phage-Solutions/raider-mate-service/internal/roster"
	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

type signupResponse struct {
	ID          string `json:"id"`
	CharacterID string `json:"character_id"`
	// Character is populated on list responses, where a client is rendering many rows
	// at once and would otherwise fetch the roster itself to turn ids into names. The
	// single-signup writes leave it nil: the caller just named the character.
	Character    *characterSummary `json:"character,omitempty"`
	Status       string            `json:"status"`
	AssignedRole *string           `json:"assigned_role,omitempty"`
	LateUntil    *time.Time        `json:"late_until,omitempty"`
	Note         *string           `json:"note,omitempty"`
	Links        Links             `json:"_links"`
}

// signupLinks is the HATEOAS decision for one signup: self and withdraw are visible
// to the character's owner or a raid lead, and to nobody else. The absence of a
// link is the authorization answer for anyone just looking at the character's own
// event, not acting on someone else's.
func signupLinks(eventID, characterID string, canAct bool) Links {
	href := "/api/events/" + eventID + "/signups/" + characterID
	links := Links{}
	links.add(canAct, "self", href, "PUT")
	links.add(canAct, "withdraw", href, "DELETE")
	return links
}

func signupToResponse(s signup.Signup, canAct bool) signupResponse {
	var assignedRole *string
	if s.AssignedRole != nil {
		r := string(*s.AssignedRole)
		assignedRole = &r
	}

	return signupResponse{
		ID:           s.ID.String(),
		CharacterID:  s.CharacterID.String(),
		Status:       string(s.Status),
		AssignedRole: assignedRole,
		LateUntil:    s.LateUntil,
		Note:         s.Note,
		Links:        signupLinks(s.EventID.String(), s.CharacterID.String(), canAct),
	}
}

type putSignupRequest struct {
	Status    string     `json:"status"`
	Note      *string    `json:"note,omitempty"`
	LateUntil *time.Time `json:"late_until,omitempty"`
}

// putSignupHandler writes a signup, self or raid lead. A player who has missed
// signup_deadline never sees a bare error here: Signups.Write returns
// ErrSignupsClosed, and this handler files a late_signup_requests row with the same
// status instead, so the response the bot renders is a request the raid lead can
// act on rather than a dead end.
func putSignupHandler(signups *signup.Signups, lateRequests *signup.LateRequests, characters *roster.Characters, events eventLookup, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		eventID, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		characterID, err := pathUUID(r, "cid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		// Before the ownership check, not inside it: the raid-lead branch below skips
		// ownership, so scoping here is what stops one guild's lead writing another's.
		if _, ok := requireEventInGuild(w, r, events, logger, eventID); !ok {
			return
		}

		// Two different questions. A raid lead may act on anyone's character, but only
		// one of their own guild's: without this, a foreign character id would be
		// written onto their event.
		inGuild, err := characters.InGuild(r.Context(), characterID, int64(actor.GuildID)) //nolint:gosec
		if err != nil {
			logger.ErrorContext(r.Context(), "checking character guild", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}
		if !inGuild {
			writeError(w, logger, http.StatusNotFound, "character not found")
			return
		}

		if !actor.IsRaidLead {
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
		}

		var body putSignupRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		status, err := parseSignupStatus(body.Status)
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		written, err := signups.Write(r.Context(), signup.SignupWrite{
			EventID: eventID, CharacterID: characterID, Status: status, Note: body.Note, LateUntil: body.LateUntil,
		}, actor.IsRaidLead)

		switch {
		case errors.Is(err, signup.ErrSignupsClosed):
			fileLateRequest(w, r, lateRequests, logger, eventID, characterID, status, body.Note, body.LateUntil, actor.IsRaidLead)
		case errors.Is(err, signup.ErrStatusRequiresRaidLead):
			writeError(w, logger, http.StatusForbidden, "status requires raid lead")
		case err != nil:
			logger.ErrorContext(r.Context(), "writing signup", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
		default:
			writeJSON(w, logger, http.StatusOK, signupToResponse(written, true))
		}
	}
}

// deleteSignupHandler withdraws a signup, self or raid lead. Past the deadline this
// closes for players the same way a new signup would: the withdrawal becomes a
// late request carrying DECLINED, not a silent delete.
func deleteSignupHandler(signups *signup.Signups, lateRequests *signup.LateRequests, characters *roster.Characters, events eventLookup, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		eventID, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		characterID, err := pathUUID(r, "cid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		if _, ok := requireEventInGuild(w, r, events, logger, eventID); !ok {
			return
		}

		// Two different questions. A raid lead may act on anyone's character, but only
		// one of their own guild's: without this, a foreign character id would be
		// written onto their event.
		inGuild, err := characters.InGuild(r.Context(), characterID, int64(actor.GuildID)) //nolint:gosec
		if err != nil {
			logger.ErrorContext(r.Context(), "checking character guild", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}
		if !inGuild {
			writeError(w, logger, http.StatusNotFound, "character not found")
			return
		}

		if !actor.IsRaidLead {
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
		}

		err = signups.Withdraw(r.Context(), eventID, characterID, actor.IsRaidLead)
		switch {
		case errors.Is(err, signup.ErrSignupsClosed):
			fileLateRequest(w, r, lateRequests, logger, eventID, characterID, db.SignupStatusDECLINED, nil, nil, actor.IsRaidLead)
		case err != nil:
			logger.ErrorContext(r.Context(), "withdrawing signup", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// fileLateRequest renders the request with the caller's real capabilities. A player
// who just tripped the deadline is not a raid lead, so they must not be handed
// approve/reject links they would get a 403 from: the absence of a link is the
// authorization answer (hard rule 7).
func fileLateRequest(w http.ResponseWriter, r *http.Request, lateRequests *signup.LateRequests, logger *slog.Logger, eventID, characterID uuid.UUID, status db.SignupStatus, note *string, lateUntil *time.Time, isRaidLead bool) {
	req, err := lateRequests.File(r.Context(), signup.LateRequestWrite{
		EventID: eventID, CharacterID: characterID, Status: status, Note: note, LateUntil: lateUntil,
	})
	if err != nil {
		logger.ErrorContext(r.Context(), "filing late request", "error", err)
		writeError(w, logger, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, logger, http.StatusAccepted, lateRequestToResponse(req, isRaidLead))
}

// listSignupsHandler returns every signup for an event. self/withdraw links appear
// on a raid lead's own view of every row, and on a player's view of only the rows
// for characters they own: the same authorization the write endpoints enforce,
// reflected back as what the caller could actually do next.
func listSignupsHandler(signups *signup.Signups, characters *roster.Characters, events eventLookup, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		eventID, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		if _, ok := requireEventInGuild(w, r, events, logger, eventID); !ok {
			return
		}

		list, err := signups.List(r.Context(), eventID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing signups", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		owned := map[uuid.UUID]bool{}
		if !actor.IsRaidLead {
			mine, err := characters.ListForUser(r.Context(), int64(actor.DiscordID), int64(actor.GuildID)) //nolint:gosec
			if err != nil {
				logger.ErrorContext(r.Context(), "listing actor characters", "error", err)
				writeError(w, logger, http.StatusInternalServerError, "internal error")
				return
			}
			for _, c := range mine {
				owned[c.ID] = true
			}
		}

		roster, err := characters.ListForGuild(r.Context(), int64(actor.GuildID)) //nolint:gosec
		if err != nil {
			logger.ErrorContext(r.Context(), "listing guild characters", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}
		byID := characterSummaries(roster)

		out := make([]signupResponse, len(list))
		for i, s := range list {
			out[i] = signupToResponse(s, actor.IsRaidLead || owned[s.CharacterID])
			out[i].Character = lookupCharacter(byID, s.CharacterID)
		}
		writeJSON(w, logger, http.StatusOK, out)
	}
}
