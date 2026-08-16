package roster

import (
	"context"
	"testing"
)

// registerCapture records what Register hands the store. The interface is embedded
// rather than stubbed out method by method: only RegisterCharacter is ever called, and
// a nil embedded interface panics loudly if that stops being true.
type registerCapture struct {
	characterStore
	got RegisterInput
}

func (r *registerCapture) RegisterCharacter(_ context.Context, in RegisterInput) (Character, error) {
	r.got = in
	return Character{Name: in.Name, Realm: in.Realm, Region: in.Region}, nil
}

func TestRegisterCanonicalisesRealmAndRegion(t *testing.T) {
	tests := []struct {
		name       string
		realm      string
		region     string
		wantRealm  string
		wantRegion string
	}{
		{"as typed in game", "Twisting Nether", "EU", "twisting-nether", "eu"},
		{"apostrophe", "Kil'jaeden", "us", "kiljaeden", "us"},
		{"already canonical", "area-52", "us", "area-52", "us"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &registerCapture{}
			characters := NewCharacters(store)

			_, err := characters.Register(context.Background(), RegisterInput{
				DiscordID: 1, DiscordGuildID: 2, Name: "Danthrax",
				Realm: tt.realm, Region: tt.region,
			})
			if err != nil {
				t.Fatalf("Register: %v", err)
			}

			if store.got.Realm != tt.wantRealm {
				t.Errorf("stored realm = %q, want %q", store.got.Realm, tt.wantRealm)
			}
			if store.got.Region != tt.wantRegion {
				t.Errorf("stored region = %q, want %q", store.got.Region, tt.wantRegion)
			}
		})
	}
}
