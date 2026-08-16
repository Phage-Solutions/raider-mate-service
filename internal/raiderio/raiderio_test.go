package raiderio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

	c := NewClient(srv.URL, "", 0)
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

	c := NewClient(srv.URL, "", 0)
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

	c := NewClient(srv.URL, "", 0)
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

	c := NewClient(srv.URL, "", 0)
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

	c := NewClient(srv.URL, "", 0)
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
	c := NewClient(srv.URL, "", interval)

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

// Raider.IO takes the key as a query parameter, so this is what "authenticated" means
// here. A client with no key must send no parameter at all rather than an empty one,
// which Raider.IO answers with 403.
func TestCharacterProfileSendsTheAccessKeyOnlyWhenConfigured(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "keyed", key: "secret-key", want: "secret-key"},
		{name: "anonymous", key: "", want: ""},
	}

	body, err := os.ReadFile("testdata/profile.json")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			var present bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query().Get("access_key")
				_, present = r.URL.Query()["access_key"]
				w.Header().Set("Content-Type", "application/json")
				w.Write(body) //nolint:errcheck
			}))
			defer srv.Close()

			c := NewClient(srv.URL, tt.key, 0)
			if _, err := c.CharacterProfile(context.Background(), "eu", "ravencrest", "Danthrax"); err != nil {
				t.Fatalf("CharacterProfile: %v", err)
			}

			if got != tt.want {
				t.Errorf("access_key = %q, want %q", got, tt.want)
			}
			if present != (tt.key != "") {
				t.Errorf("access_key present = %v, want %v", present, tt.key != "")
			}
		})
	}
}

// A rejected key is not a transient failure and not a missing character: every request
// after it fails the same way until someone fixes the configuration.
func TestCharacterProfileInvalidAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "wrong-key", 0)
	_, err := c.CharacterProfile(context.Background(), "eu", "ravencrest", "Danthrax")
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("err = %v, want ErrInvalidAPIKey", err)
	}
}

// The key rides in the URL, and a transport error prints the URL it failed on. Logging
// that would put the secret in the worker's log stream on every network blip.
func TestATransportErrorDoesNotLeakTheAccessKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // nothing is listening, so the request fails in the transport

	c := NewClient(srv.URL, "secret-key", 0)
	_, err := c.CharacterProfile(context.Background(), "eu", "ravencrest", "Danthrax")
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Errorf("error = %q, want the access key kept out of it", err)
	}
}

func TestProfileURL(t *testing.T) {
	tests := []struct {
		name                string
		region, realm, char string
		want                string
	}{
		{
			name:   "plain",
			region: "eu", realm: "ravencrest", char: "Danthrax",
			want: "https://raider.io/characters/eu/ravencrest/Danthrax",
		},
		{
			name:   "realm with a space",
			region: "eu", realm: "twisting nether", char: "Danthrax",
			want: "https://raider.io/characters/eu/twisting%20nether/Danthrax",
		},
		{
			name:   "non-ascii name",
			region: "eu", realm: "ravencrest", char: "Dánthrax",
			want: "https://raider.io/characters/eu/ravencrest/D%C3%A1nthrax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProfileURL(tt.region, tt.realm, tt.char); got != tt.want {
				t.Errorf("ProfileURL = %q, want %q", got, tt.want)
			}
		})
	}
}
