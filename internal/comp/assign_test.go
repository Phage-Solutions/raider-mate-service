package comp

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/db"
)

func charID(n byte) uuid.UUID {
	var id uuid.UUID
	id[15] = n
	return id
}

func intPtr(n int) *int { return &n }

var baseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// poolWithRoles builds one raider per role in roles, each with that single role at
// priority 1, distinct increasing signup times, and equal score.
func poolWithRoles(roles ...db.RoleEnum) []Raider {
	raiders := make([]Raider, len(roles))
	for i, role := range roles {
		raiders[i] = Raider{
			CharacterID: charID(byte(i + 1)),
			Name:        fmt.Sprintf("Raider%d", i+1),
			Roles:       []RoleChoice{{Role: role, Priority: 1}},
			IsMain:      true,
			Score:       100,
			SignedUpAt:  baseTime.Add(time.Duration(i) * time.Minute),
		}
	}
	return raiders
}

func TestTemplateResolveDerivesSuggestionForFlexRaids(t *testing.T) {
	// ModeRaidFlex floors at 10 and caps at 30; a shortfall below 10 shows up as an
	// ordinary per-role Advisory once Assign runs, not as a special case here.
	cases := []struct {
		turnout                         int
		wantTanks, wantHealers, wantDPS int
	}{
		{1, 2, 2, 6}, // clamped to size 10
		{2, 2, 2, 6}, // clamped to size 10
		{10, 2, 2, 6},
		{17, 2, 4, 11},
		{20, 2, 4, 14},
		{25, 2, 5, 18},
		{30, 2, 6, 22},
		{34, 2, 6, 22}, // capped at size 30
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("turnout=%d", c.turnout), func(t *testing.T) {
			needs, advisories := Template{}.Resolve(ModeRaidFlex, c.turnout)
			if needs.Tanks != c.wantTanks || needs.Healers != c.wantHealers || needs.DPS != c.wantDPS {
				t.Errorf("Needs = %+v, want tanks=%d healers=%d dps=%d",
					needs, c.wantTanks, c.wantHealers, c.wantDPS)
			}
			if len(advisories) != 0 {
				t.Errorf("advisories = %v, want none for the derived suggestion", advisories)
			}
			if needs.Tanks < 0 || needs.Healers < 0 || needs.DPS < 0 {
				t.Errorf("Needs = %+v, no field may be negative", needs)
			}
		})
	}
}

func TestTemplateResolveMythicIsFixedAt20(t *testing.T) {
	needs, advisories := Template{}.Resolve(ModeRaidMythic, 25)

	if needs.Tanks != 2 || needs.Healers != 4 || needs.DPS != 14 {
		t.Errorf("Needs = %+v, want 2/4/14 (the fixed 20-man split) regardless of a 25-signup turnout", needs)
	}
	if len(advisories) != 0 {
		t.Errorf("advisories = %v, want none: 25 signups is enough to fill a 20-man Mythic raid", advisories)
	}
}

func TestAssignMythicBenchesEveryoneOverTwenty(t *testing.T) {
	roles := []db.RoleEnum{db.RoleEnumTANK, db.RoleEnumTANK}
	for range 4 {
		roles = append(roles, db.RoleEnumHEALER)
	}
	for range 19 { // 2 + 4 + 19 = 25 signups for a 20-man raid
		roles = append(roles, db.RoleEnumMDPS)
	}
	raiders := poolWithRoles(roles...)

	result := Assign(raiders, Template{}, ModeRaidMythic)

	assigned, benched := 0, 0
	for _, a := range result.Assignments {
		if a.IsBench {
			benched++
		} else {
			assigned++
		}
	}
	if assigned != 20 {
		t.Errorf("assigned = %d, want exactly 20 for a Mythic raid", assigned)
	}
	if benched != 5 {
		t.Errorf("benched = %d, want 5 (25 signups - 20 seats)", benched)
	}
}

