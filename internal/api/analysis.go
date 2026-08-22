package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Raider-Mate/raider-mate-service/internal/audit"
	"github.com/Raider-Mate/raider-mate-service/internal/billing"
)

// periodResponse is the window every panel describes, embedded so a client reading two
// panels can see they cover the same stretch of time rather than assuming it.
type periodResponse struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
}

func periodToResponse(p audit.Period) periodResponse {
	return periodResponse{Since: p.Since, Until: p.Until}
}

// analysisIndexResponse carries links and nothing else. The link set is the tier: a
// free guild is handed attendance and no other transition, and a client that renders
// what it was given asks for nothing it cannot have (hard rule 7).
type analysisIndexResponse struct {
	Links Links `json:"_links"`
}

type characterRefResponse struct {
	CharacterID string  `json:"character_id"`
	Name        string  `json:"name"`
	Realm       string  `json:"realm"`
	Class       *string `json:"class,omitempty"`
}

type attendanceResponse struct {
	periodResponse
	// Events is the denominator every rate below divides by: raids that happened, not
	// raids somebody answered.
	Events  int64                      `json:"events"`
	Raiders []raiderAttendanceResponse `json:"raiders"`
	Links   Links                      `json:"_links"`
}

type raiderAttendanceResponse struct {
	characterRefResponse
	Confirmed int64 `json:"confirmed"`
	Tentative int64 `json:"tentative"`
	Declined  int64 `json:"declined"`
	Late      int64 `json:"late"`
	Absent    int64 `json:"absent"`
	NoShow    int64 `json:"no_show"`
	Answered  int64 `json:"answered"`
	// Rate and Silence are computed here rather than left to the client, because two
	// clients dividing the same two numbers is two chances to divide them differently.
	Rate    float64 `json:"rate"`
	Silence float64 `json:"silence"`
}

type compBalanceResponse struct {
	periodResponse
	Roles []roleBalanceResponse `json:"roles"`
	Bench []benchRecordResponse `json:"bench"`
	Links Links                 `json:"_links"`
}

type roleBalanceResponse struct {
	Role      string  `json:"role"`
	Placed    int64   `json:"placed"`
	Benched   int64   `json:"benched"`
	Share     float64 `json:"share"`
	BenchRate float64 `json:"bench_rate"`
}

type benchRecordResponse struct {
	characterRefResponse
	Boards  int64   `json:"boards"`
	Benched int64   `json:"benched"`
	Rate    float64 `json:"rate"`
}

type rosterHealthResponse struct {
	periodResponse
	Characters int64                  `json:"characters"`
	Mains      int64                  `json:"mains"`
	Active     int64                  `json:"active"`
	Dormant    int64                  `json:"dormant"`
	Coverage   []roleCoverageResponse `json:"coverage"`
	Links      Links                  `json:"_links"`
}

type roleCoverageResponse struct {
	Role        string `json:"role"`
	Characters  int64  `json:"characters"`
	FirstChoice int64  `json:"first_choice"`
}

type throughputResponse struct {
	periodResponse
	// Events is every raid in the window. A week's bar covers however many raids that
	// week held, so a client needs the total to say whether it is showing one raid night
	// or three.
	Events int64                    `json:"events"`
	Weeks  []throughputWeekResponse `json:"weeks"`
	Links  Links                    `json:"_links"`
}

// Every count here is people, not signup rows. A raider who confirmed for both of a
// week's raids is one confirmed raider.
type throughputWeekResponse struct {
	Week       time.Time `json:"week"`
	Events     int64     `json:"events"`
	Confirmed  int64     `json:"confirmed"`
	Declined   int64     `json:"declined"`
	NoShow     int64     `json:"no_show"`
	Placed     int64     `json:"placed"`
	Benched    int64     `json:"benched"`
	BenchRate  float64   `json:"bench_rate"`
	NoShowRate float64   `json:"no_show_rate"`
}

type ilvlSeriesResponse struct {
	periodResponse
	Weeks []ilvlWeekResponse `json:"weeks"`
	Links Links              `json:"_links"`
}

