-- name: InsertNotification :exec
INSERT INTO notifications (discord_guild_id, event_id, kind, target_kind, discord_id, role_ids, channel_id, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: InsertRosterUpdatedNotifications :execrows
-- Queues a redraw of every posted, upcoming event this character is signed up to.
-- Written as one INSERT ... SELECT rather than a read followed by inserts so the whole
-- fan-out shares the sync's transaction: no redraw is queued for a snapshot that was
-- rolled back, and none is lost for one that was not.
--
-- Past events are skipped because nobody re-reads a raid that already happened, and
-- events with no message_id were never posted, so there is nothing to edit.
--
-- ON CONFLICT DO NOTHING leans on notifications_roster_updated_pending: the second
-- character to change on the same raid finds a redraw already queued and adds nothing.
INSERT INTO notifications (discord_guild_id, event_id, kind, target_kind, channel_id, payload)
SELECT e.discord_guild_id, e.id, 'ROSTER_UPDATED', 'MESSAGE', e.channel_id, '{}'::jsonb
FROM events e
JOIN signups s ON s.event_id = e.id
WHERE s.character_id = sqlc.arg(character_id)
  AND e.starts_at > now()
  AND e.message_id IS NOT NULL
  AND e.channel_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- name: ClaimNotifications :many
-- Claiming, not just reading. The ack arrives in a later HTTP request, so no
-- transaction can span send and ack and row locks cannot help: two bot replicas
-- polling the same window would both read the same rows and DM every raider twice.
-- Stamping claimed_at inside the same statement hands each row to one poller.
--
-- claimed_before re-opens a lease so a bot that claimed rows and died still gets them
-- redelivered. That keeps delivery at-least-once, which reminders tolerate; what it
-- removes is the duplicate storm on every tick.
UPDATE notifications SET claimed_at = now()
WHERE id IN (
    SELECT n.id FROM notifications n
    WHERE n.delivered_at IS NULL
      AND (n.claimed_at IS NULL OR n.claimed_at < sqlc.arg(claimed_before))
      AND (sqlc.narg(guild_id)::bigint IS NULL OR n.discord_guild_id = sqlc.narg(guild_id))
    ORDER BY n.created_at ASC
    LIMIT sqlc.arg(row_limit)
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkNotificationDelivered :execrows
-- guild_id is optional, and the two callers differ. The bot acks across every guild
-- from behind the service key, so it passes NULL. Anything reached by a raider's
-- interaction must pass their guild, or acking by id alone would let them silently
-- suppress another guild's reminders. Returning the row count lets the caller tell
-- "not yours or not found" from "done".
UPDATE notifications SET delivered_at = now()
WHERE id = $1
  AND (sqlc.narg(guild_id)::bigint IS NULL OR discord_guild_id = sqlc.narg(guild_id));
