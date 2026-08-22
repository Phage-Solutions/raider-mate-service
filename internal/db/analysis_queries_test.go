//go:build integration

package db

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// The window every test here uses. Fixed rather than relative to time.Now(), so a slow
// suite cannot move an event out of the window it was seeded into.
var (
	windowUntil = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	windowSince = windowUntil.Add(-90 * 24 * time.Hour)
)

// seedRaid creates one past event in guild 100 at the given offset back from the end
// of the window.
func seedRaid(ctx context.Context, t *testing.T, q *Queries, title string, daysAgo int) Event {
	t.Helper()

	startsAt := windowUntil.AddDate(0, 0, -daysAgo)
	event, err := q.CreateEvent(ctx, CreateEventParams{
		ID:             NewID(),
		DiscordGuildID: 100,
		Type:           EventTypeRAID,
		Title:          title,
		StartsAt:       dbTimestamptz(startsAt),
		SignupDeadline: dbTimestamptz(startsAt.Add(-24 * time.Hour)),
		CompTemplate:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("creating event %q: %v", title, err)
	}
	return event
}

func analysisWindow(guildID int64) (int64, pgtype.Timestamptz, pgtype.Timestamptz) {
	return guildID, dbTimestamptz(windowSince), dbTimestamptz(windowUntil)
}

func seatOn(ctx context.Context, t *testing.T, q *Queries, event Event, characterID uuid.UUID, role RoleEnum, index int16, bench bool) {
	t.Helper()

	if _, err := q.UpsertComp(ctx, UpsertCompParams{
		ID: NewID(), EventID: event.ID, Name: "prog", Mode: CompModeAUTO,
	}); err != nil {
		t.Fatalf("creating comp: %v", err)
	}
	if err := q.InsertCompSlot(ctx, InsertCompSlotParams{
		ID: NewID(), EventID: event.ID, CompName: "prog", CharacterID: characterID,
		Role: role, SlotIndex: index, IsBench: bench, Reason: "seeded",
	}); err != nil {
		t.Fatalf("inserting comp slot: %v", err)
	}
}

func TestCountEventsInWindowExcludesWhatFellOutOfIt(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	seedRaid(ctx, t, q, "Recent", 7)
	seedRaid(ctx, t, q, "Also recent", 30)
	seedRaid(ctx, t, q, "Last tier", 200)
	// Scheduled, not run. The window is closed at the top for exactly this: a raid
	// nobody has attended yet must not dilute an attendance rate.
	seedRaid(ctx, t, q, "Next week", -7)

	guild, since, until := analysisWindow(100)
	count, err := q.CountEventsInWindow(ctx, CountEventsInWindowParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("counting events: %v", err)
	}
	if count != 2 {
		t.Errorf("events = %d, want 2", count)
	}
}

func TestAttendanceByCharacterSplitsTheStatuses(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	_, character := seedUserAndCharacter(ctx, t, q, 1, "Grimtusk")
	first := seedRaid(ctx, t, q, "One", 7)
	second := seedRaid(ctx, t, q, "Two", 14)
	third := seedRaid(ctx, t, q, "Three", 21)
	signUpAs(ctx, t, q, first.ID, character.ID, SignupStatusCONFIRMED)
	signUpAs(ctx, t, q, second.ID, character.ID, SignupStatusDECLINED)
	signUpAs(ctx, t, q, third.ID, character.ID, SignupStatusNOSHOW)

	guild, since, until := analysisWindow(100)
	rows, err := q.AttendanceByCharacter(ctx, AttendanceByCharacterParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("reading attendance: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want one row for one character", len(rows))
	}
	got := rows[0]
	if got.Confirmed != 1 || got.Declined != 1 || got.NoShow != 1 {
		t.Errorf("counts = %+v, want one of each", got)
	}
	// The whole reason the statuses stay split: these two are not the same problem.
	if got.Declined == 0 || got.NoShow == 0 {
		t.Error("declined and no-show collapsed into one another")
	}
}

func TestAttendanceByCharacterIsScopedToOneGuild(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	_, character := seedUserAndCharacter(ctx, t, q, 1, "Grimtusk")
	ours := seedRaid(ctx, t, q, "Ours", 7)
	signUpAs(ctx, t, q, ours.ID, character.ID, SignupStatusCONFIRMED)

	// Same character, a raid in a guild the actor is not in. It must not appear.
	theirs, err := q.CreateEvent(ctx, CreateEventParams{
		ID: NewID(), DiscordGuildID: 200, Type: EventTypeRAID, Title: "Theirs",
		StartsAt:       dbTimestamptz(windowUntil.AddDate(0, 0, -7)),
		SignupDeadline: dbTimestamptz(windowUntil.AddDate(0, 0, -8)),
		CompTemplate:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("creating foreign event: %v", err)
	}
	signUpAs(ctx, t, q, theirs.ID, character.ID, SignupStatusCONFIRMED)

	guild, since, until := analysisWindow(100)
	rows, _ := q.AttendanceByCharacter(ctx, AttendanceByCharacterParams{GuildID: guild, Since: since, Until: until})

	if len(rows) != 1 || rows[0].Confirmed != 1 {
		t.Errorf("rows = %+v, want one confirmed in guild 100 alone", rows)
	}
}

func TestCompRoleTotalsCountRosterAndBenchApart(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	_, seated := seedUserAndCharacter(ctx, t, q, 1, "Seated")
	_, benched := seedUserAndCharacter(ctx, t, q, 2, "Benched")
	event := seedRaid(ctx, t, q, "Prog", 7)
	seatOn(ctx, t, q, event, seated.ID, RoleEnumHEALER, 0, false)
	seatOn(ctx, t, q, event, benched.ID, RoleEnumHEALER, 0, true)

	guild, since, until := analysisWindow(100)
	rows, err := q.CompRoleTotals(ctx, CompRoleTotalsParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("reading role totals: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want one role", len(rows))
	}
	if rows[0].Placed != 1 || rows[0].Benched != 1 {
		t.Errorf("placed/benched = %d/%d, want 1/1", rows[0].Placed, rows[0].Benched)
	}
}

func TestBenchByCharacterOrdersTheLongestSufferingFirst(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	_, often := seedUserAndCharacter(ctx, t, q, 1, "Often")
	_, once := seedUserAndCharacter(ctx, t, q, 2, "Once")
	// Distinct slot indexes: (event, comp, is_bench, slot_index) is unique, so two
	// raiders on the same bench cannot share a seat number.
	for _, daysAgo := range []int{7, 14, 21} {
		event := seedRaid(ctx, t, q, "Night", daysAgo)
		seatOn(ctx, t, q, event, often.ID, RoleEnumRDPS, 0, true)
		seatOn(ctx, t, q, event, once.ID, RoleEnumRDPS, 1, daysAgo == 7)
	}

	guild, since, until := analysisWindow(100)
	rows, err := q.BenchByCharacter(ctx, BenchByCharacterParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("reading bench records: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want two characters", len(rows))
	}
	if rows[0].Name != "Often" {
		t.Errorf("first row = %q, want the most-benched raider first", rows[0].Name)
	}
	if rows[0].Boards != 3 || rows[0].Benched != 3 {
		t.Errorf("boards/benched = %d/%d, want 3/3", rows[0].Boards, rows[0].Benched)
	}
}

func TestRoleCoverageCountsFirstChoiceApart(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	_, willing := seedUserAndCharacter(ctx, t, q, 1, "Willing")
	_, reluctant := seedUserAndCharacter(ctx, t, q, 2, "Reluctant")
	for _, role := range []struct {
		id       uuid.UUID
		role     RoleEnum
		priority int16
	}{
		{willing.ID, RoleEnumHEALER, 1},
		{reluctant.ID, RoleEnumHEALER, 2},
		{reluctant.ID, RoleEnumRDPS, 1},
	} {
		if err := q.SetCharacterRole(ctx, SetCharacterRoleParams{
			CharacterID: role.id, Role: role.role, Priority: role.priority,
		}); err != nil {
			t.Fatalf("setting role: %v", err)
		}
	}

	rows, err := q.RoleCoverage(ctx, 100)
	if err != nil {
		t.Fatalf("reading coverage: %v", err)
	}

	var healers RoleCoverageRow
	for _, row := range rows {
		if row.Role == RoleEnumHEALER {
			healers = row
		}
	}
	if healers.Characters != 2 {
		t.Errorf("healers = %d, want 2 who can heal", healers.Characters)
	}
	// A guild with two healers who would rather be dps does not have two healers.
	if healers.FirstChoice != 1 {
		t.Errorf("first choice healers = %d, want 1", healers.FirstChoice)
	}
}

func TestRosterActivitySeparatesWhoStillTurnsUp(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	_, active := seedUserAndCharacter(ctx, t, q, 1, "Active")
	seedUserAndCharacter(ctx, t, q, 2, "Dormant")
	event := seedRaid(ctx, t, q, "Prog", 7)
	signUpAs(ctx, t, q, event.ID, active.ID, SignupStatusCONFIRMED)

	guild, since, until := analysisWindow(100)
	row, err := q.RosterActivity(ctx, RosterActivityParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("reading roster activity: %v", err)
	}
	if row.Characters != 2 {
		t.Errorf("characters = %d, want 2", row.Characters)
	}
	if row.Active != 1 {
		t.Errorf("active = %d, want 1", row.Active)
	}
}

// The lateral joins exist so signups and comp slots cannot multiply each other. With
// four signups and four slots on one event, a plain join reports sixteen of each.
func TestEventThroughputDoesNotMultiplySignupsBySlots(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedRaid(ctx, t, q, "Prog", 7)
	for i := int64(1); i <= 4; i++ {
		_, character := seedUserAndCharacter(ctx, t, q, i, "Raider"+string(rune('A'+i)))
		signUpAs(ctx, t, q, event.ID, character.ID, SignupStatusCONFIRMED)
		seatOn(ctx, t, q, event, character.ID, RoleEnumMDPS, int16(i), false)
	}

	guild, since, until := analysisWindow(100)
	rows, err := q.EventThroughput(ctx, EventThroughputParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("reading throughput: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want one week", len(rows))
	}
	if rows[0].Events != 1 {
		t.Errorf("events = %d, want 1", rows[0].Events)
	}
	if rows[0].Confirmed != 4 {
		t.Errorf("confirmed = %d, want 4 rather than 16", rows[0].Confirmed)
	}
	if rows[0].Placed != 4 {
		t.Errorf("placed = %d, want 4 rather than 16", rows[0].Placed)
	}
}

// The bar is a week, and a week holds however many raids the guild ran. Summing each
// raid's signups means twelve raiders across three nights reads as thirty-six, which is
// more people than the guild has.
func TestEventThroughputCountsPeopleNotSignupRows(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	// Two raids in the same week, the same six raiders confirmed for both.
	tuesday := seedRaid(ctx, t, q, "Tuesday", 5)
	thursday := seedRaid(ctx, t, q, "Thursday", 3)
	for i := int64(1); i <= 6; i++ {
		_, character := seedUserAndCharacter(ctx, t, q, i, "Raider"+string(rune('A'+i)))
		signUpAs(ctx, t, q, tuesday.ID, character.ID, SignupStatusCONFIRMED)
		signUpAs(ctx, t, q, thursday.ID, character.ID, SignupStatusCONFIRMED)
	}

	guild, since, until := analysisWindow(100)
	rows, err := q.EventThroughput(ctx, EventThroughputParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("reading throughput: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want one week holding both raids", len(rows))
	}
	if rows[0].Events != 2 {
		t.Errorf("events = %d, want 2", rows[0].Events)
	}
	if rows[0].Confirmed != 6 {
		t.Errorf("confirmed = %d, want the 6 raiders rather than their 12 signups", rows[0].Confirmed)
	}
}

// A sync writes a snapshot whenever Raider.IO moved, so a character who reforged four
// times in a week must not weigh four times as much as one who logged off.
func TestIlvlSeriesTakesOneSnapshotPerCharacterPerWeek(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	_, busy := seedUserAndCharacter(ctx, t, q, 1, "Busy")
	_, quiet := seedUserAndCharacter(ctx, t, q, 2, "Quiet")

	// All four inside one ISO week. windowUntil is a Saturday, so anything four days
	// back or less shares the Monday date_trunc('week') buckets on.
	seedSnapshot(ctx, t, q, busy.ID, 600, windowUntil.AddDate(0, 0, -4))
	seedSnapshot(ctx, t, q, busy.ID, 620, windowUntil.AddDate(0, 0, -3))
	seedSnapshot(ctx, t, q, busy.ID, 640, windowUntil.AddDate(0, 0, -2))
	seedSnapshot(ctx, t, q, quiet.ID, 600, windowUntil.AddDate(0, 0, -4))

	guild, since, until := analysisWindow(100)
	rows, err := q.IlvlSeries(ctx, IlvlSeriesParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("reading ilvl series: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want one week", len(rows))
	}
	if rows[0].Characters != 2 {
		t.Errorf("characters = %d, want 2 rather than 4 snapshots", rows[0].Characters)
	}
	if rows[0].MedianIlvl != 620 {
		t.Errorf("median = %v, want 620 from each character's latest snapshot", rows[0].MedianIlvl)
	}
}

// The floor of a guild's roster is whichever alt somebody registered and abandoned, so
// the curve is drawn over mains and the alt must not reach it at all.
func TestIlvlSeriesIgnoresAlts(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	user, main := seedUserAndCharacter(ctx, t, q, 1, "Main")
	alt, err := q.CreateCharacter(ctx, CreateCharacterParams{
		ID: NewID(), UserID: user.ID, Name: "Bankalt", Realm: "Area-52", Region: "us", IsMain: false,
	})
	if err != nil {
		t.Fatalf("creating alt: %v", err)
	}
	seedSnapshot(ctx, t, q, main.ID, 640, windowUntil.AddDate(0, 0, -3))
	seedSnapshot(ctx, t, q, alt.ID, 80, windowUntil.AddDate(0, 0, -3))

	guild, since, until := analysisWindow(100)
	rows, err := q.IlvlSeries(ctx, IlvlSeriesParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("reading ilvl series: %v", err)
	}
	if len(rows) != 1 || rows[0].Characters != 1 {
		t.Fatalf("rows = %+v, want the main alone", rows)
	}
	if rows[0].P25Ilvl != 640 || rows[0].P75Ilvl != 640 {
		t.Errorf("quartiles = %v to %v, want the alt nowhere near them", rows[0].P25Ilvl, rows[0].P75Ilvl)
	}
}

// Quartiles, not extremes: one badly geared main sets p25 above itself rather than
// dragging the whole band down to meet it.
func TestIlvlSeriesReportsTheMiddleHalf(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	for i, ilvl := range []int64{600, 610, 620, 630, 640} {
		_, character := seedUserAndCharacter(ctx, t, q, int64(i+1), "Raider"+string(rune('A'+i)))
		seedSnapshot(ctx, t, q, character.ID, ilvl, windowUntil.AddDate(0, 0, -3))
	}

	guild, since, until := analysisWindow(100)
	rows, err := q.IlvlSeries(ctx, IlvlSeriesParams{GuildID: guild, Since: since, Until: until})
	if err != nil {
		t.Fatalf("reading ilvl series: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want one week", len(rows))
	}
	got := rows[0]
	if got.MedianIlvl != 620 {
		t.Errorf("median = %v, want 620", got.MedianIlvl)
	}
	if got.P25Ilvl != 610 || got.P75Ilvl != 630 {
		t.Errorf("quartiles = %v to %v, want 610 to 630", got.P25Ilvl, got.P75Ilvl)
	}
}

func seedSnapshot(ctx context.Context, t *testing.T, q *Queries, characterID uuid.UUID, ilvl int64, at time.Time) {
	t.Helper()

	var value pgtype.Numeric
	if err := value.Scan(strconv.FormatInt(ilvl, 10)); err != nil {
		t.Fatalf("scanning ilvl: %v", err)
	}

	snap, err := q.InsertCharacterSnapshot(ctx, InsertCharacterSnapshotParams{
		ID:          NewID(),
		CharacterID: characterID,
		Ilvl:        value,
		MplusScore:  value,
		Gear:        []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("inserting snapshot: %v", err)
	}
	// captured_at defaults to the transaction clock, which puts every snapshot in one
	// test at the same instant. The series is about when gear moved, so the seed has
	// to say when rather than accept "now".
	if _, err := q.db.Exec(ctx,
		`UPDATE character_snapshots SET captured_at = $1 WHERE id = $2`,
		dbTimestamptz(at), snap.ID); err != nil {
		t.Fatalf("backdating snapshot: %v", err)
	}
}
