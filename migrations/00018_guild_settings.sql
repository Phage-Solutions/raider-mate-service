-- +goose Up
-- Per-guild bot configuration. One row per guild, created on first write, so a guild
-- that has never configured anything is an absent row rather than a row of nulls.
--
-- events_channel_id is where the bot posts event messages. It is nullable because the
-- capability has to work before anyone configures it: with no channel set the bot
-- posts wherever the command was run, which is what a guild trying the bot for the
-- first time expects.
--
-- Keyed on the Discord snowflake, not a UUIDv7 surrogate: this is configuration
-- belonging to a guild, and there is exactly one row per guild by definition.
CREATE TABLE guild_settings (
    discord_guild_id  bigint PRIMARY KEY,
    events_channel_id bigint,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE guild_settings;
