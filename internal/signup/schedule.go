package signup

import (
	"time"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// Job is one job jobsFor decided to schedule: a kind and when it should run.
type Job struct {
	Kind  db.JobEnum
	RunAt time.Time
}

// jobsFor computes the reminder/deadline schedule for an event from its timing alone
// (design.md section 6). A job whose run_at already lies in the past relative to now
// is not scheduled: an event created 3 hours out gets COMP_NAG and REMINDER_1H only,
// never a retroactive 24-hour reminder.
func jobsFor(startsAt, deadline, now time.Time) []Job {
	candidates := []Job{
		{Kind: db.JobEnumSIGNUPDEADLINE, RunAt: deadline},
		{Kind: db.JobEnumREMINDER24H, RunAt: startsAt.Add(-24 * time.Hour)},
		{Kind: db.JobEnumCOMPNAG, RunAt: startsAt.Add(-2 * time.Hour)},
		{Kind: db.JobEnumREMINDER1H, RunAt: startsAt.Add(-1 * time.Hour)},
	}

	jobs := make([]Job, 0, len(candidates))
	for _, c := range candidates {
		if c.RunAt.After(now) {
			jobs = append(jobs, c)
		}
	}
	return jobs
}
