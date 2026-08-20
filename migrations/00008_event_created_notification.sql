-- +goose Up
-- An event created outside Discord has no message and no channel, so the bot never
-- posts its signup sheet and every later reminder finds nowhere to speak. This is the
-- notification that tells the bot to go and post one.
--
-- Only the value is added here. Postgres refuses to use a new enum value in the
-- transaction that created it, and goose runs a migration in one.
ALTER TYPE notification_kind ADD VALUE 'EVENT_CREATED';

-- +goose Down
-- Postgres cannot drop an enum value. Leaving it is harmless once the emitting query
-- is gone.
SELECT 1;
