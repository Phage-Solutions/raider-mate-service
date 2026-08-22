package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/billing"
	"github.com/Raider-Mate/raider-mate-service/internal/db"
)

// stubStore answers with whatever the test put in it. Every field is the raw shape the
// real Store returns, with no rates filled in: computing those is what these tests are
// about.
type stubStore struct {
	events     int64
	attendance []RaiderAttendance
	roles      []RoleBalance
	bench      []BenchRecord
	coverage   []RoleCoverage
	characters int64
	mains      int64
	active     int64
	throughput []ThroughputWeek
	ilvl       []IlvlWeek
	err        error
}

func (s stubStore) CountEvents(context.Context, int64, Period) (int64, error) {
	return s.events, s.err
}

func (s stubStore) Attendance(context.Context, int64, Period) ([]RaiderAttendance, error) {
	return s.attendance, s.err
}

func (s stubStore) RoleTotals(context.Context, int64, Period) ([]RoleBalance, error) {
	return s.roles, s.err
}

func (s stubStore) BenchRecords(context.Context, int64, Period) ([]BenchRecord, error) {
	return s.bench, s.err
}

func (s stubStore) RoleCoverage(context.Context, int64) ([]RoleCoverage, error) {
	return s.coverage, s.err
}

func (s stubStore) RosterActivity(context.Context, int64, Period) (int64, int64, int64, error) {
	return s.characters, s.mains, s.active, s.err
}

func (s stubStore) Throughput(context.Context, int64, Period) ([]ThroughputWeek, error) {
	return s.throughput, s.err
}

func (s stubStore) IlvlWeeks(context.Context, int64, Period) ([]IlvlWeek, error) {
	return s.ilvl, s.err
}

// stubTiers is the gate, answering the same way for every guild.
type stubTiers struct {
	tier billing.Tier
}

func (s stubTiers) For(context.Context, int64) (billing.Tier, error) {
	return s.tier, nil
}

func (s stubTiers) Require(_ context.Context, _ int64, want billing.Tier) error {
	if want == billing.TierPremium && s.tier != billing.TierPremium {
		return billing.ErrTierRequired
	}
	return nil
}

func free(store stubStore) *Analysis {
	return NewAnalysis(store, stubTiers{tier: billing.TierFree})
}

func premium(store stubStore) *Analysis {
	return NewAnalysis(store, stubTiers{tier: billing.TierPremium})
}

func raider(name string) CharacterRef {
	return CharacterRef{ID: uuid.New(), Name: name, Realm: "Silvermoon"}
}

func TestAttendanceIsFree(t *testing.T) {
	// Ten events. Six confirmed and two late is eight turnouts; one declined and one
	// no-show are answers too, so this raider answered every event and is never silent.
	store := stubStore{
		events: 10,
		attendance: []RaiderAttendance{
			{Character: raider("Grimtusk"), Confirmed: 6, Late: 2, Declined: 1, NoShow: 1},
		},
	}

	got, err := free(store).Attendance(context.Background(), 100)

	if err != nil {
		t.Fatalf("Attendance() error = %v, want nil for a free guild", err)
	}
	if got.Events != 10 {
		t.Errorf("Events = %d, want 10", got.Events)
	}
	r := got.Raiders[0]
	if r.Answered != 10 {
		t.Errorf("Answered = %d, want 10", r.Answered)
	}
	if r.Rate != 0.8 {
		t.Errorf("Rate = %v, want 0.8, since LATE is still turning up", r.Rate)
	}
	if r.Silence != 0 {
		t.Errorf("Silence = %v, want 0: declining is an answer", r.Silence)
	}
}

// The distinction the panel exists for. Somebody who declined every raid in advance is
// not the same problem as somebody who never replied, and the two numbers must part.
func TestAttendanceSeparatesDecliningFromSilence(t *testing.T) {
	store := stubStore{
		events: 10,
		attendance: []RaiderAttendance{
			{Character: raider("Declines"), Declined: 10},
			{Character: raider("Silent"), Confirmed: 1},
		},
	}

	got, _ := free(store).Attendance(context.Background(), 100)

	if got.Raiders[0].Silence != 0 {
		t.Errorf("Silence = %v for a raider who answered everything", got.Raiders[0].Silence)
	}
	if got.Raiders[1].Silence != 0.9 {
		t.Errorf("Silence = %v, want 0.9 for a raider who answered one of ten", got.Raiders[1].Silence)
	}
}

