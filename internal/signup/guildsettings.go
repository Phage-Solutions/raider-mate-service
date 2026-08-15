package signup

import (
	"context"
	"fmt"
)

// GuildSettings is a guild's bot configuration. Every field is optional: a guild that
// has configured nothing is valid, and is what every guild starts as.
type GuildSettings struct {
	DiscordGuildID int64
	// EventsChannelID is where event messages are posted. Nil means the bot posts
	// wherever the create command was run, which is the behaviour before anyone
	// configures anything.
	EventsChannelID *int64
	// Timezone is an IANA name the guild types its raid times in. Nil means times must
	// carry an explicit offset, since nothing else can resolve them.
	Timezone *string
	// EventMentionRoleIDs are pinged when an event message is posted. Empty means the
	// message goes up without pinging anyone, which is a real choice and not a
	// missing setting.
	EventMentionRoleIDs []int64
	// EventBannerURL is artwork shown under the roster. Nil for a plain card.
	EventBannerURL *string
}

// guildSettingsStore is the persistence Settings needs. Declared here, by the consumer.
type guildSettingsStore interface {
	GuildSettings(ctx context.Context, discordGuildID int64) (GuildSettings, error)
	UpsertGuildSettings(ctx context.Context, settings GuildSettings) (GuildSettings, error)
}

// Settings reads and writes per-guild bot configuration.
type Settings struct {
	store guildSettingsStore
}

// NewSettings builds a Settings.
func NewSettings(store guildSettingsStore) *Settings {
	return &Settings{store: store}
}

// Get returns a guild's settings. A guild with no row is not an error: it is a guild
// that has not configured anything, and the zero value says so.
func (s *Settings) Get(ctx context.Context, discordGuildID int64) (GuildSettings, error) {
	settings, err := s.store.GuildSettings(ctx, discordGuildID)
	if err != nil {
		return GuildSettings{}, fmt.Errorf("reading guild settings: %w", err)
	}
	return settings, nil
}

// Replace writes a guild's whole settings row. Like the raid-lead role mapping, this
// resource supports one write and it is a PUT: the fields are configuration a guild
// submits together, not rows with lives of their own.
func (s *Settings) Replace(ctx context.Context, settings GuildSettings) (GuildSettings, error) {
	written, err := s.store.UpsertGuildSettings(ctx, settings)
	if err != nil {
		return GuildSettings{}, fmt.Errorf("writing guild settings: %w", err)
	}
	return written, nil
}
