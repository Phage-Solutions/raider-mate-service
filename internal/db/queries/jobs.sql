-- name: ScheduleJob :exec
INSERT INTO scheduled_jobs (event_id, job_type, run_at)
VALUES ($1, $2, $3);

-- name: ClaimDueJobs :many
SELECT * FROM scheduled_jobs
WHERE status = 'PENDING' AND run_at <= now()
ORDER BY run_at
LIMIT sqlc.arg(row_limit)
FOR UPDATE SKIP LOCKED;

-- name: MarkJobSent :exec
UPDATE scheduled_jobs SET status = 'SENT'
WHERE id = $1;

-- name: MarkJobFailed :exec
-- Caller decides PENDING (retry) or FAILED (give up) based on the attempts it read
-- off the claimed row; this just records the attempt.
UPDATE scheduled_jobs SET status = $2, attempts = attempts + 1
WHERE id = $1;

-- name: CancelJobsForEvent :exec
UPDATE scheduled_jobs SET status = 'CANCELED'
WHERE event_id = $1 AND status = 'PENDING';
