package api

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/signup"
)

func TestLateRequestLinksApproveRejectOnlyForPendingRaidLead(t *testing.T) {
	pending := signup.LateRequest{ID: uuid.New(), EventID: uuid.New(), State: "PENDING"}

	raidLead := lateRequestToResponse(pending, true)
	if _, ok := raidLead.Links["approve"]; !ok {
		t.Errorf("links = %v, want approve for a raid lead on a pending request", raidLead.Links)
	}
	if _, ok := raidLead.Links["reject"]; !ok {
		t.Errorf("links = %v, want reject for a raid lead on a pending request", raidLead.Links)
	}

	player := lateRequestToResponse(pending, false)
	if _, ok := player.Links["approve"]; ok {
		t.Errorf("links = %v, want no approve for a non-raid-lead actor", player.Links)
	}
}

func TestLateRequestLinksAbsentOnceDecided(t *testing.T) {
	decided := signup.LateRequest{ID: uuid.New(), EventID: uuid.New(), State: "APPROVED"}

	got := lateRequestToResponse(decided, true)
	if _, ok := got.Links["approve"]; ok {
		t.Errorf("links = %v, want no approve on an already-decided request", got.Links)
	}
	if _, ok := got.Links["reject"]; ok {
		t.Errorf("links = %v, want no reject on an already-decided request", got.Links)
	}
	if _, ok := got.Links["self"]; !ok {
		t.Errorf("links = %v, want self always present", got.Links)
	}
}
