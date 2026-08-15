package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

type notificationResponse struct {
	ID         string   `json:"id"`
	EventID    string   `json:"event_id"`
	Kind       string   `json:"kind"`
	TargetKind string   `json:"target_kind"`
	DiscordID  *string  `json:"discord_id,omitempty"`
	RoleIDs    []string `json:"role_ids,omitempty"`
	ChannelID  *string  `json:"channel_id,omitempty"`
	Payload    rawJSON  `json:"payload"`
	Links      Links    `json:"_links"`
}

// rawJSON marshals its bytes verbatim: the payload column is already JSON.
type rawJSON []byte

func (r rawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

func notificationToResponse(n signup.StoredNotification) notificationResponse {
	roleIDs := make([]string, len(n.RoleIDs))
	for i, id := range n.RoleIDs {
		roleIDs[i] = strconv.FormatInt(id, 10)
	}

	links := Links{}
	links.add(true, "delivered", "/api/notifications/"+n.ID.String()+"/delivered", "POST")

	return notificationResponse{
		ID:         n.ID.String(),
		EventID:    n.EventID.String(),
		Kind:       string(n.Kind),
		TargetKind: string(n.TargetKind),
		DiscordID:  snowflakePtrToString(n.DiscordID),
		RoleIDs:    roleIDs,
		ChannelID:  snowflakePtrToString(n.ChannelID),
		Payload:    n.Payload,
		Links:      links,
	}
}

const (
	defaultNotificationLimit = 50
	maxNotificationLimit     = 200
)

// listNotificationsHandler claims and returns undelivered notifications for the
// actor's guild.
//
// The guild is taken from the actor, never from the query string. Every Discord
// interaction carries a guild, so the bot always knows which one it is polling for,
// and a caller-supplied filter would mean any authenticated raider could omit it and
// read every guild's outbox: recipient ids, channel ids and DM payloads.
func listNotificationsHandler(outbox *signup.Outbox, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		guildID := int64(actor.GuildID) //nolint:gosec

		limit := int32(defaultNotificationLimit)
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 32)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, "limit: "+err.Error())
				return
			}
			// Unclamped, ?limit=-1 reaches Postgres as a negative LIMIT and 500s, and
			// ?limit=0 quietly returns nothing forever.
			if n < 1 || n > maxNotificationLimit {
				writeError(w, logger, http.StatusBadRequest, "limit: must be between 1 and 200")
				return
			}
			limit = int32(n)
		}

		list, err := outbox.Claim(r.Context(), &guildID, limit)
		if err != nil {
			logger.ErrorContext(r.Context(), "claiming notifications", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		out := make([]notificationResponse, len(list))
		for i, n := range list {
			out[i] = notificationToResponse(n)
		}
		writeJSON(w, logger, http.StatusOK, out)
	}
}

// markNotificationDeliveredHandler acks one notification, scoped to the actor's
// guild: acking by id alone would let any caller suppress another guild's reminders.
func markNotificationDeliveredHandler(outbox *signup.Outbox, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		id, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		err = outbox.MarkDelivered(r.Context(), id, int64(actor.GuildID)) //nolint:gosec
		switch {
		case errors.Is(err, signup.ErrNotificationNotFound):
			writeError(w, logger, http.StatusNotFound, "notification not found")
		case err != nil:
			logger.ErrorContext(r.Context(), "marking notification delivered", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
