-- +goose Up
-- Raider.IO character lookup requires a region, and realm names repeat across
-- regions, so the old (user_id, name, realm) uniqueness is not enough on its own.
-- The default only backfills existing rows; it is dropped straight away so every
-- insert has to name a region rather than silently landing in eu.
ALTER TABLE characters ADD COLUMN region text NOT NULL DEFAULT 'eu';
ALTER TABLE characters ALTER COLUMN region DROP DEFAULT;

ALTER TABLE characters DROP CONSTRAINT characters_user_id_name_realm_key;
ALTER TABLE characters ADD CONSTRAINT characters_user_id_name_realm_region_key
    UNIQUE (user_id, name, realm, region);

-- +goose Down
ALTER TABLE characters DROP CONSTRAINT characters_user_id_name_realm_region_key;
ALTER TABLE characters ADD CONSTRAINT characters_user_id_name_realm_key
    UNIQUE (user_id, name, realm);

ALTER TABLE characters DROP COLUMN region;
