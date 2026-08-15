package signup

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

func TestFileWritesTheRequestAndNotifiesWhenAChannelIsKnown(t *testing.T) {
	store := newFakeSignupStore()
	channelID := int64(555)
	store.event = Event{DiscordGuildID: 100, ChannelID: &channelID}
	store.roleIDs = []int64{781, 799}

	req, err := NewLateRequests(store).File(context.Background(), LateRequestWrite{
		EventID: uuid.New(), CharacterID: uuid.New(), Status: db.SignupStatusLATE,
	})
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if req.State != db.RequestStatePENDING {
		t.Errorf("state = %s, want PENDING", req.State)
	}
	if len(store.notified) != 1 {
		t.Fatalf("wrote %d notifications, want 1", len(store.notified))
	}
	n := store.notified[0]
	if n.Kind != db.NotificationKindLATEREQUESTFILED {
		t.Errorf("kind = %s, want LATE_REQUEST_FILED", n.Kind)
	}
	if n.TargetKind != db.NotificationTargetROLE {
		t.Errorf("target_kind = %s, want ROLE", n.TargetKind)
	}
	if len(n.RoleIDs) != 2 {
		t.Errorf("role_ids = %v, want the mapped raid lead roles", n.RoleIDs)
	}
}

func TestFileSkipsTheNotificationWithNoChannelKnown(t *testing.T) {
	store := newFakeSignupStore()
	store.event = Event{DiscordGuildID: 100, ChannelID: nil}

	_, err := NewLateRequests(store).File(context.Background(), LateRequestWrite{
		EventID: uuid.New(), CharacterID: uuid.New(), Status: db.SignupStatusLATE,
	})
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if len(store.notified) != 0 {
		t.Errorf("wrote %d notifications, want none: no channel to post in", len(store.notified))
	}
	if len(store.lateWritten) != 1 {
		t.Errorf("wrote %d late requests, want 1: filing still succeeds without a channel", len(store.lateWritten))
	}
}

func TestApproveWritesTheSignupAndMarksDecided(t *testing.T) {
	store := newFakeSignupStore()
	eventID, characterID := uuid.New(), uuid.New()
	req, err := store.UpsertLateRequest(context.Background(), LateRequestWrite{
		EventID: eventID, CharacterID: characterID, Status: db.SignupStatusDECLINED,
	})
	if err != nil {
		t.Fatalf("seeding late request: %v", err)
	}

	if err := NewLateRequests(store).Approve(context.Background(), req.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if len(store.written) != 1 {
		t.Fatalf("wrote %d signups, want 1", len(store.written))
	}
	if store.written[0].Status != db.SignupStatusDECLINED {
		t.Errorf("signup status = %s, want DECLINED (the requested status)", store.written[0].Status)
	}
	if store.decided[req.ID] != db.RequestStateAPPROVED {
		t.Errorf("decided state = %s, want APPROVED", store.decided[req.ID])
	}
}

func TestRejectMarksDecidedWithoutTouchingTheSignup(t *testing.T) {
	store := newFakeSignupStore()
	req, err := store.UpsertLateRequest(context.Background(), LateRequestWrite{
		EventID: uuid.New(), CharacterID: uuid.New(), Status: db.SignupStatusLATE,
	})
	if err != nil {
		t.Fatalf("seeding late request: %v", err)
	}

	if err := NewLateRequests(store).Reject(context.Background(), req.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if len(store.written) != 0 {
		t.Errorf("wrote %d signups, want none: a rejection never touches the signup", len(store.written))
	}
	if store.decided[req.ID] != db.RequestStateREJECTED {
		t.Errorf("decided state = %s, want REJECTED", store.decided[req.ID])
	}
}
