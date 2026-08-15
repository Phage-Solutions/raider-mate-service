-- +goose Up
-- An image shown under the roster on every event message, the way a raid card usually
-- carries artwork for the tier being run.
--
-- One per guild rather than one per raid tier: the bot has no concept of a tier, only a
-- free-text title, so there is nothing to key a per-raid image off yet. When events
-- gain that, this becomes the fallback rather than the only answer.
ALTER TABLE guild_settings ADD COLUMN event_banner_url text;

-- +goose Down
ALTER TABLE guild_settings DROP COLUMN event_banner_url;
