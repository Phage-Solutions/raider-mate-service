package signup

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// notifyChannel is the Postgres channel the notifications insert trigger raises. The
// name is duplicated in migrations/00024_notification_notify.sql; there is nowhere to
// share a constant between SQL and Go.
const notifyChannel = "notification_queued"

// Hub fans one "something was queued" signal out to everything waiting for it, today
// the SSE streams held open for connected bots.
//
// Subscribers get a buffered channel of one and a non-blocking send, which coalesces
// for free: a bot busy sending a batch while three more inserts land wakes once more
// afterwards, not three times. The signal carries no value because there is nothing to
// carry. What was queued is whatever the claim query returns.
type Hub struct {
	mu   sync.Mutex
	subs map[int]chan struct{}
	next int
}

// NewHub builds a Hub.
func NewHub() *Hub {
	return &Hub{subs: map[int]chan struct{}{}}
}

// Subscribe returns a channel that receives on every broadcast, and the function that
// stops it. Calling the returned function twice is safe.
func (h *Hub) Subscribe() (<-chan struct{}, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	id := h.next
	h.next++
	ch := make(chan struct{}, 1)
	h.subs[id] = ch

	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs, id)
	}
}

// Broadcast wakes every subscriber. A subscriber that has not drained its previous
// signal is left alone: it is about to look at the table anyway.
func (h *Hub) Broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// listenBackoff is how long the listener waits before redialling. Long enough not to
// hammer a database that is down, short enough that a restart is not felt: the poll the
// bot keeps as a fallback runs every five minutes, so this only has to beat that.
const listenBackoff = 5 * time.Second

// Listener turns Postgres notifications into Hub broadcasts. It owns its connection
// outright rather than borrowing one per wait, because a connection inside LISTEN is
// not usable for anything else and must not go back to a pool.
type Listener struct {
	connect func(ctx context.Context) (*pgx.Conn, error)
	hub     *Hub
	logger  *slog.Logger
}

// NewListener builds a Listener. connect is called once per connection, so a listener
// that loses its database gets a fresh one on the next attempt.
func NewListener(connect func(ctx context.Context) (*pgx.Conn, error), hub *Hub, logger *slog.Logger) *Listener {
	return &Listener{connect: connect, hub: hub, logger: logger}
}

// Run listens until ctx is cancelled, reconnecting on failure. It never returns an
// error: losing the signal costs latency, not delivery, because the bot still polls.
// Taking the API process down over it would cost both.
func (l *Listener) Run(ctx context.Context) {
	for {
		if err := l.listen(ctx); err != nil && ctx.Err() == nil {
			l.logger.ErrorContext(ctx, "notification listener", "error", err, "retry_in", listenBackoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(listenBackoff):
		}
	}
}

// listen holds one connection until it fails or ctx ends.
func (l *Listener) listen(ctx context.Context) error {
	conn, err := l.connect(ctx)
	if err != nil {
		return fmt.Errorf("connecting to listen: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return fmt.Errorf("listening on %s: %w", notifyChannel, err)
	}
	l.logger.InfoContext(ctx, "listening for queued notifications", "channel", notifyChannel)

	for {
		if _, err := conn.WaitForNotification(ctx); err != nil {
			return fmt.Errorf("waiting for notification: %w", err)
		}
		l.hub.Broadcast()
	}
}
