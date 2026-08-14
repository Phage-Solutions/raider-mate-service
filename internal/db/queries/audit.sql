-- name: InsertCharacterSnapshot :one
INSERT INTO character_snapshots (character_id, ilvl, mplus_score, gear)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetLatestCharacterSnapshot :one
-- captured_at is transaction start time, so it ties for snapshots written in the
-- same transaction; id (UUIDv7) breaks the tie in insertion order.
SELECT * FROM character_snapshots
WHERE character_id = $1
ORDER BY captured_at DESC, id DESC
LIMIT 1;
