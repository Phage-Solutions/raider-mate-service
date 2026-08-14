-- name: UpsertUser :one
INSERT INTO users (discord_id, discord_guild_id)
VALUES ($1, $2)
ON CONFLICT (discord_id, discord_guild_id) DO UPDATE SET discord_id = excluded.discord_id
RETURNING *;

-- name: GetUserByDiscord :one
SELECT * FROM users
WHERE discord_id = $1 AND discord_guild_id = $2;

-- name: CreateCharacter :one
INSERT INTO characters (user_id, name, realm, is_main)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetCharacterInGuild :one
SELECT c.* FROM characters c
JOIN users u ON u.id = c.user_id
WHERE c.id = $1 AND u.discord_guild_id = $2;

-- name: ListCharactersByUser :many
SELECT * FROM characters
WHERE user_id = $1
ORDER BY name;

-- name: ListCharactersByDiscord :many
SELECT c.* FROM characters c
JOIN users u ON u.id = c.user_id
WHERE u.discord_id = $1 AND u.discord_guild_id = $2
ORDER BY c.name;

-- name: SetCharacterRole :exec
INSERT INTO character_roles (character_id, role, priority)
VALUES ($1, $2, $3)
ON CONFLICT (character_id, role) DO UPDATE SET priority = excluded.priority;

-- name: DeleteCharacterRoles :exec
DELETE FROM character_roles
WHERE character_id = $1;

-- name: ListCharacterRoles :many
SELECT * FROM character_roles
WHERE character_id = $1
-- Nothing stops two roles sharing a priority; role keeps the menu order stable.
ORDER BY priority, role;
