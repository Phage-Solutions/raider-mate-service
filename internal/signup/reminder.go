package signup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// maxJobAttempts is how many failed resolutions a job tolerates before it stops
// retrying and flips to FAILED. Without this, attempts is a column nothing ever reads.
const maxJobAttempts = 3

// ConfirmedSignup is one row from ListConfirmedWithRole: a person who holds an
// assigned seat, whether or not their signup status literally reads CONFIRMED (a
// LATE raider with a seat still needs the hour-out reminder).
type ConfirmedSignup struct {
	DiscordID    int64
	AssignedRole *db.RoleEnum
}

// reminderStore is the persistence Runner needs. Declared here, by the consumer.
type reminderStore interface {
	// Transact runs fn against a store bound to one transaction: fn's writes commit
	// together or not at all, and ClaimDueJobs's FOR UPDATE SKIP LOCKED lock is held
	// for fn's whole duration. A crash inside fn rolls the transaction back, so any
	// job fn had not yet reached MarkJobSent/MarkJobFailed for is still PENDING, per
	// design.md section 6 and the "no lease" rationale in migration 00005's comment.
	Transact(ctx context.Context, fn func(ctx context.Context, tx reminderStore) error) error

	ClaimDueJobs(ctx context.Context, limit int32) ([]db.ScheduledJob, error)
	MarkJobSent(ctx context.Context, id uuid.UUID) error
	MarkJobFailed(ctx context.Context, id uuid.UUID, status db.JobStatus) error

	GetEvent(ctx context.Context, id uuid.UUID) (Event, error)
	ListSignupsForEvent(ctx context.Context, eventID uuid.UUID) ([]Signup, error)
	ListUndecidedForEvent(ctx context.Context, eventID uuid.UUID) ([]int64, error)
	ListConfirmedWithRole(ctx context.Context, eventID uuid.UUID) ([]ConfirmedSignup, error)
	CountCompSlotsForEvent(ctx context.Context, eventID uuid.UUID) (int64, error)
	RaidLeadRoleIDs(ctx context.Context, discordGuildID int64) ([]int64, error)
	InsertNotification(ctx context.Context, n Notification) error
}

// Runner drains due scheduled_jobs into the notifications outbox.
type Runner struct {
	store  reminderStore
	logger *slog.Logger
}

// NewRunner builds a Runner.
func NewRunner(store reminderStore, logger *slog.Logger) *Runner {
	return &Runner{store: store, logger: logger}
}

// RunDue claims up to limit due jobs and resolves each into notification rows, all in
// one transaction. A job's own resolution failure is recorded via MarkJobFailed and
// the loop moves on, matching roster.Syncer.SyncDue; an error from the transactional
// store itself aborts the whole tick, since continuing to issue writes against a
// failed connection cannot do anything useful and the transaction rolls everything
// back regardless.
func (r *Runner) RunDue(ctx context.Context, limit int32) error {
	return r.store.Transact(ctx, func(ctx context.Context, tx reminderStore) error {
		jobs, err := tx.ClaimDueJobs(ctx, limit)
		if err != nil {
			return fmt.Errorf("claiming due jobs: %w", err)
		}

		for _, job := range jobs {
			if err := r.resolve(ctx, tx, job); err != nil {
				return fmt.Errorf("resolving job %s: %w", job.ID, err)
			}
		}
		return nil
	})
}

// resolve builds and writes the notifications for one job, then marks it SENT, or
// records a failed attempt on it. The returned error is infrastructure-class only:
// a business-level outcome (nothing to notify, no channel to post in, a resolution
// error) is fully handled here and never bubbles up to abort the tick.
func (r *Runner) resolve(ctx context.Context, tx reminderStore, job db.ScheduledJob) error {
	notifications, skip, err := r.buildNotifications(ctx, tx, job)
	if err != nil {
		r.logger.ErrorContext(ctx, "resolving job", "job_id", job.ID, "job_type", job.JobType, "error", err)

		status := db.JobStatusPENDING
		if job.Attempts+1 >= maxJobAttempts {
			status = db.JobStatusFAILED
		}
		if err := tx.MarkJobFailed(ctx, job.ID, status); err != nil {
			return fmt.Errorf("marking job failed: %w", err)
		}
		return nil
	}

	if !skip {
		for _, n := range notifications {
			if err := tx.InsertNotification(ctx, n); err != nil {
				return fmt.Errorf("inserting notification: %w", err)
			}
		}
	}

	if err := tx.MarkJobSent(ctx, job.ID); err != nil {
		return fmt.Errorf("marking job sent: %w", err)
	}
	return nil
}

