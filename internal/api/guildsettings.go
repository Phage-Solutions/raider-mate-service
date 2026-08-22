package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	// The zone database, compiled into the binary. parseTimezone below is the only
	// caller of time.LoadLocation, and without this it can only resolve a zone the
	// runtime image happens to ship. The one this service deploys on does not ship any,
	// so every guild that had set a timezone was refused its own stored value on the
	// next settings write, with "unknown zone" as the reason.
	//
	// Imported here rather than in cmd/api, so it travels with the code that needs it
	// and a second binary cannot lose it.
	_ "time/tzdata"

	"github.com/Raider-Mate/raider-mate-service/internal/db"
	"github.com/Raider-Mate/raider-mate-service/internal/signup"
)

type guildSettingsResponse struct {
	EventsChannelID *string `json:"events_channel_id,omitempty"`
	Timezone        *string `json:"timezone,omitempty"`
	// EventMentionRoleIDs is always present, empty included: a client rendering the
	// setting needs to tell "ping nobody" from "not configured", and both are states a
	// guild can deliberately be in.
	EventMentionRoleIDs []string `json:"event_mention_role_ids"`
	EventBannerURL      *string  `json:"event_banner_url,omitempty"`
	// ReminderLeadMinutes and ReminderDelivery are omitted when the guild has chosen
	// neither, which is not the same as choosing the defaults: a client showing the
	// setting needs to say "default" rather than claim the guild picked 30.
	ReminderLeadMinutes *int32  `json:"reminder_lead_minutes,omitempty"`
	ReminderDelivery    *string `json:"reminder_delivery,omitempty"`
	Links               Links   `json:"_links"`
}

type guildSettingsRequest struct {
	EventsChannelID     *string  `json:"events_channel_id,omitempty"`
	Timezone            *string  `json:"timezone,omitempty"`
	EventMentionRoleIDs []string `json:"event_mention_role_ids,omitempty"`
	EventBannerURL      *string  `json:"event_banner_url,omitempty"`
	ReminderLeadMinutes *int32   `json:"reminder_lead_minutes,omitempty"`
	ReminderDelivery    *string  `json:"reminder_delivery,omitempty"`
}

// guildSettingsLinks gates the write on admin and leaves the read open to the guild.
// Anyone can see where events get posted, since the answer is a channel they can
// already see; changing it is configuration.
func guildSettingsLinks(guildID int64, isAdmin bool) Links {
	href := "/api/guilds/" + strconv.FormatInt(guildID, 10) + "/settings"
	links := Links{}
	links.add(true, "self", href, "")
	links.add(isAdmin, "replace", href, "PUT")
	return links
}

func settingsToResponse(settings signup.GuildSettings, guildID int64, isAdmin bool) guildSettingsResponse {
	return guildSettingsResponse{
		EventsChannelID:     snowflakePtrToString(settings.EventsChannelID),
		Timezone:            settings.Timezone,
		EventMentionRoleIDs: snowflakesToStrings(settings.EventMentionRoleIDs),
		EventBannerURL:      settings.EventBannerURL,
		ReminderLeadMinutes: settings.ReminderLeadMinutes,
		ReminderDelivery:    reminderDeliveryToString(settings.ReminderDelivery),
		Links:               guildSettingsLinks(guildID, isAdmin),
	}
}

func reminderDeliveryToString(d *db.ReminderDelivery) *string {
	if d == nil {
		return nil
	}
	s := string(*d)
	return &s
}

// getGuildSettingsHandler returns a guild's bot configuration. Readable by any member:
// the bot loads this on every event creation to decide where to post, and a raid lead
// is not necessarily an admin.
func getGuildSettingsHandler(settings *signup.Settings, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())

		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		current, err := settings.Get(r.Context(), guildID)
		if err != nil {
			logger.ErrorContext(r.Context(), "reading guild settings", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusOK, settingsToResponse(current, guildID, actor.MayConfigureGuild()))
	}
}

// putGuildSettingsHandler replaces a guild's settings. Admin only, and a whole-row
// write: an omitted events_channel_id clears it, returning the guild to posting
// wherever the create command was run.
func putGuildSettingsHandler(settings *signup.Settings, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromContext(r.Context())
		if !actor.MayConfigureGuild() {
			writeError(w, logger, http.StatusForbidden, "guild admin or raid lead required")
			return
		}

		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		var body guildSettingsRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		var channelID *int64
		if body.EventsChannelID != nil {
			parsed, err := strconv.ParseInt(*body.EventsChannelID, 10, 64)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, "events_channel_id: "+err.Error())
				return
			}
			// Zero is never a real snowflake, and storing it would send every event
			// message to a channel that cannot exist.
			if parsed <= 0 {
				writeError(w, logger, http.StatusBadRequest, "events_channel_id: invalid snowflake")
				return
			}
			channelID = &parsed
		}

		timezone, err := parseTimezone(body.Timezone)
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, "timezone: "+err.Error())
			return
		}

		mentionRoleIDs, err := stringsToSnowflakes(body.EventMentionRoleIDs)
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, "event_mention_role_ids: "+err.Error())
			return
		}

		bannerURL, err := parseBannerURL(body.EventBannerURL)
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, "event_banner_url: "+err.Error())
			return
		}

		if !validReminderLead(body.ReminderLeadMinutes) {
			writeError(w, logger, http.StatusBadRequest,
				"reminder_lead_minutes must be between 0 and 1440")
			return
		}

		var delivery *db.ReminderDelivery
		if body.ReminderDelivery != nil && *body.ReminderDelivery != "" {
			parsed, err := parseReminderDelivery(*body.ReminderDelivery)
			if err != nil {
				writeError(w, logger, http.StatusBadRequest, err.Error())
				return
			}
			delivery = &parsed
		}

		written, err := settings.Replace(r.Context(), signup.GuildSettings{
			DiscordGuildID: guildID, EventsChannelID: channelID, Timezone: timezone,
			EventMentionRoleIDs: mentionRoleIDs, EventBannerURL: bannerURL,
			ReminderLeadMinutes: body.ReminderLeadMinutes, ReminderDelivery: delivery,
		})
		if err != nil {
			logger.ErrorContext(r.Context(), "writing guild settings", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, logger, http.StatusOK, settingsToResponse(written, guildID, true))
	}
}

// parseTimezone checks that a zone name resolves before it is stored. An unknown name
// would otherwise be found out much later, by the bot, while trying to read a raid time
// a raid lead already typed.
//
// IANA names only, and that is the point: a fixed offset like +02:00 is correct until
// the clocks change, and a raid schedule outlives October.
func parseTimezone(name *string) (*string, error) {
	if name == nil || *name == "" {
		return nil, nil //nolint:nilnil
	}
	if _, err := time.LoadLocation(*name); err != nil {
		return nil, fmt.Errorf("unknown zone %q, want an IANA name like Europe/Bratislava", *name)
	}
	return name, nil
}

// parseBannerURL checks that a banner is an https image URL before it is stored.
//
// Discord refuses to render anything else, so an http or a data URL would be accepted
// here and then silently produce an event card with no artwork, which is the kind of
// failure nobody thinks to look for.
func parseBannerURL(raw *string) (*string, error) {
	if raw == nil || *raw == "" {
		return nil, nil //nolint:nilnil
	}

	parsed, err := url.Parse(*raw)
	if err != nil {
		return nil, fmt.Errorf("not a URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("must be an https URL")
	}
	return raw, nil
}
