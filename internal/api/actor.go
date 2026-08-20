package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// Actor is who is making this request, translated out of Discord's raw facts into
// what this service's authorization decisions need. The bot reports the facts; the
// service decides what they mean, since Discord role names vary per guild and the
// service cannot ask Discord itself to check (hard rule 6).
type Actor struct {
	DiscordID    uint64
	GuildID      uint64
	RoleIDs      []uint64
	IsGuildAdmin bool
	// IsRaidLead is resolved once by the auth middleware against
	// guild_raid_lead_roles, not recomputed by every handler that needs it.
	IsRaidLead bool
}

// actorFromHeaders parses the raw actor facts. It does not resolve IsRaidLead: that
// needs a database lookup, which the auth middleware does once per request.
func actorFromHeaders(h http.Header) (Actor, error) {
	discordID, err := parseSnowflake(h.Get("X-Actor-Discord-Id"))
	if err != nil {
		return Actor{}, fmt.Errorf("X-Actor-Discord-Id: %w", err)
	}
	guildID, err := parseSnowflake(h.Get("X-Actor-Guild-Id"))
	if err != nil {
		return Actor{}, fmt.Errorf("X-Actor-Guild-Id: %w", err)
	}
	roleIDs, err := parseSnowflakeList(h.Get("X-Actor-Roles"))
	if err != nil {
		return Actor{}, fmt.Errorf("X-Actor-Roles: %w", err)
	}
	isAdmin, err := parseBool(h.Get("X-Actor-Guild-Admin"))
	if err != nil {
		return Actor{}, fmt.Errorf("X-Actor-Guild-Admin: %w", err)
	}

	return Actor{DiscordID: discordID, GuildID: guildID, RoleIDs: roleIDs, IsGuildAdmin: isAdmin}, nil
}

// resolveIsRaidLead decides IsRaidLead from a guild's mapped role IDs, and from
// nothing else. Holding one of the roles the guild mapped is the whole test.
//
// Discord's own administrator flag used to qualify on its own, as a bootstrap so a
// guild that had mapped nothing was not stuck. It no longer does, because running
// raids and administering a Discord server are different jobs and plenty of guilds
// hand the second to people who should not be editing the raid schedule. The bootstrap
// still works without the shortcut: mapping raid-lead roles and editing guild settings
// are gated on IsGuildAdmin separately, so an admin of a fresh guild maps a role and
// then holds the capability through it, like everyone else.
//
// A guild with no mapped roles therefore grants the capability to nobody, which is the
// honest reading of "nobody has said who runs raids yet".
func resolveIsRaidLead(actor Actor, raidLeadRoleIDs []uint64) bool {
	for _, role := range actor.RoleIDs {
		if slices.Contains(raidLeadRoleIDs, role) {
			return true
		}
	}
	return false
}

// MayConfigureGuild reports whether an actor may read and change a guild's
// configuration: its raid-lead role mapping, its event settings, and the Discord
// catalogue behind both.
//
// Discord admins and raid leads both qualify. Running the server and running its raids
// are different jobs, but configuring how raids run is squarely the second one, and a
// raid lead who cannot set the events channel has to go and find an admin for it. The
// admin half also keeps the bootstrap open: raid leads are defined here, so a guild
// that has mapped nothing still has someone who can map it.
func (a Actor) MayConfigureGuild() bool {
	return a.IsGuildAdmin || a.IsRaidLead
}

// parseSnowflake parses a Discord snowflake. It stays a string on the wire and
// through this parse because a snowflake exceeds 2^53 and does not survive a
// round trip through JSON's float64 number type unscathed.
func parseSnowflake(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("missing")
	}
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid snowflake %q: %w", s, err)
	}
	// Zero is never a real snowflake, and an actor whose guild id is 0 would scope
	// every guild-scoped query to a guild that cannot exist.
	if id == 0 {
		return 0, fmt.Errorf("invalid snowflake %q", s)
	}
	return id, nil
}

func parseSnowflakeList(s string) ([]uint64, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	ids := make([]uint64, len(parts))
	for i, p := range parts {
		id, err := parseSnowflake(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func parseBool(s string) (bool, error) {
	if s == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("invalid bool %q: %w", s, err)
	}
	return b, nil
}

type actorContextKey struct{}

// withActor stores actor on the request context.
func withActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

// actorFromContext retrieves the actor the auth middleware resolved and stored.
// Handlers call this rather than parsing headers themselves.
func actorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorContextKey{}).(Actor)
	return actor, ok
}
