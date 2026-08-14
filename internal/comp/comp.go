// Package comp owns the assignment algorithm and the comp lock: turning a pool of
// signups into a composition, and persisting that decision.
package comp

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Mode selects which sizing rule Resolve applies. It comes from the event, not the
// Template: Normal and Heroic raids are flex, Mythic raids are locked to exactly 20,
// and Mythic+ is always a single 5-person group.
type Mode int

const (
	ModeRaidFlex   Mode = iota // Normal / Heroic: size = turnout, clamped to [10, 30]
	ModeRaidMythic             // Mythic: size fixed at 20 regardless of turnout
	ModeMythicPlus             // Mythic+: size fixed at 5 regardless of turnout
)

// RoleChoice is one role a character can play, in signup-menu priority order.
// Roles live on the character, not the signup (hard rule 2): this is the menu the
// player picked when they registered the character, not anything about one event.
type RoleChoice struct {
	Role     db.RoleEnum
	Priority int16 // 1 = main spec, higher = more reluctant
}

// Raider is a candidate for a slot in the comp: one confirmed or late signup, with
// the character's role menu and a score for ranking.
type Raider struct {
	CharacterID uuid.UUID
	Name        string
	Roles       []RoleChoice
	IsMain      bool
	Score       float64 // ilvl for a raid, mplus_score for Mythic+
	SignedUpAt  time.Time
}

// Template is the raid lead's configuration for one event's comp_template column.
// Every field is a suggestion override, never a requirement: a nil field means "use
// the derived default," and a set field is honoured as written even when it
// contradicts the suggestion. Nothing in this package rejects a Template. The one
// thing an override cannot exceed is the raid size itself, which is a rule of the
// game rather than a preference; Resolve trims and reports that case.
type Template struct {
	Tanks     *int `json:"tanks,omitempty"`
	Healers   *int `json:"healers,omitempty"`
	MaxMelee  *int `json:"max_melee,omitempty"`
	MaxRanged *int `json:"max_ranged,omitempty"`
}

// Needs is the resolved role targets for one lock: the derived suggestion with any
// Template overrides applied. No field is ever negative.
type Needs struct {
	Tanks     int
	Healers   int
	DPS       int
	MaxMelee  *int
	MaxRanged *int
}

// Advisory is something worth telling the officer without blocking the lock. It
// covers several distinct situations that all read the same way on a comp screen:
// the pool could not fill a role, the lead's own Template departs from the
// suggestion, or a Template value had to be clamped to stay non-negative.
type Advisory struct {
	Role    db.RoleEnum // zero value when not about one specific role
	Message string
}

// Assignment is one raider's outcome from a lock: a seat in the comp, or the bench.
// Role is always set, even on the bench, so a bench list can still be grouped by
// what the raider would have played; comp_slots.role is NOT NULL for exactly this
// reason.
type Assignment struct {
	CharacterID uuid.UUID
	Role        db.RoleEnum
	SlotIndex   int16
	IsBench     bool
	Reason      string
}

// Result is the outcome of Assign: who plays what, and what the officer should know.
type Result struct {
	Assignments []Assignment
	Advisories  []Advisory
}

