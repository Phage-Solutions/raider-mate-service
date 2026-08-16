//go:build integration

package db

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func seedEventForJobs(ctx context.Context, t *testing.T, q *Queries, discordID int64) Event {
	t.Helper()

	event, err := q.CreateEvent(ctx, CreateEventParams{
		ID:             NewID(),
		DiscordGuildID: 100,
		Type:           EventTypeRAID,
		Title:          "Prog Night",
		StartsAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		SignupDeadline: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CompTemplate:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("creating event: %v", err)
	}
	return event
}

func TestClaimDueJobsOnlyClaimsPendingAndDue(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 30)

	due := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Hour)

	if err := q.ScheduleJob(ctx, ScheduleJobParams{
		ID:      NewID(),
		EventID: event.ID, JobType: JobEnumREMINDER24H, RunAt: pgtype.Timestamptz{Time: due, Valid: true},
	}); err != nil {
		t.Fatalf("scheduling due job: %v", err)
	}
	if err := q.ScheduleJob(ctx, ScheduleJobParams{
		ID:      NewID(),
		EventID: event.ID, JobType: JobEnumREMINDERPREEVENT, RunAt: pgtype.Timestamptz{Time: future, Valid: true},
	}); err != nil {
		t.Fatalf("scheduling future job: %v", err)
	}

	claimed, err := q.ClaimDueJobs(ctx, 10)
	if err != nil {
		t.Fatalf("claiming due jobs: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d jobs, want 1 (the due one only)", len(claimed))
	}
	if claimed[0].JobType != JobEnumREMINDER24H {
		t.Errorf("claimed job = %s, want REMINDER_24H", claimed[0].JobType)
	}
}

func TestClaimDueJobsUnderConcurrentTransactionsReturnsDisjointSets(t *testing.T) {
	ctx := context.Background()

	// Seed outside any test transaction so the two claims below see committed rows
	// through their own independent transactions.
	seed, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning seed tx: %v", err)
	}
	qSeed := New(seed)
	event := seedEventForJobs(ctx, t, qSeed, 31)
	due := time.Now().Add(-time.Minute)
	for range 4 {
		if err := qSeed.ScheduleJob(ctx, ScheduleJobParams{
			ID:      NewID(),
			EventID: event.ID, JobType: JobEnumREMINDER24H, RunAt: pgtype.Timestamptz{Time: due, Valid: true},
		}); err != nil {
			t.Fatalf("scheduling job: %v", err)
		}
	}
	if err := seed.Commit(ctx); err != nil {
		t.Fatalf("committing seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM events WHERE id = $1", event.ID)
	})

	// Two independent transactions, not the shared-tx helper: concurrency is the
	// point, and newTxQueries hands out one tx for the whole test.
	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning tx A: %v", err)
	}
	defer func() { _ = txA.Rollback(context.Background()) }()
	qA := New(txA)

	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning tx B: %v", err)
	}
	defer func() { _ = txB.Rollback(context.Background()) }()
	qB := New(txB)

	var claimedA, claimedB []ScheduledJob
	var errA, errB error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		claimedA, errA = qA.ClaimDueJobs(ctx, 2)
	}()
	go func() {
		defer wg.Done()
		claimedB, errB = qB.ClaimDueJobs(ctx, 2)
	}()
	wg.Wait()

	if errA != nil {
		t.Fatalf("claim A: %v", errA)
	}
	if errB != nil {
		t.Fatalf("claim B: %v", errB)
	}
	if len(claimedA) != 2 || len(claimedB) != 2 {
		t.Fatalf("claimed %d and %d jobs, want 2 and 2 (SKIP LOCKED splitting the four)", len(claimedA), len(claimedB))
	}

	seen := make(map[[16]byte]bool, 4)
	for _, j := range append(claimedA, claimedB...) {
		if seen[j.ID] {
			t.Fatalf("job %s claimed by both transactions, want disjoint sets", j.ID)
		}
		seen[j.ID] = true
	}
}

