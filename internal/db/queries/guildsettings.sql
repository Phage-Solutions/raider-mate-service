-- name: GetGuildSettings :one
SELECT * FROM guild_settings
WHERE discord_guild_id = $1;

-- name: UpsertGuildSettings :one
-- Whole-row write. There is one settable field today and a guild that has never been
-- configured has no row, so an upsert is both the create and the update.
INSERT INTO guild_settings (discord_guild_id, events_channel_id, timezone, event_mention_role_ids, event_banner_url)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (discord_guild_id) DO UPDATE
SET events_channel_id = excluded.events_channel_id,
    timezone = excluded.timezone,
    event_mention_role_ids = excluded.event_mention_role_ids,
    event_banner_url = excluded.event_banner_url,
    updated_at = now()
RETURNING *;
