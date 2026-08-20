package signup

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// raidLeadStore is the persistence RaidLeads needs. Declared here, by the consumer.
// RaidLeadRoleIDs is the same method lateStore and reminderStore already declare;
// Store's one implementation of it serves all three.
type raidLeadStore interface {
	RaidLeadRoleIDs(ctx context.Context, discordGuildID int64) ([]int64, error)
	ReplaceRaidLeadRoleIDs(ctx context.Context, discordGuildID int64, roleIDs []int64) error
	// HighestGuildRoleID reports the top of the guild's role hierarchy, and false when
	// the bot has not catalogued the guild's roles yet.
	HighestGuildRoleID(ctx context.Context, discordGuildID int64) (int64, bool, error)
}

// ErrHighestRoleRequired means a mapping was submitted without the guild's highest
// role. Keeping it in is what stops a guild from locking itself out of running raids:
// whoever sits at the top of the hierarchy can always fix the rest.
var ErrHighestRoleRequired = errors.New("the guild's highest role must stay a raid lead role")

// RaidLeads manages the guild-owned mapping from Discord role IDs to the raid-lead
// capability. Many roles, one capability: a guild maps its own, since role names
// vary per guild and the service cannot ask Discord to resolve them (hard rule 6).
type RaidLeads struct {
	store raidLeadStore
}

// NewRaidLeads builds a RaidLeads.
func NewRaidLeads(store raidLeadStore) *RaidLeads {
	return &RaidLeads{store: store}
}

func (r *RaidLeads) List(ctx context.Context, discordGuildID int64) ([]int64, error) {
	roleIDs, err := r.store.RaidLeadRoleIDs(ctx, discordGuildID)
	if err != nil {
		return nil, fmt.Errorf("listing raid lead roles: %w", err)
	}
	return roleIDs, nil
}

// Replace overwrites the whole mapping for a guild: a PUT is the only write this
// resource supports, since role IDs are a set with no per-row meaning of their own.
//
// The guild's highest role has to be in the set. Without that rule an admin can untick
// everything, or tick only a role nobody holds, and the guild is left unable to create
// an event with no way to tell from inside the product why. The check lives here rather
// than in the handler because it is a rule about the mapping, not about HTTP, and the
// bot writes this mapping too.
func (r *RaidLeads) Replace(ctx context.Context, discordGuildID int64, roleIDs []int64) error {
	highest, known, err := r.store.HighestGuildRoleID(ctx, discordGuildID)
	if err != nil {
		return fmt.Errorf("reading the guild's highest role: %w", err)
	}
	// A guild whose roles have never been catalogued has no highest role to insist on.
	// Refusing every write until the bot has synced would be a worse lockout than the
	// one this rule exists to prevent.
	if known && !slices.Contains(roleIDs, highest) {
		return ErrHighestRoleRequired
	}

	if err := r.store.ReplaceRaidLeadRoleIDs(ctx, discordGuildID, roleIDs); err != nil {
		return fmt.Errorf("replacing raid lead roles: %w", err)
	}
	return nil
}
