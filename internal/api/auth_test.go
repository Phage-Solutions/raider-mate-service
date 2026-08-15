package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeRoleLister struct {
	roleIDs []int64
	err     error
}

func (f fakeRoleLister) RaidLeadRoleIDs(context.Context, int64) ([]int64, error) {
	return f.roleIDs, f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newAuthedRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/whatever", nil)
	r.Header.Set("Authorization", "Bearer secret-key")
	r.Header.Set("X-Actor-Discord-Id", "1")
	r.Header.Set("X-Actor-Guild-Id", "100")
	r.Header.Set("X-Actor-Roles", "781")
	return r
}

func TestRequireAuthPassesAndStoresTheResolvedActor(t *testing.T) {
	var gotActor Actor
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotActor, gotOK = actorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := requireAuth(next, "secret-key", fakeRoleLister{roleIDs: []int64{781, 799}}, testLogger())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newAuthedRequest())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !gotOK {
		t.Fatalf("actor not found on context")
	}
	if !gotActor.IsRaidLead {
		t.Errorf("is_raid_lead = false, want true: role 781 is mapped")
	}
}

func TestRequireAuthRejectsAWrongKey(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler called, want the request rejected before it")
	})

	handler := requireAuth(next, "secret-key", fakeRoleLister{}, testLogger())
	r := newAuthedRequest()
	r.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuthRejectsAMissingKey(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler called, want the request rejected before it")
	})

	handler := requireAuth(next, "secret-key", fakeRoleLister{}, testLogger())
	r := newAuthedRequest()
	r.Header.Del("Authorization")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuthRejectsUnparseableActorHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler called, want the request rejected before it")
	})

	handler := requireAuth(next, "secret-key", fakeRoleLister{}, testLogger())
	r := newAuthedRequest()
	r.Header.Set("X-Actor-Discord-Id", "not-a-number")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRequireAuthReturns500OnARoleLookupFailure(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler called, want the request rejected before it")
	})

	handler := requireAuth(next, "secret-key", fakeRoleLister{err: errors.New("db down")}, testLogger())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newAuthedRequest())

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestValidAPIKeyRequiresTheBearerPrefix(t *testing.T) {
	if validAPIKey("secret-key", "secret-key") {
		t.Errorf("validAPIKey without the Bearer prefix = true, want false")
	}
	if !validAPIKey("Bearer secret-key", "secret-key") {
		t.Errorf("validAPIKey with the Bearer prefix = false, want true")
	}
}
