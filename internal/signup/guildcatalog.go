package signup

import (
	"context"
	"fmt"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Channel is one of a guild's Discord channels, as the bot last reported it.
type Channel struct {
	DiscordChannelID int64
	Name             string
	Type             db.DiscordChannelType
}

// Role is one of a guild's Discord roles, as the bot last reported it.
type Role struct {
	DiscordRoleID int64
	Name          string
	Color         int32
	Position      int32
}

// guildCatalogStore is the persistence GuildCatalog needs. Declared here, by the
// consumer.
type guildCatalogStore interface {
	GuildChannels(ctx context.Context, discordGuildID int64) ([]Channel, error)
	ReplaceGuildChannels(ctx context.Context, discordGuildID int64, channels []Channel) error
	GuildRoles(ctx context.Context, discordGuildID int64) ([]Role, error)
	ReplaceGuildRoles(ctx context.Context, discordGuildID int64, roles []Role) error
}

// GuildCatalog is the guild's Discord channels and roles, as the bot last reported
// them. It exists so the dashboard can offer a picker for guild_settings'
// events_channel_id and event_mention_role_ids without this service ever calling
// Discord itself (hard rule 6): the bot pushes its view, this service only stores it.
type GuildCatalog struct {
	store guildCatalogStore
}

// NewGuildCatalog builds a GuildCatalog.
func NewGuildCatalog(store guildCatalogStore) *GuildCatalog {
	return &GuildCatalog{store: store}
}

func (c *GuildCatalog) Channels(ctx context.Context, discordGuildID int64) ([]Channel, error) {
	channels, err := c.store.GuildChannels(ctx, discordGuildID)
	if err != nil {
		return nil, fmt.Errorf("listing guild channels: %w", err)
	}
	return channels, nil
}

// ReplaceChannels overwrites a guild's whole channel catalog: a push that
// half-applied would leave a deleted channel selectable in the dashboard alongside
// the bot's current set.
func (c *GuildCatalog) ReplaceChannels(ctx context.Context, discordGuildID int64, channels []Channel) error {
	if err := c.store.ReplaceGuildChannels(ctx, discordGuildID, channels); err != nil {
		return fmt.Errorf("replacing guild channels: %w", err)
	}
	return nil
}

func (c *GuildCatalog) Roles(ctx context.Context, discordGuildID int64) ([]Role, error) {
	roles, err := c.store.GuildRoles(ctx, discordGuildID)
	if err != nil {
		return nil, fmt.Errorf("listing guild roles: %w", err)
	}
	return roles, nil
}

// ReplaceRoles overwrites a guild's whole role catalog, same reasoning as
// ReplaceChannels.
func (c *GuildCatalog) ReplaceRoles(ctx context.Context, discordGuildID int64, roles []Role) error {
	if err := c.store.ReplaceGuildRoles(ctx, discordGuildID, roles); err != nil {
		return fmt.Errorf("replacing guild roles: %w", err)
	}
	return nil
}
