-- name: InsertNotification :exec
INSERT INTO notifications (discord_guild_id, event_id, kind, target_kind, discord_id, role_ids, channel_id, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

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
