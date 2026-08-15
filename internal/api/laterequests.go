package api

import (
	"log/slog"
	"net/http"

	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

type lateRequestResponse struct {
	ID          string  `json:"id"`
	CharacterID string  `json:"character_id"`
	Status      string  `json:"status"`
	Note        *string `json:"note,omitempty"`
	State       string  `json:"state"`
	Links       Links   `json:"_links"`
}

func lateRequestToResponse(req signup.LateRequest, isRaidLead bool) lateRequestResponse {
	href := "/api/events/" + req.EventID.String() + "/late-requests/" + req.ID.String()
	pending := req.State == "PENDING"

	links := Links{}
	links.add(true, "self", href, "")
	links.add(isRaidLead && pending, "approve", href+"/approve", "POST")
	links.add(isRaidLead && pending, "reject", href+"/reject", "POST")

	return lateRequestResponse{
		ID:          req.ID.String(),
		CharacterID: req.CharacterID.String(),
		Status:      string(req.Status),
		Note:        req.Note,
		State:       string(req.State),
		Links:       links,
	}
}

// listLateRequestsHandler returns every late request for an event, most recent
// first. Raid lead only: this is their queue to work through.
func listLateRequestsHandler(lateRequests *signup.LateRequests, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsRaidLead {
			writeError(w, logger, http.StatusForbidden, "raid lead required")
			return
		}

		eventID, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		list, err := lateRequests.List(r.Context(), eventID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing late requests", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		out := make([]lateRequestResponse, len(list))
		for i, req := range list {
			out[i] = lateRequestToResponse(req, true)
		}
		writeJSON(w, logger, http.StatusOK, out)
	}
}

// approveLateRequestHandler upserts the signup with the requested status and marks
// the request decided. Raid lead only.
func approveLateRequestHandler(lateRequests *signup.LateRequests, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsRaidLead {
			writeError(w, logger, http.StatusForbidden, "raid lead required")
			return
		}

		id, err := pathUUID(r, "rid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		if err := lateRequests.Approve(r.Context(), id); err != nil {
			logger.ErrorContext(r.Context(), "approving late request", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// rejectLateRequestHandler marks the request decided without touching the signup.
// Raid lead only.
func rejectLateRequestHandler(lateRequests *signup.LateRequests, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsRaidLead {
			writeError(w, logger, http.StatusForbidden, "raid lead required")
			return
		}

		id, err := pathUUID(r, "rid")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		if err := lateRequests.Reject(r.Context(), id); err != nil {
			logger.ErrorContext(r.Context(), "rejecting late request", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
