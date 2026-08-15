-- +goose Up
-- The zone a guild types its raid times in. Discord exposes no timezone for a guild or
-- a user, so a bot parsing "tomorrow 20:00" has nothing to resolve it against and would
-- have to guess. A guild says once, and every later time entry is unambiguous.
--
-- An IANA name (Europe/Bratislava), not a fixed offset, because a raid schedule
-- outlives a daylight saving change and "+02:00" silently becomes wrong in October.
-- Nullable: a guild that has not set one must still be able to create events, by
-- writing an explicit offset each time.
ALTER TABLE guild_settings ADD COLUMN timezone text;

-- +goose Down
ALTER TABLE guild_settings DROP COLUMN timezone;
