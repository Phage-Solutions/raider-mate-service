-- +goose Up
-- Squashed baseline. The v0.1 schema was built up over 24 migrations, all of which
-- gave uuid primary keys `DEFAULT uuidv7()` and so could only be applied to Postgres
-- 18. IDs are now generated in Go (db.NewID), which removes the version floor, so the
-- chain was folded into this one file rather than carrying two migrations that exist
-- only to undo each other.
--
-- No uuid column has a DEFAULT. A missing ID is meant to fail loudly rather than
-- silently land a v4 and destroy the index locality v7 was chosen for.

CREATE TYPE role_enum AS ENUM ('TANK', 'HEALER', 'MDPS', 'RDPS');
CREATE TYPE event_type AS ENUM ('RAID', 'MYTHIC_PLUS');

-- BENCH is deliberately absent. Bench membership lives on comp_slots.is_bench,
-- decided fresh by every lock; a signup carrying BENCH would be a second, stale
-- source of truth that the assigner cannot see.
CREATE TYPE signup_status AS ENUM (
    'CONFIRMED', 'TENTATIVE', 'DECLINED', 'LATE', 'ABSENT', 'NO_SHOW'
);

CREATE TYPE job_enum AS ENUM ('SIGNUP_DEADLINE', 'REMINDER_24H', 'REMINDER_1H', 'COMP_NAG');
CREATE TYPE job_status AS ENUM ('PENDING', 'SENT', 'FAILED', 'CANCELED');

-- Normal and Heroic raids are flex (10-30). Mythic raids are locked to exactly 20
-- and cannot zone in at any other size, so the assigner needs to tell Mythic apart
-- from flex rather than treating every raid the same. NULL for Mythic+ events,
-- which have no difficulty concept of their own.
CREATE TYPE raid_difficulty AS ENUM ('NORMAL', 'HEROIC', 'MYTHIC');

-- A raid lead building a comp by hand needs the assigner to keep its hands off, and
-- the assigner needs somewhere to read that from.
CREATE TYPE comp_mode AS ENUM ('AUTO', 'MANUAL');

-- Its own enum rather than a reuse of job_enum, because not every notification traces
-- back to a scheduled job: LATE_REQUEST_FILED fires the moment a player files a late
-- request, with no scheduled_jobs row behind it, and adding it to job_enum would make
-- it schedulable, which it is not.
--
-- ROSTER_UPDATED tells the bot to redraw an event it already posted. A character's
-- cached Raider.IO data goes stale silently, so a raid where nobody has clicked for
-- days would otherwise show last week's gear.
CREATE TYPE notification_kind AS ENUM (
    'REMINDER_24H', 'REMINDER_1H', 'SIGNUP_DEADLINE', 'COMP_NAG', 'LATE_REQUEST_FILED',
    'ROSTER_UPDATED'
);

-- Three shapes of message. REMINDER_24H and REMINDER_1H address a person and become
-- DMs. SIGNUP_DEADLINE and COMP_NAG address the raid lead, whom the service knows
-- only as role IDs, so they carry channel_id and become a channel post with a role
-- mention instead of a per-user resolution. MESSAGE addresses nobody: it is an edit
-- of the message the event already owns, because pinging a raid lead every time
-- somebody replaced a trinket is not a feature.
CREATE TYPE notification_target AS ENUM ('USER', 'ROLE', 'MESSAGE');

CREATE TYPE request_state AS ENUM ('PENDING', 'APPROVED', 'REJECTED');

CREATE TABLE users (
    id                uuid PRIMARY KEY,
    discord_id        bigint NOT NULL,
    discord_guild_id  bigint NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (discord_id, discord_guild_id)
);

-- region is part of identity, not decoration: Raider.IO character lookup requires it
-- and realm names repeat across regions, so (user_id, name, realm) is not unique.
--
-- last_synced means "cached data is current as of". A character whose fetch keeps
-- failing never gets one, so ordering the sync queue by last_synced alone parks it at
-- the head of every batch forever and starves everyone behind it. sync_attempted_at
-- records the attempt separately and moves failures to the back.
CREATE TABLE characters (
    id                 uuid PRIMARY KEY,
    user_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name               text NOT NULL,
    realm              text NOT NULL,
    class              text,
    spec               text,
    ilvl               numeric,
    mplus_score        numeric,
    last_synced        timestamptz,
    is_main            boolean NOT NULL DEFAULT false,
    region             text NOT NULL,
    sync_attempted_at  timestamptz,
    CONSTRAINT characters_user_id_name_realm_region_key
        UNIQUE (user_id, name, realm, region)
);

-- A raider has at most one main.
CREATE UNIQUE INDEX characters_one_main_per_user
    ON characters (user_id)
    WHERE is_main;

-- Serves the sync queue's ordering and its staleness filter.
CREATE INDEX characters_sync_attempted_at
    ON characters (sync_attempted_at ASC NULLS FIRST);

