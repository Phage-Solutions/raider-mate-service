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