func TestMarkJobFailedIncrementsAttempts(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 32)
	if err := q.ScheduleJob(ctx, ScheduleJobParams{
		ID:      NewID(),
		EventID: event.ID, JobType: JobEnumCOMPNAG,
		RunAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("scheduling job: %v", err)
	}

	claimed, err := q.ClaimDueJobs(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claiming job: claimed=%d err=%v", len(claimed), err)
	}

	if err := q.MarkJobFailed(ctx, MarkJobFailedParams{ID: claimed[0].ID, Status: JobStatusPENDING}); err != nil {
		t.Fatalf("marking job failed (retry): %v", err)
	}

	requeued, err := q.ClaimDueJobs(ctx, 1)
	if err != nil || len(requeued) != 1 {
		t.Fatalf("re-claiming job: claimed=%d err=%v", len(requeued), err)
	}
	if requeued[0].Attempts != 1 {
		t.Errorf("attempts = %d, want 1 after one failed attempt", requeued[0].Attempts)
	}
	if requeued[0].Status != JobStatusPENDING {
		t.Errorf("status = %s, want PENDING (still retryable)", requeued[0].Status)
	}
}

func TestMarkJobSentTransitionsOutOfPending(t *testing.T) {
	ctx := context.Background()
	q, _ := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 33)
	if err := q.ScheduleJob(ctx, ScheduleJobParams{
		ID:      NewID(),
		EventID: event.ID, JobType: JobEnumREMINDERPREEVENT,
		RunAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("scheduling job: %v", err)
	}

	claimed, err := q.ClaimDueJobs(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claiming job: claimed=%d err=%v", len(claimed), err)
	}
	if err := q.MarkJobSent(ctx, claimed[0].ID); err != nil {
		t.Fatalf("marking job sent: %v", err)
	}

	again, err := q.ClaimDueJobs(ctx, 10)
	if err != nil {
		t.Fatalf("re-listing due jobs: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("got %d due jobs after marking SENT, want 0", len(again))
	}
}

func TestCancelJobsForEventTouchesOnlyPending(t *testing.T) {
	ctx := context.Background()
	q, tx := newTxQueries(ctx, t)

	event := seedEventForJobs(ctx, t, q, 34)
	future := pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}

	if err := q.ScheduleJob(ctx, ScheduleJobParams{ID: NewID(), EventID: event.ID, JobType: JobEnumREMINDER24H, RunAt: future}); err != nil {
		t.Fatalf("scheduling pending job: %v", err)
	}
	if err := q.ScheduleJob(ctx, ScheduleJobParams{ID: NewID(), EventID: event.ID, JobType: JobEnumREMINDERPREEVENT, RunAt: future}); err != nil {
		t.Fatalf("scheduling second job: %v", err)
	}

	// Mark one SENT by hand so CancelJobsForEvent has something it must not touch.
	if _, err := tx.Exec(ctx,
		`UPDATE scheduled_jobs SET status = 'SENT' WHERE event_id = $1 AND job_type = $2`,
		event.ID, JobEnumREMINDER24H,
	); err != nil {
		t.Fatalf("marking one job sent: %v", err)
	}

	if err := q.CancelJobsForEvent(ctx, event.ID); err != nil {
		t.Fatalf("cancelling jobs for event: %v", err)
	}

	rows, err := tx.Query(ctx, `SELECT job_type, status FROM scheduled_jobs WHERE event_id = $1`, event.ID)
	if err != nil {
		t.Fatalf("listing jobs after cancel: %v", err)
	}
	defer rows.Close()

	var sent, canceled int
	for rows.Next() {
		var jobType JobEnum
		var status JobStatus
		if err := rows.Scan(&jobType, &status); err != nil {
			t.Fatalf("scanning job row: %v", err)
		}
		switch status {
		case JobStatusSENT:
			sent++
		case JobStatusCANCELED:
			canceled++
		}
	}
	if sent != 1 {
		t.Errorf("sent jobs = %d, want 1 (untouched by cancel)", sent)
	}
	if canceled != 1 {
		t.Errorf("canceled jobs = %d, want 1", canceled)
	}
}
