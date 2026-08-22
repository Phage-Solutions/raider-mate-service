package signup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/db"
)

// maxJobAttempts is how many failed resolutions a job tolerates before it stops
// retrying and flips to FAILED. Without this, attempts is a column nothing ever reads.
const maxJobAttempts = 3

// AttendingSignup is one row from ListAttendingForEvent: a person who said they are
// turning up, with the role they were assigned if the comp has been locked and they
// hold a seat in it.
type AttendingSignup struct {
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
	ListAttendingForEvent(ctx context.Context, eventID uuid.UUID) ([]AttendingSignup, error)
	RaidLeadRoleIDs(ctx context.Context, discordGuildID int64) ([]int64, error)
	GuildSettings(ctx context.Context, discordGuildID int64) (GuildSettings, error)
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
// job is done (marked SENT) without writing anything: a ROLE job with no channel_id to
// post in, or a job of a kind that is no longer sent.
func (r *Runner) buildNotifications(ctx context.Context, tx reminderStore, job db.ScheduledJob) (notifications []Notification, skip bool, err error) {
	event, err := tx.GetEvent(ctx, job.EventID)
	if err != nil {
		return nil, false, fmt.Errorf("loading event: %w", err)
	}

	switch job.JobType {
	case db.JobEnumREMINDER24H:
		return r.buildReminder24h(ctx, tx, event)
	case db.JobEnumREMINDERPREEVENT:
		return r.buildReminderPreEvent(ctx, tx, event)
	case db.JobEnumSIGNUPDEADLINE:
		return r.buildSignupDeadline(ctx, tx, event)
	// Nothing nags about an unlocked comp any more. Rows scheduled by an older release
	// are still in the table, so they are drained rather than left to fail three times.
	case db.JobEnumCOMPNAG:
		return nil, true, nil
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

// reminderPreEventPingPayload is the body of the channel ping. The mentions themselves
// ride on the notification row's discord_ids, the same way a ROLE notification carries
// its role ids there; only what the message says belongs in here.
type reminderPreEventPingPayload struct {
	Title    string    `json:"title"`
	StartsAt time.Time `json:"starts_at"`
	// MessageID lets the bot link back to the signup sheet, which already shows the
	// comp the ping deliberately does not repeat. Nil before the bot has posted.
	MessageID *int64 `json:"message_id"`
}

type reminderPreEventDMPayload struct {
	Title        string       `json:"title"`
	StartsAt     time.Time    `json:"starts_at"`
	AssignedRole *db.RoleEnum `json:"assigned_role"`
}

// buildReminderPreEvent tells everyone who said they are coming that the event is about
// to start. It does not read the comp: a raider left out of a locked twenty still wants
// to know, and an unlocked event has assigned nobody.
//
// The guild chooses how it arrives. A PING is one message in the events channel that
// mentions them all, which is what a raider actually sees ten minutes before an invite;
// DM is one message each, and carries the assigned role when there is one.
func (r *Runner) buildReminderPreEvent(ctx context.Context, tx reminderStore, event Event) ([]Notification, bool, error) {
	rows, err := tx.ListAttendingForEvent(ctx, event.ID)
	if err != nil {
		return nil, false, fmt.Errorf("listing attending: %w", err)
	}
	if len(rows) == 0 {
		return nil, true, nil
	}

	settings, err := tx.GuildSettings(ctx, event.DiscordGuildID)
	if err != nil {
		return nil, false, fmt.Errorf("reading guild settings: %w", err)
	}
	delivery := settings.Delivery()

	var notifications []Notification

	if delivery == db.ReminderDeliveryPING || delivery == db.ReminderDeliveryBOTH {
		if event.ChannelID == nil {
			r.logger.WarnContext(ctx, "pre-event reminder has no channel to ping in", "event_id", event.ID)
		} else {
			payload, err := json.Marshal(reminderPreEventPingPayload{
				Title: event.Title, StartsAt: event.StartsAt, MessageID: event.MessageID,
			})
			if err != nil {
				return nil, false, fmt.Errorf("encoding payload: %w", err)
			}

			mentions := make([]int64, len(rows))
			for i, row := range rows {
				mentions[i] = row.DiscordID
			}
			notifications = append(notifications, Notification{
				DiscordGuildID: event.DiscordGuildID,
				EventID:        event.ID,
				Kind:           db.NotificationKindREMINDERPREEVENT,
				TargetKind:     db.NotificationTargetCHANNEL,
				DiscordIDs:     mentions,
				ChannelID:      event.ChannelID,
				Payload:        payload,
			})
		}
	}

	if delivery == db.ReminderDeliveryDM || delivery == db.ReminderDeliveryBOTH {
		for _, row := range rows {
			payload, err := json.Marshal(reminderPreEventDMPayload{
				Title: event.Title, StartsAt: event.StartsAt, AssignedRole: row.AssignedRole,
			})
			if err != nil {
				return nil, false, fmt.Errorf("encoding payload: %w", err)
			}

			discordID := row.DiscordID
			notifications = append(notifications, Notification{
				DiscordGuildID: event.DiscordGuildID,
				EventID:        event.ID,
				Kind:           db.NotificationKindREMINDERPREEVENT,
				TargetKind:     db.NotificationTargetUSER,
				DiscordID:      &discordID,
				Payload:        payload,
			})
		}
	}

	return notifications, len(notifications) == 0, nil
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
