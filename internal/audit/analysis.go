// Package audit reads the guild's own history back to it: who turned up, what got
// fielded, how the roster is holding, and where the gear went. It computes; it never
// stores. Everything it reads was captured for every guild regardless of tier, and the
// gate is here, on the way out.
package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/billing"
	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Window is how far back every panel looks. One number, applied to all of them, so a
// raid lead reading two panels side by side is reading the same stretch of time twice
// rather than comparing ninety days against all time.
//
// Ninety days is roughly a raid tier. Shorter than that and a guild raiding twice a
// week has too few events for an attendance rate to mean anything.
const Window = 90 * 24 * time.Hour

// Period is the closed-open interval a request describes. Both ends are carried
// explicitly rather than left to now() inside a query, so every panel in one response
// agrees on where the window ends, and so a test can pin it.
type Period struct {
	Since time.Time
	Until time.Time
}

// periodEndingAt builds the window backwards from a moment.
func periodEndingAt(now time.Time) Period {
	return Period{Since: now.Add(-Window), Until: now}
}

// CharacterRef is who a row is about. Enough to render a name and colour it by class,
// and no more: this is analysis, not a second roster endpoint.
type CharacterRef struct {
	ID    uuid.UUID
	Name  string
	Realm string
	Class *string
}

// Attendance is the free panel. Raw counts per raider against the number of events the
// guild actually ran, which is the "basic attendance, per-event, raw percentage" the
// product has always described as free.
type Attendance struct {
	Period Period
	// Events is the denominator: how many raids happened, not how many people answered.
	Events  int64
	Raiders []RaiderAttendance
}

// RaiderAttendance is one raider's answers over the window. The statuses stay split
// rather than summed, because DECLINED and NO_SHOW are the two a raid lead most needs
// to tell apart and a single "missed" number destroys exactly that distinction.
type RaiderAttendance struct {
	Character CharacterRef
	Confirmed int64
	Tentative int64
	Declined  int64
	Late      int64
	Absent    int64
	NoShow    int64
	// Answered is how many of the guild's events this raider replied to at all.
	Answered int64
	// Rate is turnout over events in the window, 0 to 1. LATE counts: somebody who
	// said they would be twenty minutes behind and was, turned up.
	Rate float64
	// Silence is events the raider never answered, over events in the window. Not the
	// complement of Rate: declining is an answer, and a guild that cannot tell the two
	// apart ends up chasing the wrong people.
	Silence float64
}

// CompBalance is what the guild fielded, by role and by person.
type CompBalance struct {
	Period Period
	Roles  []RoleBalance
	// Bench is ordered by how often somebody sat out, longest-suffering first.
	Bench []BenchRecord
}

// RoleBalance is one role's share of the boards built in the window.
type RoleBalance struct {
	Role    db.RoleEnum
	Placed  int64
	Benched int64
	// Share is this role's placed slots over all placed slots, 0 to 1.
	Share float64
	// BenchRate is how often this role ended up on the bench when it was on a board.
	BenchRate float64
}

// BenchRecord is the bench fairness tracker, one row per raider who was on a board.
type BenchRecord struct {
	Character CharacterRef
	// Boards is how many comps this raider appeared on, bench included.
	Boards  int64
	Benched int64
	// Rate is Benched over Boards, 0 to 1.
	Rate float64
}

// RosterHealth is the roster's shape and how much of it is still turning up.
type RosterHealth struct {
	Period     Period
	Characters int64
	Mains      int64
	// Active answered at least one event in the window. Dormant is the rest, and the
	// two always sum to Characters because Dormant is derived, never selected.
	Active   int64
	Dormant  int64
	Coverage []RoleCoverage
}

// RoleCoverage is what the roster can play, from the roles on the characters rather
// than from what a comp happened to use.
type RoleCoverage struct {
	Role       db.RoleEnum
	Characters int64
	// FirstChoice is how many of them put this role at the top of their menu. A guild
	// with nine healers who would all rather be dps does not have nine healers.
	FirstChoice int64
}

// Throughput is the raid week, weekly.
type Throughput struct {
	Period Period
	// Events is every raid in the window. A week's bar covers however many raids that
	// week held, so the total is what tells a reader whether they are looking at one
	// raid night or three.
	Events int64
	Weeks  []ThroughputWeek
}

