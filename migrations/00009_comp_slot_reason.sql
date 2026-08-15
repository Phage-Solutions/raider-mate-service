-- +goose Up
-- Raid leads ask why Bob got benched days after the comp locked. The reason needs to
-- outlive the lock message, so it lives on the row rather than only in the API
-- response at lock time.
ALTER TABLE comp_slots ADD COLUMN reason text NOT NULL DEFAULT '';
ALTER TABLE comp_slots ALTER COLUMN reason DROP DEFAULT;

-- +goose Down
ALTER TABLE comp_slots DROP COLUMN reason;
