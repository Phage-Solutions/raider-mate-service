package api

import (
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

const defaultNotificationLimit = 50

// listNotificationsHandler returns undelivered notifications, optionally scoped to
// the actor's guild via ?guild_id=, with an optional ?limit= so one bot process
// serving many guilds can page.
func listNotificationsHandler(outbox *signup.Outbox, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var guildID *int64
		if raw := r.URL.Query().Get("guild_id"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, "guild_id: "+err.Error())
				return
			}
			guildID = &id
		}

		limit := int32(defaultNotificationLimit)
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 32)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, "limit: "+err.Error())
				return
			}
			limit = int32(n)
		}

		list, err := outbox.ListUndelivered(r.Context(), guildID, limit)
		if err != nil {
			logger.ErrorContext(r.Context(), "listing notifications", "error", err)
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

// markNotificationDeliveredHandler acks one notification.
func markNotificationDeliveredHandler(outbox *signup.Outbox, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathUUID(r, "id")
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		if err := outbox.MarkDelivered(r.Context(), id); err != nil {
			logger.ErrorContext(r.Context(), "marking notification delivered", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
