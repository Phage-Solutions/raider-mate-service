package api

import (
	"slices"
	"testing"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
	"github.com/Phage-Solutions/raider-mate-service/internal/signup"
)

func TestSignupLinksPresentWhenActorCanAct(t *testing.T) {
	links := signupLinks("event-1", "char-1", true)

	if _, ok := links["self"]; !ok {
		t.Errorf("links = %v, want self present", links)
	}
	if _, ok := links["withdraw"]; !ok {
		t.Errorf("links = %v, want withdraw present", links)
	}
}

func TestSignupLinksAbsentForAnUnrelatedActor(t *testing.T) {
	links := signupLinks("event-1", "char-1", false)

	if len(links) != 0 {
		t.Errorf("links = %v, want none: the absence of a link is the authorization answer", links)
	}
}

func TestSignupAllowedStatusesMatchWhatTheCallerMayWrite(t *testing.T) {
	tests := []struct {
		name       string
		canAct     bool
		isRaidLead bool
		want       []db.SignupStatus
	}{
		{"player", true, false, signup.AllowedStatuses(false)},
		{"raid lead", true, true, signup.AllStatuses()},
		{"unrelated actor", false, false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := signupToResponse(signup.Signup{}, tt.canAct, tt.isRaidLead).AllowedStatuses

			want := make([]string, 0, len(tt.want))
			for _, status := range tt.want {
				want = append(want, string(status))
			}
			if len(got) != len(want) || !slices.Equal(got, want) {
				t.Errorf("allowed_statuses = %v, want %v", got, want)
			}
		})
	}
}

// NO_SHOW is the raid lead's judgement about the night, so offering it to a player
// would advertise a transition the write path answers with a 403.
func TestSignupAllowedStatusesWithholdNoShowFromAPlayer(t *testing.T) {
	got := signupToResponse(signup.Signup{}, true, false).AllowedStatuses

	if slices.Contains(got, string(db.SignupStatusNOSHOW)) {
		t.Errorf("allowed_statuses = %v, want NO_SHOW withheld", got)
	}
}
