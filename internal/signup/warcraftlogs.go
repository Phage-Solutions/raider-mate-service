package signup

import (
	"errors"
	"net/url"
	"strings"
)

// ErrNotAWarcraftLogsReport is returned when the URL a raid lead pasted is not a
// WarcraftLogs report. It is a caller error, so the API answers 400 rather than 500.
var ErrNotAWarcraftLogsReport = errors.New("not a warcraftlogs report url")

const warcraftLogsHost = "warcraftlogs.com"

// NormalizeWarcraftLogsURL checks that a pasted URL is a WarcraftLogs report and
// returns it stripped of the query string and fragment.
//
// Raid leads copy the address bar, which on WarcraftLogs carries the fight and player
// they were looking at when they hit copy (`#fight=12&source=4`). Storing that would
// open every raider on a different fight, so only the report itself is kept.
//
// The empty string is the caller's way of taking a log back off an event and passes
// through untouched. Nothing here contacts WarcraftLogs: whether the report exists is
// its problem, not this service's.
func NormalizeWarcraftLogsURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", ErrNotAWarcraftLogsReport
	}
	if parsed.Scheme != "https" {
		return "", ErrNotAWarcraftLogsReport
	}

	// Subdomains are how the site separates game versions: classic and fresh sit
	// alongside www, and a guild raiding Classic pastes a classic. link.
	host := strings.ToLower(parsed.Hostname())
	if host != warcraftLogsHost && !strings.HasSuffix(host, "."+warcraftLogsHost) {
		return "", ErrNotAWarcraftLogsReport
	}

	code, ok := strings.CutPrefix(parsed.EscapedPath(), "/reports/")
	if !ok {
		return "", ErrNotAWarcraftLogsReport
	}
	code = strings.Trim(code, "/")
	// A report code is one path segment. Anything deeper is a page inside the report.
	if code == "" || strings.Contains(code, "/") {
		return "", ErrNotAWarcraftLogsReport
	}

	return "https://" + host + "/reports/" + code, nil
}
