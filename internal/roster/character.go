package roster

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Character is a roster character, translated out of pgtype.
type Character struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Name       string
	Realm      string
	Region     string
	Class      *string
	Spec       *string
	Ilvl       *float64
	MplusScore *float64
	IsMain     bool
	Synced     bool
}

// RoleChoice is one role a character can play, in signup-menu priority order.
type RoleChoice struct {
	Role     db.RoleEnum
	Priority int16
}

// RegisterInput is what registering a character needs.
type RegisterInput struct {
	DiscordID      int64
	DiscordGuildID int64
	Name           string
	Realm          string
	Region         string
	IsMain         bool
}

// characterStore is the persistence Characters needs. Declared here, by the
// consumer.
type characterStore interface {
	UpsertUser(ctx context.Context, discordID, discordGuildID int64) (uuid.UUID, error)
	CreateCharacter(ctx context.Context, userID uuid.UUID, in RegisterInput) (Character, error)
	GetCharacterInGuild(ctx context.Context, characterID uuid.UUID, discordGuildID int64) (Character, error)
	GetCharacterOwner(ctx context.Context, characterID uuid.UUID, discordGuildID int64) (int64, error)
	DeleteCharacter(ctx context.Context, characterID uuid.UUID, discordGuildID int64) (bool, error)
	SetCharacterMain(ctx context.Context, characterID uuid.UUID, discordGuildID int64, isMain bool) (bool, error)
	ListCharactersInGuild(ctx context.Context, discordGuildID int64) ([]Character, error)
	ListCharactersByDiscord(ctx context.Context, discordID, discordGuildID int64) ([]Character, error)
	ReplaceCharacterRoles(ctx context.Context, characterID uuid.UUID, roles []RoleChoice) error
	ListCharacterRoles(ctx context.Context, characterID uuid.UUID) ([]RoleChoice, error)
}

// Characters registers characters and manages their role menus. Roles live here,
// on the character, never on a signup (hard rule 2): the ephemeral role select in
// design.md section 4 is this write, entirely separate from the signup write it
// precedes.
type Characters struct {
	store characterStore
}

// NewCharacters builds a Characters.
func NewCharacters(store characterStore) *Characters {
	return &Characters{store: store}
}

// Register creates a character, upserting the owning user first. A new character
// has no ilvl until the next worker sync tick (hard rule 5 forbids calling
// Raider.IO from this request handler); Character.Synced reports that honestly.
func (c *Characters) Register(ctx context.Context, in RegisterInput) (Character, error) {
	userID, err := c.store.UpsertUser(ctx, in.DiscordID, in.DiscordGuildID)
	if err != nil {
		return Character{}, fmt.Errorf("upserting user: %w", err)
	}
	character, err := c.store.CreateCharacter(ctx, userID, in)
	if err != nil {
		return Character{}, fmt.Errorf("creating character: %w", err)
	}
	return character, nil
}

// GetInGuild loads a character, scoped to a guild so a caller cannot fetch someone
// else's character through a foreign guild id.
func (c *Characters) GetInGuild(ctx context.Context, characterID uuid.UUID, discordGuildID int64) (Character, error) {
	character, err := c.store.GetCharacterInGuild(ctx, characterID, discordGuildID)
	if err != nil {
		return Character{}, fmt.Errorf("loading character: %w", err)
	}
	return character, nil
}

// InGuild reports whether characterID exists in discordGuildID at all, regardless of
// who owns it. Ownership answers "is this mine"; this answers "is this ours", which
// is the question for a raid lead acting on someone else's character.
func (c *Characters) InGuild(ctx context.Context, characterID uuid.UUID, discordGuildID int64) (bool, error) {
	_, err := c.store.GetCharacterInGuild(ctx, characterID, discordGuildID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("loading character: %w", err)
	}
	return true, nil
}

// OwnedByDiscord reports whether characterID belongs to discordID within
// discordGuildID. A character that does not exist, or lives in another guild, is
// simply not owned: that is a "no", not an infrastructure failure, and callers turn
// it into a 403 rather than a 500 and a spurious error log.
func (c *Characters) OwnedByDiscord(ctx context.Context, characterID uuid.UUID, discordGuildID, discordID int64) (bool, error) {
	owner, err := c.store.GetCharacterOwner(ctx, characterID, discordGuildID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("loading character owner: %w", err)
	}
	return owner == discordID, nil
}

func (c *Characters) ListForGuild(ctx context.Context, discordGuildID int64) ([]Character, error) {
	characters, err := c.store.ListCharactersInGuild(ctx, discordGuildID)
	if err != nil {
		return nil, fmt.Errorf("listing characters: %w", err)
	}
	return characters, nil
}

func (c *Characters) ListForUser(ctx context.Context, discordID, discordGuildID int64) ([]Character, error) {
	characters, err := c.store.ListCharactersByDiscord(ctx, discordID, discordGuildID)
	if err != nil {
		return nil, fmt.Errorf("listing characters: %w", err)
	}
	return characters, nil
}

// ErrCharacterNotFound means no such character exists in that guild. Whether it
// never existed or lives in another guild is deliberately not distinguished.
var ErrCharacterNotFound = errors.New("character not found")

// Delete removes a character. character_roles, signups, comp_slots, snapshots and
// late requests all carry ON DELETE CASCADE, so this takes the raider's history with
// it: it is for a mistyped registration, not for a raider leaving.
func (c *Characters) Delete(ctx context.Context, characterID uuid.UUID, discordGuildID int64) error {
	deleted, err := c.store.DeleteCharacter(ctx, characterID, discordGuildID)
	if err != nil {
		return fmt.Errorf("deleting character: %w", err)
	}
	if !deleted {
		return ErrCharacterNotFound
	}
	return nil
}

// SetMain flags which character is the raider's main. Nothing enforces one main per
// raider: an alt-heavy roster routinely has none, and the flag is a display hint, not
// a constraint the assigner reads.
func (c *Characters) SetMain(ctx context.Context, characterID uuid.UUID, discordGuildID int64, isMain bool) error {
	updated, err := c.store.SetCharacterMain(ctx, characterID, discordGuildID, isMain)
	if err != nil {
		return fmt.Errorf("setting character main: %w", err)
	}
	if !updated {
		return ErrCharacterNotFound
	}
	return nil
}

// SetRoles replaces a character's whole role menu.
func (c *Characters) SetRoles(ctx context.Context, characterID uuid.UUID, roles []RoleChoice) error {
	if err := c.store.ReplaceCharacterRoles(ctx, characterID, roles); err != nil {
		return fmt.Errorf("setting roles: %w", err)
	}
	return nil
}

func (c *Characters) ListRoles(ctx context.Context, characterID uuid.UUID) ([]RoleChoice, error) {
	roles, err := c.store.ListCharacterRoles(ctx, characterID)
	if err != nil {
		return nil, fmt.Errorf("listing roles: %w", err)
	}
	return roles, nil
}
