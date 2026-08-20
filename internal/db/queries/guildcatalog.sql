-- name: ListGuildChannels :many
SELECT discord_channel_id, name, type FROM guild_channels
WHERE discord_guild_id = $1
ORDER BY name;

-- name: DeleteGuildChannels :exec
DELETE FROM guild_channels
WHERE discord_guild_id = $1;

-- name: InsertGuildChannel :exec
INSERT INTO guild_channels (discord_guild_id, discord_channel_id, name, type)
VALUES ($1, $2, $3, $4);

-- name: ListGuildRoles :many
SELECT discord_role_id, name, color, position FROM guild_roles
WHERE discord_guild_id = $1
ORDER BY position DESC;

-- name: DeleteGuildRoles :exec
DELETE FROM guild_roles
WHERE discord_guild_id = $1;

-- name: InsertGuildRole :exec
INSERT INTO guild_roles (discord_guild_id, discord_role_id, name, color, position)
VALUES ($1, $2, $3, $4, $5);

-- name: HighestGuildRole :one
-- The top of the guild's role hierarchy, which is the one raid-lead mapping that may
-- never be removed. Ties break on id so the answer does not move between calls.
--
-- The bot already leaves @everyone and integration-managed roles out of this catalogue,
-- so this filter is belt rather than braces. It is here because the two ship on separate
-- cycles: an older bot that pushed @everyone would make it the guild's highest role, and
-- pinning that as a raid-lead role hands the capability to every member.
SELECT discord_role_id FROM guild_roles
WHERE discord_guild_id = $1 AND discord_role_id <> discord_guild_id
ORDER BY position DESC, discord_role_id DESC
LIMIT 1;
