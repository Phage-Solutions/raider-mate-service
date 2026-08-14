-- name: ListAssignmentPoolForEvent :many
-- Confirmed and late signups only; declined, tentative, bench, absent, and no-show
-- never reach the assigner.
SELECT
    s.character_id,
    c.name,
    c.is_main,
    c.ilvl,
    c.mplus_score,
    s.created_at AS signed_up_at
FROM signups s
JOIN characters c ON c.id = s.character_id
WHERE s.event_id = $1 AND s.status IN ('CONFIRMED', 'LATE')
ORDER BY s.created_at ASC, s.id ASC;

-- name: ListRolesForCharacters :many
SELECT * FROM character_roles
WHERE character_id = ANY(sqlc.arg(character_ids)::uuid[])
ORDER BY character_id, priority, role;

-- name: UpsertComp :one
-- Creating a comp is idempotent, but the mode is set once at creation: flipping an
-- existing comp between AUTO and MANUAL is SetCompMode's job, so a stray create
-- cannot quietly hand an officer's hand-built comp back to the assigner.
INSERT INTO comps (event_id, name, mode)
VALUES ($1, $2, $3)
ON CONFLICT (event_id, name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: GetComp :one
SELECT * FROM comps
WHERE event_id = $1 AND name = $2;

-- name: ListComps :many
SELECT * FROM comps
WHERE event_id = $1
ORDER BY name;

-- name: SetCompMode :exec
UPDATE comps SET mode = $3
WHERE event_id = $1 AND name = $2;

-- name: DeleteComp :exec
DELETE FROM comps
WHERE event_id = $1 AND name = $2;

-- name: DeleteCompSlots :exec
DELETE FROM comp_slots
WHERE event_id = $1 AND comp_name = $2;

-- name: InsertCompSlot :exec
INSERT INTO comp_slots (event_id, comp_name, character_id, role, slot_index, is_bench, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListCompSlots :many
SELECT * FROM comp_slots
WHERE event_id = $1 AND comp_name = $2
ORDER BY is_bench, slot_index;

-- name: ClearAssignedRoles :exec
-- Locking any comp_name overwrites the whole event's assigned_role, since that
-- column is single-valued while comp_slots holds multiple named drafts.
UPDATE signups SET assigned_role = NULL
WHERE event_id = $1;

-- name: SetSignupAssignedRole :exec
UPDATE signups SET assigned_role = $3
WHERE event_id = $1 AND character_id = $2;
