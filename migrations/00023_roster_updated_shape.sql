-- +goose Up
-- The anonymous CHECK from 00014 knew two targets. MESSAGE needs a channel like ROLE
-- does, and gets a name this time so the next change does not have to look it up.
ALTER TABLE notifications DROP CONSTRAINT notifications_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_target_shape CHECK (
    (target_kind = 'USER' AND discord_id IS NOT NULL)
    OR (target_kind = 'ROLE' AND channel_id IS NOT NULL)
    OR (target_kind = 'MESSAGE' AND channel_id IS NOT NULL)
);

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

-- +goose Down
DROP INDEX notifications_roster_updated_pending;
ALTER TABLE notifications DROP CONSTRAINT notifications_target_shape;
ALTER TABLE notifications ADD CHECK (
    (target_kind = 'USER' AND discord_id IS NOT NULL)
    OR (target_kind = 'ROLE' AND channel_id IS NOT NULL)
);
