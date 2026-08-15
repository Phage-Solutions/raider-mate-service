package api

import "testing"

func TestSignupLinksPresentWhenActorCanAct(t *testing.T) {
	links := signupLinks("event-1", "char-1", true)

	if _, ok := links["self"]; !ok {
		t.Errorf("links = %v, want self present", links)
	}
	if _, ok := links["withdraw"]; !ok {
		t.Errorf("links = %v, want withdraw present", links)
	}
}

func TestSignupLinksAbsentForAnUnrelatedActor(t *testing.T) {
	links := signupLinks("event-1", "char-1", false)

	if len(links) != 0 {
		t.Errorf("links = %v, want none: the absence of a link is the authorization answer", links)
	}
}
