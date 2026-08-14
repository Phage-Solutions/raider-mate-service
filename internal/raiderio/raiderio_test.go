package raiderio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestCharacterProfileParsesFields(t *testing.T) {
	body, err := os.ReadFile("testdata/profile.json")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body) //nolint:errcheck
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	profile, err := c.CharacterProfile(context.Background(), "eu", "ravencrest", "Danthrax")
	if err != nil {
		t.Fatalf("CharacterProfile: %v", err)
	}

	if profile.Class != "Warrior" {
		t.Errorf("Class = %q, want Warrior", profile.Class)
	}
	if profile.Spec != "Protection" {
		t.Errorf("Spec = %q, want Protection", profile.Spec)
	}
	if profile.ItemLevel != 415.5 {
		t.Errorf("ItemLevel = %v, want 415.5", profile.ItemLevel)
	}
	if profile.MythicPlusScore != 3012.4 {
		t.Errorf("MythicPlusScore = %v, want 3012.4", profile.MythicPlusScore)
	}
	if len(profile.Gear) != 2 {
		t.Fatalf("len(Gear) = %d, want 2", len(profile.Gear))
	}
	if profile.Gear[0].Slot != "head" || profile.Gear[1].Slot != "neck" {
		t.Errorf("Gear not sorted by slot: %+v", profile.Gear)
	}
}

func TestCharacterProfileNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	_, err := c.CharacterProfile(context.Background(), "eu", "ravencrest", "Nobody")
	if !errors.Is(err, ErrCharacterNotFound) {
		t.Fatalf("err = %v, want ErrCharacterNotFound", err)
	}
}

func TestCharacterProfileBadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	_, err := c.CharacterProfile(context.Background(), "eu", "ravencrest", "Danthrax")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestCharacterProfileRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	_, err := c.CharacterProfile(context.Background(), "eu", "ravencrest", "Danthrax")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestCharacterProfileMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json")) //nolint:errcheck
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	_, err := c.CharacterProfile(context.Background(), "eu", "ravencrest", "Danthrax")
	if err == nil {
		t.Fatal("expected error for malformed body, got nil")
	}
}

func TestClientGatesRequests(t *testing.T) {
	body, err := os.ReadFile("testdata/profile.json")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body) //nolint:errcheck
	}))
	defer srv.Close()

	const (
		n        = 4
		interval = 20 * time.Millisecond
	)
	c := NewClient(srv.URL, interval)

	start := time.Now()
	for range n {
		if _, err := c.CharacterProfile(context.Background(), "eu", "ravencrest", "Danthrax"); err != nil {
			t.Fatalf("CharacterProfile: %v", err)
		}
	}
	elapsed := time.Since(start)

	want := (n - 1) * interval
	if elapsed < want {
		t.Errorf("elapsed = %v, want at least %v", elapsed, want)
	}
}
