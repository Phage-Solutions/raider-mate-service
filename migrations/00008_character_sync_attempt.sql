-- +goose Up
-- last_synced means "cached data is current as of". A character whose fetch keeps
-- failing never gets one, so ordering the sync queue by last_synced alone parks it
-- at the head of every batch forever and starves everyone behind it. Recording the
-- attempt separately moves failures to the back.
ALTER TABLE characters ADD COLUMN sync_attempted_at timestamptz;

-- Serves the sync queue's ordering and its staleness filter.
CREATE INDEX characters_sync_attempted_at
    ON characters (sync_attempted_at ASC NULLS FIRST);

-- +goose Down
DROP INDEX characters_sync_attempted_at;
ALTER TABLE characters DROP COLUMN sync_attempted_at;
