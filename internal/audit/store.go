package audit

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Store implements analysisStore over Postgres. It maps rows and nothing else: every
// rate and share in this package is computed in Go, so there is one place a percentage
// comes from and it is readable.
type Store struct {
	queries *db.Queries
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: db.New(pool)}
}

func (s *Store) CountEvents(ctx context.Context, discordGuildID int64, p Period) (int64, error) {
	return s.queries.CountEventsInWindow(ctx, db.CountEventsInWindowParams{
		GuildID: discordGuildID,
		Since:   stamp(p.Since),
		Until:   stamp(p.Until),
	})
}

func (s *Store) Attendance(ctx context.Context, discordGuildID int64, p Period) ([]RaiderAttendance, error) {
	rows, err := s.queries.AttendanceByCharacter(ctx, db.AttendanceByCharacterParams{
		GuildID: discordGuildID,
		Since:   stamp(p.Since),
		Until:   stamp(p.Until),
	})
	if err != nil {
		return nil, err
	}

	raiders := make([]RaiderAttendance, len(rows))
	for i, row := range rows {
		raiders[i] = RaiderAttendance{
			Character: CharacterRef{ID: row.CharacterID, Name: row.Name, Realm: row.Realm, Class: row.Class},
			Confirmed: row.Confirmed,
			Tentative: row.Tentative,
			Declined:  row.Declined,
			Late:      row.Late,
			Absent:    row.Absent,
			NoShow:    row.NoShow,
		}
	}
	return raiders, nil
}

func (s *Store) RoleTotals(ctx context.Context, discordGuildID int64, p Period) ([]RoleBalance, error) {
	rows, err := s.queries.CompRoleTotals(ctx, db.CompRoleTotalsParams{
		GuildID: discordGuildID,
		Since:   stamp(p.Since),
		Until:   stamp(p.Until),
	})
	if err != nil {
		return nil, err
	}

	roles := make([]RoleBalance, len(rows))
	for i, row := range rows {
		roles[i] = RoleBalance{Role: row.Role, Placed: row.Placed, Benched: row.Benched}
	}
	return roles, nil
}

func (s *Store) BenchRecords(ctx context.Context, discordGuildID int64, p Period) ([]BenchRecord, error) {
	rows, err := s.queries.BenchByCharacter(ctx, db.BenchByCharacterParams{
		GuildID: discordGuildID,
		Since:   stamp(p.Since),
		Until:   stamp(p.Until),
	})
	if err != nil {
		return nil, err
	}

	bench := make([]BenchRecord, len(rows))
	for i, row := range rows {
		bench[i] = BenchRecord{
			Character: CharacterRef{ID: row.CharacterID, Name: row.Name, Realm: row.Realm, Class: row.Class},
			Boards:    row.Boards,
			Benched:   row.Benched,
		}
	}
	return bench, nil
}

// RoleCoverage takes no period: what a character can play is current fact, not
// history, and windowing it would answer a question nobody asked.
func (s *Store) RoleCoverage(ctx context.Context, discordGuildID int64) ([]RoleCoverage, error) {
	rows, err := s.queries.RoleCoverage(ctx, discordGuildID)
	if err != nil {
		return nil, err
	}

	coverage := make([]RoleCoverage, len(rows))
	for i, row := range rows {
		coverage[i] = RoleCoverage{Role: row.Role, Characters: row.Characters, FirstChoice: row.FirstChoice}
	}
	return coverage, nil
}

func (s *Store) RosterActivity(ctx context.Context, discordGuildID int64, p Period) (int64, int64, int64, error) {
	row, err := s.queries.RosterActivity(ctx, db.RosterActivityParams{
		GuildID: discordGuildID,
		Since:   stamp(p.Since),
		Until:   stamp(p.Until),
	})
	if err != nil {
		return 0, 0, 0, err
	}
	return row.Characters, row.Mains, row.Active, nil
}

func (s *Store) Throughput(ctx context.Context, discordGuildID int64, p Period) ([]ThroughputWeek, error) {
	rows, err := s.queries.EventThroughput(ctx, db.EventThroughputParams{
		GuildID: discordGuildID,
		Since:   stamp(p.Since),
		Until:   stamp(p.Until),
	})
	if err != nil {
		return nil, err
	}

	weeks := make([]ThroughputWeek, len(rows))
	for i, row := range rows {
		weeks[i] = ThroughputWeek{
			Week:      row.Week.Time,
			Events:    row.Events,
			Confirmed: row.Confirmed,
			Declined:  row.Declined,
			NoShow:    row.NoShow,
			Placed:    row.Placed,
			Benched:   row.Benched,
		}
	}
	return weeks, nil
}

func (s *Store) IlvlWeeks(ctx context.Context, discordGuildID int64, p Period) ([]IlvlWeek, error) {
	rows, err := s.queries.IlvlSeries(ctx, db.IlvlSeriesParams{
		GuildID: discordGuildID,
		Since:   stamp(p.Since),
		Until:   stamp(p.Until),
	})
	if err != nil {
		return nil, err
	}

	weeks := make([]IlvlWeek, len(rows))
	for i, row := range rows {
		weeks[i] = IlvlWeek{
			Week:       row.Week.Time,
			Characters: row.Characters,
			P25:        row.P25Ilvl,
			Median:     row.MedianIlvl,
			P75:        row.P75Ilvl,
		}
	}
	return weeks, nil
}

func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
