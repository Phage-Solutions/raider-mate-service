-- +goose Up
-- A character's cached Raider.IO data goes stale silently: the event message renders
-- item level and score at the moment someone last clicked a button, so a raid where
-- nobody has signed up for days shows last week's gear. ROSTER_UPDATED tells the bot
-- to redraw an event it already posted.
ALTER TYPE notification_kind ADD VALUE 'ROSTER_UPDATED';

-- Neither existing target fits. This one addresses nobody: no DM, no mention, just an
-- edit of the message the event already owns. Sending it as a channel post would ping
-- a raid lead every time somebody replaced a trinket.
ALTER TYPE notification_target ADD VALUE 'MESSAGE';

-- The values are added on their own because Postgres refuses to use a new enum value
-- in the transaction that created it, and the index in 00023 tests kind against it.

-- +goose Down
-- Postgres cannot drop an enum value. Leaving them is harmless: no row references
-- them once 00023 rolls back and the emitting query is gone.
SELECT 1;