func TestAssignMythicShortfallRaisesAdvisoryInsteadOfShrinking(t *testing.T) {
	// 17 signups for a raid that needs exactly 20: this must not quietly build a
	// 17-person comp. It has to come out short against 20 and say so.
	roles := []db.RoleEnum{db.RoleEnumTANK, db.RoleEnumTANK}
	for range 4 {
		roles = append(roles, db.RoleEnumHEALER)
	}
	for range 11 {
		roles = append(roles, db.RoleEnumMDPS)
	}
	raiders := poolWithRoles(roles...) // 2 + 4 + 11 = 17

	result := Assign(raiders, Template{}, ModeRaidMythic)

	for _, a := range result.Assignments {
		if a.IsBench {
			t.Errorf("character %s benched in a pool that is already short of 20", a.CharacterID)
		}
	}

	found := false
	for _, a := range result.Advisories {
		if a.Role == "" && a.Message == "DPS: 11 of 14, not enough signups" {
			found = true
		}
	}
	if !found {
		t.Errorf("advisories = %+v, want a DPS shortfall against the fixed 20-man split", result.Advisories)
	}
}

func TestTemplateResolveMythicPlusIsOneOneThree(t *testing.T) {
	needs, advisories := Template{}.Resolve(ModeMythicPlus, 5)

	if needs.Tanks != 1 || needs.Healers != 1 || needs.DPS != 3 {
		t.Errorf("Needs = %+v, want 1/1/3 for Mythic+", needs)
	}
	if len(advisories) != 0 {
		t.Errorf("advisories = %v, want none", advisories)
	}
}

func TestAssignMythicPlusFixedGroupOfFive(t *testing.T) {
	raiders := poolWithRoles(
		db.RoleEnumTANK, db.RoleEnumHEALER, db.RoleEnumMDPS, db.RoleEnumMDPS, db.RoleEnumRDPS,
	)

	result := Assign(raiders, Template{}, ModeMythicPlus)

	for _, a := range result.Assignments {
		if a.IsBench {
			t.Errorf("character %s benched in an exactly-matching 5-man M+ pool", a.CharacterID)
		}
	}
	if len(result.Advisories) != 0 {
		t.Errorf("advisories = %v, want none", result.Advisories)
	}
}

func TestTemplateResolveNegativeOverrideClampsToZeroWithAdvisory(t *testing.T) {
	needs, advisories := Template{Tanks: intPtr(-3)}.Resolve(ModeRaidFlex, 20)

	if needs.Tanks != 0 {
		t.Errorf("Tanks = %d, want clamped to 0", needs.Tanks)
	}
	if len(advisories) == 0 {
		t.Fatalf("advisories = %v, want at least one reporting the clamp", advisories)
	}
}

func TestTemplateResolveOverridesExceedingSizeAreTrimmedWithAdvisory(t *testing.T) {
	needs, advisories := Template{Healers: intPtr(20)}.Resolve(ModeRaidFlex, 10)

	if needs.DPS != 0 {
		t.Errorf("DPS = %d, want 0 (2 tanks + 20 healers exceeds size 10)", needs.DPS)
	}
	if total := needs.Tanks + needs.Healers + needs.DPS; total != 10 {
		t.Errorf("total need = %d, want exactly the 10-raider size", total)
	}
	if len(advisories) == 0 {
		t.Fatalf("advisories = %+v, want the trim reported", advisories)
	}
}

func TestTemplateResolveMythicStaysAtTwentySeats(t *testing.T) {
	// An override cannot buy seats a Mythic raid does not have.
	needs, advisories := Template{Tanks: intPtr(0), Healers: intPtr(25)}.Resolve(ModeRaidMythic, 30)

	if total := needs.Tanks + needs.Healers + needs.DPS; total != 20 {
		t.Errorf("total need = %d, want exactly 20 for a Mythic raid", total)
	}
	if len(advisories) == 0 {
		t.Fatalf("advisories = %+v, want the trim reported", advisories)
	}
}

