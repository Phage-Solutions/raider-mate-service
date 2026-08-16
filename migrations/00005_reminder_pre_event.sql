-- +goose Up
-- The pre-event reminder stops being an hour. It becomes a lead time a raid lead sets
-- per event, so the enum value that named the hour no longer describes the job.
-- Renaming rather than adding a second value keeps every PENDING row pointing at
-- something the worker still handles: an ADD VALUE would need a backfill and would
-- leave a window where a scheduled job named a value nothing resolves.
ALTER TYPE job_enum RENAME VALUE 'REMINDER_1H' TO 'REMINDER_PRE_EVENT';
ALTER TYPE notification_kind RENAME VALUE 'REMINDER_1H' TO 'REMINDER_PRE_EVENT';

-- CHANNEL is a message posted in a channel that mentions named users, as opposed to
-- ROLE, which mentions roles. The reminder pings whoever signed up, and a user id
-- rendered with the role syntax pings nobody.
--
-- Only the value is added here. Postgres refuses to use a new enum value in the
-- transaction that created it, and goose runs a migration in one, so the constraint
-- that reads it lives in 00006.
ALTER TYPE notification_target ADD VALUE 'CHANNEL';

-- +goose Down
ALTER TYPE notification_kind RENAME VALUE 'REMINDER_PRE_EVENT' TO 'REMINDER_1H';
ALTER TYPE job_enum RENAME VALUE 'REMINDER_PRE_EVENT' TO 'REMINDER_1H';
-- Postgres cannot drop an enum value. Leaving CHANNEL is harmless once nothing writes it.
