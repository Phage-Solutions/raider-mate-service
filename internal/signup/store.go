package signup

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Store implements eventStore, signupStore, and lateStore over Postgres.
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: db.New(pool)}
}

// CreateEvent inserts the event and its initial job schedule in one transaction: the
// event's UUIDv7 is only known once the INSERT returns, so the jobs referencing it
// cannot be a separate, riskier round trip.
func (s *Store) CreateEvent(ctx context.Context, in CreateEventInput) (Event, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Event{}, fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := s.queries.WithTx(tx)

	row, err := q.CreateEvent(ctx, db.CreateEventParams{
		DiscordGuildID: in.DiscordGuildID,
		Type:           in.Type,
		Title:          in.Title,
		StartsAt:       pgtype.Timestamptz{Time: in.StartsAt, Valid: true},
		SignupDeadline: pgtype.Timestamptz{Time: in.SignupDeadline, Valid: true},
		CompTemplate:   in.CompTemplate,
		Difficulty:     in.Difficulty,
	})
	if err != nil {
		return Event{}, fmt.Errorf("inserting event: %w", err)
	}

	if err := scheduleJobs(ctx, q, row.ID, in.StartsAt, in.SignupDeadline); err != nil {
		return Event{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Event{}, fmt.Errorf("committing tx: %w", err)
	}
	return eventFromRow(row), nil
}

func (s *Store) GetEvent(ctx context.Context, id uuid.UUID) (Event, error) {
	row, err := s.queries.GetEvent(ctx, id)
	if err != nil {
		return Event{}, err
	}
	return eventFromRow(row), nil
}

// UpdateEvent applies a partial edit and, whenever StartsAt or SignupDeadline moved,
// cancels every PENDING job for the event and reschedules from the new times, all in
// one transaction (design.md section 6: cancel on edit rather than validating at fire
// time).
func (s *Store) UpdateEvent(ctx context.Context, in UpdateEventInput) (Event, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Event{}, fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := s.queries.WithTx(tx)

	params := db.UpdateEventParams{
		ID:           in.ID,
		Title:        in.Title,
		CompTemplate: in.CompTemplate,
		Difficulty:   in.Difficulty,
		MessageID:    in.MessageID,
		ChannelID:    in.ChannelID,
	}
	if in.StartsAt != nil {
		params.StartsAt = pgtype.Timestamptz{Time: *in.StartsAt, Valid: true}
	}
	if in.SignupDeadline != nil {
		params.SignupDeadline = pgtype.Timestamptz{Time: *in.SignupDeadline, Valid: true}
	}

	row, err := q.UpdateEvent(ctx, params)
	if err != nil {
		return Event{}, fmt.Errorf("updating event: %w", err)
	}

	if in.StartsAt != nil || in.SignupDeadline != nil {
		if err := q.CancelJobsForEvent(ctx, row.ID); err != nil {
			return Event{}, fmt.Errorf("cancelling old jobs: %w", err)
		}
		if err := scheduleJobs(ctx, q, row.ID, row.StartsAt.Time, row.SignupDeadline.Time); err != nil {
			return Event{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Event{}, fmt.Errorf("committing tx: %w", err)
	}
	return eventFromRow(row), nil
}

func (s *Store) DeleteEvent(ctx context.Context, id uuid.UUID) error {
	return s.queries.DeleteEvent(ctx, id)
}

// scheduleJobs writes the reminder/deadline schedule jobsFor computes for an event.
func scheduleJobs(ctx context.Context, q *db.Queries, eventID uuid.UUID, startsAt, deadline time.Time) error {
	for _, job := range jobsFor(startsAt, deadline, time.Now()) {
		if err := q.ScheduleJob(ctx, db.ScheduleJobParams{
			EventID: eventID, JobType: job.Kind, RunAt: pgtype.Timestamptz{Time: job.RunAt, Valid: true},
		}); err != nil {
			return fmt.Errorf("scheduling %s: %w", job.Kind, err)
		}
	}
	return nil
}

func eventFromRow(row db.Event) Event {
	return Event{
		ID:             row.ID,
		DiscordGuildID: row.DiscordGuildID,
		Type:           row.Type,
		Title:          row.Title,
		StartsAt:       row.StartsAt.Time,
		SignupDeadline: row.SignupDeadline.Time,
		CompTemplate:   row.CompTemplate,
		MessageID:      row.MessageID,
		ChannelID:      row.ChannelID,
		Difficulty:     row.Difficulty,
	}
}

func (s *Store) UpsertSignup(ctx context.Context, in SignupWrite) (Signup, error) {
	params := db.UpsertSignupParams{
		EventID: in.EventID, CharacterID: in.CharacterID, Status: in.Status, Note: in.Note,
	}
	if in.LateUntil != nil {
		params.LateUntil = pgtype.Timestamptz{Time: *in.LateUntil, Valid: true}
	}
	row, err := s.queries.UpsertSignup(ctx, params)
	if err != nil {
		return Signup{}, err
	}
	return signupFromRow(row), nil
}

func (s *Store) DeleteSignup(ctx context.Context, eventID, characterID uuid.UUID) error {
	return s.queries.DeleteSignup(ctx, db.DeleteSignupParams{EventID: eventID, CharacterID: characterID})
}

func (s *Store) ListSignupsForEvent(ctx context.Context, eventID uuid.UUID) ([]Signup, error) {
	rows, err := s.queries.ListSignupsForEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	signups := make([]Signup, len(rows))
	for i, row := range rows {
		signups[i] = signupFromRow(row)
	}
	return signups, nil
}

func signupFromRow(row db.Signup) Signup {
	signup := Signup{
		ID:           row.ID,
		EventID:      row.EventID,
		CharacterID:  row.CharacterID,
		Status:       row.Status,
		AssignedRole: row.AssignedRole,
		Note:         row.Note,
		CreatedAt:    row.CreatedAt.Time,
	}
	if row.LateUntil.Valid {
		t := row.LateUntil.Time
		signup.LateUntil = &t
	}
	return signup
}

func (s *Store) UpsertLateRequest(ctx context.Context, in LateRequestWrite) (LateRequest, error) {
	row, err := s.queries.UpsertLateRequest(ctx, db.UpsertLateRequestParams{
		EventID: in.EventID, CharacterID: in.CharacterID, Status: in.Status, Note: in.Note,
	})
	if err != nil {
		return LateRequest{}, err
	}
	return lateRequestFromRow(row), nil
}

func (s *Store) GetLateRequest(ctx context.Context, id uuid.UUID) (LateRequest, error) {
	row, err := s.queries.GetLateRequest(ctx, id)
	if err != nil {
		return LateRequest{}, err
	}
	return lateRequestFromRow(row), nil
}

func (s *Store) ListLateRequests(ctx context.Context, eventID uuid.UUID) ([]LateRequest, error) {
	rows, err := s.queries.ListLateRequests(ctx, eventID)
	if err != nil {
		return nil, err
	}
	reqs := make([]LateRequest, len(rows))
	for i, row := range rows {
		reqs[i] = lateRequestFromRow(row)
	}
	return reqs, nil
}

func (s *Store) DecideLateRequest(ctx context.Context, id uuid.UUID, state db.RequestState) error {
	return s.queries.DecideLateRequest(ctx, db.DecideLateRequestParams{ID: id, State: state})
}

func lateRequestFromRow(row db.LateSignupRequest) LateRequest {
	req := LateRequest{
		ID:          row.ID,
		EventID:     row.EventID,
		CharacterID: row.CharacterID,
		Status:      row.Status,
		Note:        row.Note,
		State:       row.State,
		CreatedAt:   row.CreatedAt.Time,
	}
	if row.DecidedAt.Valid {
		t := row.DecidedAt.Time
		req.DecidedAt = &t
	}
	return req
}

func (s *Store) RaidLeadRoleIDs(ctx context.Context, discordGuildID int64) ([]int64, error) {
	return s.queries.ListRaidLeadRoles(ctx, discordGuildID)
}

func (s *Store) InsertNotification(ctx context.Context, n Notification) error {
	return s.queries.InsertNotification(ctx, db.InsertNotificationParams{
		DiscordGuildID: n.DiscordGuildID,
		EventID:        n.EventID,
		Kind:           n.Kind,
		TargetKind:     n.TargetKind,
		DiscordID:      n.DiscordID,
		RoleIds:        n.RoleIDs,
		ChannelID:      n.ChannelID,
		Payload:        n.Payload,
	})
}