// buildNotifications resolves recipients and payloads for one job. skip means the
// job is done (marked SENT) without writing anything: a COMP_NAG on an already-locked
// comp, or a ROLE job with no channel_id to post in.
func (r *Runner) buildNotifications(ctx context.Context, tx reminderStore, job db.ScheduledJob) (notifications []Notification, skip bool, err error) {
	event, err := tx.GetEvent(ctx, job.EventID)
	if err != nil {
		return nil, false, fmt.Errorf("loading event: %w", err)
	}

	switch job.JobType {
	case db.JobEnumREMINDER24H:
		return r.buildReminder24h(ctx, tx, event)
	case db.JobEnumREMINDER1H:
		return r.buildReminder1h(ctx, tx, event)
	case db.JobEnumSIGNUPDEADLINE:
		return r.buildSignupDeadline(ctx, tx, event)
	case db.JobEnumCOMPNAG:
		return r.buildCompNag(ctx, tx, event)
	default:
		return nil, false, fmt.Errorf("unknown job type %q", job.JobType)
	}
}

type reminder24hPayload struct {
	Title    string    `json:"title"`
	StartsAt time.Time `json:"starts_at"`
	Deadline time.Time `json:"deadline"`
}

// buildReminder24h DMs whoever has not answered. ListUndecidedForEvent already
// groups by discord_id, not by character, so a raider with four unsigned alts gets
// one row here, not four.
func (r *Runner) buildReminder24h(ctx context.Context, tx reminderStore, event Event) ([]Notification, bool, error) {
	discordIDs, err := tx.ListUndecidedForEvent(ctx, event.ID)
	if err != nil {
		return nil, false, fmt.Errorf("listing undecided: %w", err)
	}

	payload, err := json.Marshal(reminder24hPayload{
		Title: event.Title, StartsAt: event.StartsAt, Deadline: event.SignupDeadline,
	})
	if err != nil {
		return nil, false, fmt.Errorf("encoding payload: %w", err)
	}

	notifications := make([]Notification, len(discordIDs))
	for i, discordID := range discordIDs {
		notifications[i] = Notification{
			DiscordGuildID: event.DiscordGuildID,
			EventID:        event.ID,
			Kind:           db.NotificationKindREMINDER24H,
			TargetKind:     db.NotificationTargetUSER,
			DiscordID:      &discordID,
			Payload:        payload,
		}
	}
	return notifications, false, nil
}

type reminder1hPayload struct {
	Title        string       `json:"title"`
	StartsAt     time.Time    `json:"starts_at"`
	AssignedRole *db.RoleEnum `json:"assigned_role"`
}

// buildReminder1h DMs whoever holds an assigned seat, each with their own role.
func (r *Runner) buildReminder1h(ctx context.Context, tx reminderStore, event Event) ([]Notification, bool, error) {
	rows, err := tx.ListConfirmedWithRole(ctx, event.ID)
	if err != nil {
		return nil, false, fmt.Errorf("listing confirmed: %w", err)
	}

	notifications := make([]Notification, len(rows))
	for i, row := range rows {
		payload, err := json.Marshal(reminder1hPayload{
			Title: event.Title, StartsAt: event.StartsAt, AssignedRole: row.AssignedRole,
		})
		if err != nil {
			return nil, false, fmt.Errorf("encoding payload: %w", err)
		}

		discordID := row.DiscordID
		notifications[i] = Notification{
			DiscordGuildID: event.DiscordGuildID,
			EventID:        event.ID,
			Kind:           db.NotificationKindREMINDER1H,
			TargetKind:     db.NotificationTargetUSER,
			DiscordID:      &discordID,
			Payload:        payload,
		}
	}
	return notifications, false, nil
}

