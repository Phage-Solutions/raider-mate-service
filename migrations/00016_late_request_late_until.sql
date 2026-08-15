-- +goose Up
-- A LATE signup without late_until is "I'll be late" with no idea how late, which is
-- the one thing that makes the status actionable. The request has to carry it or
-- approving one silently drops it.
ALTER TABLE late_signup_requests ADD COLUMN late_until timestamptz;

-- +goose Down
ALTER TABLE late_signup_requests DROP COLUMN late_until;
