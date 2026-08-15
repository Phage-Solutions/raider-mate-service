//go:build integration

package db

import (
	"context"
	"testing"
)

func TestRaidLeadRolesRoundTripAndReplace(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	guildID := int64(500)

	count, err := q.CountRaidLeadRoles(ctx, guildID)
	if err != nil {
		t.Fatalf("counting roles before any mapping: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 before any role is mapped (bootstrap case)", count)
	}

	for _, roleID := range []int64{781, 799} {
		if err := q.InsertRaidLeadRole(ctx, InsertRaidLeadRoleParams{
			DiscordGuildID: guildID, DiscordRoleID: roleID,
		}); err != nil {
			t.Fatalf("inserting role %d: %v", roleID, err)
		}
	}

	roles, err := q.ListRaidLeadRoles(ctx, guildID)
	if err != nil {
		t.Fatalf("listing roles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("roles = %v, want 2", roles)
	}

	count, err = q.CountRaidLeadRoles(ctx, guildID)
	if err != nil {
		t.Fatalf("counting roles after mapping: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	// A PUT replaces the whole mapping: delete then re-insert.
	if err := q.DeleteRaidLeadRoles(ctx, guildID); err != nil {
		t.Fatalf("deleting roles: %v", err)
	}
	if err := q.InsertRaidLeadRole(ctx, InsertRaidLeadRoleParams{
		DiscordGuildID: guildID, DiscordRoleID: 900,
	}); err != nil {
		t.Fatalf("inserting replacement role: %v", err)
	}

	roles, err = q.ListRaidLeadRoles(ctx, guildID)
	if err != nil {
		t.Fatalf("listing roles after replace: %v", err)
	}
	if len(roles) != 1 || roles[0] != 900 {
		t.Fatalf("roles after replace = %v, want exactly [900]", roles)
	}
}
