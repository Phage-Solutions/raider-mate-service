package comp

import (
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// group names double as the Advisory/reason vocabulary. DPS covers both MDPS and
// RDPS; which specific enum a DPS-group raider gets is resolved by placeInGroup.
const (
	groupTank   = "TANK"
	groupHealer = "HEALER"
	groupDPS    = "DPS"
)

// placement is where one raider landed: the specific role_enum for comp_slots, the
// group used for scarcity/repair bookkeeping, and the priority that placed them.
type placement struct {
	role     db.RoleEnum
	group    string
	priority int16
}

// candidate is a raider ranked for one group, carrying the priority that matched.
type candidate struct {
	raider   Raider
	priority int16
}

// Assign turns a pool of raiders into a comp. It never fails: every configuration
// produces a result, with Advisories attached wherever the outcome is not what the
// suggestion would have produced. Calling Assign twice with the same input produces
// byte-identical output.
func Assign(raiders []Raider, tmpl Template, mode Mode) Result {
	// A character with an empty role menu cannot be placed in any group, and cannot
	// be benched either: comp_slots.role is NOT NULL and there is no role to record.
	// Drop them from the pool and name them, rather than letting the write fail.
	raiders, roleless := partitionByRoleMenu(raiders)

	needs, advisories := tmpl.Resolve(mode, len(raiders))
	for _, r := range roleless {
		advisories = append(advisories, Advisory{
			Message: fmt.Sprintf("%s: skipped, the character has no roles set", r.Name),
		})
	}

	ranks := signupRanks(raiders)

	assigned := make(map[uuid.UUID]placement, len(raiders))

	// Pass 1+2: scarcest role first. Scarcity is ascending need count, which for the
	// derived suggestion is always TANK, HEALER, DPS in that order; a lead's Template
	// override can reorder it, and the repair pass below exists for exactly that case.
	type groupSpec struct {
		name string
		need int
	}
	groups := []groupSpec{{groupTank, needs.Tanks}, {groupHealer, needs.Healers}, {groupDPS, needs.DPS}}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].need < groups[j].need })

	got := make(map[string]int, 3)
	for _, g := range groups {
		got[g.name] = fillGroup(raiders, assigned, g.name, g.need, needs, ranks)
	}

	// Pass 3: repair. Correct scarcity order does not guarantee a correct outcome: a
	// role that is easy to fill (small need, many candidates) can still process before
	// a role that is hard to fill (small need, few candidates) and claim the only flex
	// raider the hard role had. Swap them back, but only when the vacancy they leave
	// can be backfilled from the remaining pool, so a fix here cannot create a new
	// shortage elsewhere. Bounded by len(raiders) so a cycle cannot hang the caller.
	order := []string{groupTank, groupHealer, groupDPS}
	need := map[string]int{groupTank: needs.Tanks, groupHealer: needs.Healers, groupDPS: needs.DPS}
	for range raiders {
		swapped := false
		for _, g := range order {
			if got[g] >= need[g] {
				continue
			}
			if trySwapInto(raiders, assigned, g, needs, ranks) {
				got[g]++
				swapped = true
				break
			}
		}
		if !swapped {
			break
		}
	}

	for _, g := range order {
		if got[g] < need[g] {
			advisories = append(advisories, Advisory{
				Role:    roleForGroup(g),
				Message: fmt.Sprintf("%s: %d of %d, not enough signups", g, got[g], need[g]),
			})
		}
	}

	// Pass 4: everyone left unassigned goes to the bench, ordered by signup time.
	return Result{
		Assignments: buildAssignments(raiders, assigned, ranks),
		Advisories:  advisories,
	}
}

// partitionByRoleMenu splits the pool into raiders who can play something and those
// with no role choices at all. Hard rule 2 puts the menu on the character, so a
// character registered without one reaches the assigner unplaceable.
func partitionByRoleMenu(raiders []Raider) (playable, roleless []Raider) {
	for _, r := range raiders {
		if len(r.Roles) == 0 {
			roleless = append(roleless, r)
			continue
		}
		playable = append(playable, r)
	}
	return playable, roleless
}

