-- +goose Up
-- Discord role names vary per guild, so the raid-lead capability cannot hardcode one.
-- A guild maps its own role IDs to the capability instead; many roles, one capability.
-- Set membership, not an entity, so this follows character_roles' composite key rather
-- than a UUIDv7 surrogate.
CREATE TABLE guild_raid_lead_roles (
    discord_guild_id bigint NOT NULL,
    discord_role_id  bigint NOT NULL,
    PRIMARY KEY (discord_guild_id, discord_role_id)
);

-- +goose Down
DROP TABLE guild_raid_lead_roles;
