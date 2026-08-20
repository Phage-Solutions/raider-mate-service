-- +goose Up
-- The WarcraftLogs report a raid lead attaches after the night, so an event links to
-- what actually happened in it. Nullable and set by hand: nothing here fetches or
-- verifies the report, and a raid that was never logged simply has no URL. The shape is
-- validated in internal/signup before it reaches this column.
ALTER TABLE events ADD COLUMN warcraftlogs_url text;

-- Both event lists read a guild's events ordered by start time, and the past list grows
-- for as long as the guild runs raids. Upcoming stayed small enough not to need this;
-- past does not.
CREATE INDEX events_guild_starts_at_idx ON events (discord_guild_id, starts_at);

-- +goose Down
DROP INDEX events_guild_starts_at_idx;
ALTER TABLE events DROP COLUMN warcraftlogs_url;