// A guild that ran no raids has no denominator. Every rate is 0, and nothing divides
// by zero on the way there.
func TestAttendanceSurvivesAGuildThatRanNothing(t *testing.T) {
	store := stubStore{events: 0, attendance: []RaiderAttendance{{Character: raider("Nobody")}}}

	got, err := free(store).Attendance(context.Background(), 100)

	if err != nil {
		t.Fatalf("Attendance() error = %v", err)
	}
	if got.Raiders[0].Rate != 0 || got.Raiders[0].Silence != 0 {
		t.Errorf("rates = %v/%v, want 0/0 with no events to divide by",
			got.Raiders[0].Rate, got.Raiders[0].Silence)
	}
}

func TestPremiumPanelsRefuseAFreeGuild(t *testing.T) {
	analysis := free(stubStore{})
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"comp balance", func() error { _, err := analysis.CompBalance(ctx, 100); return err }},
		{"roster health", func() error { _, err := analysis.RosterHealth(ctx, 100); return err }},
		{"throughput", func() error { _, err := analysis.Throughput(ctx, 100); return err }},
		{"ilvl", func() error { _, err := analysis.Ilvl(ctx, 100); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, billing.ErrTierRequired) {
				t.Errorf("error = %v, want ErrTierRequired", err)
			}
		})
	}
}

func TestCompBalanceSharesAndBenchRates(t *testing.T) {
	store := stubStore{
		roles: []RoleBalance{
			{Role: db.RoleEnumTANK, Placed: 2, Benched: 0},
			{Role: db.RoleEnumHEALER, Placed: 4, Benched: 2},
			{Role: db.RoleEnumMDPS, Placed: 14, Benched: 0},
		},
		bench: []BenchRecord{{Character: raider("Warms"), Boards: 8, Benched: 6}},
	}

	got, err := premium(store).CompBalance(context.Background(), 100)

	if err != nil {
		t.Fatalf("CompBalance() error = %v", err)
	}
	if got.Roles[0].Share != 0.1 {
		t.Errorf("tank share = %v, want 0.1 of twenty placed slots", got.Roles[0].Share)
	}
	if got.Roles[1].BenchRate != 2.0/6.0 {
		t.Errorf("healer bench rate = %v, want 2 of 6 appearances", got.Roles[1].BenchRate)
	}
	if got.Bench[0].Rate != 0.75 {
		t.Errorf("bench rate = %v, want 0.75", got.Bench[0].Rate)
	}
}

// Dormant is derived rather than selected, so it cannot disagree with the two numbers
// it sits between.
func TestRosterHealthDerivesDormant(t *testing.T) {
	store := stubStore{characters: 30, mains: 22, active: 18}

	got, err := premium(store).RosterHealth(context.Background(), 100)

	if err != nil {
		t.Fatalf("RosterHealth() error = %v", err)
	}
	if got.Dormant != 12 {
		t.Errorf("Dormant = %d, want 12", got.Dormant)
	}
	if got.Active+got.Dormant != got.Characters {
		t.Errorf("active + dormant = %d, want %d", got.Active+got.Dormant, got.Characters)
	}
}

func TestThroughputRates(t *testing.T) {
	store := stubStore{
		throughput: []ThroughputWeek{
			{Week: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
				Events: 2, Confirmed: 40, NoShow: 4, Placed: 30, Benched: 10},
		},
	}

	got, err := premium(store).Throughput(context.Background(), 100)

	if err != nil {
		t.Fatalf("Throughput() error = %v", err)
	}
	if got.Weeks[0].BenchRate != 0.25 {
		t.Errorf("BenchRate = %v, want 0.25", got.Weeks[0].BenchRate)
	}
	if got.Weeks[0].NoShowRate != 0.1 {
		t.Errorf("NoShowRate = %v, want 0.1", got.Weeks[0].NoShowRate)
	}
	// The bar is a week; the total is what says whether that week was one raid or three.
	if got.Events != 2 {
		t.Errorf("Events = %d, want the window total of 2", got.Events)
	}
}

// Every panel in one response describes the same window, so a raid lead reading two of
// them side by side is not comparing ninety days against something else.
func TestEveryPanelReportsTheSameWidthOfWindow(t *testing.T) {
	analysis := premium(stubStore{})
	ctx := context.Background()

	attendance, _ := analysis.Attendance(ctx, 100)
	balance, _ := analysis.CompBalance(ctx, 100)

	if width := attendance.Period.Until.Sub(attendance.Period.Since); width != Window {
		t.Errorf("attendance window = %v, want %v", width, Window)
	}
	if width := balance.Period.Until.Sub(balance.Period.Since); width != Window {
		t.Errorf("comp balance window = %v, want %v", width, Window)
	}
}

func TestAStoreFailureIsNotAnEmptyPanel(t *testing.T) {
	store := stubStore{err: errors.New("no database")}

	if _, err := free(store).Attendance(context.Background(), 100); err == nil {
		t.Error("Attendance() error = nil, want the store failure surfaced")
	}
}
