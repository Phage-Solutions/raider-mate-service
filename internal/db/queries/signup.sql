-- name: CreateEvent :one
-- difficulty is NULL for MYTHIC_PLUS events, which have no difficulty of their own.
-- For a raid it decides the comp size rule, so the assigner cannot tell a Mythic
-- raid from a flex one without it.
INSERT INTO events (discord_guild_id, type, title, starts_at, signup_deadline, comp_template, message_id, channel_id, difficulty)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: SetEventDifficulty :exec
UPDATE events SET difficulty = $2
WHERE id = $1;

-- name: GetEvent :one
SELECT * FROM events
WHERE id = $1;

-- name: ListUpcomingEvents :many
SELECT * FROM events
WHERE discord_guild_id = $1 AND starts_at >= now()
ORDER BY starts_at ASC;

-- name: UpsertSignup :one
INSERT INTO signups (event_id, character_id, status, note)
VALUES ($1, $2, $3, $4)
-- A status change invalidates whatever the comp lock decided, so the assignment is
-- dropped. Editing only the note leaves an existing assignment alone.
ON CONFLICT (event_id, character_id) DO UPDATE SET
    status = excluded.status,
    note = excluded.note,
    assigned_role = CASE
        WHEN signups.status IS DISTINCT FROM excluded.status THEN NULL
        ELSE signups.assigned_role
    END,
    late_until = CASE
        WHEN signups.status IS DISTINCT FROM excluded.status THEN NULL
        ELSE signups.late_until
    END
RETURNING *;

-- name: ListSignupsForEvent :many
SELECT * FROM signups
WHERE event_id = $1
-- created_at is transaction start time, so it ties for signups written together.
ORDER BY created_at ASC, id ASC;
