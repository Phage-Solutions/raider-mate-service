-- +goose Up
-- Realm and region reached this table exactly as a raider typed them, which is not
-- what Raider.IO's API takes. A realm of "Twisting Nether" or a region of "EU" makes
-- every fetch fail, and a failed fetch deliberately leaves last_synced alone, so those
-- characters sat at NULL forever with nothing but a log line to say why. roster.Register
-- canonicalises both from now on; this fixes the rows already on file.
--
-- The translate() pairs are the same twenty-seven runes as roster.deaccent, in the same
-- order. Blizzard's slugs strip diacritics rather than dropping the letter.
--
-- The NOT EXISTS guard is for the case where a raider registered the same character
-- twice under two spellings of its realm: normalising both would collide on
-- characters_user_id_name_realm_region_key and fail the migration. Those rows keep
-- their broken realm, which is what they had before, and need a person to decide which
-- of the duplicates to keep.
UPDATE characters c SET
    region = lower(btrim(c.region)),
    realm = btrim(
        regexp_replace(
            translate(
                replace(replace(lower(c.realm), '''', ''), '’', ''),
                'áàâäãåéèêëíìîïóòôöõúùûüñçýÿ',
                'aaaaaaeeeeiiiiooooouuuuncyy'
            ),
            '[^a-z0-9]+', '-', 'g'
        ),
        '-'
    )
WHERE NOT EXISTS (
    SELECT 1 FROM characters other
    WHERE other.user_id = c.user_id
      AND other.id <> c.id
      AND other.name = c.name
      AND other.realm = btrim(
            regexp_replace(
                translate(
                    replace(replace(lower(c.realm), '''', ''), '’', ''),
                    'áàâäãåéèêëíìîïóòôöõúùûüñçýÿ',
                    'aaaaaaeeeeiiiiooooouuuuncyy'
                ),
                '[^a-z0-9]+', '-', 'g'
            ),
            '-'
        )
      AND other.region = lower(btrim(c.region))
);

-- Anything this touched has stale cached data by definition: it either never synced or
-- synced under a realm string that is no longer what is stored. Clearing the queue
-- position makes the next worker tick pick them up instead of waiting out
-- SYNC_STALE_AFTER.
UPDATE characters SET sync_attempted_at = NULL WHERE last_synced IS NULL;

-- +goose Down
-- The original strings are gone; there is nothing to restore them from.
SELECT 1;
