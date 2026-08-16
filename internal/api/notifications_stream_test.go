package api

import (
	"bufio"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeWatcher stands in for signup.Hub. The handler only ever waits on the channel,
// so a test can drive it by hand.
type fakeWatcher struct {
	queued      chan struct{}
	unsubscribe int
}

func (f *fakeWatcher) Subscribe() (<-chan struct{}, func()) {
	return f.queued, func() { f.unsubscribe++ }
}

// readStreamEvent reads until the blank line that ends one SSE frame, so a test blocks
// on the stream the way a bot does instead of racing it with a sleep.
func readStreamEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()

	var frame strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading stream: %v", err)
		}
		if line == "\n" {
			return frame.String()
		}
		frame.WriteString(line)
	}
}

func TestStreamNotificationsEmitsOnConnectAndOnQueue(t *testing.T) {
	watcher := &fakeWatcher{queued: make(chan struct{}, 1)}
	srv := httptest.NewServer(streamNotificationsHandler(watcher, slog.New(slog.DiscardHandler)))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("opening stream: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}

	reader := bufio.NewReader(resp.Body)

	// Rows queued while the bot was disconnected are claimed because of this one,
	// without either side special-casing a reconnect.
	if got, want := readStreamEvent(t, reader), "event: notification\ndata: {}\n"; got != want {
		t.Fatalf("connect frame = %q, want %q", got, want)
	}

	watcher.queued <- struct{}{}
	if got, want := readStreamEvent(t, reader), "event: notification\ndata: {}\n"; got != want {
		t.Fatalf("queued frame = %q, want %q", got, want)
	}
}

func TestStreamNotificationsUnsubscribesWhenTheClientGoesAway(t *testing.T) {
	watcher := &fakeWatcher{queued: make(chan struct{}, 1)}
	handler := streamNotificationsHandler(watcher, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/notifications/stream", nil)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler(w, req)
		close(done)
	}()

	// A bot that dies holding a stream must not leave a subscriber behind: the hub
	// would keep waking a connection nobody is reading, one leak per reconnect.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return after the request context was cancelled")
	}
	if watcher.unsubscribe != 1 {
		t.Errorf("unsubscribe calls = %d, want 1", watcher.unsubscribe)
	}
}
