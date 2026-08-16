-- +goose Up
CREATE TYPE discord_channel_type AS ENUM (
    'TEXT', 'ANNOUNCEMENT', 'VOICE', 'STAGE_VOICE', 'FORUM', 'CATEGORY', 'OTHER'
);

-- The guild's full channel list, as the bot sees it, so the dashboard can offer a
-- picker for guild_settings.events_channel_id without this service ever calling
-- Discord itself (hard rule 6). Wholesale-replaced on every bot push, same shape as
-- guild_raid_lead_roles: no per-row meaning survives a name or type change upstream.
CREATE TABLE guild_channels (
    discord_guild_id   bigint NOT NULL,
    discord_channel_id bigint NOT NULL,
    name                text NOT NULL,
    type                discord_channel_type NOT NULL,
    PRIMARY KEY (discord_guild_id, discord_channel_id)
);

-- The guild's full role list, so the dashboard can offer a picker for
-- guild_settings.event_mention_role_ids. color/position are display only (badge
-- color, sort order matching Discord's own role list); no domain logic reads them.
CREATE TABLE guild_roles (
    discord_guild_id bigint NOT NULL,
    discord_role_id  bigint NOT NULL,
    name              text NOT NULL,
    color             integer NOT NULL,
    position          integer NOT NULL,
    PRIMARY KEY (discord_guild_id, discord_role_id)
);

-- +goose Down
DROP TABLE guild_roles;
DROP TABLE guild_channels;
DROP TYPE discord_channel_type;
