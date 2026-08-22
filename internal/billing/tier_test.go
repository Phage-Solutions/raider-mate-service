package billing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func at(t *testing.T, s string) time.Time {
	t.Helper()
	// Pinned rather than derived from time.Now(): a relative offset makes the test pass
	// or fail on how long the suite took to reach it.
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return parsed
}

func TestEffectiveTier(t *testing.T) {
	now := at(t, "2026-08-22T12:00:00Z")
	future := at(t, "2026-09-22T12:00:00Z")
	past := at(t, "2026-07-22T12:00:00Z")

	tests := []struct {
		name string
		sub  Subscription
		want Tier
	}{
		{
			name: "a guild that never subscribed",
			sub:  Subscription{},
			want: TierFree,
		},
		{
			name: "premium, active, paid through next month",
			sub:  Subscription{Tier: TierPremium, Status: StatusActive, CurrentPeriodEnd: &future},
			want: TierPremium,
		},
		{
			name: "premium, active, open ended",
			sub:  Subscription{Tier: TierPremium, Status: StatusActive},
			want: TierPremium,
		},
		{
			name: "trialing counts, because a trial is the product working",
			sub:  Subscription{Tier: TierPremium, Status: StatusTrialing, CurrentPeriodEnd: &future},
			want: TierPremium,
		},
		{
			name: "past due reads free while the card is chased",
			sub:  Subscription{Tier: TierPremium, Status: StatusPastDue, CurrentPeriodEnd: &future},
			want: TierFree,
		},
		{
			name: "canceled reads free",
			sub:  Subscription{Tier: TierPremium, Status: StatusCanceled, CurrentPeriodEnd: &future},
			want: TierFree,
		},
		{
			// The shape a missed webhook leaves behind. Reading it as Premium keeps
			// giving the paid views away after the guild stopped paying for them.
			name: "premium and active but the paid period ran out",
			sub:  Subscription{Tier: TierPremium, Status: StatusActive, CurrentPeriodEnd: &past},
			want: TierFree,
		},
		{
			name: "the period ends exactly now, which is over",
			sub:  Subscription{Tier: TierPremium, Status: StatusActive, CurrentPeriodEnd: &now},
			want: TierFree,
		},
		{
			name: "a free row is free however healthy it looks",
			sub:  Subscription{Tier: TierFree, Status: StatusActive, CurrentPeriodEnd: &future},
			want: TierFree,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sub.Effective(now); got != tt.want {
				t.Errorf("Effective() = %q, want %q", got, tt.want)
			}
		})
	}
}

// stubStore answers with one subscription, or with none.
type stubStore struct {
	sub   Subscription
	found bool
	err   error
}

func (s stubStore) Subscription(context.Context, int64) (Subscription, bool, error) {
	return s.sub, s.found, s.err
}

func TestRequireRefusesAFreeGuild(t *testing.T) {
	tiers := NewTiers(stubStore{})

	err := tiers.Require(context.Background(), 100, TierPremium)

	if !errors.Is(err, ErrTierRequired) {
		t.Errorf("Require() = %v, want ErrTierRequired", err)
	}
}

func TestRequireAdmitsAPremiumGuild(t *testing.T) {
	tiers := NewTiers(stubStore{sub: Subscription{Tier: TierPremium, Status: StatusActive}, found: true})

	if err := tiers.Require(context.Background(), 100, TierPremium); err != nil {
		t.Errorf("Require() = %v, want nil", err)
	}
}

// Free is what every guild holds, so requiring it is not a gate and must never refuse.
func TestRequireFreeAdmitsEveryone(t *testing.T) {
	tiers := NewTiers(stubStore{})

	if err := tiers.Require(context.Background(), 100, TierFree); err != nil {
		t.Errorf("Require(TierFree) = %v, want nil", err)
	}
}

// A store that cannot answer must not read as Premium, and must not read as a clean
// FREE either: the caller has to see that the question could not be put.
func TestForReportsAStoreFailure(t *testing.T) {
	tiers := NewTiers(stubStore{err: errors.New("no database")})

	tier, err := tiers.For(context.Background(), 100)

	if err == nil {
		t.Fatal("For() error = nil, want the store failure")
	}
	if tier == TierPremium {
		t.Errorf("For() = %q, want anything but Premium on a failed read", tier)
	}
}
