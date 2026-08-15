package api

import (
	"context"
	"net/http"
	"testing"
)

func headers(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestActorFromHeadersParsesSnowflakesAsUint64(t *testing.T) {
	// A snowflake exceeding 2^53, to make sure it survives ParseUint intact.
	h := headers(
		"X-Actor-Discord-Id", "9223372036854775807",
		"X-Actor-Guild-Id", "902123456789012345",
		"X-Actor-Roles", "781111111111111111,799222222222222222",
		"X-Actor-Guild-Admin", "true",
	)

	actor, err := actorFromHeaders(h)
	if err != nil {
		t.Fatalf("actorFromHeaders: %v", err)
	}
	if actor.DiscordID != 9223372036854775807 {
		t.Errorf("discord_id = %d, want 9223372036854775807", actor.DiscordID)
	}
	if actor.GuildID != 902123456789012345 {
		t.Errorf("guild_id = %d, want 902123456789012345", actor.GuildID)
	}
	if len(actor.RoleIDs) != 2 || actor.RoleIDs[0] != 781111111111111111 || actor.RoleIDs[1] != 799222222222222222 {
		t.Errorf("role_ids = %v, want the two parsed roles", actor.RoleIDs)
	}
	if !actor.IsGuildAdmin {
		t.Errorf("is_guild_admin = false, want true")
	}
}

func TestActorFromHeadersDefaultsMissingRolesAndAdmin(t *testing.T) {
	h := headers("X-Actor-Discord-Id", "1", "X-Actor-Guild-Id", "2")

	actor, err := actorFromHeaders(h)
	if err != nil {
		t.Fatalf("actorFromHeaders: %v", err)
	}
	if actor.RoleIDs != nil {
		t.Errorf("role_ids = %v, want nil for an absent header", actor.RoleIDs)
	}
	if actor.IsGuildAdmin {
		t.Errorf("is_guild_admin = true, want false by default")
	}
}

func TestActorFromHeadersRejectsAMissingDiscordID(t *testing.T) {
	h := headers("X-Actor-Guild-Id", "2")

	if _, err := actorFromHeaders(h); err == nil {
		t.Fatalf("actorFromHeaders = nil error, want one for the missing discord id")
	}
}

func TestActorFromHeadersRejectsAnUnparseableRole(t *testing.T) {
	h := headers("X-Actor-Discord-Id", "1", "X-Actor-Guild-Id", "2", "X-Actor-Roles", "781,not-a-number")

	if _, err := actorFromHeaders(h); err == nil {
		t.Fatalf("actorFromHeaders = nil error, want one for the unparseable role")
	}
}

func TestResolveIsRaidLeadForAMappedRole(t *testing.T) {
	actor := Actor{RoleIDs: []uint64{781, 555}}
	if !resolveIsRaidLead(actor, []uint64{781, 799}) {
		t.Errorf("resolveIsRaidLead = false, want true: 781 is mapped")
	}
}

func TestResolveIsRaidLeadForAnUnmappedRole(t *testing.T) {
	actor := Actor{RoleIDs: []uint64{555}}
	if resolveIsRaidLead(actor, []uint64{781, 799}) {
		t.Errorf("resolveIsRaidLead = true, want false: 555 is not mapped")
	}
}

func TestResolveIsRaidLeadAdminFallback(t *testing.T) {
	actor := Actor{IsGuildAdmin: true, RoleIDs: []uint64{555}}
	if !resolveIsRaidLead(actor, []uint64{781, 799}) {
		t.Errorf("resolveIsRaidLead = false, want true: admins always qualify")
	}
}

func TestResolveIsRaidLeadEmptyMappingBootstrap(t *testing.T) {
	admin := Actor{IsGuildAdmin: true}
	if !resolveIsRaidLead(admin, nil) {
		t.Errorf("resolveIsRaidLead(admin, nil) = false, want true: bootstrap treats admins as raid leads")
	}

	player := Actor{RoleIDs: []uint64{555}}
	if resolveIsRaidLead(player, nil) {
		t.Errorf("resolveIsRaidLead(player, nil) = true, want false: an unmapped guild grants nobody else the capability")
	}
}

func TestActorContextRoundTrips(t *testing.T) {
	want := Actor{DiscordID: 1, GuildID: 2, IsRaidLead: true}
	ctx := withActor(context.Background(), want)

	got, ok := actorFromContext(ctx)
	if !ok {
		t.Fatalf("actorFromContext: not found")
	}
	if got.DiscordID != want.DiscordID || got.GuildID != want.GuildID || got.IsRaidLead != want.IsRaidLead {
		t.Errorf("actor = %+v, want %+v", got, want)
	}
}

func TestActorFromContextMissing(t *testing.T) {
	if _, ok := actorFromContext(context.Background()); ok {
		t.Errorf("actorFromContext = found, want not found on a bare context")
	}
}
