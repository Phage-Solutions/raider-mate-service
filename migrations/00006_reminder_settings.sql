-- +goose Up
-- How the pre-event reminder reaches people. PING is one message in the events channel
-- mentioning everyone signed up, DM is one direct message each, BOTH is both.
CREATE TYPE reminder_delivery AS ENUM ('PING', 'DM', 'BOTH');

-- Nullable, like timezone and event_banner_url: NULL means the guild has not chosen,
-- and the fallback (30 minutes, PING) is resolved in Go so it lives in one place rather
-- than being split between a column default and the code that reads it.
ALTER TABLE guild_settings
    ADD COLUMN reminder_lead_minutes int,
    ADD COLUMN reminder_delivery reminder_delivery;

-- Resolved at creation from the request, then the guild setting, then the default, and
-- stored here. Keeping the effective value on the event makes the schedule a function of
-- the event row alone, so a later settings change cannot silently re-time a raid that is
-- already posted. NULL on rows created before this migration; those events keep the
-- jobs they already have.
ALTER TABLE events ADD COLUMN reminder_lead_minutes int;

-- The mentions a CHANNEL notification carries. role_ids is not reused: the two render
-- with different mention syntax and a wrong guess pings nobody.
ALTER TABLE notifications ADD COLUMN discord_ids bigint[];

ALTER TABLE notifications DROP CONSTRAINT notifications_target_shape;
ALTER TABLE notifications ADD CONSTRAINT notifications_target_shape CHECK (
    (target_kind = 'USER' AND discord_id IS NOT NULL)
    OR (target_kind = 'ROLE' AND channel_id IS NOT NULL)
    OR (target_kind = 'MESSAGE' AND channel_id IS NOT NULL)
    OR (target_kind = 'CHANNEL' AND channel_id IS NOT NULL)
);

-- +goose Down
ALTER TABLE notifications DROP CONSTRAINT notifications_target_shape;
ALTER TABLE notifications ADD CONSTRAINT notifications_target_shape CHECK (
    (target_kind = 'USER' AND discord_id IS NOT NULL)
    OR (target_kind = 'ROLE' AND channel_id IS NOT NULL)
    OR (target_kind = 'MESSAGE' AND channel_id IS NOT NULL)
);
ALTER TABLE notifications DROP COLUMN discord_ids;
ALTER TABLE events DROP COLUMN reminder_lead_minutes;
ALTER TABLE guild_settings
    DROP COLUMN reminder_delivery,
    DROP COLUMN reminder_lead_minutes;
DROP TYPE reminder_delivery;
