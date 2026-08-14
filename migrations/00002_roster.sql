-- +goose Up
CREATE TABLE users (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    discord_id        bigint NOT NULL,
    discord_guild_id  bigint NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (discord_id, discord_guild_id)
);

CREATE TABLE characters (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name          text NOT NULL,
    realm         text NOT NULL,
    class         text,
    spec          text,
    ilvl          numeric,
    mplus_score   numeric,
    last_synced   timestamptz,
    is_main       boolean NOT NULL DEFAULT false,
    UNIQUE (user_id, name, realm)
);

-- A raider has at most one main.
CREATE UNIQUE INDEX characters_one_main_per_user
    ON characters (user_id)
    WHERE is_main;

-- What a character CAN play, in preference order.
CREATE TABLE character_roles (
    character_id  uuid NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    role          role_enum NOT NULL,
    priority      smallint NOT NULL CHECK (priority BETWEEN 1 AND 3),
    PRIMARY KEY (character_id, role)
);

-- +goose Down
DROP TABLE character_roles;
DROP TABLE characters;
DROP TABLE users;
