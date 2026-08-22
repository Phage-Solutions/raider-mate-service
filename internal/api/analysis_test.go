package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Raider-Mate/raider-mate-service/internal/audit"
	"github.com/Raider-Mate/raider-mate-service/internal/billing"
)

// emptyAnalysisStore answers every read with nothing. These tests are about link sets
// and status codes, so the numbers do not matter and their absence is the point: a
// guild with no history still gets a well-formed panel.
type emptyAnalysisStore struct{}

func (emptyAnalysisStore) CountEvents(context.Context, int64, audit.Period) (int64, error) {
	return 0, nil
}

func (emptyAnalysisStore) Attendance(context.Context, int64, audit.Period) ([]audit.RaiderAttendance, error) {
	return nil, nil
}

func (emptyAnalysisStore) RoleTotals(context.Context, int64, audit.Period) ([]audit.RoleBalance, error) {
	return nil, nil
}

func (emptyAnalysisStore) BenchRecords(context.Context, int64, audit.Period) ([]audit.BenchRecord, error) {
	return nil, nil
}

func (emptyAnalysisStore) RoleCoverage(context.Context, int64) ([]audit.RoleCoverage, error) {
	return nil, nil
}

func (emptyAnalysisStore) RosterActivity(context.Context, int64, audit.Period) (int64, int64, int64, error) {
	return 0, 0, 0, nil
}

func (emptyAnalysisStore) Throughput(context.Context, int64, audit.Period) ([]audit.ThroughputWeek, error) {
	return nil, nil
}

func (emptyAnalysisStore) IlvlWeeks(context.Context, int64, audit.Period) ([]audit.IlvlWeek, error) {
	return nil, nil
}

type fixedTiers struct {
	tier billing.Tier
}

func (f fixedTiers) For(context.Context, int64) (billing.Tier, error) {
	return f.tier, nil
}

func (f fixedTiers) Require(_ context.Context, _ int64, want billing.Tier) error {
	if want == billing.TierPremium && f.tier != billing.TierPremium {
		return billing.ErrTierRequired
	}
	return nil
}

func analysisFor(tier billing.Tier) *audit.Analysis {
	return audit.NewAnalysis(emptyAnalysisStore{}, fixedTiers{tier: tier})
}

// callAnalysis runs one handler as a member of the home guild and reports the status
// and the decoded links.
func callAnalysis(t *testing.T, handler http.HandlerFunc, path string) (int, Links) {
	t.Helper()

	guild := strconv.FormatInt(homeGuild, 10)
	w := httptest.NewRecorder()
	r := requestAs(http.MethodGet, "/api/guilds/"+guild+path, homeActor(false), "")
	r.SetPathValue("gid", guild)

	handler.ServeHTTP(w, r)

	var body struct {
		Links Links `json:"_links"`
	}
	// A 402 body carries an error rather than links, and decoding it into the same
	// shape leaves Links nil, which is what the caller checks for anyway.
	_ = json.NewDecoder(w.Body).Decode(&body)
	return w.Code, body.Links
}

func TestAnalysisIndexOffersOnlyAttendanceToAFreeGuild(t *testing.T) {
	status, links := callAnalysis(t, getAnalysisIndexHandler(analysisFor(billing.TierFree), testLogger()), "/analysis")

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: the index is readable by every guild", status)
	}
	if _, ok := links["attendance"]; !ok {
		t.Error("want an attendance link for a free guild")
	}
	for _, gated := range []string{"comp-balance", "roster-health", "throughput", "ilvl"} {
		if _, ok := links[gated]; ok {
			t.Errorf("links = %v, want no %s link for a free guild", links, gated)
		}
	}
}

func TestAnalysisIndexOffersEveryPanelToAPremiumGuild(t *testing.T) {
	status, links := callAnalysis(t, getAnalysisIndexHandler(analysisFor(billing.TierPremium), testLogger()), "/analysis")

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	for _, panel := range []string{"attendance", "comp-balance", "roster-health", "throughput", "ilvl"} {
		if _, ok := links[panel]; !ok {
			t.Errorf("links = %v, want a %s link for a premium guild", links, panel)
		}
	}
}

// Attendance is the free panel, and a guild that has never raided still gets it.
func TestAttendanceIsReadableByAFreeGuild(t *testing.T) {
	status, links := callAnalysis(t, getAttendanceHandler(analysisFor(billing.TierFree), testLogger()), "/analysis/attendance")

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, ok := links["index"]; !ok {
		t.Errorf("links = %v, want a way back to the index", links)
	}
}

// 402 rather than 403: nobody in the guild may read this, and the fix is a
// subscription rather than a role. A client has to tell those apart to know whether to
// show an upsell or an apology.
func TestGatedPanelsAnswer402ToAFreeGuild(t *testing.T) {
	analysis := analysisFor(billing.TierFree)
	logger := testLogger()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"comp balance", getCompBalanceHandler(analysis, logger), "/analysis/comp-balance"},
		{"roster health", getRosterHealthHandler(analysis, logger), "/analysis/roster-health"},
		{"throughput", getThroughputHandler(analysis, logger), "/analysis/throughput"},
		{"ilvl", getIlvlSeriesHandler(analysis, logger), "/analysis/ilvl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _ := callAnalysis(t, tt.handler, tt.path)
			if status != http.StatusPaymentRequired {
				t.Errorf("status = %d, want 402", status)
			}
		})
	}
}

func TestGatedPanelsAnswerAPremiumGuild(t *testing.T) {
	analysis := analysisFor(billing.TierPremium)
	logger := testLogger()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"comp balance", getCompBalanceHandler(analysis, logger), "/analysis/comp-balance"},
		{"roster health", getRosterHealthHandler(analysis, logger), "/analysis/roster-health"},
		{"throughput", getThroughputHandler(analysis, logger), "/analysis/throughput"},
		{"ilvl", getIlvlSeriesHandler(analysis, logger), "/analysis/ilvl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, links := callAnalysis(t, tt.handler, tt.path)
			if status != http.StatusOK {
				t.Errorf("status = %d, want 200", status)
			}
			if _, ok := links["self"]; !ok {
				t.Errorf("links = %v, want a self link", links)
			}
		})
	}
}

// Analysis is guild-scoped like everything else under /api/guilds: a member of one
// guild asking about another is refused before the tier is even consulted.
func TestAnalysisRefusesAForeignGuild(t *testing.T) {
	guild := strconv.FormatInt(foreignGuild, 10)
	w := httptest.NewRecorder()
	r := requestAs(http.MethodGet, "/api/guilds/"+guild+"/analysis", homeActor(false), "")
	r.SetPathValue("gid", guild)

	getAnalysisIndexHandler(analysisFor(billing.TierPremium), testLogger()).ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// An empty panel is a well-formed panel. A guild that has raided nothing must not get
// a null list a client has to guard against.
func TestAnEmptyPanelStillCarriesItsLists(t *testing.T) {
	guild := strconv.FormatInt(homeGuild, 10)
	w := httptest.NewRecorder()
	r := requestAs(http.MethodGet, "/api/guilds/"+guild+"/analysis/attendance", homeActor(false), "")
	r.SetPathValue("gid", guild)

	getAttendanceHandler(analysisFor(billing.TierFree), testLogger()).ServeHTTP(w, r)

	var got attendanceResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.Raiders == nil {
		t.Error("raiders = null, want an empty list")
	}
	if got.Since.IsZero() || got.Until.IsZero() {
		t.Errorf("period = %v to %v, want both ends set", got.Since, got.Until)
	}
}
