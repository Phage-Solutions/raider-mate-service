package signup

import (
	"errors"
	"testing"
)

func TestNormalizeWarcraftLogsURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain report", "https://www.warcraftlogs.com/reports/aBcD1234", "https://www.warcraftlogs.com/reports/aBcD1234"},
		{"no subdomain", "https://warcraftlogs.com/reports/aBcD1234", "https://warcraftlogs.com/reports/aBcD1234"},
		{"classic subdomain", "https://classic.warcraftlogs.com/reports/aBcD1234", "https://classic.warcraftlogs.com/reports/aBcD1234"},
		{"uppercase host", "https://WWW.WarcraftLogs.com/reports/aBcD1234", "https://www.warcraftlogs.com/reports/aBcD1234"},
		{"surrounding space", "  https://www.warcraftlogs.com/reports/aBcD1234  ", "https://www.warcraftlogs.com/reports/aBcD1234"},
		{"trailing slash", "https://www.warcraftlogs.com/reports/aBcD1234/", "https://www.warcraftlogs.com/reports/aBcD1234"},
		// What copying the address bar mid-analysis actually gives you.
		{"fragment dropped", "https://www.warcraftlogs.com/reports/aBcD1234#fight=12&type=damage-done", "https://www.warcraftlogs.com/reports/aBcD1234"},
		{"query dropped", "https://www.warcraftlogs.com/reports/aBcD1234?fight=3", "https://www.warcraftlogs.com/reports/aBcD1234"},
		{"empty takes the log off", "", ""},
		{"blank takes the log off", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeWarcraftLogsURL(tt.in)
			if err != nil {
				t.Fatalf("NormalizeWarcraftLogsURL(%q) returned %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeWarcraftLogsURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeWarcraftLogsURLRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"another site", "https://raider.io/reports/aBcD1234"},
		// A lookalike domain is the whole reason this matches on the suffix with a dot
		// rather than on Contains.
		{"lookalike host", "https://notwarcraftlogs.com/reports/aBcD1234"},
		{"host as a path", "https://evil.example.com/warcraftlogs.com/reports/aBcD1234"},
		{"plain http", "http://www.warcraftlogs.com/reports/aBcD1234"},
		{"character page, not a report", "https://www.warcraftlogs.com/character/eu/silvermoon/someone"},
		{"no report code", "https://www.warcraftlogs.com/reports/"},
		{"a page inside a report", "https://www.warcraftlogs.com/reports/aBcD1234/deeper"},
		{"not a url", "this is not a url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeWarcraftLogsURL(tt.in)
			if !errors.Is(err, ErrNotAWarcraftLogsReport) {
				t.Fatalf("NormalizeWarcraftLogsURL(%q) = %q, %v; want ErrNotAWarcraftLogsReport", tt.in, got, err)
			}
		})
	}
}