func TestAssignMythicNeverSeatsMoreThanTwentyDespiteOverride(t *testing.T) {
	raiders := make([]Raider, 0, 30)
	for i := range 30 {
		raiders = append(raiders, Raider{
			CharacterID: uuid.New(),
			Name:        fmt.Sprintf("Healer%02d", i),
			Roles:       []RoleChoice{{Role: db.RoleEnumHEALER, Priority: 1}},
			SignedUpAt:  time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	result := Assign(raiders, Template{Tanks: intPtr(0), Healers: intPtr(25)}, ModeRaidMythic)

	seated := 0
	for _, a := range result.Assignments {
		if !a.IsBench {
			seated++
		}
	}
	if seated != 20 {
		t.Errorf("seated = %d, want 20: a Mythic raid cannot zone in at any other size", seated)
	}
}

func TestTemplateResolveNegativeCapClampsToZeroWithAdvisory(t *testing.T) {
	needs, advisories := Template{MaxMelee: intPtr(-1)}.Resolve(ModeRaidFlex, 20)

	if needs.MaxMelee == nil || *needs.MaxMelee != 0 {
		t.Errorf("MaxMelee = %v, want clamped to 0", needs.MaxMelee)
	}
	if len(advisories) == 0 {
		t.Fatalf("advisories = %+v, want the clamp reported", advisories)
	}
}

func TestAssignNegativeMeleeCapIsExplained(t *testing.T) {
	raiders := make([]Raider, 0, 3)
	for i := range 3 {
		raiders = append(raiders, Raider{
			CharacterID: uuid.New(),
			Name:        fmt.Sprintf("Melee%d", i),
			Roles:       []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}},
			SignedUpAt:  time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	result := Assign(raiders, Template{MaxMelee: intPtr(-1)}, ModeMythicPlus)

	// The typo still benches them; the point is that the raid lead is told why.
	if len(result.Advisories) == 0 {
		t.Fatalf("advisories = %+v, want the negative cap reported", result.Advisories)
	}
	found := false
	for _, a := range result.Advisories {
		if strings.Contains(a.Message, "max_melee") {
			found = true
		}
	}
	if !found {
		t.Errorf("advisories = %+v, want one naming max_melee", result.Advisories)
	}
}

func TestAssignRolelessCharacterIsSkippedAndNamed(t *testing.T) {
	roleless := Raider{CharacterID: uuid.New(), Name: "Unconfigured", SignedUpAt: time.Now()}
	tank := Raider{
		CharacterID: uuid.New(),
		Name:        "Tanky",
		Roles:       []RoleChoice{{Role: db.RoleEnumTANK, Priority: 1}},
		SignedUpAt:  time.Now().Add(time.Minute),
	}

	result := Assign([]Raider{roleless, tank}, Template{}, ModeRaidFlex)

	for _, a := range result.Assignments {
		if a.CharacterID == roleless.CharacterID {
			t.Errorf("roleless character got a slot with role %q; comp_slots.role is NOT NULL", a.Role)
		}
		if a.Role == "" {
			t.Errorf("assignment for %s has an empty role, which Postgres rejects", a.CharacterID)
		}
	}
	found := false
	for _, a := range result.Advisories {
		if strings.Contains(a.Message, "Unconfigured") {
			found = true
		}
	}
	if !found {
		t.Errorf("advisories = %+v, want one naming the skipped character", result.Advisories)
	}
}

func TestAssignReasonReportsThePriorityOfTheRoleActuallyPlayed(t *testing.T) {
	// Melee is their main, ranged their reluctant off-spec, and melee is capped at 0,
	// so they play ranged. The reason must not claim they are on their main.
	flex := Raider{
		CharacterID: uuid.New(),
		Name:        "Flexer",
		Roles: []RoleChoice{
			{Role: db.RoleEnumMDPS, Priority: 1},
			{Role: db.RoleEnumRDPS, Priority: 3},
		},
		SignedUpAt: time.Now(),
	}

	result := Assign([]Raider{flex}, Template{Tanks: intPtr(0), Healers: intPtr(0), MaxMelee: intPtr(0)}, ModeMythicPlus)

	for _, a := range result.Assignments {
		if a.IsBench {
			continue
		}
		if a.Role != db.RoleEnumRDPS {
			t.Fatalf("role = %s, want RDPS with melee capped at 0", a.Role)
		}
		if !strings.Contains(a.Reason, "priority 3") {
			t.Errorf("reason = %q, want the RDPS priority 3 they actually played", a.Reason)
		}
	}
}

func TestTemplateResolveNeverProducesANegativeCount(t *testing.T) {
	cases := []Template{
		{Tanks: intPtr(-1), Healers: intPtr(-1)},
		{Tanks: intPtr(50)},
		{Healers: intPtr(50)},
	}
	for _, tmpl := range cases {
		for turnout := 1; turnout <= 3; turnout++ {
			needs, _ := tmpl.Resolve(ModeRaidFlex, turnout)
			if needs.Tanks < 0 || needs.Healers < 0 || needs.DPS < 0 {
				t.Errorf("turnout=%d tmpl=%+v: Needs = %+v, no field may be negative", turnout, tmpl, needs)
			}
		}
	}
}

func TestTemplateResolveCapDeadlockRaisesAdvisory(t *testing.T) {
	// dps=14 at size 20, but max_melee + max_ranged = 5 + 5 = 10: four DPS seats are
	// structurally unfillable no matter who signs up.
	needs, advisories := Template{MaxMelee: intPtr(5), MaxRanged: intPtr(5)}.Resolve(ModeRaidFlex, 20)

	if needs.DPS != 14 {
		t.Fatalf("DPS = %d, want 14 (needs.DPS is the target, not the fillable count)", needs.DPS)
	}
	found := false
	for _, a := range advisories {
		if a.Message == "DPS: caps allow at most 10, need is 14" {
			found = true
		}
	}
	if !found {
		t.Errorf("advisories = %+v, want a cap-deadlock advisory", advisories)
	}
}

func TestAssignCapDeadlockBenchesTheUnfillableOverflow(t *testing.T) {
	roles := []db.RoleEnum{db.RoleEnumTANK, db.RoleEnumTANK}
	for range 4 {
		roles = append(roles, db.RoleEnumHEALER)
	}
	for range 6 {
		roles = append(roles, db.RoleEnumMDPS)
	}
	for range 6 {
		roles = append(roles, db.RoleEnumRDPS)
	}
	raiders := poolWithRoles(roles...) // 2 tank, 4 heal, 6 melee, 6 ranged = 18 -> size 18? no, flex clamps min 10

	result := Assign(raiders, Template{MaxMelee: intPtr(5), MaxRanged: intPtr(5)}, ModeRaidFlex)

	benched := 0
	for _, a := range result.Assignments {
		if a.IsBench {
			benched++
		}
	}
	if benched == 0 {
		t.Errorf("benched = 0, want at least one raider benched by the melee/ranged caps")
	}
}

func TestAssignFillOrderIsStableWhenNeedsTie(t *testing.T) {
	// At turnout 10, Tanks and Healers both need 2 (a genuine tie). Group fill
	// order must come from a sorted slice, never a map, or this flakes instead of
	// failing cleanly. Run it many times and require every run to agree, including
	// which specific raider landed in which role.
	roles := []db.RoleEnum{
		db.RoleEnumTANK, db.RoleEnumTANK,
		db.RoleEnumHEALER, db.RoleEnumHEALER,
		db.RoleEnumMDPS, db.RoleEnumMDPS, db.RoleEnumMDPS,
		db.RoleEnumRDPS, db.RoleEnumRDPS, db.RoleEnumRDPS,
	}
	raiders := poolWithRoles(roles...)

	first := Assign(raiders, Template{}, ModeRaidFlex)
	for range 50 {
		got := Assign(raiders, Template{}, ModeRaidFlex)
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("Assign at a TANK/HEALER need tie is not stable across repeated calls:\nfirst: %+v\ngot:   %+v", first, got)
		}
	}
}

func TestTemplateResolveOverrideAboveSuggestionRaisesAdvisory(t *testing.T) {
	needs, advisories := Template{Healers: intPtr(5)}.Resolve(ModeRaidFlex, 20)

	if needs.Healers != 5 {
		t.Errorf("Healers = %d, want 5", needs.Healers)
	}
	if needs.DPS != 13 {
		t.Errorf("DPS = %d, want 13 (20 - 2 tanks - 5 healers)", needs.DPS)
	}
	if len(advisories) != 1 || advisories[0].Role != db.RoleEnumHEALER {
		t.Fatalf("advisories = %+v, want exactly one HEALER advisory", advisories)
	}
}

func TestTemplateResolveOverrideBelowSuggestionHonouredNotRejected(t *testing.T) {
	needs, advisories := Template{Healers: intPtr(3)}.Resolve(ModeRaidFlex, 20)

	if needs.Healers != 3 {
		t.Errorf("Healers = %d, want 3 honoured as written, not clamped to the floor", needs.Healers)
	}
	if len(advisories) != 1 {
		t.Fatalf("advisories = %+v, want one advisory reporting the departure, not a rejection", advisories)
	}
}

func TestAssignOddConfigStillProducesFullComp(t *testing.T) {
	roles := []db.RoleEnum{db.RoleEnumTANK}
	for range 7 {
		roles = append(roles, db.RoleEnumHEALER)
	}
	for range 12 {
		roles = append(roles, db.RoleEnumMDPS)
	}
	raiders := poolWithRoles(roles...)

	result := Assign(raiders, Template{Tanks: intPtr(1), Healers: intPtr(7)}, ModeRaidFlex)

	if len(result.Assignments) != 20 {
		t.Fatalf("assignments = %d, want 20", len(result.Assignments))
	}
	for _, a := range result.Assignments {
		if a.IsBench {
			t.Errorf("character %s benched, want everyone seated in a pool that exactly matches the config", a.CharacterID)
		}
	}
}

func TestAssignScarcityOrderProtectsFlexTanksFromDPSPool(t *testing.T) {
	var raiders []Raider

	flexers := []uuid.UUID{charID(1), charID(2)}
	for i, id := range flexers {
		raiders = append(raiders, Raider{
			CharacterID: id,
			Name:        fmt.Sprintf("Flex%d", i+1),
			Roles: []RoleChoice{
				{Role: db.RoleEnumTANK, Priority: 1},
				{Role: db.RoleEnumMDPS, Priority: 1},
			},
			IsMain:     true,
			Score:      500, // highest in the pool, so a DPS-first pass would want them
			SignedUpAt: baseTime,
		})
	}
	for i := range 14 {
		raiders = append(raiders, Raider{
			CharacterID: charID(byte(10 + i)),
			Name:        fmt.Sprintf("DPS%d", i+1),
			Roles:       []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}},
			IsMain:      true,
			Score:       100,
			SignedUpAt:  baseTime.Add(time.Duration(i+1) * time.Minute),
		})
	}

	result := Assign(raiders, Template{}, ModeRaidFlex)

	tanksSeated := 0
	for _, a := range result.Assignments {
		if a.CharacterID != flexers[0] && a.CharacterID != flexers[1] {
			continue
		}
		if a.IsBench || a.Role != db.RoleEnumTANK {
			t.Errorf("flexer %s got role=%s bench=%v, want TANK seated", a.CharacterID, a.Role, a.IsBench)
			continue
		}
		tanksSeated++
	}
	if tanksSeated != 2 {
		t.Errorf("flex tanks seated = %d, want 2", tanksSeated)
	}
}

func TestAssignRepairPassFixesShortTank(t *testing.T) {
	// Fill order is ascending need. At the flex floor of 10, overriding Tanks to 2 and
	// Healers to 7 leaves DPS needing 1, so DPS fills first, then TANK, then HEALER.
	// A and B are the only tank-capable raiders, and A is also the pool's strongest
	// DPS: without repair the DPS pass claims A and TANK comes out one short.
	raiders := []Raider{
		{
			CharacterID: charID(1), Name: "A",
			Roles:  []RoleChoice{{Role: db.RoleEnumTANK, Priority: 1}, {Role: db.RoleEnumMDPS, Priority: 1}},
			IsMain: true, Score: 500, SignedUpAt: baseTime,
		},
		{
			CharacterID: charID(2), Name: "B",
			Roles:  []RoleChoice{{Role: db.RoleEnumTANK, Priority: 1}},
			IsMain: true, Score: 400, SignedUpAt: baseTime.Add(time.Minute),
		},
		{
			CharacterID: charID(3), Name: "E",
			Roles:  []RoleChoice{{Role: db.RoleEnumHEALER, Priority: 1}},
			IsMain: true, Score: 300, SignedUpAt: baseTime.Add(2 * time.Minute),
		},
		{
			CharacterID: charID(4), Name: "D",
			Roles:  []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}},
			IsMain: false, Score: 50, SignedUpAt: baseTime.Add(3 * time.Minute),
		},
	}

	result := Assign(raiders, Template{Tanks: intPtr(2), Healers: intPtr(7)}, ModeRaidFlex)

	roleOf := make(map[uuid.UUID]db.RoleEnum, len(result.Assignments))
	for _, a := range result.Assignments {
		if !a.IsBench {
			roleOf[a.CharacterID] = a.Role
		}
	}

	if roleOf[charID(1)] != db.RoleEnumTANK || roleOf[charID(2)] != db.RoleEnumTANK {
		t.Errorf("tanks = {A:%s, B:%s}, want both TANK", roleOf[charID(1)], roleOf[charID(2)])
	}
	if roleOf[charID(4)] != db.RoleEnumMDPS {
		t.Errorf("D = %s, want MDPS backfilled into the vacancy A left", roleOf[charID(4)])
	}
	for _, a := range result.Advisories {
		if a.Role == db.RoleEnumTANK {
			t.Errorf("unexpected TANK shortfall advisory after repair: %+v", a)
		}
	}
}

