package api

import "testing"

func TestLinksAddOnlyWhenIncluded(t *testing.T) {
	links := Links{}
	links.add(true, "self", "/api/events/1", "")
	links.add(false, "withdraw", "/api/events/1", "DELETE")

	if _, ok := links["self"]; !ok {
		t.Errorf("links = %v, want self present", links)
	}
	if _, ok := links["withdraw"]; ok {
		t.Errorf("links = %v, want withdraw absent", links)
	}
	if links["self"].Href != "/api/events/1" || links["self"].Method != "" {
		t.Errorf("self = %+v, want href set and method omitted for a GET", links["self"])
	}
}

func TestLinksAddSetsTheMethod(t *testing.T) {
	links := Links{}
	links.add(true, "lock", "/api/events/1/comps/prog/lock", "POST")

	if links["lock"].Method != "POST" {
		t.Errorf("method = %q, want POST", links["lock"].Method)
	}
}