// fillGroup ranks every eligible, still-unassigned raider for group and takes up to
// need of them, resolving each one's specific role via placeInGroup.
func fillGroup(raiders []Raider, assigned map[uuid.UUID]placement, group string, need int, needs Needs, ranks map[uuid.UUID]int) int {
	var cands []candidate
	for _, r := range raiders {
		if _, taken := assigned[r.CharacterID]; taken {
			continue
		}
		if p, ok := groupPriority(r, group); ok {
			cands = append(cands, candidate{r, p})
		}
	}
	rankCandidates(cands, ranks)

	took := 0
	for _, c := range cands {
		if took >= need {
			break
		}
		melee, ranged := recount(assigned)
		role, priority, ok := placeInGroup(c.raider, group, needs, melee, ranged)
		if !ok {
			continue
		}
		assigned[c.raider.CharacterID] = placement{role: role, group: group, priority: priority}
		took++
	}
	return took
}

// trySwapInto looks for an assigned raider in another group who could play short,
// and a replacement for the vacancy they would leave. It only commits when both
// sides resolve; the search runs on a scratch copy so a failed attempt never
// mutates assigned.
func trySwapInto(raiders []Raider, assigned map[uuid.UUID]placement, short string, needs Needs, ranks map[uuid.UUID]int) bool {
	var movable []candidate
	for _, r := range raiders {
		pl, ok := assigned[r.CharacterID]
		if !ok || pl.group == short {
			continue
		}
		if p, ok := groupPriority(r, short); ok {
			movable = append(movable, candidate{r, p})
		}
	}
	rankCandidates(movable, ranks)

	for _, m := range movable {
		from := assigned[m.raider.CharacterID]

		scratch := cloneAssigned(assigned)
		delete(scratch, m.raider.CharacterID)

		melee, ranged := recount(scratch)
		role, priority, ok := placeInGroup(m.raider, short, needs, melee, ranged)
		if !ok {
			continue
		}
		scratch[m.raider.CharacterID] = placement{role: role, group: short, priority: priority}

		// Walk the whole backfill list: the best-ranked candidate may be unplaceable
		// because their only DPS side is capped out, while the next one fits.
		if !backfill(raiders, scratch, from.group, needs, ranks) {
			continue
		}

		clear(assigned)
		for k, v := range scratch {
			assigned[k] = v
		}
		return true
	}
	return false
}

// backfill places the best available raider into group on scratch, reporting whether
// any of them could be placed.
func backfill(raiders []Raider, scratch map[uuid.UUID]placement, group string, needs Needs, ranks map[uuid.UUID]int) bool {
	var cands []candidate
	for _, r := range raiders {
		if _, taken := scratch[r.CharacterID]; taken {
			continue
		}
		if p, ok := groupPriority(r, group); ok {
			cands = append(cands, candidate{r, p})
		}
	}
	rankCandidates(cands, ranks)

	melee, ranged := recount(scratch)
	for _, c := range cands {
		role, priority, ok := placeInGroup(c.raider, group, needs, melee, ranged)
		if !ok {
			continue
		}
		scratch[c.raider.CharacterID] = placement{role: role, group: group, priority: priority}
		return true
	}
	return false
}

// groupPriority reports whether r can play group at all, and the priority to rank
// them by. For DPS that is the better of their MDPS/RDPS priority.
func groupPriority(r Raider, group string) (int16, bool) {
	switch group {
	case groupTank:
		return canPlay(r, db.RoleEnumTANK)
	case groupHealer:
		return canPlay(r, db.RoleEnumHEALER)
	case groupDPS:
		return bestDPSPriority(r)
	default:
		return 0, false
	}
}

