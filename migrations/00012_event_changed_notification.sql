-- +goose Up
-- An event edited outside Discord left the signup sheet in the channel showing the old
-- title and the old pull time. Every other write that changes what the sheet says
-- queues a redraw; an edit was the one that did not, so a raid lead moving a raid by an
-- hour moved it for nobody who was reading the message.
--
-- The bot needs no change to read this. It redraws on any MESSAGE-target notification
-- before it looks at the kind, so the kind exists to make the outbox honest about what
-- happened rather than to be dispatched on.
--
-- Only the value is added here. Postgres refuses to use a new enum value in the
-- transaction that created it, and goose runs a migration in one.
ALTER TYPE notification_kind ADD VALUE 'EVENT_CHANGED';

-- +goose Down
-- Postgres cannot drop an enum value. Leaving it is harmless once the emitting query
-- is gone.
SELECT 1;
