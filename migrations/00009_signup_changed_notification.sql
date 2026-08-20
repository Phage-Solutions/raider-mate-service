-- +goose Up
-- A signup written anywhere but a Discord button left the event message showing the
-- old answers. The bot redraws on its own clicks and on a Raider.IO sync, and nothing
-- else, so every write from the dashboard was invisible in the channel it matters in.
--
-- Only the value is added here. Postgres refuses to use a new enum value in the
-- transaction that created it, and goose runs a migration in one.
ALTER TYPE notification_kind ADD VALUE 'SIGNUP_CHANGED';

-- +goose Down
-- Postgres cannot drop an enum value. Leaving it is harmless once the emitting query
-- is gone.
SELECT 1;
