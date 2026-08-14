-- +goose Up
CREATE TABLE events (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    discord_guild_id  bigint NOT NULL,
    type              event_type NOT NULL,
    title             text NOT NULL,
    starts_at         timestamptz NOT NULL,
    signup_deadline   timestamptz NOT NULL,
    comp_template     jsonb NOT NULL,
    message_id        bigint,
    channel_id        bigint
);

CREATE TABLE signups (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    event_id      uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    character_id  uuid NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    status        signup_status NOT NULL,
    assigned_role role_enum,
    late_until    timestamptz,
    note          text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (event_id, character_id)
);

-- Supports the cascade from characters. The unique index above leads with event_id,
-- so it cannot serve a delete by character.
CREATE INDEX signups_character_id ON signups (character_id);

-- +goose Down
DROP TABLE signups;
DROP TABLE events;