type signupDeadlinePayload struct {
	Title  string                  `json:"title"`
	Counts map[db.SignupStatus]int `json:"counts"`
}

// buildSignupDeadline pings the raid lead with signup counts by status. Signups
// themselves are already read-only past the deadline, since the deadline gate reads
// events.signup_deadline directly; this job only notifies.
func (r *Runner) buildSignupDeadline(ctx context.Context, tx reminderStore, event Event) ([]Notification, bool, error) {
	if event.ChannelID == nil {
		r.logger.WarnContext(ctx, "SIGNUP_DEADLINE has no channel to post in", "event_id", event.ID)
		return nil, true, nil
	}

	signups, err := tx.ListSignupsForEvent(ctx, event.ID)
	if err != nil {
		return nil, false, fmt.Errorf("listing signups: %w", err)
	}
	// Seeded with every status rather than only the ones present, so the bot can
	// render "0 absent" without knowing the enum itself.
	all := AllStatuses()
	counts := make(map[db.SignupStatus]int, len(all))
	for _, status := range all {
		counts[status] = 0
	}
	for _, s := range signups {
		counts[s.Status]++
	}

	roleIDs, err := tx.RaidLeadRoleIDs(ctx, event.DiscordGuildID)
	if err != nil {
		return nil, false, fmt.Errorf("loading raid lead roles: %w", err)
	}
	payload, err := json.Marshal(signupDeadlinePayload{Title: event.Title, Counts: counts})
	if err != nil {
		return nil, false, fmt.Errorf("encoding payload: %w", err)
	}

	return []Notification{{
		DiscordGuildID: event.DiscordGuildID,
		EventID:        event.ID,
		Kind:           db.NotificationKindSIGNUPDEADLINE,
		TargetKind:     db.NotificationTargetROLE,
		RoleIDs:        roleIDs,
		ChannelID:      event.ChannelID,
		Payload:        payload,
	}}, false, nil
}

type compNagPayload struct {
	Title    string    `json:"title"`
	StartsAt time.Time `json:"starts_at"`
}

// buildCompNag pings the raid lead only if nothing has been locked yet. "Locked" is
// inferred from comp_slots existing for the event (internal/db/queries/signup.sql's
// CountCompSlotsForEvent comment): there is no separate lock timestamp, and this is
// true by construction, since locking is what writes slots.
func (r *Runner) buildCompNag(ctx context.Context, tx reminderStore, event Event) ([]Notification, bool, error) {
	count, err := tx.CountCompSlotsForEvent(ctx, event.ID)
	if err != nil {
		return nil, false, fmt.Errorf("counting comp slots: %w", err)
	}
	if count > 0 {
		return nil, true, nil
	}
	if event.ChannelID == nil {
		r.logger.WarnContext(ctx, "COMP_NAG has no channel to post in", "event_id", event.ID)
		return nil, true, nil
	}

	roleIDs, err := tx.RaidLeadRoleIDs(ctx, event.DiscordGuildID)
	if err != nil {
		return nil, false, fmt.Errorf("loading raid lead roles: %w", err)
	}
	payload, err := json.Marshal(compNagPayload{Title: event.Title, StartsAt: event.StartsAt})
	if err != nil {
		return nil, false, fmt.Errorf("encoding payload: %w", err)
	}

	return []Notification{{
		DiscordGuildID: event.DiscordGuildID,
		EventID:        event.ID,
		Kind:           db.NotificationKindCOMPNAG,
		TargetKind:     db.NotificationTargetROLE,
		RoleIDs:        roleIDs,
		ChannelID:      event.ChannelID,
		Payload:        payload,
	}}, false, nil
}
