-- +goose Up
-- The bot polled this table on a timer, so every reminder waited out the remainder of
-- an interval the service had no reason to wait for. This is the signal that replaces
-- the wait: the API listens on the channel and holds an SSE stream open per connected
-- bot.
--
-- A trigger rather than a call in each insert path, because there are three of those
-- already (reminders in the worker, late requests in the API, roster redraws in the
-- syncer) and a fourth that forgot to signal would look like a bot bug, not a missing
-- line.
--
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

-- Per statement, not per row: one sync batch can insert forty roster redraws, and the
-- bot has the same work to do whether it hears about that once or forty times.
CREATE TRIGGER notifications_notify_queued
AFTER INSERT ON notifications
FOR EACH STATEMENT
EXECUTE FUNCTION notify_notification_queued();

-- +goose Down
DROP TRIGGER notifications_notify_queued ON notifications;
DROP FUNCTION notify_notification_queued();
