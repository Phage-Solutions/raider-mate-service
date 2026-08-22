-- name: GetSubscription :one
-- No row is not an error to the caller: internal/billing reads pgx.ErrNoRows as FREE,
-- which is what a guild that has never subscribed is.
SELECT * FROM subscriptions
WHERE discord_guild_id = $1;

-- name: UpsertSubscription :one
-- No HTTP route writes this yet. It exists so the tier can be set from a migration,
-- a console, or the billing webhooks that come later, without a second shape to keep
-- in step with the one internal/billing reads.
INSERT INTO subscriptions (
    id, discord_guild_id, tier, billing_period, provider_sub_id, status,
    price_locked_at, current_period_end
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (discord_guild_id) DO UPDATE SET
    tier               = excluded.tier,
    billing_period     = excluded.billing_period,
    provider_sub_id    = excluded.provider_sub_id,
    status             = excluded.status,
    price_locked_at    = excluded.price_locked_at,
    current_period_end = excluded.current_period_end,
    updated_at         = now()
RETURNING *;
