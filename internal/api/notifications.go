package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

type notificationResponse struct {
	ID string `json:"id"`
	// DiscordGuildID is here because one claim now spans every guild: without it the
	// bot cannot tell which guild's session to deliver through, or log which guild a
	// failed delivery belonged to.
	DiscordGuildID string   `json:"discord_guild_id"`
	EventID        string   `json:"event_id"`
	Kind           string   `json:"kind"`
	TargetKind     string   `json:"target_kind"`
	DiscordID      *string  `json:"discord_id,omitempty"`
	RoleIDs        []string `json:"role_ids,omitempty"`
	ChannelID      *string  `json:"channel_id,omitempty"`
	Payload        rawJSON  `json:"payload"`
	Links          Links    `json:"_links"`
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
		ID:             n.ID.String(),
		DiscordGuildID: strconv.FormatInt(n.DiscordGuildID, 10),
		EventID:        n.EventID.String(),
		Kind:           string(n.Kind),
		TargetKind:     string(n.TargetKind),
		DiscordID:      snowflakePtrToString(n.DiscordID),
		RoleIDs:        roleIDs,
		ChannelID:      snowflakePtrToString(n.ChannelID),
		Payload:        n.Payload,
		Links:          links,
	}
}

const (
	defaultNotificationLimit = 50
	maxNotificationLimit     = 200
)

// listNotificationsHandler claims and returns undelivered notifications across every
// guild.
//
// This route sits behind requireServiceKey, not requireAuth, so there is no actor and
// no guild to scope to. That is deliberate: the outbox has exactly one reader, the bot
// process, and a per-guild scope would cost it one request per guild per tick to poll
// a table it is entitled to read whole. No route reachable by a raider's interaction
// reaches this handler, so recipient ids, channel ids and DM payloads stay out of
// their hands.
func listNotificationsHandler(outbox *signup.Outbox, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		list, err := outbox.Claim(r.Context(), nil, limit)
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

// markNotificationDeliveredHandler acks one notification by id. The guild scope the
// ack used to carry existed to stop one guild's authenticated raider suppressing
// another's reminders; behind requireServiceKey there is no raider to stop, and the
// bot acks whatever it just claimed.
func markNotificationDeliveredHandler(outbox *signup.Outbox, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		err = outbox.MarkDelivered(r.Context(), id, nil)
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
