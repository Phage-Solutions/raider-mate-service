-- +goose Up
-- Normal and Heroic raids are flex (10-30). Mythic raids are locked to exactly 20
-- and cannot zone in at any other size, so the assigner needs to tell Mythic apart
-- from flex rather than treating every raid the same. NULL for Mythic+ events,
-- which have no difficulty concept of their own.
CREATE TYPE raid_difficulty AS ENUM ('NORMAL', 'HEROIC', 'MYTHIC');

ALTER TABLE events ADD COLUMN difficulty raid_difficulty;

-- +goose Down
ALTER TABLE events DROP COLUMN difficulty;
DROP TYPE raid_difficulty;
