-- +goose Up
-- Two bot replicas polling the outbox both saw every undelivered row and both sent,
-- because the ack is a separate HTTP request and no transaction can span it. A claim
-- stamp hands each row to one poller for the length of a lease.
ALTER TABLE notifications ADD COLUMN claimed_at timestamptz;

-- Serves the claim query's predicate: undelivered rows, oldest first.
CREATE INDEX notifications_claimable
    ON notifications (created_at ASC)
    WHERE delivered_at IS NULL;

-- +goose Down
DROP INDEX notifications_claimable;
ALTER TABLE notifications DROP COLUMN claimed_at;