func TestBackfillFallsThroughToAPlaceableCandidate(t *testing.T) {
	// The best-ranked backfill is melee-only and melee is capped out; the next one is
	// ranged and fits. Taking only the top candidate would abandon a repairable swap.
	melee := Raider{
		CharacterID: charID(1), Name: "Melee",
		Roles:  []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}},
		IsMain: true, Score: 500, SignedUpAt: baseTime,
	}
	ranged := Raider{
		CharacterID: charID(2), Name: "Ranged",
		Roles:  []RoleChoice{{Role: db.RoleEnumRDPS, Priority: 1}},
		IsMain: true, Score: 100, SignedUpAt: baseTime.Add(time.Minute),
	}
	raiders := []Raider{melee, ranged}

	scratch := make(map[uuid.UUID]placement)
	needs := Needs{DPS: 1, MaxMelee: intPtr(0)}

	if !backfill(raiders, scratch, groupDPS, needs, signupRanks(raiders)) {
		t.Fatalf("backfill = false, want the ranged candidate placed")
	}
	if pl, ok := scratch[charID(2)]; !ok || pl.role != db.RoleEnumRDPS {
		t.Errorf("scratch = %+v, want Ranged placed as RDPS", scratch)
	}
	if _, ok := scratch[charID(1)]; ok {
		t.Errorf("Melee was placed despite max_melee 0")
	}
}

