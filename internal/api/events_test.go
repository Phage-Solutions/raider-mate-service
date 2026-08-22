package api

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Raider-Mate/raider-mate-service/internal/signup"
)

func TestEventLinksOfferAttachingLogsToRaidLeadsOnly(t *testing.T) {
	event := signup.Event{ID: uuid.New()}

	lead := eventToResponse(event, Actor{IsRaidLead: true})
	if _, ok := lead.Links["set-warcraftlogs"]; !ok {
		t.Errorf("links = %v, want set-warcraftlogs for a raid lead", lead.Links)
	}
	if lead.Links["set-warcraftlogs"].Method != "PATCH" {
		t.Errorf("set-warcraftlogs method = %q, want PATCH", lead.Links["set-warcraftlogs"].Method)
	}

	// A raider sees the report if there is one, but is never offered the control. The
	// dashboard renders from these links alone, so an extra rel here is a 403 waiting
	// to happen on the client.
	raider := eventToResponse(event, Actor{})
	if _, ok := raider.Links["set-warcraftlogs"]; ok {
		t.Errorf("links = %v, want set-warcraftlogs absent for a raider", raider.Links)
	}
}

func TestEventLinksCarryTheReportOnlyOnceAttached(t *testing.T) {
	event := signup.Event{ID: uuid.New()}

	if _, ok := eventToResponse(event, Actor{IsRaidLead: true}).Links["warcraftlogs"]; ok {
		t.Error("want no warcraftlogs link on an event with no report")
	}

	report := "https://www.warcraftlogs.com/reports/aBcD1234"
	event.WarcraftLogsURL = &report

	got := eventToResponse(event, Actor{})
	if got.Links["warcraftlogs"].Href != report {
		t.Errorf("warcraftlogs href = %q, want %q", got.Links["warcraftlogs"].Href, report)
	}
	if got.WarcraftLogsURL == nil || *got.WarcraftLogsURL != report {
		t.Errorf("warcraftlogs_url = %v, want %q", got.WarcraftLogsURL, report)
	}
}