// ThroughputWeek is one week's raids and what happened in them. Weeks with no events
// are absent rather than zero-filled: a guild that skipped a week did not run zero
// raids badly, it took a week off, and a chart should show the gap.
//
// Every count here is people, not rows. A raider who confirmed for both of the week's
// raids is one confirmed raider, because the alternative reports more people than the
// guild has.
type ThroughputWeek struct {
	Week      time.Time
	Events    int64
	Confirmed int64
	Declined  int64
	NoShow    int64
	Placed    int64
	Benched   int64
	// BenchRate is benched slots over all slots on the week's boards, 0 to 1.
	BenchRate float64
	// NoShowRate is no-shows over people who said they were coming, 0 to 1. This is the
	// number that costs a raid its pull time.
	NoShowRate float64
}

// IlvlSeries is the gear curve, over registered mains. The first read of
// character_snapshots that is not "the latest one for this character".
type IlvlSeries struct {
	Period Period
	Weeks  []IlvlWeek
}

// IlvlWeek is the roster's gear in one week: a median to follow, and the middle half of
// the raid around it. P25 to P75 rather than lowest to highest, because a roster holds
// abandoned alts and one of them sets the floor at whatever it was when it was
// abandoned; the quartiles describe the raid, and their distance apart is the gear gap.
type IlvlWeek struct {
	Week       time.Time
	Characters int64
	P25        float64
	Median     float64
	P75        float64
}

// analysisStore is the persistence Analysis needs. Declared here, by the consumer.
// Every method returns raw counts; the derived rates are this package's job, so there
// is one place a percentage is computed and it is not in SQL.
type analysisStore interface {
	CountEvents(ctx context.Context, discordGuildID int64, p Period) (int64, error)
	Attendance(ctx context.Context, discordGuildID int64, p Period) ([]RaiderAttendance, error)
	RoleTotals(ctx context.Context, discordGuildID int64, p Period) ([]RoleBalance, error)
	BenchRecords(ctx context.Context, discordGuildID int64, p Period) ([]BenchRecord, error)
	RoleCoverage(ctx context.Context, discordGuildID int64) ([]RoleCoverage, error)
	RosterActivity(ctx context.Context, discordGuildID int64, p Period) (characters, mains, active int64, err error)
	Throughput(ctx context.Context, discordGuildID int64, p Period) ([]ThroughputWeek, error)
	IlvlWeeks(ctx context.Context, discordGuildID int64, p Period) ([]IlvlWeek, error)
}

// tierGate is the tier check Analysis needs. Declared here so this package depends on
// the decision and not on how it is stored.
type tierGate interface {
	Require(ctx context.Context, discordGuildID int64, want billing.Tier) error
	For(ctx context.Context, discordGuildID int64) (billing.Tier, error)
}

// Analysis is the read side of the guild's history. Every gated method opens with the
// gate and does nothing else about tiers (hard rule 1); handlers map the error and
// never compare a tier themselves.
type Analysis struct {
	store analysisStore
	tiers tierGate
}

// NewAnalysis builds an Analysis.
func NewAnalysis(store analysisStore, tiers tierGate) *Analysis {
	return &Analysis{store: store, tiers: tiers}
}

// Tier reports what the guild may read, for building the link set that says so.
func (a *Analysis) Tier(ctx context.Context, discordGuildID int64) (billing.Tier, error) {
	return a.tiers.For(ctx, discordGuildID)
}

// Attendance is free for every guild.
func (a *Analysis) Attendance(ctx context.Context, discordGuildID int64) (Attendance, error) {
	period := periodEndingAt(time.Now())

	events, err := a.store.CountEvents(ctx, discordGuildID, period)
	if err != nil {
		return Attendance{}, fmt.Errorf("counting events: %w", err)
	}
	raiders, err := a.store.Attendance(ctx, discordGuildID, period)
	if err != nil {
		return Attendance{}, fmt.Errorf("reading attendance: %w", err)
	}

	for i := range raiders {
		r := &raiders[i]
		r.Answered = r.Confirmed + r.Tentative + r.Declined + r.Late + r.Absent + r.NoShow
		r.Rate = ratio(r.Confirmed+r.Late, events)
		r.Silence = ratio(events-r.Answered, events)
	}

	return Attendance{Period: period, Events: events, Raiders: raiders}, nil
}

