package signup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Store implements eventStore, signupStore, lateStore, and reminderStore over
// Postgres.
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: db.New(pool)}
}

// Transact runs fn against a Store bound to one transaction. Committing or rolling
// back is Transact's job; fn only returns whether its work succeeded.
func (s *Store) Transact(ctx context.Context, fn func(ctx context.Context, tx reminderStore) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	txStore := &Store{pool: s.pool, queries: s.queries.WithTx(tx)}
	if err := fn(ctx, txStore); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing tx: %w", err)
	}
	return nil
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

func (s *Store) ListUpcomingEvents(ctx context.Context, discordGuildID int64) ([]Event, error) {
	rows, err := s.queries.ListUpcomingEvents(ctx, discordGuildID)
	if err != nil {
		return nil, err
	}
	events := make([]Event, len(rows))
	for i, row := range rows {
		events[i] = eventFromRow(row)
	}
	return events, nil
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
	row, err := s.queries.UpsertSignup(ctx, db.UpsertSignupParams{
		EventID: in.EventID, CharacterID: in.CharacterID, Status: in.Status, Note: in.Note,
		LateUntil: timestamptzFromPtr(in.LateUntil),
	})
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
		LateUntil: timestamptzFromPtr(in.LateUntil),
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

// timestamptzFromPtr maps a nil time to SQL NULL. Go's zero time is a real instant,
// so writing it unguarded would store year 1 rather than "unset".
func timestamptzFromPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
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
	if row.LateUntil.Valid {
		t := row.LateUntil.Time
		req.LateUntil = &t
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

// ReplaceRaidLeadRoleIDs overwrites a guild's whole mapping in one transaction: a
// PUT that half-applies would leave a stale role granting the capability alongside
// the caller's intended set.
func (s *Store) ReplaceRaidLeadRoleIDs(ctx context.Context, discordGuildID int64, roleIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := s.queries.WithTx(tx)

	if err := q.DeleteRaidLeadRoles(ctx, discordGuildID); err != nil {
		return fmt.Errorf("clearing existing roles: %w", err)
	}
	for _, roleID := range roleIDs {
		if err := q.InsertRaidLeadRole(ctx, db.InsertRaidLeadRoleParams{
			DiscordGuildID: discordGuildID, DiscordRoleID: roleID,
		}); err != nil {
			return fmt.Errorf("inserting role %d: %w", roleID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing tx: %w", err)
	}
	return nil
}

// GuildSettings reads a guild's configuration. A guild that has configured nothing has
// no row, which is not an error: the zero value is the correct answer, and every guild
// starts there.
func (s *Store) GuildSettings(ctx context.Context, discordGuildID int64) (GuildSettings, error) {
	row, err := s.queries.GetGuildSettings(ctx, discordGuildID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GuildSettings{DiscordGuildID: discordGuildID}, nil
	}
	if err != nil {
		return GuildSettings{}, err
	}
	return GuildSettings{
		DiscordGuildID:      row.DiscordGuildID,
		EventsChannelID:     row.EventsChannelID,
		Timezone:            row.Timezone,
		EventMentionRoleIDs: row.EventMentionRoleIds,
		EventBannerURL:      row.EventBannerUrl,
	}, nil
}

func (s *Store) UpsertGuildSettings(ctx context.Context, settings GuildSettings) (GuildSettings, error) {
	row, err := s.queries.UpsertGuildSettings(ctx, db.UpsertGuildSettingsParams{
		DiscordGuildID:      settings.DiscordGuildID,
		EventsChannelID:     settings.EventsChannelID,
		Timezone:            settings.Timezone,
		EventMentionRoleIds: settings.EventMentionRoleIDs,
		EventBannerUrl:      settings.EventBannerURL,
	})
	if err != nil {
		return GuildSettings{}, err
	}
	return GuildSettings{
		DiscordGuildID:      row.DiscordGuildID,
		EventsChannelID:     row.EventsChannelID,
		Timezone:            row.Timezone,
		EventMentionRoleIDs: row.EventMentionRoleIds,
		EventBannerURL:      row.EventBannerUrl,
	}, nil
}

func (s *Store) ClaimNotifications(ctx context.Context, guildID *int64, claimedBefore time.Time, limit int32) ([]StoredNotification, error) {
	rows, err := s.queries.ClaimNotifications(ctx, db.ClaimNotificationsParams{
		GuildID:       guildID,
		ClaimedBefore: pgtype.Timestamptz{Time: claimedBefore, Valid: true},
		RowLimit:      limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]StoredNotification, len(rows))
	for i, row := range rows {
		out[i] = StoredNotification{
			ID:             row.ID,
			DiscordGuildID: row.DiscordGuildID,
			EventID:        row.EventID,
			Kind:           row.Kind,
			TargetKind:     row.TargetKind,
			DiscordID:      row.DiscordID,
			RoleIDs:        row.RoleIds,
			ChannelID:      row.ChannelID,
			Payload:        row.Payload,
			CreatedAt:      row.CreatedAt.Time,
		}
	}
	return out, nil
}

func (s *Store) MarkNotificationDelivered(ctx context.Context, id uuid.UUID, discordGuildID *int64) error {
	rows, err := s.queries.MarkNotificationDelivered(ctx, db.MarkNotificationDeliveredParams{
		ID: id, GuildID: discordGuildID,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotificationNotFound
	}
	return nil
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

func (s *Store) ClaimDueJobs(ctx context.Context, limit int32) ([]db.ScheduledJob, error) {
	return s.queries.ClaimDueJobs(ctx, limit)
}

func (s *Store) MarkJobSent(ctx context.Context, id uuid.UUID) error {
	return s.queries.MarkJobSent(ctx, id)
}

func (s *Store) MarkJobFailed(ctx context.Context, id uuid.UUID, status db.JobStatus) error {
	return s.queries.MarkJobFailed(ctx, db.MarkJobFailedParams{ID: id, Status: status})
}

func (s *Store) ListUndecidedForEvent(ctx context.Context, eventID uuid.UUID) ([]int64, error) {
	return s.queries.ListUndecidedForEvent(ctx, eventID)
}

func (s *Store) ListConfirmedWithRole(ctx context.Context, eventID uuid.UUID) ([]ConfirmedSignup, error) {
	rows, err := s.queries.ListConfirmedWithRole(ctx, eventID)
	if err != nil {
		return nil, err
	}
	out := make([]ConfirmedSignup, len(rows))
	for i, row := range rows {
		out[i] = ConfirmedSignup{DiscordID: row.DiscordID, AssignedRole: row.AssignedRole}
	}
	return out, nil
}

func (s *Store) CountCompSlotsForEvent(ctx context.Context, eventID uuid.UUID) (int64, error) {
	return s.queries.CountCompSlotsForEvent(ctx, eventID)
}