func TestAssignMeleeCapLimitsDPSAndBenchesTheRest(t *testing.T) {
	raiders := []Raider{
		{CharacterID: charID(1), Name: "Melee1", Roles: []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}}, IsMain: true, Score: 100, SignedUpAt: baseTime},
		{CharacterID: charID(2), Name: "Melee2", Roles: []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}}, IsMain: true, Score: 90, SignedUpAt: baseTime.Add(time.Minute)},
		{CharacterID: charID(3), Name: "Melee3", Roles: []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}}, IsMain: true, Score: 80, SignedUpAt: baseTime.Add(2 * time.Minute)},
		{CharacterID: charID(4), Name: "Ranged1", Roles: []RoleChoice{{Role: db.RoleEnumRDPS, Priority: 1}}, IsMain: true, Score: 70, SignedUpAt: baseTime.Add(3 * time.Minute)},
		{CharacterID: charID(5), Name: "Ranged2", Roles: []RoleChoice{{Role: db.RoleEnumRDPS, Priority: 1}}, IsMain: true, Score: 60, SignedUpAt: baseTime.Add(4 * time.Minute)},
	}

	// ModeMythicPlus fixes size at 5, matching this pool exactly; ModeRaidFlex's
	// floor of 10 would otherwise report an unrelated tank/healer shortfall against
	// a 5-person pool and muddy this test's DPS-cap assertion.
	result := Assign(raiders, Template{Tanks: intPtr(0), Healers: intPtr(0), MaxMelee: intPtr(2)}, ModeMythicPlus)

	melee, ranged, benched := 0, 0, 0
	for _, a := range result.Assignments {
		switch {
		case a.IsBench:
			benched++
			if a.CharacterID != charID(3) {
				t.Errorf("benched %s, want Melee3 (lowest-score melee, cap reached)", a.CharacterID)
			}
		case a.Role == db.RoleEnumMDPS:
			melee++
		case a.Role == db.RoleEnumRDPS:
			ranged++
		}
	}
	if melee != 2 {
		t.Errorf("melee assigned = %d, want 2 (the cap)", melee)
	}
	if ranged != 2 {
		t.Errorf("ranged assigned = %d, want 2 (uncapped)", ranged)
	}
	if benched != 1 {
		t.Errorf("benched = %d, want 1", benched)
	}

	foundShortfall := false
	for _, a := range result.Advisories {
		if a.Message == "DPS: 4 of 5, not enough signups" {
			foundShortfall = true
		}
	}
	if !foundShortfall {
		t.Errorf("advisories = %+v, want a DPS shortfall caused by the melee cap", result.Advisories)
	}
}

