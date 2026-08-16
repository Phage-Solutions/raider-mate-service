package signup

import (
	"testing"
)

func TestHubWakesEverySubscriber(t *testing.T) {
	hub := NewHub()
	first, stopFirst := hub.Subscribe()
	defer stopFirst()
	second, stopSecond := hub.Subscribe()
	defer stopSecond()

	hub.Broadcast()

	for i, ch := range []<-chan struct{}{first, second} {
		select {
		case <-ch:
		default:
			t.Errorf("subscriber %d was not woken", i)
		}
	}
}

func TestHubCoalescesABurstIntoOneWake(t *testing.T) {
	hub := NewHub()
	queued, stop := hub.Subscribe()
	defer stop()

	// A sync batch inserting redraws for forty raiders raises one notification per
	// statement. The bot's answer to all of them is the same single claim, so the
	// signals have to collapse rather than queue up behind it.
	for range 40 {
		hub.Broadcast()
	}

	if len(queued) != 1 {
		t.Fatalf("pending wakes = %d, want 1", len(queued))
	}
	<-queued
	select {
	case <-queued:
		t.Error("a second wake was queued behind the first")
	default:
	}
}

func TestHubStopsWakingAfterUnsubscribe(t *testing.T) {
	hub := NewHub()
	queued, stop := hub.Subscribe()
	stop()

	hub.Broadcast()

	select {
	case <-queued:
		t.Error("an unsubscribed stream was woken")
	default:
	}

	// Unsubscribing twice happens whenever a handler's defer runs after an explicit
	// stop, and must not panic on the second delete.
	stop()
}
