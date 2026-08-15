package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

type eventResponse struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	Title          string          `json:"title"`
	StartsAt       time.Time       `json:"starts_at"`
	SignupDeadline time.Time       `json:"signup_deadline"`
	CompTemplate   json.RawMessage `json:"comp_template"`
	MessageID      *string         `json:"message_id,omitempty"`
	ChannelID      *string         `json:"channel_id,omitempty"`
	Difficulty     *string         `json:"difficulty,omitempty"`
	Links          Links           `json:"_links"`
}

func eventToResponse(e signup.Event, actor Actor) eventResponse {
	href := "/api/events/" + e.ID.String()

	links := Links{}
	links.add(true, "self", href, "")
	links.add(true, "signups", href+"/signups", "")
	links.add(true, "comps", href+"/comps", "")
	links.add(actor.IsRaidLead, "edit", href, "PATCH")
	links.add(actor.IsRaidLead, "delete", href, "DELETE")
	links.add(actor.IsRaidLead, "late-requests", href+"/late-requests", "")

	return eventResponse{
		ID:             e.ID.String(),
		Type:           string(e.Type),
		Title:          e.Title,
		StartsAt:       e.StartsAt,
		SignupDeadline: e.SignupDeadline,
		CompTemplate:   e.CompTemplate,
		MessageID:      snowflakePtrToString(e.MessageID),
		ChannelID:      snowflakePtrToString(e.ChannelID),
		Difficulty:     raidDifficultyToString(e.Difficulty),
		Links:          links,
	}
}

func snowflakePtrToString(id *int64) *string {
	if id == nil {
		return nil
	}
	s := strconv.FormatInt(*id, 10)
	return &s
}

func stringToSnowflakePtr(s *string) (*int64, error) {
	if s == nil {
		return nil, nil
	}
	id, err := strconv.ParseInt(*s, 10, 64)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func raidDifficultyToString(d *db.RaidDifficulty) *string {
	if d == nil {
		return nil
	}
	s := string(*d)
	return &s
}

type createEventRequest struct {
	Type           string          `json:"type"`
	Title          string          `json:"title"`
	StartsAt       time.Time       `json:"starts_at"`
	SignupDeadline time.Time       `json:"signup_deadline"`
	CompTemplate   json.RawMessage `json:"comp_template"`
	Difficulty     *string         `json:"difficulty,omitempty"`
}

// createEventHandler creates an event and, in the same store transaction, schedules
// its reminder/deadline jobs (schedule.go's jobsFor). Raid lead only.
func createEventHandler(events *signup.Events, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsRaidLead {
			writeError(w, logger, http.StatusForbidden, "raid lead required")
			return
		}

		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		var body createEventRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}
		compTemplate := body.CompTemplate
		if len(compTemplate) == 0 {
			compTemplate = []byte("{}")
		}

		eventType, err := parseEventType(body.Type)
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		var difficulty *db.RaidDifficulty
		if body.Difficulty != nil {
			d, err := parseRaidDifficulty(*body.Difficulty)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, err.Error())
				return
			}
			difficulty = &d
		}

		event, err := events.Create(r.Context(), signup.CreateEventInput{
			DiscordGuildID: guildID,
			Type:           eventType,
			Title:          body.Title,
			StartsAt:       body.StartsAt,
			SignupDeadline: body.SignupDeadline,
			CompTemplate:   compTemplate,
			Difficulty:     difficulty,
		})
		if err != nil {
			logger.ErrorContext(r.Context(), "creating event", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusCreated, eventToResponse(event, actor))
	}
}

// listGuildEventsHandler returns a guild's upcoming events.
func listGuildEventsHandler(events *signup.Events, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		list, err := events.ListUpcoming(r.Context(), guildID)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing events", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		out := make([]eventResponse, len(list))
		for i, e := range list {
			out[i] = eventToResponse(e, actor)
		}
		writeJSON(w, logger, http.StatusOK, out)
	}
}

// getEventHandler loads one event, scoped to the actor's guild: a foreign guild's
// event id reads as 404, not 403, so its existence is not confirmed either way.
func getEventHandler(events *signup.Events, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		id, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		event, ok := requireEventInGuild(w, r, events, logger, id)
		if !ok {
			return
		}

		writeJSON(w, logger, http.StatusOK, eventToResponse(event, actor))
	}
}

type updateEventRequest struct {
	Title          *string         `json:"title,omitempty"`
	StartsAt       *time.Time      `json:"starts_at,omitempty"`
	SignupDeadline *time.Time      `json:"signup_deadline,omitempty"`
	CompTemplate   json.RawMessage `json:"comp_template,omitempty"`
	Difficulty     *string         `json:"difficulty,omitempty"`
	MessageID      *string         `json:"message_id,omitempty"`
	ChannelID      *string         `json:"channel_id,omitempty"`
}

// patchEventHandler applies a partial edit. Raid lead only. Rescheduling on a
// starts_at/signup_deadline change happens inside Events.Update's store
// transaction; message_id/channel_id (the bot learning its own post) never touches
// scheduled_jobs.
func patchEventHandler(events *signup.Events, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsRaidLead {
			writeError(w, logger, http.StatusForbidden, "raid lead required")
			return
		}

		id, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		if _, ok := requireEventInGuild(w, r, events, logger, id); !ok {
			return
		}

		var body updateEventRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		messageID, err := stringToSnowflakePtr(body.MessageID)
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, "message_id: "+err.Error())
			return
		}
		channelID, err := stringToSnowflakePtr(body.ChannelID)
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, "channel_id: "+err.Error())
			return
		}
		var difficulty *db.RaidDifficulty
		if body.Difficulty != nil {
			d := db.RaidDifficulty(*body.Difficulty)
			difficulty = &d
		}

		event, err := events.Update(r.Context(), signup.UpdateEventInput{
			ID:             id,
			Title:          body.Title,
			StartsAt:       body.StartsAt,
			SignupDeadline: body.SignupDeadline,
			CompTemplate:   body.CompTemplate,
			Difficulty:     difficulty,
			MessageID:      messageID,
			ChannelID:      channelID,
		})
		if err != nil {
			logger.ErrorContext(r.Context(), "updating event", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusOK, eventToResponse(event, actor))
	}
}

// deleteEventHandler removes an event. Raid lead only. Signups, scheduled_jobs,
// comps, and notifications all carry ON DELETE CASCADE.
func deleteEventHandler(events *signup.Events, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.IsRaidLead {
			writeError(w, logger, http.StatusForbidden, "raid lead required")
			return
		}

		id, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		if _, ok := requireEventInGuild(w, r, events, logger, id); !ok {
			return
		}

		if err := events.Delete(r.Context(), id); err != nil {
			logger.ErrorContext(r.Context(), "deleting event", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