type ilvlWeekResponse struct {
	Week       time.Time `json:"week"`
	Characters int64     `json:"characters"`
	// The middle half of the raid, and the line through it. Their distance apart is the
	// gear gap; lowest and highest are deliberately not reported, because the lowest is
	// whichever alt somebody abandoned at level twenty.
	P25    float64 `json:"p25"`
	Median float64 `json:"median"`
	P75    float64 `json:"p75"`
}

// analysisHref is the index every panel hangs off.
func analysisHref(guildID int64) string {
	return "/api/guilds/" + strconv.FormatInt(guildID, 10) + "/analysis"
}

// analysisLinks is where the tier shows. Attendance is offered to every guild, which
// is the free "per-event, raw percentage" the product has always described; everything
// computed across events is Premium and simply is not offered.
//
// A client is expected to render a locked state for a panel it was not handed a link
// to. The absence is the answer, and it is the same absence a lapsed subscription
// produces, so nothing special happens on the way down from Premium.
func analysisLinks(guildID int64, tier billing.Tier) Links {
	href := analysisHref(guildID)
	premium := tier == billing.TierPremium

	links := Links{}
	links.add(true, "self", href, "")
	links.add(true, "attendance", href+"/attendance", "")
	links.add(premium, "comp-balance", href+"/comp-balance", "")
	links.add(premium, "roster-health", href+"/roster-health", "")
	links.add(premium, "throughput", href+"/throughput", "")
	links.add(premium, "ilvl", href+"/ilvl", "")
	return links
}

// panelLinks is what a single panel carries: itself, and the way back to the index.
func panelLinks(guildID int64, panel string) Links {
	href := analysisHref(guildID)
	links := Links{}
	links.add(true, "self", href+"/"+panel, "")
	links.add(true, "index", href, "")
	return links
}

// writeAnalysisFailure maps a failed read to a status. ErrTierRequired becomes 402
// rather than 403 because it is not a permission problem: nobody in the guild may read
// this, and the fix is a subscription rather than a role. A client needs to tell those
// apart to know whether to show an upsell or an apology.
func writeAnalysisFailure(w http.ResponseWriter, r *http.Request, logger *slog.Logger, what string, err error) {
	if errors.Is(err, billing.ErrTierRequired) {
		writeError(w, logger, http.StatusPaymentRequired, "this analysis is part of Premium")
		return
	}
	logger.ErrorContext(r.Context(), what, "error", err)
	writeError(w, logger, http.StatusInternalServerError, "internal error")
}

// getAnalysisIndexHandler answers "what analysis may this guild read".
//
// Open to anyone in the guild, like the roster and the event list: this is the guild's
// own history, and a raider who turned up to twelve raids is entitled to see that they
// did. It reports no numbers itself, only where the numbers are.
func getAnalysisIndexHandler(analysis *audit.Analysis, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		tier, err := analysis.Tier(r.Context(), guildID)
		if err != nil {
			writeAnalysisFailure(w, r, logger, "reading tier", err)
			return
		}

		writeJSON(w, logger, http.StatusOK, analysisIndexResponse{Links: analysisLinks(guildID, tier)})
	}
}

func getAttendanceHandler(analysis *audit.Analysis, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		result, err := analysis.Attendance(r.Context(), guildID)
		if err != nil {
			writeAnalysisFailure(w, r, logger, "reading attendance", err)
			return
		}

		raiders := make([]raiderAttendanceResponse, len(result.Raiders))
		for i, raider := range result.Raiders {
			raiders[i] = raiderAttendanceResponse{
				characterRefResponse: characterRefToResponse(raider.Character),
				Confirmed:            raider.Confirmed,
				Tentative:            raider.Tentative,
				Declined:             raider.Declined,
				Late:                 raider.Late,
				Absent:               raider.Absent,
				NoShow:               raider.NoShow,
				Answered:             raider.Answered,
				Rate:                 raider.Rate,
				Silence:              raider.Silence,
			}
		}

		writeJSON(w, logger, http.StatusOK, attendanceResponse{
			periodResponse: periodToResponse(result.Period),
			Events:         result.Events,
			Raiders:        raiders,
			Links:          panelLinks(guildID, "attendance"),
		})
	}
}

