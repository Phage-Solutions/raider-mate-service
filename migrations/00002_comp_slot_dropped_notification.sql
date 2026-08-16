-- +goose Up
-- A raider who leaves the assignment pool after the comp is locked has their comp
-- slots deleted with the write. Without a notification the raid lead learns about the
-- hole by re-reading the roster, which is exactly what they stopped doing once the
-- comp was locked.
--
-- Only the value is added here. Postgres refuses to use a new enum value in the
-- transaction that created it, and goose runs a migration in one.
ALTER TYPE notification_kind ADD VALUE 'COMP_SLOT_DROPPED';

-- +goose Down
-- Postgres cannot drop an enum value. Leaving it is harmless once the emitting query
-- is gone.
SELECT 1;