func TestAssignUnreachedCapRaisesNoDPSAdvisory(t *testing.T) {
	// Three MDPS-only raiders against Mythic+'s fixed 1/1/3 split: DPS need is
	// exactly 3, so an unreached max_melee cap must not touch the DPS bucket. The
	// pool has nobody tank- or healer-capable, so those two shortfalls are expected
	// and irrelevant to what this test checks.
	raiders := poolWithRoles(db.RoleEnumMDPS, db.RoleEnumMDPS, db.RoleEnumMDPS)

	result := Assign(raiders, Template{MaxMelee: intPtr(10)}, ModeMythicPlus)

	for _, a := range result.Advisories {
		if a.Role == "" {
			t.Errorf("unexpected DPS advisory %+v: the cap was never reached", a)
		}
	}
	for _, a := range result.Assignments {
		if a.IsBench {
			t.Errorf("character %s benched, want every DPS-capable raider seated", a.CharacterID)
		}
	}
}

func TestAssignBenchOrderedBySignupTimeNotScore(t *testing.T) {
	// Scores are set so the two highest scorers win the two DPS seats regardless of
	// signup order; the three lowest scorers are benched in an order that does NOT
	// match their score order, so this only passes if the bench sort key is signup
	// time.
	raiders := []Raider{
		{CharacterID: charID(1), Name: "R1", Roles: []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}}, IsMain: true, Score: 10, SignedUpAt: baseTime.Add(4 * time.Minute)},
		{CharacterID: charID(2), Name: "R2", Roles: []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}}, IsMain: true, Score: 20, SignedUpAt: baseTime},
		{CharacterID: charID(3), Name: "R3", Roles: []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}}, IsMain: true, Score: 30, SignedUpAt: baseTime.Add(2 * time.Minute)},
		{CharacterID: charID(4), Name: "R4", Roles: []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}}, IsMain: true, Score: 90, SignedUpAt: baseTime.Add(time.Minute)},
		{CharacterID: charID(5), Name: "R5", Roles: []RoleChoice{{Role: db.RoleEnumMDPS, Priority: 1}}, IsMain: true, Score: 80, SignedUpAt: baseTime.Add(3 * time.Minute)},
	}

	// ModeRaidFlex floors size at 10; overriding Tanks/Healers to 4 each (instead of
	// 0) brings DPS need down to 2 despite the floor, so the pool of 5 still yields
	// exactly the 2-assigned/3-benched split this test needs.
	result := Assign(raiders, Template{Tanks: intPtr(4), Healers: intPtr(4)}, ModeRaidFlex)

	var benchOrder []uuid.UUID
	for _, a := range result.Assignments {
		if a.IsBench {
			benchOrder = append(benchOrder, a.CharacterID)
		}
	}

	want := []uuid.UUID{charID(2), charID(3), charID(1)} // signup order: R2, R3, R1
	if !reflect.DeepEqual(benchOrder, want) {
		t.Errorf("bench order = %v, want %v (signup-time order)", benchOrder, want)
	}
}

