// Package billing owns what a guild pays for and, through that, what the read side is
// allowed to return. It holds no prices and talks to no payment provider yet: rows are
// set by hand until there is a billing integration to set them.
package billing

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Tier is what a guild reads as right now. Two values, because the product sells two.
type Tier string

const (
	TierFree    Tier = "FREE"
	TierPremium Tier = "PREMIUM"
)

// Status is the payment provider's lifecycle for a subscription. PAST_DUE and CANCELED
// both read as FREE, but a guild whose card bounced and a guild that quit are different
// conversations, so the distinction survives into here.
type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusPastDue  Status = "PAST_DUE"
	StatusCanceled Status = "CANCELED"
	StatusTrialing Status = "TRIALING"
)

// ErrTierRequired means the guild does not hold the tier this read needs. The data is
// still there and is not touched: a lapse hides, it never deletes (hard rule 4).
var ErrTierRequired = errors.New("premium tier required")

// Subscription is what the guild bought, as far as tier gating cares. Prices, billing
// periods and the provider's own identifiers live in the table and stay there: nothing
// that decides what to return needs them.
type Subscription struct {
	Tier   Tier
	Status Status
	// CurrentPeriodEnd is what the guild has already paid through. Nil means open
	// ended, which is what a hand-set row and a trialing guild both look like.
	CurrentPeriodEnd *time.Time
}

// Effective reports the tier this subscription actually grants at now.
//
// Three things have to hold at once, and it is worth being explicit about why rather
// than collapsing them: the guild bought Premium, the subscription is in a state that
// is being honoured, and the period it paid for has not run out. A row left at
// PREMIUM/ACTIVE with a period end last month is the shape a missed webhook leaves
// behind, and reading it as Premium would keep giving away the paid views for free.
func (s Subscription) Effective(now time.Time) Tier {
	if s.Tier != TierPremium {
		return TierFree
	}
	if s.Status != StatusActive && s.Status != StatusTrialing {
		return TierFree
	}
	if s.CurrentPeriodEnd != nil && !now.Before(*s.CurrentPeriodEnd) {
		return TierFree
	}
	return TierPremium
}

// tierStore is the persistence Tiers needs. Declared here, by the consumer.
type tierStore interface {
	// Subscription returns the guild's row. found is false for a guild that has never
	// subscribed, which is not an error: it is what every guild starts as.
	Subscription(ctx context.Context, discordGuildID int64) (sub Subscription, found bool, err error)
}

// Tiers answers what a guild may read. It is the only place that decides, so a handler
// never compares a tier itself (hard rule 1).
type Tiers struct {
	store tierStore
}

// NewTiers builds a Tiers.
func NewTiers(store tierStore) *Tiers {
	return &Tiers{store: store}
}

// For returns a guild's effective tier. A guild with no subscription row is FREE, and
// that is the default the schema deliberately leaves unwritten.
func (t *Tiers) For(ctx context.Context, discordGuildID int64) (Tier, error) {
	sub, found, err := t.store.Subscription(ctx, discordGuildID)
	if err != nil {
		return TierFree, fmt.Errorf("reading subscription: %w", err)
	}
	if !found {
		return TierFree, nil
	}
	return sub.Effective(time.Now()), nil
}

// Require reports whether a guild holds want, returning ErrTierRequired when it does
// not. Callers wrap the read they were about to do in this, rather than branching on
// For, so there is one sentence to map to a status code.
func (t *Tiers) Require(ctx context.Context, discordGuildID int64, want Tier) error {
	have, err := t.For(ctx, discordGuildID)
	if err != nil {
		return err
	}
	if want == TierPremium && have != TierPremium {
		return ErrTierRequired
	}
	return nil
}
