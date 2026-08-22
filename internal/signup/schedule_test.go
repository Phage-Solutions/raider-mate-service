package signup

import (
	"testing"
	"time"

	"github.com/Raider-Mate/raider-mate-service/internal/db"
)

func TestJobsForSchedulesAllThreeWhenFarOut(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startsAt := now.Add(72 * time.Hour)
	deadline := now.Add(48 * time.Hour)

	jobs := jobsFor(startsAt, deadline, DefaultReminderLeadMinutes, now)

	want := map[db.JobEnum]time.Time{
		db.JobEnumSIGNUPDEADLINE:   deadline,
		db.JobEnumREMINDER24H:      startsAt.Add(-24 * time.Hour),
		db.JobEnumREMINDERPREEVENT: startsAt.Add(-30 * time.Minute),
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

	jobs := jobsFor(startsAt, deadline, DefaultReminderLeadMinutes, now)

	kinds := kindsOf(jobs)
	if kinds[db.JobEnumREMINDER24H] {
		t.Errorf("jobs = %+v, want no retroactive REMINDER_24H", jobs)
	}
	if !kinds[db.JobEnumREMINDERPREEVENT] || !kinds[db.JobEnumSIGNUPDEADLINE] {
		t.Errorf("jobs = %+v, want SIGNUP_DEADLINE and REMINDER_PRE_EVENT", jobs)
	}
}

func TestJobsForInsideTheLeadTimeSkipsThePreEventReminderToo(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startsAt := now.Add(20 * time.Minute)
	deadline := now.Add(-time.Minute) // deadline already passed

	jobs := jobsFor(startsAt, deadline, DefaultReminderLeadMinutes, now)

	kinds := kindsOf(jobs)
	if kinds[db.JobEnumREMINDER24H] || kinds[db.JobEnumREMINDERPREEVENT] || kinds[db.JobEnumSIGNUPDEADLINE] {
		t.Errorf("jobs = %+v, want none: every candidate run_at is already in the past", jobs)
	}
}

func TestJobsForUsesTheLeadTimeItIsGiven(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startsAt := now.Add(72 * time.Hour)
	deadline := now.Add(48 * time.Hour)

	jobs := jobsFor(startsAt, deadline, 90, now)

	for _, j := range jobs {
		if j.Kind != db.JobEnumREMINDERPREEVENT {
			continue
		}
		if want := startsAt.Add(-90 * time.Minute); !j.RunAt.Equal(want) {
			t.Fatalf("run_at = %v, want %v", j.RunAt, want)
		}
		return
	}
	t.Fatalf("jobs = %+v, want a REMINDER_PRE_EVENT", jobs)
}

// Zero is a raid lead switching the reminder off, not an unset value. Scheduling it at
// the start time instead would page everyone as the pull happens.
func TestJobsForLeadOfZeroSchedulesNoPreEventReminder(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startsAt := now.Add(72 * time.Hour)
	deadline := now.Add(48 * time.Hour)

	jobs := jobsFor(startsAt, deadline, 0, now)

	if kindsOf(jobs)[db.JobEnumREMINDERPREEVENT] {
		t.Errorf("jobs = %+v, want no REMINDER_PRE_EVENT", jobs)
	}
	if len(jobs) != 2 {
		t.Errorf("jobs = %+v, want the other two still scheduled", jobs)
	}
}

func kindsOf(jobs []Job) map[db.JobEnum]bool {
	out := make(map[db.JobEnum]bool, len(jobs))
	for _, j := range jobs {
		out[j.Kind] = true
	}
	return out
}
