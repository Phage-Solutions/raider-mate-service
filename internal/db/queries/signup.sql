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

-- name: UpdateEvent :one
-- Partial update: a field left NULL keeps its stored value, same COALESCE pattern as
-- UpdateCharacterFromSync. Covers message_id/channel_id (learned only after the bot
-- posts) and the schedule fields a PATCH reschedules jobs from.
UPDATE events SET
    title = COALESCE(sqlc.narg(title), title),
    starts_at = COALESCE(sqlc.narg(starts_at), starts_at),
    signup_deadline = COALESCE(sqlc.narg(signup_deadline), signup_deadline),
    comp_template = COALESCE(sqlc.narg(comp_template), comp_template),
    difficulty = COALESCE(sqlc.narg(difficulty), difficulty),
    message_id = COALESCE(sqlc.narg(message_id), message_id),
    channel_id = COALESCE(sqlc.narg(channel_id), channel_id)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteEvent :exec
DELETE FROM events
WHERE id = $1;

-- name: GetEvent :one
SELECT * FROM events
WHERE id = $1;

-- name: ListUpcomingEvents :many
SELECT * FROM events
WHERE discord_guild_id = $1 AND starts_at >= now()
ORDER BY starts_at ASC;

-- name: UpsertSignup :one
-- late_until is a plain write-through field, same as note: internal/signup owns the
-- rule that it is only meaningful alongside status = LATE and nils it otherwise.
INSERT INTO signups (event_id, character_id, status, note, late_until)
VALUES ($1, $2, $3, $4, $5)
-- A status change invalidates whatever the comp lock decided, so the assignment is
-- dropped. Editing only the note leaves an existing assignment alone.
ON CONFLICT (event_id, character_id) DO UPDATE SET
    status = excluded.status,
    note = excluded.note,
    late_until = excluded.late_until,
    assigned_role = CASE
        WHEN signups.status IS DISTINCT FROM excluded.status THEN NULL
        ELSE signups.assigned_role
    END
RETURNING *;

-- name: DeleteSignup :exec
DELETE FROM signups
WHERE event_id = $1 AND character_id = $2;

-- name: ListSignupsForEvent :many
SELECT * FROM signups
WHERE event_id = $1
-- created_at is transaction start time, so it ties for signups written together.
ORDER BY created_at ASC, id ASC;

-- name: ListUndecidedForEvent :many
-- Grouped by discord_id, not by character: a raider with four alts and no signup is
-- one person who has not answered, and four DMs would be a bug.
SELECT DISTINCT u.discord_id
FROM characters c
JOIN users u ON u.id = c.user_id
JOIN events e ON e.discord_guild_id = u.discord_guild_id
LEFT JOIN signups s ON s.event_id = e.id AND s.character_id = c.id
WHERE e.id = $1 AND s.id IS NULL
ORDER BY u.discord_id;

-- name: ListConfirmedWithRole :many
-- assigned_role IS NOT NULL rather than status = 'CONFIRMED': the assignment pool is
-- CONFIRMED and LATE both (design.md section 5), and REMINDER_1H is for whoever
-- actually holds a seat, not for whoever merely confirmed.
SELECT u.discord_id, s.assigned_role
FROM signups s
JOIN characters c ON c.id = s.character_id
JOIN users u ON u.id = c.user_id
WHERE s.event_id = $1 AND s.assigned_role IS NOT NULL;

-- name: CountCompSlotsForEvent :one
SELECT count(*) FROM comp_slots WHERE event_id = $1;

-- name: UpsertLateRequest :one
-- A re-request resets state to PENDING and clears any prior decision, since the
-- unique constraint makes this an upsert rather than a pile of rows.
INSERT INTO late_signup_requests (event_id, character_id, status, note)
VALUES ($1, $2, $3, $4)
ON CONFLICT (event_id, character_id) DO UPDATE SET
    status = excluded.status,
    note = excluded.note,
    state = 'PENDING',
    decided_at = NULL
RETURNING *;

-- name: ListLateRequests :many
SELECT * FROM late_signup_requests
WHERE event_id = $1
ORDER BY created_at DESC;

-- name: DecideLateRequest :exec
UPDATE late_signup_requests SET state = $2, decided_at = now()
WHERE id = $1;