func TestAssignAdvisoryWhenPoolCannotFillARole(t *testing.T) {
	raiders := poolWithRoles(db.RoleEnumMDPS, db.RoleEnumMDPS)

	result := Assign(raiders, Template{}, ModeRaidFlex)

	found := false
	for _, a := range result.Advisories {
		if a.Role == db.RoleEnumTANK && a.Message == "TANK: 0 of 2, not enough signups" {
			found = true
		}
	}
	if !found {
		t.Errorf("advisories = %+v, want a TANK shortfall since nobody in the pool can tank", result.Advisories)
	}
}

func TestAssignIsDeterministic(t *testing.T) {
	roles := []db.RoleEnum{
		db.RoleEnumTANK, db.RoleEnumTANK, db.RoleEnumTANK,
		db.RoleEnumHEALER, db.RoleEnumHEALER, db.RoleEnumHEALER, db.RoleEnumHEALER, db.RoleEnumHEALER,
		db.RoleEnumMDPS, db.RoleEnumMDPS, db.RoleEnumMDPS, db.RoleEnumMDPS,
		db.RoleEnumRDPS, db.RoleEnumRDPS, db.RoleEnumRDPS, db.RoleEnumRDPS,
	}
	raiders := poolWithRoles(roles...)

	first := Assign(raiders, Template{}, ModeRaidFlex)
	for range 20 {
		got := Assign(raiders, Template{}, ModeRaidFlex)
		if !reflect.DeepEqual(first, got) {
			t.Fatalf("two runs over identical input produced different results:\nfirst: %+v\ngot:   %+v", first, got)
		}
	}
}

func TestAssignEveryAssignmentHasAReason(t *testing.T) {
	roles := []db.RoleEnum{
		db.RoleEnumTANK, db.RoleEnumHEALER, db.RoleEnumHEALER,
		db.RoleEnumMDPS, db.RoleEnumRDPS, db.RoleEnumMDPS,
	}
	raiders := poolWithRoles(roles...)

	result := Assign(raiders, Template{}, ModeRaidFlex)

	for _, a := range result.Assignments {
		if a.Reason == "" {
			t.Errorf("character %s has an empty reason (bench=%v)", a.CharacterID, a.IsBench)
		}
	}
}