// placeInGroup resolves the specific role_enum a raider takes within group, and the
// priority of that role on their menu. For DPS this is cap-aware: once a side's cap
// is reached, a raider who can only play that side is no longer eligible, and one
// who can play both may land on their second choice. The returned priority is the
// one for the role they actually got, which is what the reason string reports.
func placeInGroup(r Raider, group string, needs Needs, meleeCount, rangedCount int) (db.RoleEnum, int16, bool) {
	switch group {
	case groupTank:
		if p, ok := canPlay(r, db.RoleEnumTANK); ok {
			return db.RoleEnumTANK, p, true
		}
		return "", 0, false
	case groupHealer:
		if p, ok := canPlay(r, db.RoleEnumHEALER); ok {
			return db.RoleEnumHEALER, p, true
		}
		return "", 0, false
	case groupDPS:
		role, ok := chooseDPSRole(r, needs.MaxMelee, needs.MaxRanged, meleeCount, rangedCount)
		if !ok {
			return "", 0, false
		}
		p, _ := canPlay(r, role)
		return role, p, true
	default:
		return "", 0, false
	}
}

// chooseDPSRole picks melee or ranged for a DPS-eligible raider: their preferred
// side (lower priority number) if it has room under the cap, otherwise their other
// side if they have one and it has room. A cap is a ceiling, never a target: an
// unset cap always has room.
func chooseDPSRole(r Raider, maxMelee, maxRanged *int, meleeCount, rangedCount int) (db.RoleEnum, bool) {
	mPrio, mOK := canPlay(r, db.RoleEnumMDPS)
	rPrio, rOK := canPlay(r, db.RoleEnumRDPS)
	meleeOpen := maxMelee == nil || meleeCount < *maxMelee
	rangedOpen := maxRanged == nil || rangedCount < *maxRanged

	preferMelee := mOK && (!rOK || mPrio <= rPrio)

	if preferMelee && meleeOpen {
		return db.RoleEnumMDPS, true
	}
	if rOK && rangedOpen {
		return db.RoleEnumRDPS, true
	}
	if mOK && meleeOpen {
		return db.RoleEnumMDPS, true
	}
	return "", false
}

func canPlay(r Raider, role db.RoleEnum) (int16, bool) {
	for _, rc := range r.Roles {
		if rc.Role == role {
			return rc.Priority, true
		}
	}
	return 0, false
}

func bestDPSPriority(r Raider) (int16, bool) {
	mPrio, mOK := canPlay(r, db.RoleEnumMDPS)
	rPrio, rOK := canPlay(r, db.RoleEnumRDPS)
	switch {
	case mOK && rOK && mPrio <= rPrio:
		return mPrio, true
	case rOK:
		return rPrio, true
	case mOK:
		return mPrio, true
	default:
		return 0, false
	}
}

func roleForGroup(group string) db.RoleEnum {
	switch group {
	case groupTank:
		return db.RoleEnumTANK
	case groupHealer:
		return db.RoleEnumHEALER
	default:
		return "" // DPS has no single enum; the advisory is about the bucket, not a role.
	}
}

func cloneAssigned(a map[uuid.UUID]placement) map[uuid.UUID]placement {
	c := make(map[uuid.UUID]placement, len(a))
	for k, v := range a {
		c[k] = v
	}
	return c
}

func recount(a map[uuid.UUID]placement) (melee, ranged int) {
	for _, p := range a {
		switch p.role {
		case db.RoleEnumMDPS:
			melee++
		case db.RoleEnumRDPS:
			ranged++
		}
	}
	return melee, ranged
}

// rankCandidates orders by the ranking design.md section 5 specifies: role priority
// (1 = main spec first), mains before alts, score descending, then earliest signup.
// CharacterID is a final tiebreak so output is deterministic even when every other
// factor ties.
func rankCandidates(cands []candidate, ranks map[uuid.UUID]int) {
	sort.Slice(cands, func(i, j int) bool { return lessCandidate(cands[i], cands[j], ranks) })
}

func lessCandidate(a, b candidate, ranks map[uuid.UUID]int) bool {
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	if a.raider.IsMain != b.raider.IsMain {
		return a.raider.IsMain
	}
	if a.raider.Score != b.raider.Score {
		return a.raider.Score > b.raider.Score
	}
	if ra, rb := ranks[a.raider.CharacterID], ranks[b.raider.CharacterID]; ra != rb {
		return ra < rb
	}
	return a.raider.CharacterID.String() < b.raider.CharacterID.String()
}

