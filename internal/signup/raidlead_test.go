package signup

import (
	"context"
	"errors"
	"testing"
)

type fakeRaidLeadStore struct {
	highest      int64
	highestKnown bool
	written      []int64
	writes       int
}

func (f *fakeRaidLeadStore) RaidLeadRoleIDs(context.Context, int64) ([]int64, error) {
	return nil, nil
}

func (f *fakeRaidLeadStore) ReplaceRaidLeadRoleIDs(_ context.Context, _ int64, roleIDs []int64) error {
	f.writes++
	f.written = roleIDs
	return nil
}

func (f *fakeRaidLeadStore) HighestGuildRoleID(context.Context, int64) (int64, bool, error) {
	return f.highest, f.highestKnown, nil
}

func TestReplaceRefusesAMappingWithoutTheHighestRole(t *testing.T) {
	tests := []struct {
		name    string
		roleIDs []int64
	}{
		{"nothing at all, which is the lockout this rule exists for", nil},
		{"only lower roles", []int64{20, 30}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeRaidLeadStore{highest: 10, highestKnown: true}

			err := NewRaidLeads(store).Replace(context.Background(), 100, tt.roleIDs)

			if !errors.Is(err, ErrHighestRoleRequired) {
				t.Fatalf("Replace = %v, want ErrHighestRoleRequired", err)
			}
			if store.writes != 0 {
				t.Errorf("the store was written %d times; the mapping must be refused before it lands", store.writes)
			}
		})
	}
}

func TestReplaceAcceptsAMappingThatKeepsTheHighestRole(t *testing.T) {
	store := &fakeRaidLeadStore{highest: 10, highestKnown: true}

	if err := NewRaidLeads(store).Replace(context.Background(), 100, []int64{30, 10}); err != nil {
		t.Fatalf("Replace = %v, want nil", err)
	}
	if len(store.written) != 2 {
		t.Errorf("written = %v, want both roles through untouched", store.written)
	}
}

func TestReplaceAllowsAnythingBeforeTheRolesAreCatalogued(t *testing.T) {
	// A guild the bot has not synced has no highest role to insist on. Refusing every
	// write until then would be a worse lockout than the one the rule prevents.
	store := &fakeRaidLeadStore{highestKnown: false}

	if err := NewRaidLeads(store).Replace(context.Background(), 100, []int64{30}); err != nil {
		t.Fatalf("Replace = %v, want nil while the catalogue is empty", err)
	}
	if store.writes != 1 {
		t.Errorf("writes = %d, want the mapping to land", store.writes)
	}
}
