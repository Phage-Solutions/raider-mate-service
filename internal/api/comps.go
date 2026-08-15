package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Phage-Solutions/raider-mate-service/internal/comp"
)

type compInfoResponse struct {
	Name  string `json:"name"`
	Mode  string `json:"mode"`
	Links Links  `json:"_links"`
}

type assignmentResponse struct {
	CharacterID string `json:"character_id"`
	Role        string `json:"role"`
	SlotIndex   int16  `json:"slot_index"`
	IsBench     bool   `json:"is_bench"`
	Reason      string `json:"reason"`
}

type advisoryResponse struct {
	Role    string `json:"role,omitempty"`
	Message string `json:"message"`
}

type boardResponse struct {
	Name       string               `json:"name"`
	Mode       string               `json:"mode"`
	Slots      []assignmentResponse `json:"slots"`
	Advisories []advisoryResponse   `json:"advisories,omitempty"`
	Links      Links                `json:"_links"`
}

func compHref(eventID, name string) string {
	return "/api/events/" + eventID + "/comps/" + name
}

func compInfoLinks(eventID string, info comp.CompInfo, isRaidLead bool) Links {
	links := Links{}
	links.add(true, "self", compHref(eventID, info.Name), "")
	links.add(isRaidLead, "lock", compHref(eventID, info.Name)+"/lock", "POST")
	return links
}

func assignmentsToResponse(assignments []comp.Assignment) []assignmentResponse {
	out := make([]assignmentResponse, len(assignments))
	for i, a := range assignments {
		out[i] = assignmentResponse{
			CharacterID: a.CharacterID.String(), Role: string(a.Role),
			SlotIndex: a.SlotIndex, IsBench: a.IsBench, Reason: a.Reason,
		}
	}
	return out
}

func advisoriesToResponse(advisories []comp.Advisory) []advisoryResponse {
	if len(advisories) == 0 {
		return nil
	}
	out := make([]advisoryResponse, len(advisories))
	for i, a := range advisories {
		out[i] = advisoryResponse{Role: string(a.Role), Message: a.Message}
	}
	return out
}

// listCompsHandler returns every named comp for an event.
func listCompsHandler(reader *comp.Reader, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		eventID, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		list, err := reader.List(r.Context(), eventID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing comps", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		out := make([]compInfoResponse, len(list))
		for i, info := range list {
			out[i] = compInfoResponse{Name: info.Name, Mode: string(info.Mode), Links: compInfoLinks(eventID.String(), info, actor.IsRaidLead)}
		}
		writeJSON(w, logger, http.StatusOK, out)
	}
}

// getCompHandler returns one named comp's mode and slots.
func getCompHandler(reader *comp.Reader, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		eventID, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		name := r.PathValue("name")

		board, found, err := reader.Get(r.Context(), eventID, name)
		if err != nil {
			logger.ErrorContext(r.Context(), "loading comp", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}
		if !found {
			writeError(w, logger, http.StatusNotFound, "comp not found")
			return
		}

		links := Links{}
		links.add(true, "self", compHref(eventID.String(), name), "")
		links.add(actor.IsRaidLead, "lock", compHref(eventID.String(), name)+"/lock", "POST")

		writeJSON(w, logger, http.StatusOK, boardResponse{
			Name: board.Name, Mode: string(board.Mode), Slots: assignmentsToResponse(board.Slots), Links: links,
		})
	}
}

// lockCompHandler runs the assigner and persists the result. Raid lead only. A
// manual comp refuses the lock (ErrCompIsManual) rather than overwriting a
// hand-built board.
func lockCompHandler(locker *comp.Locker, logger *slog.Logger) http.HandlerFunc {
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
		name := r.PathValue("name")

		result, err := locker.Lock(r.Context(), eventID, name)
		if errors.Is(err, comp.ErrCompIsManual) {
			writeError(w, logger, http.StatusConflict, "comp is manual; convert it before locking")
			return
		}
		if err != nil {
			logger.ErrorContext(r.Context(), "locking comp", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		links := Links{}
		links.add(true, "self", compHref(eventID.String(), name), "")

		writeJSON(w, logger, http.StatusOK, boardResponse{
			Name: name, Mode: "AUTO", Slots: assignmentsToResponse(result.Assignments),
			Advisories: advisoriesToResponse(result.Advisories), Links: links,
		})
	}
}
