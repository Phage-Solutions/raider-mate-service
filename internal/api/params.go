package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/db"
	"github.com/Raider-Mate/raider-mate-service/internal/signup"
)

// pathUUID parses a path value as a UUID.
func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", name, err)
	}
	return id, nil
}

// pathSnowflake parses a path value as a Discord snowflake. Zero and negatives are
// rejected: a snowflake encodes a millisecond timestamp since 2015 in its high bits,
// so it is always positive, and letting "0" or "-1" through means a guild id that
// matches no guild travelling all the way to a query.
func pathSnowflake(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if id <= 0 {
		return 0, fmt.Errorf("%s: %d is not a valid snowflake", name, id)
	}
	return id, nil
}

// The enum parsers below exist because a bare db.SignupStatus("nonsense") conversion
// compiles happily and only fails at the INSERT, where Postgres reports an invalid
// enum value. That surfaces as a 500 and an error-log line for what is a client
// mistake: "raid" instead of "RAID" should be a 400 the caller can act on.

func parseEnum[T ~string](value string, allowed []T, what string) (T, error) {
	for _, a := range allowed {
		if string(a) == value {
			return a, nil
		}
	}
	return "", fmt.Errorf("%s: %q is not one of %v", what, value, allowed)
}

func parseEventType(s string) (db.EventType, error) {
	return parseEnum(s, []db.EventType{db.EventTypeRAID, db.EventTypeMYTHICPLUS}, "type")
}

// parseSignupStatus accepts every value of the enum. Which of them this caller may
// actually write is Signups.Write's answer, not a parse error.
func parseSignupStatus(s string) (db.SignupStatus, error) {
	return parseEnum(s, signup.AllStatuses(), "status")
}

func parseRaidDifficulty(s string) (db.RaidDifficulty, error) {
	return parseEnum(s, []db.RaidDifficulty{
		db.RaidDifficultyNORMAL, db.RaidDifficultyHEROIC, db.RaidDifficultyMYTHIC,
	}, "difficulty")
}

func parseRole(s string) (db.RoleEnum, error) {
	return parseEnum(s, []db.RoleEnum{
		db.RoleEnumTANK, db.RoleEnumHEALER, db.RoleEnumMDPS, db.RoleEnumRDPS,
	}, "role")
}

func parseCompMode(s string) (db.CompMode, error) {
	return parseEnum(s, []db.CompMode{db.CompModeAUTO, db.CompModeMANUAL}, "mode")
}

func parseReminderDelivery(s string) (db.ReminderDelivery, error) {
	return parseEnum(s, []db.ReminderDelivery{
		db.ReminderDeliveryPING, db.ReminderDeliveryDM, db.ReminderDeliveryBOTH,
	}, "reminder_delivery")
}

func parseChannelType(s string) (db.DiscordChannelType, error) {
	return parseEnum(s, []db.DiscordChannelType{
		db.DiscordChannelTypeTEXT, db.DiscordChannelTypeANNOUNCEMENT, db.DiscordChannelTypeVOICE,
		db.DiscordChannelTypeSTAGEVOICE, db.DiscordChannelTypeFORUM, db.DiscordChannelTypeCATEGORY,
		db.DiscordChannelTypeOTHER,
	}, "type")
}
