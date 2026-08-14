-- +goose Up
-- Named compositions per event ("prog comp" vs "farm comp").
CREATE TABLE comp_slots (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    event_id      uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    comp_name     text NOT NULL,
    character_id  uuid NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    role          role_enum NOT NULL,
    slot_index    smallint NOT NULL,
    is_bench      boolean NOT NULL DEFAULT false,
    UNIQUE (event_id, comp_name, character_id),
    -- slot_index is a position, not display order. Roster and bench number separately.
    UNIQUE (event_id, comp_name, is_bench, slot_index)
);

-- Supports the cascade from characters.
CREATE INDEX comp_slots_character_id ON comp_slots (character_id);

-- +goose Down
DROP TABLE comp_slots;
