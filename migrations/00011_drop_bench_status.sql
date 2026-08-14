-- +goose Up
-- Bench membership moved to comp_slots.is_bench, decided fresh by every lock.
-- Leaving BENCH in signup_status keeps a second, stale source of truth that the
-- assigner cannot see: ListAssignmentPoolForEvent filters to CONFIRMED and LATE, so
-- a signup still carrying BENCH would drop out of every future lock permanently.
-- Postgres cannot remove a value from an enum, so the type is rebuilt.
UPDATE signups SET status = 'CONFIRMED' WHERE status = 'BENCH';

ALTER TYPE signup_status RENAME TO signup_status_old;
CREATE TYPE signup_status AS ENUM (
    'CONFIRMED', 'TENTATIVE', 'DECLINED', 'LATE', 'ABSENT', 'NO_SHOW'
);
ALTER TABLE signups
    ALTER COLUMN status TYPE signup_status USING status::text::signup_status;
DROP TYPE signup_status_old;

-- +goose Down
ALTER TYPE signup_status RENAME TO signup_status_new;
CREATE TYPE signup_status AS ENUM (
    'CONFIRMED', 'TENTATIVE', 'DECLINED', 'LATE', 'BENCH', 'ABSENT', 'NO_SHOW'
);
ALTER TABLE signups
    ALTER COLUMN status TYPE signup_status USING status::text::signup_status;
DROP TYPE signup_status_new;