// CompBalance is Premium.
func (a *Analysis) CompBalance(ctx context.Context, discordGuildID int64) (CompBalance, error) {
	if err := a.tiers.Require(ctx, discordGuildID, billing.TierPremium); err != nil {
		return CompBalance{}, err
	}
	period := periodEndingAt(time.Now())

	roles, err := a.store.RoleTotals(ctx, discordGuildID, period)
	if err != nil {
		return CompBalance{}, fmt.Errorf("reading role totals: %w", err)
	}
	bench, err := a.store.BenchRecords(ctx, discordGuildID, period)
	if err != nil {
		return CompBalance{}, fmt.Errorf("reading bench records: %w", err)
	}

	var placed int64
	for _, role := range roles {
		placed += role.Placed
	}
	for i := range roles {
		r := &roles[i]
		r.Share = ratio(r.Placed, placed)
		r.BenchRate = ratio(r.Benched, r.Placed+r.Benched)
	}
	for i := range bench {
		b := &bench[i]
		b.Rate = ratio(b.Benched, b.Boards)
	}

	return CompBalance{Period: period, Roles: roles, Bench: bench}, nil
}

// RosterHealth is Premium.
func (a *Analysis) RosterHealth(ctx context.Context, discordGuildID int64) (RosterHealth, error) {
	if err := a.tiers.Require(ctx, discordGuildID, billing.TierPremium); err != nil {
		return RosterHealth{}, err
	}
	period := periodEndingAt(time.Now())

	characters, mains, active, err := a.store.RosterActivity(ctx, discordGuildID, period)
	if err != nil {
		return RosterHealth{}, fmt.Errorf("reading roster activity: %w", err)
	}
	coverage, err := a.store.RoleCoverage(ctx, discordGuildID)
	if err != nil {
		return RosterHealth{}, fmt.Errorf("reading role coverage: %w", err)
	}

	return RosterHealth{
		Period:     period,
		Characters: characters,
		Mains:      mains,
		Active:     active,
		Dormant:    characters - active,
		Coverage:   coverage,
	}, nil
}

// Throughput is Premium.
func (a *Analysis) Throughput(ctx context.Context, discordGuildID int64) (Throughput, error) {
	if err := a.tiers.Require(ctx, discordGuildID, billing.TierPremium); err != nil {
		return Throughput{}, err
	}
	period := periodEndingAt(time.Now())

	weeks, err := a.store.Throughput(ctx, discordGuildID, period)
	if err != nil {
		return Throughput{}, fmt.Errorf("reading throughput: %w", err)
	}

	var events int64
	for i := range weeks {
		w := &weeks[i]
		events += w.Events
		w.BenchRate = ratio(w.Benched, w.Placed+w.Benched)
		w.NoShowRate = ratio(w.NoShow, w.Confirmed)
	}

	return Throughput{Period: period, Events: events, Weeks: weeks}, nil
}

// Ilvl is Premium.
func (a *Analysis) Ilvl(ctx context.Context, discordGuildID int64) (IlvlSeries, error) {
	if err := a.tiers.Require(ctx, discordGuildID, billing.TierPremium); err != nil {
		return IlvlSeries{}, err
	}
	period := periodEndingAt(time.Now())

	weeks, err := a.store.IlvlWeeks(ctx, discordGuildID, period)
	if err != nil {
		return IlvlSeries{}, fmt.Errorf("reading ilvl series: %w", err)
	}

	return IlvlSeries{Period: period, Weeks: weeks}, nil
}

// ratio divides and reports 0 for an empty denominator, which is the honest answer for
// a guild that ran no raids. A negative numerator clamps to 0: Silence subtracts
// answers from events, and a raider who answered an event that later moved out of the
// window would otherwise be reported as negatively silent.
func ratio(part, whole int64) float64 {
	if whole <= 0 || part <= 0 {
		return 0
	}
	if part > whole {
		return 1
	}
	return float64(part) / float64(whole)
}
