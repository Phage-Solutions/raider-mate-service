-- name: InsertNotification :exec
INSERT INTO notifications (discord_guild_id, event_id, kind, target_kind, discord_id, role_ids, channel_id, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListUndeliveredNotifications :many
SELECT * FROM notifications
WHERE delivered_at IS NULL
  AND (sqlc.narg(guild_id)::bigint IS NULL OR discord_guild_id = sqlc.narg(guild_id))
ORDER BY created_at ASC
LIMIT sqlc.arg(row_limit);

-- name: MarkNotificationDelivered :exec
UPDATE notifications SET delivered_at = now()
WHERE id = $1;