-- What a character CAN play, in preference order.
CREATE TABLE character_roles (
    character_id  uuid NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    role          role_enum NOT NULL,
    priority      smallint NOT NULL CHECK (priority BETWEEN 1 AND 3),
    PRIMARY KEY (character_id, role)
);

CREATE TABLE events (
    id                uuid PRIMARY KEY,
    discord_guild_id  bigint NOT NULL,
    type              event_type NOT NULL,
    title             text NOT NULL,
    starts_at         timestamptz NOT NULL,
    signup_deadline   timestamptz NOT NULL,
    comp_template     jsonb NOT NULL,
    message_id        bigint,
    channel_id        bigint,
    difficulty        raid_difficulty
);

CREATE TABLE signups (
    id            uuid PRIMARY KEY,
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

-- Named compositions per event ("prog comp" vs "farm comp"). The table exists so a
-- comp can be empty and can say who owns it; the slots alone can express neither.
CREATE TABLE comps (
    id        uuid PRIMARY KEY,
    event_id  uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    name      text NOT NULL,
    mode      comp_mode NOT NULL DEFAULT 'AUTO',
    UNIQUE (event_id, name)
);

-- reason outlives the lock message: raid leads ask why Bob got benched days after the
-- comp locked, so it lives on the row rather than only in the API response.
CREATE TABLE comp_slots (
    id            uuid PRIMARY KEY,
    event_id      uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    comp_name     text NOT NULL,
    character_id  uuid NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    role          role_enum NOT NULL,
    slot_index    smallint NOT NULL,
    is_bench      boolean NOT NULL DEFAULT false,
    reason        text NOT NULL,
    UNIQUE (event_id, comp_name, character_id),
    -- slot_index is a position, not display order. Roster and bench number separately.
    UNIQUE (event_id, comp_name, is_bench, slot_index),
    CONSTRAINT comp_slots_comp_fkey
        FOREIGN KEY (event_id, comp_name) REFERENCES comps (event_id, name)
        ON DELETE CASCADE
);

-- Supports the cascade from characters.
CREATE INDEX comp_slots_character_id ON comp_slots (character_id);

CREATE TABLE scheduled_jobs (
    id          uuid PRIMARY KEY,
    event_id    uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    job_type    job_enum NOT NULL,
    run_at      timestamptz NOT NULL,
    status      job_status NOT NULL DEFAULT 'PENDING',
    attempts    smallint NOT NULL DEFAULT 0
);

-- Shaped for `WHERE status = 'PENDING' AND run_at <= now()` with FOR UPDATE SKIP LOCKED.
CREATE INDEX scheduled_jobs_pending_run_at
    ON scheduled_jobs (status, run_at)
    WHERE status = 'PENDING';

-- Supports the cascade from events; the partial index above cannot serve it.
CREATE INDEX scheduled_jobs_event_id ON scheduled_jobs (event_id);

-- Time-series. Captured for every guild regardless of tier; exposure to premium
-- analytics happens at the read side, not the write side.
CREATE TABLE character_snapshots (
    id            uuid PRIMARY KEY,
    character_id  uuid NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    captured_at   timestamptz NOT NULL DEFAULT now(),
    ilvl          numeric,
    mplus_score   numeric,
    gear          jsonb NOT NULL
);

-- Serves both the "latest snapshot per character" read and the cascade from
-- characters, so no separate character_id-only index is needed.
CREATE INDEX character_snapshots_character_id_captured_at
    ON character_snapshots (character_id, captured_at DESC);

-- Discord role names vary per guild, so the raid-lead capability cannot hardcode one.
-- A guild maps its own role IDs to the capability instead; many roles, one capability.
-- Set membership, not an entity, so this follows character_roles' composite key rather
-- than a UUIDv7 surrogate.
CREATE TABLE guild_raid_lead_roles (
    discord_guild_id bigint NOT NULL,
    discord_role_id  bigint NOT NULL,
    PRIMARY KEY (discord_guild_id, discord_role_id)
);

-- The outbox. The worker claims a scheduled job, resolves recipients, and writes one
-- row per recipient here. The bot claims and acks.
CREATE TABLE notifications (
    id                uuid PRIMARY KEY,
    discord_guild_id  bigint NOT NULL,
    event_id          uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    kind              notification_kind NOT NULL,
    target_kind       notification_target NOT NULL,
    discord_id        bigint,
    role_ids          bigint[],
    channel_id        bigint,
    payload           jsonb NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    delivered_at      timestamptz,
    -- Two bot replicas polling the outbox both saw every undelivered row and both
    -- sent, because the ack is a separate HTTP request and no transaction can span
    -- it. A claim stamp hands each row to one poller for the length of a lease.
    claimed_at        timestamptz,
    CONSTRAINT notifications_target_shape CHECK (
        (target_kind = 'USER' AND discord_id IS NOT NULL)
        OR (target_kind = 'ROLE' AND channel_id IS NOT NULL)
        OR (target_kind = 'MESSAGE' AND channel_id IS NOT NULL)
    )
);

CREATE INDEX notifications_undelivered ON notifications (created_at)
    WHERE delivered_at IS NULL;

-- Serves the claim query's predicate: undelivered rows, oldest first.
CREATE INDEX notifications_claimable
    ON notifications (created_at ASC)
    WHERE delivered_at IS NULL;

-- One redraw per event, however many characters changed. A sync batch of fifty can
-- touch fifty raiders on the same raid, and fifty rows here would be fifty identical
-- message edits into Discord's per-channel edit limit.
--
-- Claimed rows are excluded rather than counted as pending: a row the bot is already
-- rendering cannot include a change that arrived after the claim, so a later change
-- must be free to queue a fresh redraw behind it.
CREATE UNIQUE INDEX notifications_roster_updated_pending
    ON notifications (event_id)
    WHERE kind = 'ROSTER_UPDATED' AND delivered_at IS NULL AND claimed_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION notify_notification_queued() RETURNS trigger AS $$
BEGIN
    -- Empty payload on purpose. The signal says "claim now", never what to claim, so
    -- the reader stays the existing claim query and Postgres collapses the duplicate
    -- notifications a single transaction raises.
    PERFORM pg_notify('notification_queued', '');
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- A trigger rather than a call in each insert path, because there are three of those
-- already (reminders in the worker, late requests in the API, roster redraws in the
-- syncer) and a fourth that forgot to signal would look like a bot bug, not a missing
-- line.
--
-- Per statement, not per row: one sync batch can insert forty roster redraws, and the
-- bot has the same work to do whether it hears about that once or forty times.
CREATE TRIGGER notifications_notify_queued
AFTER INSERT ON notifications
FOR EACH STATEMENT
EXECUTE FUNCTION notify_notification_queued();

-- Past signup_deadline, a player write becomes a request instead of an error the bot
-- has to invent a message for. status is what the raider asked for, so approving is a
-- plain UpsertSignup with it. Named for when a request happens, not for the status it
-- carries: a withdrawal past the deadline is a request carrying DECLINED.
CREATE TABLE late_signup_requests (
    id            uuid PRIMARY KEY,
    event_id      uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    character_id  uuid NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    status        signup_status NOT NULL,
    note          text,
    state         request_state NOT NULL DEFAULT 'PENDING',
    created_at    timestamptz NOT NULL DEFAULT now(),
    decided_at    timestamptz,
    -- A LATE signup without late_until is "I'll be late" with no idea how late, which
    -- is the one thing that makes the status actionable.
    late_until    timestamptz,
    -- A re-request upserts rather than piling up rows.
    UNIQUE (event_id, character_id)
);

-- Per-guild bot configuration. One row per guild, created on first write, so a guild
-- that has never configured anything is an absent row rather than a row of nulls.
--
-- Keyed on the Discord snowflake, not a UUIDv7 surrogate: this is configuration
-- belonging to a guild, and there is exactly one row per guild by definition.
--
-- events_channel_id is nullable because the capability has to work before anyone
-- configures it: with no channel set the bot posts wherever the command was run.
--
-- timezone is an IANA name (Europe/Bratislava), not a fixed offset, because a raid
-- schedule outlives a daylight saving change and "+02:00" silently becomes wrong in
-- October. Nullable: a guild without one writes an explicit offset each time.
--
-- event_mention_role_ids is an array here rather than a join table like
-- guild_raid_lead_roles because the bot already loads this row on every event
-- creation, and nothing ever looks a guild up by one of these role ids. NOT NULL with
-- an empty default, so "ping nobody" is a real state rather than a null to check for.
--
-- event_banner_url is one image per guild rather than one per raid tier: the bot has
-- no concept of a tier, only a free-text title, so there is nothing to key a per-raid
-- image off yet.
CREATE TABLE guild_settings (
    discord_guild_id        bigint PRIMARY KEY,
    events_channel_id       bigint,
    updated_at              timestamptz NOT NULL DEFAULT now(),
    timezone                text,
    event_mention_role_ids  bigint[] NOT NULL DEFAULT '{}',
    event_banner_url        text
);

-- +goose Down
DROP TABLE guild_settings;
DROP TABLE late_signup_requests;
DROP TRIGGER notifications_notify_queued ON notifications;
DROP FUNCTION notify_notification_queued();
DROP TABLE notifications;
DROP TABLE guild_raid_lead_roles;
DROP TABLE character_snapshots;
DROP TABLE scheduled_jobs;
DROP TABLE comp_slots;
DROP TABLE comps;
DROP TABLE signups;
DROP TABLE events;
DROP TABLE character_roles;
DROP TABLE characters;
DROP TABLE users;
DROP TYPE request_state;
DROP TYPE notification_target;
DROP TYPE notification_kind;
DROP TYPE comp_mode;
DROP TYPE raid_difficulty;
DROP TYPE job_status;
DROP TYPE job_enum;
DROP TYPE signup_status;
DROP TYPE event_type;
DROP TYPE role_enum;
