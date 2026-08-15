-- name: ListRaidLeadRoles :many
SELECT discord_role_id FROM guild_raid_lead_roles
WHERE discord_guild_id = $1
ORDER BY discord_role_id;

-- name: DeleteRaidLeadRoles :exec
DELETE FROM guild_raid_lead_roles
WHERE discord_guild_id = $1;

-- name: InsertRaidLeadRole :exec
INSERT INTO guild_raid_lead_roles (discord_guild_id, discord_role_id)
VALUES ($1, $2)
ON CONFLICT (discord_guild_id, discord_role_id) DO NOTHING;

-- name: CountRaidLeadRoles :one
-- Drives the bootstrap rule: a guild with no mapped roles treats Discord admins as
-- raid leads, so a fresh install is never bricked.
SELECT count(*) FROM guild_raid_lead_roles
WHERE discord_guild_id = $1;
