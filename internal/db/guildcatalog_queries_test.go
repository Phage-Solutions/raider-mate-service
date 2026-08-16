//go:build integration

package db

import (
	"context"
	"testing"
)

func TestGuildChannelsRoundTripAndReplace(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	guildID := int64(501)

	channels, err := q.ListGuildChannels(ctx, guildID)
	if err != nil {
		t.Fatalf("listing channels before any push: %v", err)
	}
	if len(channels) != 0 {
		t.Fatalf("channels = %v, want none before any push", channels)
	}

	for _, c := range []InsertGuildChannelParams{
		{DiscordGuildID: guildID, DiscordChannelID: 1, Name: "general", Type: DiscordChannelTypeTEXT},
		{DiscordGuildID: guildID, DiscordChannelID: 2, Name: "raid-announcements", Type: DiscordChannelTypeANNOUNCEMENT},
	} {
		if err := q.InsertGuildChannel(ctx, c); err != nil {
			t.Fatalf("inserting channel %d: %v", c.DiscordChannelID, err)
		}
	}

	channels, err = q.ListGuildChannels(ctx, guildID)
	if err != nil {
		t.Fatalf("listing channels: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("channels = %v, want 2", channels)
	}

	// A PUT replaces the whole catalog: delete then re-insert.
	if err := q.DeleteGuildChannels(ctx, guildID); err != nil {
		t.Fatalf("deleting channels: %v", err)
	}
	if err := q.InsertGuildChannel(ctx, InsertGuildChannelParams{
		DiscordGuildID: guildID, DiscordChannelID: 3, Name: "voice-1", Type: DiscordChannelTypeVOICE,
	}); err != nil {
		t.Fatalf("inserting replacement channel: %v", err)
	}

	channels, err = q.ListGuildChannels(ctx, guildID)
	if err != nil {
		t.Fatalf("listing channels after replace: %v", err)
	}
	if len(channels) != 1 || channels[0].DiscordChannelID != 3 {
		t.Fatalf("channels after replace = %v, want exactly channel 3", channels)
	}
}

func TestGuildRolesRoundTripAndReplace(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	guildID := int64(502)

	roles, err := q.ListGuildRoles(ctx, guildID)
	if err != nil {
		t.Fatalf("listing roles before any push: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("roles = %v, want none before any push", roles)
	}

	for _, r := range []InsertGuildRoleParams{
		{DiscordGuildID: guildID, DiscordRoleID: 781, Name: "Raid Lead", Color: 0xE74C3C, Position: 5},
		{DiscordGuildID: guildID, DiscordRoleID: 799, Name: "Raider", Color: 0x2ECC71, Position: 1},
	} {
		if err := q.InsertGuildRole(ctx, r); err != nil {
			t.Fatalf("inserting role %d: %v", r.DiscordRoleID, err)
		}
	}

	roles, err = q.ListGuildRoles(ctx, guildID)
	if err != nil {
		t.Fatalf("listing roles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("roles = %v, want 2", roles)
	}

	// A PUT replaces the whole catalog: delete then re-insert.
	if err := q.DeleteGuildRoles(ctx, guildID); err != nil {
		t.Fatalf("deleting roles: %v", err)
	}
	if err := q.InsertGuildRole(ctx, InsertGuildRoleParams{
		DiscordGuildID: guildID, DiscordRoleID: 900, Name: "Officer", Color: 0x3498DB, Position: 9,
	}); err != nil {
		t.Fatalf("inserting replacement role: %v", err)
	}

	roles, err = q.ListGuildRoles(ctx, guildID)
	if err != nil {
		t.Fatalf("listing roles after replace: %v", err)
	}
	if len(roles) != 1 || roles[0].DiscordRoleID != 900 {
		t.Fatalf("roles after replace = %v, want exactly role 900", roles)
	}
}
