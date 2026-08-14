-- +goose Up
CREATE TABLE scheduled_jobs (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    event_id    uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    job_type    job_enum NOT NULL,
    run_at      timestamptz NOT NULL,
    status      job_status NOT NULL DEFAULT 'PENDING',
    attempts    smallint NOT NULL DEFAULT 0
);

-- Shaped for `WHERE status = 'PENDING' AND run_at <= now()` with FOR UPDATE SKIP LOCKED.
CREATE INDEX scheduled_jobs_pending_run_at
    ON scheduled_jobs (status, run_at)
    WHERE status = 'PENDING';

-- Supports the cascade from events; the partial index above cannot serve it.
CREATE INDEX scheduled_jobs_event_id ON scheduled_jobs (event_id);

-- +goose Down
DROP TABLE scheduled_jobs;
