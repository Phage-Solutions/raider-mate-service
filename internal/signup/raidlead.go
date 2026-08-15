package signup

import (
	"context"
	"fmt"
)

// raidLeadStore is the persistence RaidLeads needs. Declared here, by the consumer.
// RaidLeadRoleIDs is the same method lateStore and reminderStore already declare;
// Store's one implementation of it serves all three.
type raidLeadStore interface {
	RaidLeadRoleIDs(ctx context.Context, discordGuildID int64) ([]int64, error)
	ReplaceRaidLeadRoleIDs(ctx context.Context, discordGuildID int64, roleIDs []int64) error
}

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
func (r *RaidLeads) Replace(ctx context.Context, discordGuildID int64, roleIDs []int64) error {
	if err := r.store.ReplaceRaidLeadRoleIDs(ctx, discordGuildID, roleIDs); err != nil {
		return fmt.Errorf("replacing raid lead roles: %w", err)
	}
	return nil
}
