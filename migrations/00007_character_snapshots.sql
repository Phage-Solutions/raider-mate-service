-- +goose Up
-- Time-series. Captured for every guild regardless of tier; exposure to premium
-- analytics happens at the read side, not the write side.
CREATE TABLE character_snapshots (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    character_id  uuid NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    captured_at   timestamptz NOT NULL DEFAULT now(),
    ilvl          numeric,
    mplus_score   numeric,
    gear          jsonb NOT NULL
);

-- Serves both the "latest snapshot per character" read and the cascade from
-- characters, so no separate character_id-only index is needed.
CREATE INDEX character_snapshots_character_id_captured_at
    ON character_snapshots (character_id, captured_at DESC);

-- +goose Down
DROP TABLE character_snapshots;
