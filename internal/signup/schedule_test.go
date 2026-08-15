package signup

import (
	"testing"
	"time"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

func TestJobsForSchedulesAllFourWhenFarOut(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startsAt := now.Add(72 * time.Hour)
	deadline := now.Add(48 * time.Hour)

	jobs := jobsFor(startsAt, deadline, now)

	want := map[db.JobEnum]time.Time{
		db.JobEnumSIGNUPDEADLINE: deadline,
		db.JobEnumREMINDER24H:    startsAt.Add(-24 * time.Hour),
		db.JobEnumCOMPNAG:        startsAt.Add(-2 * time.Hour),
		db.JobEnumREMINDER1H:     startsAt.Add(-1 * time.Hour),
	}
	if len(jobs) != len(want) {
		t.Fatalf("jobs = %+v, want %d entries", jobs, len(want))
	}
	for _, j := range jobs {
		runAt, ok := want[j.Kind]
		if !ok {
			t.Fatalf("unexpected job kind %s", j.Kind)
		}
		if !j.RunAt.Equal(runAt) {
			t.Errorf("%s run_at = %v, want %v", j.Kind, j.RunAt, runAt)
		}
	}
}

func TestJobsForUnder24HoursOutSkipsThe24HourReminder(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startsAt := now.Add(3 * time.Hour)
	deadline := now.Add(2 * time.Hour)

	jobs := jobsFor(startsAt, deadline, now)

	kinds := kindsOf(jobs)
	if kinds[db.JobEnumREMINDER24H] {
		t.Errorf("jobs = %+v, want no retroactive REMINDER_24H", jobs)
	}
	if !kinds[db.JobEnumCOMPNAG] || !kinds[db.JobEnumREMINDER1H] || !kinds[db.JobEnumSIGNUPDEADLINE] {
		t.Errorf("jobs = %+v, want SIGNUP_DEADLINE, COMP_NAG, and REMINDER_1H", jobs)
	}
}

func TestJobsForUnder1HourOutSkipsThe1HourReminderToo(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startsAt := now.Add(30 * time.Minute)
	deadline := now.Add(-time.Minute) // deadline already passed

	jobs := jobsFor(startsAt, deadline, now)

	kinds := kindsOf(jobs)
	if kinds[db.JobEnumREMINDER24H] || kinds[db.JobEnumREMINDER1H] || kinds[db.JobEnumCOMPNAG] || kinds[db.JobEnumSIGNUPDEADLINE] {
		t.Errorf("jobs = %+v, want none: every candidate run_at is already in the past", jobs)
	}
}

func kindsOf(jobs []Job) map[db.JobEnum]bool {
	out := make(map[db.JobEnum]bool, len(jobs))
	for _, j := range jobs {
		out[j.Kind] = true
	}
	return out
}
