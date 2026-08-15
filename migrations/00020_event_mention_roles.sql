-- +goose Up
-- Which Discord roles get pinged when an event message is posted, typically the ones a
-- guild uses for raiders and trials.
--
-- An array on the settings row rather than a join table like guild_raid_lead_roles,
-- which is the older precedent for a role set. The difference is how it is read: the
-- bot already loads guild_settings on every event creation to find the channel, so
-- keeping this beside it costs no extra query, and nothing ever needs to look up a
-- guild by one of these role ids.
--
-- NOT NULL with an empty default, so "ping nobody" is a real state a guild can be in
-- rather than a null to check for at every use.
ALTER TABLE guild_settings
    ADD COLUMN event_mention_role_ids bigint[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE guild_settings DROP COLUMN event_mention_role_ids;