// signupRanks orders every raider by signup time (ties broken by CharacterID), so
// reasons can say "first signup" / "signup #N" and the bench can be ordered by it.
func signupRanks(raiders []Raider) map[uuid.UUID]int {
	sorted := make([]Raider, len(raiders))
	copy(sorted, raiders)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].SignedUpAt.Equal(sorted[j].SignedUpAt) {
			return sorted[i].SignedUpAt.Before(sorted[j].SignedUpAt)
		}
		return sorted[i].CharacterID.String() < sorted[j].CharacterID.String()
	})
	ranks := make(map[uuid.UUID]int, len(sorted))
	for i, r := range sorted {
		ranks[r.CharacterID] = i
	}
	return ranks
}

func buildAssignments(raiders []Raider, assigned map[uuid.UUID]placement, ranks map[uuid.UUID]int) []Assignment {
	assignedList := make([]Raider, 0, len(assigned))
	benchList := make([]Raider, 0, len(raiders)-len(assigned))
	for _, r := range raiders {
		if _, ok := assigned[r.CharacterID]; ok {
			assignedList = append(assignedList, r)
		} else {
			benchList = append(benchList, r)
		}
	}

	sort.Slice(assignedList, func(i, j int) bool {
		pi, pj := assigned[assignedList[i].CharacterID], assigned[assignedList[j].CharacterID]
		if oi, oj := groupOrder(pi.group), groupOrder(pj.group); oi != oj {
			return oi < oj
		}
		return lessCandidate(candidate{assignedList[i], pi.priority}, candidate{assignedList[j], pj.priority}, ranks)
	})
	// Bench order is signup time alone (design.md section 5, pass 4), not the
	// priority/isMain/score ranking that decides who gets a seat.
	sort.Slice(benchList, func(i, j int) bool {
		if ri, rj := ranks[benchList[i].CharacterID], ranks[benchList[j].CharacterID]; ri != rj {
			return ri < rj
		}
		return benchList[i].CharacterID.String() < benchList[j].CharacterID.String()
	})

	// slot_index is a position within its is_bench partition, not a display order
	// (see migration 00004's comment on comp_slots), so a plain running index per
	// partition satisfies the schema.
	out := make([]Assignment, 0, len(raiders))
	for i, r := range assignedList {
		pl := assigned[r.CharacterID]
		out = append(out, Assignment{
			CharacterID: r.CharacterID,
			Role:        pl.role,
			SlotIndex:   int16(i),
			IsBench:     false,
			Reason:      reasonFor(pl.group, pl.priority, r, ranks[r.CharacterID]),
		})
	}
	for i, r := range benchList {
		out = append(out, Assignment{
			CharacterID: r.CharacterID,
			Role:        benchRole(r),
			SlotIndex:   int16(i),
			IsBench:     true,
			Reason:      fmt.Sprintf("BENCH: no open role, %s", signupPhrase(ranks[r.CharacterID])),
		})
	}
	return out
}

func groupOrder(group string) int {
	switch group {
	case groupTank:
		return 0
	case groupHealer:
		return 1
	default:
		return 2
	}
}

func reasonFor(group string, priority int16, r Raider, rank int) string {
	mainOrAlt := "alt"
	if r.IsMain {
		mainOrAlt = "main"
	}
	return fmt.Sprintf("%s: priority %d, %s, %s", group, priority, mainOrAlt, signupPhrase(rank))
}

func signupPhrase(rank int) string {
	if rank == 0 {
		return "first signup"
	}
	return fmt.Sprintf("signup #%d", rank+1)
}

// benchRole is what a benched raider would have played: their highest-preference
// role choice. comp_slots.role is NOT NULL even for bench rows, which is why Assign
// drops raiders with an empty menu up front: by here every raider has one.
func benchRole(r Raider) db.RoleEnum {
	best := r.Roles[0]
	for _, rc := range r.Roles[1:] {
		if rc.Priority < best.Priority {
			best = rc
		}
	}
	return best.Role
}
