-- +goose Up
-- Tier, at last. Everything in the schema until now was captured for every guild
-- regardless of what they pay, which is deliberate (see the comment on
-- character_snapshots): exposure happens at the read side. This is the read side's
-- source of truth.
--
-- A guild with no row is FREE. That is the whole default, and it means nothing has to
-- be written when a guild adds the bot, and nothing has to be backfilled here.
CREATE TYPE tier_enum AS ENUM ('FREE', 'PREMIUM');

-- Monthly and yearly are the two prices the product quotes. Nullable because a guild
-- that has never subscribed is not on a billing period.
CREATE TYPE billing_period_enum AS ENUM ('MONTHLY', 'YEARLY');

-- The provider's lifecycle, not ours. PAST_DUE and CANCELED both read as FREE, but
-- they are different conversations to have with a guild, so they stay distinct.
CREATE TYPE sub_status_enum AS ENUM ('ACTIVE', 'PAST_DUE', 'CANCELED', 'TRIALING');

-- provider_sub_id is the payment provider's identifier, kept so a webhook can find
-- the row it is about. Nothing writes it yet: there is no billing integration, and
-- rows are set by hand until there is.
--
-- price_locked_at records what an early subscriber was promised, so a later price rise
-- does not silently apply to them. NULL means the current list price.
CREATE TABLE subscriptions (
    id                  uuid PRIMARY KEY,
    discord_guild_id    bigint NOT NULL UNIQUE,
    tier                tier_enum NOT NULL DEFAULT 'FREE',
    billing_period      billing_period_enum,
    provider_sub_id     text,
    status              sub_status_enum NOT NULL DEFAULT 'ACTIVE',
    price_locked_at     numeric,
    current_period_end  timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- Every analysis read starts by asking this table what a guild is, so the lookup is
-- by guild and the UNIQUE constraint above already serves it. No second index.

-- Reads the whole guild's snapshot history rather than one character's latest, which
-- the existing (character_id, captured_at DESC) index cannot serve: the guild filter
-- lives on characters, so the planner walks snapshots by time and joins back.
CREATE INDEX character_snapshots_captured_at ON character_snapshots (captured_at DESC);

-- +goose Down
DROP INDEX character_snapshots_captured_at;
DROP TABLE subscriptions;
DROP TYPE sub_status_enum;
DROP TYPE billing_period_enum;
DROP TYPE tier_enum;
