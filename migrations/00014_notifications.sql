-- +goose Up
-- The worker drains scheduled_jobs into this outbox: it claims a job, resolves
-- recipients, and writes one row per recipient here. The bot polls and acks. Its own
-- enum rather than a reuse of job_enum, because not every notification traces back to
-- a scheduled job: LATE_REQUEST_FILED fires the moment a player files a late request,
-- with no scheduled_jobs row behind it, and adding it to job_enum would make it
-- schedulable, which it is not.
CREATE TYPE notification_kind AS ENUM (
    'REMINDER_24H', 'REMINDER_1H', 'SIGNUP_DEADLINE', 'COMP_NAG', 'LATE_REQUEST_FILED'
);

-- Two shapes of message. REMINDER_24H and REMINDER_1H address a person and become
-- DMs. SIGNUP_DEADLINE and COMP_NAG address the raid lead, whom the service knows
-- only as role IDs, so they carry channel_id and become a channel post with a role
-- mention instead of a per-user resolution.
CREATE TYPE notification_target AS ENUM ('USER', 'ROLE');

CREATE TABLE notifications (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
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
    CHECK ((target_kind = 'USER' AND discord_id IS NOT NULL)
        OR (target_kind = 'ROLE' AND channel_id IS NOT NULL))
);

CREATE INDEX notifications_undelivered ON notifications (created_at)
    WHERE delivered_at IS NULL;

-- +goose Down
DROP TABLE notifications;
DROP TYPE notification_target;
DROP TYPE notification_kind;