// Resolve derives a suggested composition for turnout raiders under mode, then
// applies any Template overrides on top. A departure from the suggestion is
// reported as an Advisory, never rejected; a value that would make another count
// negative is clamped to zero and also reported.
func (t Template) Resolve(mode Mode, turnout int) (Needs, []Advisory) {
	size := sizeFor(mode, turnout)

	suggestedTanks := 2
	if mode == ModeMythicPlus {
		suggestedTanks = 1
	}
	// ceilDiv(size, 5) also gives Mythic+'s single healer at size 5, so no separate
	// M+ case is needed here.
	suggestedHealers := ceilDiv(size, 5)

	var advisories []Advisory

	tanks, a := resolveCount(t.Tanks, suggestedTanks, size, db.RoleEnumTANK, "TANK")
	advisories = append(advisories, a...)

	healers, a := resolveCount(t.Healers, suggestedHealers, size, db.RoleEnumHEALER, "HEALER")
	advisories = append(advisories, a...)

	// size is the one thing an override cannot argue with: a Mythic raid seats 20 or
	// nobody zones in, and a flex raid tops out at 30. Trim in fill order so the
	// scarcest role keeps its seats.
	if tanks > size {
		advisories = append(advisories, Advisory{
			Role:    db.RoleEnumTANK,
			Message: fmt.Sprintf("TANK: %d exceeds the %d-raider size, trimmed to %d", tanks, size, size),
		})
		tanks = size
	}
	if tanks+healers > size {
		trimmed := size - tanks
		advisories = append(advisories, Advisory{
			Role: db.RoleEnumHEALER,
			Message: fmt.Sprintf(
				"HEALER: %d leaves the %d-raider size short of TANK (%d), trimmed to %d",
				healers, size, tanks, trimmed,
			),
		})
		healers = trimmed
	}

	dps := size - tanks - healers
	if dps == 0 && size > 0 {
		advisories = append(advisories, Advisory{
			Message: fmt.Sprintf("DPS: 0, TANK+HEALER (%d) fills the %d-raider size", tanks+healers, size),
		})
	}

	maxMelee, a := resolveCap(t.MaxMelee, "max_melee")
	advisories = append(advisories, a...)
	maxRanged, a := resolveCap(t.MaxRanged, "max_ranged")
	advisories = append(advisories, a...)

	if maxMelee != nil && maxRanged != nil {
		if capacity := *maxMelee + *maxRanged; capacity < dps {
			advisories = append(advisories, Advisory{
				Message: fmt.Sprintf("DPS: caps allow at most %d, need is %d", capacity, dps),
			})
		}
	}

	return Needs{
		Tanks:     tanks,
		Healers:   healers,
		DPS:       dps,
		MaxMelee:  maxMelee,
		MaxRanged: maxRanged,
	}, advisories
}

// resolveCap clamps a DPS side cap at zero. A negative cap is not merely useless:
// chooseDPSRole compares count < *cap, so it closes the side permanently and every
// raider who can only play it benches with no explanation.
func resolveCap(limit *int, label string) (*int, []Advisory) {
	if limit == nil || *limit >= 0 {
		return limit, nil
	}
	clamped := 0
	return &clamped, []Advisory{{
		Message: fmt.Sprintf("%s: %d is negative, clamped to 0", label, *limit),
	}}
}

// resolveCount applies one Template override on top of a suggested count: honoured
// exactly as written, with an advisory when it departs from the suggestion, and
// clamped to zero (with a further advisory) if it was set negative.
func resolveCount(override *int, suggested, size int, role db.RoleEnum, label string) (int, []Advisory) {
	count := suggested
	var advisories []Advisory

	if override != nil {
		count = *override
		if count != suggested {
			advisories = append(advisories, Advisory{
				Role: role,
				Message: fmt.Sprintf(
					"%s: %d, suggestion for %d raiders is %d", label, count, size, suggested,
				),
			})
		}
	}

	if count < 0 {
		advisories = append(advisories, Advisory{
			Role:    role,
			Message: fmt.Sprintf("%s: %d is negative, clamped to 0", label, count),
		})
		count = 0
	}

	return count, advisories
}

// sizeFor is the raid-size rule per Mode. Normal and Heroic are flex; Mythic and
// Mythic+ are fixed regardless of turnout, so a short pool falls out as an ordinary
// per-role shortfall Advisory rather than needing a special case.
func sizeFor(mode Mode, turnout int) int {
	switch mode {
	case ModeRaidMythic:
		return 20
	case ModeMythicPlus:
		return 5
	default: // ModeRaidFlex
		return clamp(turnout, 10, 30)
	}
}

func clamp(n, lo, hi int) int {
	return max(lo, min(n, hi))
}

func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}
