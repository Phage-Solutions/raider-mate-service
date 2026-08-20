-- +goose Up
-- A comp written from the dashboard left the event message showing the old board. The
-- bot redraws on its own button clicks, on a Raider.IO sync, and on a signup write,
-- and nothing else: saving a hand-built comp, locking one, renaming one or handing one
-- back to the assigner were all invisible in the channel they matter in.
--
-- The bot needs no change to read this. It redraws on any MESSAGE-target notification
-- before it looks at the kind, so the kind exists to make the outbox honest about what
-- happened rather than to be dispatched on.
--
-- Only the value is added here. Postgres refuses to use a new enum value in the
-- transaction that created it, and goose runs a migration in one.
ALTER TYPE notification_kind ADD VALUE 'COMP_CHANGED';

-- +goose Down
-- Postgres cannot drop an enum value. Leaving it is harmless once the emitting query
-- is gone.
SELECT 1;