func getCompBalanceHandler(analysis *audit.Analysis, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		result, err := analysis.CompBalance(r.Context(), guildID)
		if err != nil {
			writeAnalysisFailure(w, r, logger, "reading comp balance", err)
			return
		}

		roles := make([]roleBalanceResponse, len(result.Roles))
		for i, role := range result.Roles {
			roles[i] = roleBalanceResponse{
				Role:      string(role.Role),
				Placed:    role.Placed,
				Benched:   role.Benched,
				Share:     role.Share,
				BenchRate: role.BenchRate,
			}
		}
		bench := make([]benchRecordResponse, len(result.Bench))
		for i, record := range result.Bench {
			bench[i] = benchRecordResponse{
				characterRefResponse: characterRefToResponse(record.Character),
				Boards:               record.Boards,
				Benched:              record.Benched,
				Rate:                 record.Rate,
			}
		}

		writeJSON(w, logger, http.StatusOK, compBalanceResponse{
			periodResponse: periodToResponse(result.Period),
			Roles:          roles,
			Bench:          bench,
			Links:          panelLinks(guildID, "comp-balance"),
		})
	}
}

func getRosterHealthHandler(analysis *audit.Analysis, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		result, err := analysis.RosterHealth(r.Context(), guildID)
		if err != nil {
			writeAnalysisFailure(w, r, logger, "reading roster health", err)
			return
		}

		coverage := make([]roleCoverageResponse, len(result.Coverage))
		for i, role := range result.Coverage {
			coverage[i] = roleCoverageResponse{
				Role:        string(role.Role),
				Characters:  role.Characters,
				FirstChoice: role.FirstChoice,
			}
		}

		writeJSON(w, logger, http.StatusOK, rosterHealthResponse{
			periodResponse: periodToResponse(result.Period),
			Characters:     result.Characters,
			Mains:          result.Mains,
			Active:         result.Active,
			Dormant:        result.Dormant,
			Coverage:       coverage,
			Links:          panelLinks(guildID, "roster-health"),
		})
	}
}

func getThroughputHandler(analysis *audit.Analysis, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		result, err := analysis.Throughput(r.Context(), guildID)
		if err != nil {
			writeAnalysisFailure(w, r, logger, "reading throughput", err)
			return
		}

		weeks := make([]throughputWeekResponse, len(result.Weeks))
		for i, week := range result.Weeks {
			weeks[i] = throughputWeekResponse{
				Week:       week.Week,
				Events:     week.Events,
				Confirmed:  week.Confirmed,
				Declined:   week.Declined,
				NoShow:     week.NoShow,
				Placed:     week.Placed,
				Benched:    week.Benched,
				BenchRate:  week.BenchRate,
				NoShowRate: week.NoShowRate,
			}
		}

		writeJSON(w, logger, http.StatusOK, throughputResponse{
			periodResponse: periodToResponse(result.Period),
			Events:         result.Events,
			Weeks:          weeks,
			Links:          panelLinks(guildID, "throughput"),
		})
	}
}

func getIlvlSeriesHandler(analysis *audit.Analysis, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		guildID, ok := requireGuildPath(w, r, logger, "gid")
		if !ok {
			return
		}

		result, err := analysis.Ilvl(r.Context(), guildID)
		if err != nil {
			writeAnalysisFailure(w, r, logger, "reading ilvl series", err)
			return
		}

		weeks := make([]ilvlWeekResponse, len(result.Weeks))
		for i, week := range result.Weeks {
			weeks[i] = ilvlWeekResponse{
				Week:       week.Week,
				Characters: week.Characters,
				P25:        week.P25,
				Median:     week.Median,
				P75:        week.P75,
			}
		}

		writeJSON(w, logger, http.StatusOK, ilvlSeriesResponse{
			periodResponse: periodToResponse(result.Period),
			Weeks:          weeks,
			Links:          panelLinks(guildID, "ilvl"),
		})
	}
}

func characterRefToResponse(ref audit.CharacterRef) characterRefResponse {
	return characterRefResponse{
		CharacterID: ref.ID.String(),
		Name:        ref.Name,
		Realm:       ref.Realm,
		Class:       ref.Class,
	}
}
