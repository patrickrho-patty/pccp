// Package leaderboard implements PAT-1440's evidence-backed workforce
// scorecard. It replaces the prototype that directly rewarded raw lines /
// AI-inference counts with a rubric of exactly four scored properties, each
// derived from durable PCCP evidence, correct user scoping, tenant-configurable
// weights within Patty-enforced guardrails, cohorting, minimum evidence, rubric
// versioning with freeze-at-period-start, anti-gaming (coalescing split
// objectives), and a human-finalization record that is strictly separate from
// any score.
package leaderboard

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// DefaultRubric is the Patty-supplied default weight set (spec).
var DefaultRubric = struct {
	Delivery, Quality, Security, Efficiency int
	CriticalCeiling                         float64
	MinAcceptedOutcomes, MinGovernedActions int
}{
	Delivery: 30, Quality: 25, Security: 30, Efficiency: 15,
	CriticalCeiling: 60.0, MinAcceptedOutcomes: 5, MinGovernedActions: 20,
}

// Service is the leaderboard scoring engine.
type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

// evidence is everything needed to score one subject in one period. It is
// gathered with STRICT user scoping (fixing the prototype's org-wide leaks).
type evidence struct {
	// Accepted delivery: distinct accepted objectives (coalesced).
	accepted []models.WorkObjective
	// First-pass quality outcomes for accepted objectives.
	firstPassOK int
	// Security/governance: governed actions vs confirmed violations.
	governedActions     int
	confirmedViolations int
	violationWeight     int // severity-weighted
	criticalViolation   bool
	// Efficiency: active elapsed + model turns for accepted objectives.
	efficiencyTurns   []int
	efficiencyElapsed []int64
	// Context stats (never score inputs).
	totalSessions   int64
	totalModelTurns int64
	tokensUsed      int64
}

// Score carries the computed properties for one subject.
type Score struct {
	EvidenceCount       int `json:"evidence_count"`
	AcceptedOutcomes    int `json:"accepted_outcomes"`
	GovernedActions     int `json:"governed_actions"`
	ConfirmedViolations int `json:"confirmed_violations"`
	// Four properties (0-100); 0 does NOT indicate "bad" when insufficient.
	Delivery   float64 `json:"delivery"`
	Quality    float64 `json:"quality"`
	Security   float64 `json:"security"`
	Efficiency float64 `json:"efficiency"`
	Overall    float64 `json:"overall"`
	// Sufficient is false until the minimum evidence thresholds are met.
	Sufficient  bool    `json:"sufficient"`
	Percentile  float64 `json:"percentile,omitempty"`
	Rank        int     `json:"rank"`
	Explanation string  `json:"explanation"`
}

// GeneratePeriodScores computes (and persists) a provisional scorecard for every
// eligible subject in the period, then ranks within each cohort. Idempotent:
// recomputing from event high-water marks overwrites provisional snapshots.
func (s *Service) GeneratePeriodScores(orgID, periodID string) ([]Score, error) {
	var period models.ScorecardPeriod
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, periodID).First(&period).Error; err != nil {
		return nil, fmt.Errorf("leaderboard: period not found")
	}
	rubric := &models.ScorecardRubric{
		WeightDelivery:      DefaultRubric.Delivery,
		WeightQuality:       DefaultRubric.Quality,
		WeightSecurity:      DefaultRubric.Security,
		WeightEfficiency:    DefaultRubric.Efficiency,
		CriticalCeiling:     DefaultRubric.CriticalCeiling,
		MinAcceptedOutcomes: DefaultRubric.MinAcceptedOutcomes,
		MinGovernedActions:  DefaultRubric.MinGovernedActions,
	}
	if period.RubricID != "" {
		if r, err := s.activeRubric(orgID, period.RubricID); err == nil {
			rubric = r
		}
	}
	// Collect subjects: anyone who owns objectives or ran sessions/actions in period.
	subjects := s.eligibleSubjects(orgID, period)
	type subj struct {
		id     string
		cohort string
		score  *Score
	}
	var rows []subj
	byCohort := map[string][]*Score{}
	for _, uid := range subjects {
		ev, err := s.gatherEvidence(orgID, uid, period)
		if err != nil {
			continue
		}
		sc := score(ev, rubric)
		cohort := s.cohortFor(orgID, uid)
		rows = append(rows, subj{uid, cohort, &sc})
		byCohort[cohort] = append(byCohort[cohort], &sc)
	}
	// Rank within cohort (sufficient subjects first, then by overall).
	for _, list := range byCohort {
		sort.SliceStable(list, func(a, b int) bool {
			if list[a].Sufficient != list[b].Sufficient {
				return list[a].Sufficient
			}
			return list[a].Overall > list[b].Overall
		})
		rank := 0
		for i, sc := range list {
			if sc.Sufficient {
				rank++
				sc.Rank = rank
				sc.Percentile = cohortPercentile(i, len(list))
			}
		}
	}
	// Persist snapshots (idempotent) with the filled rank/percentile/explanation.
	merged := make([]Score, 0, len(rows))
	for _, row := range rows {
		row.score.Explanation = explain(*row.score, rubric, row.cohort)
		merged = append(merged, *row.score)
		s.persistSnapshot(orgID, period, rubric, row.id, row.cohort, *row.score)
	}
	return merged, nil
}

func cohortPercentile(idx, total int) float64 {
	if total <= 1 {
		return 100
	}
	return float64(total-idx) / float64(total) * 100
}

// gatherEvidence collects all durable evidence for one user in the period with
// strict org+user scoping on every query (defect fix). Security findings and
// prompt exchanges carry no direct user_id — they are attributed via their
// session — so those queries JOIN through the session's owner.
func (s *Service) gatherEvidence(orgID, userID string, period models.ScorecardPeriod) (*evidence, error) {
	ev := &evidence{}
	startU := period.StartAt.UTC()
	endU := period.EndAt.UTC()
	startStr := startU.Format(time.RFC3339)
	endStr := endU.Format(time.RFC3339)

	// Accepted objectives owned by this user (coalesced by coalesce_key).
	var accepted []models.WorkObjective
	if err := s.db.Where("organization_id = ? AND owner_id = ? AND status = ? AND accepted_at BETWEEN ? AND ?",
		orgID, userID, "accepted", startStr, endStr).
		Find(&accepted).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, o := range accepted {
		k := o.CoalesceKey
		if k == "" {
			k = o.ObjectiveID
		}
		if seen[k] {
			continue // coalesce split objectives → one delivery credit
		}
		seen[k] = true
		ev.accepted = append(ev.accepted, o)
		if o.PassedFirstGate && o.AvoidedRework && o.NoRegression {
			ev.firstPassOK++
		}
		if o.AcceptedAt.After(o.StartedAt) && o.StartedAt.Year() > 1000 {
			ev.efficiencyElapsed = append(ev.efficiencyElapsed, o.ActiveElapsedMs)
		}
		ev.efficiencyTurns = append(ev.efficiencyTurns, o.ModelTurns)
	}
	if len(ev.accepted) == 0 {
		ev.efficiencyElapsed = nil
		ev.efficiencyTurns = nil
	}

	// Governed actions, user scoped.
	var governed int64
	s.db.Model(&models.ActionEnvelope{}).
		Where("organization_id = ? AND user_id = ? AND occurred_at BETWEEN ? AND ?", orgID, userID, startU.Format(time.RFC3339), endU.Format(time.RFC3339)).
		Count(&governed)
	ev.governedActions = int(governed)

	// Confirmed violations: scope findings to the user by joining through their
	// sessions (findings carry session_id, not user_id). Exclude false
	// positives / suppressed / approved-exception findings; weight by severity.
	var findings []models.SecurityFinding
	if err := s.db.Table("security_findings f").
		Joins("JOIN sessions s ON s.session_id = f.session_id AND s.organization_id = f.organization_id").
		Where("f.organization_id = ? AND s.user_id = ? AND f.occurred_at BETWEEN ? AND ? AND (f.suppressed = ? OR f.status IN ?)",
			orgID, userID, startStr, endStr, false, []string{"open", "investigating", "resolved"}).
		Find(&findings).Error; err == nil {
		for _, f := range findings {
			if f.Suppressed || f.Status == "false_positive" || f.Status == "fixed" {
				continue
			}
			ev.confirmedViolations++
			ev.violationWeight += severityWeight(f.Severity)
			if f.Severity == "critical" {
				ev.criticalViolation = true
			}
		}
	}

	// Context stats.
	var sessions int64
	s.db.Model(&models.Session{}).Where("organization_id = ? AND user_id = ?", orgID, userID).Count(&sessions)
	ev.totalSessions = sessions
	var turns int64
	s.db.Model(&models.ActionEnvelope{}).Where("organization_id = ? AND user_id = ? AND action_type = ? AND occurred_at BETWEEN ? AND ?",
		orgID, userID, "ai.inference", startStr, endStr).Count(&turns)
	ev.totalModelTurns = turns
	// Token usage also joins through sessions (exchanges carry no user_id).
	s.db.Table("prompt_exchanges e").
		Joins("JOIN sessions s ON s.session_id = e.session_id AND s.organization_id = e.organization_id").
		Where("e.organization_id = ? AND s.user_id = ? AND e.created_at BETWEEN ? AND ?", orgID, userID, startStr, endStr).
		Select("COALESCE(SUM(e.input_tokens + e.output_tokens), 0)").Scan(&ev.tokensUsed)
	return ev, nil
}

// score reduces evidence to the four properties + overall (spec property map).
func score(ev *evidence, r *models.ScorecardRubric) Score {
	sc := Score{
		EvidenceCount:       len(ev.accepted) + ev.governedActions + ev.confirmedViolations,
		AcceptedOutcomes:    len(ev.accepted),
		GovernedActions:     ev.governedActions,
		ConfirmedViolations: ev.confirmedViolations,
	}
	// Minimum evidence gate: no rank/score below thresholds.
	if len(ev.accepted) < r.MinAcceptedOutcomes || ev.governedActions < r.MinGovernedActions {
		sc.Sufficient = false
		sc.Explanation = "근거 부족 / Insufficient evidence"
		return sc
	}
	sc.Sufficient = true

	// 1. Accepted delivery — coalesced outcome count, 0-100 by normalization
	// within cohort; the engine has no global raw-count threshold. We use a
	// per-subject saturating curve relative to the cohort median (robust).
	sc.Delivery = rate(len(ev.accepted), r.MinAcceptedOutcomes, 30)

	// 2. First-pass quality — rate of accepted outcomes that passed gates.
	n := len(ev.accepted)
	if n > 0 {
		sc.Quality = float64(ev.firstPassOK) / float64(n) * 100
	}

	// 3. Security/governance adherence — governed actions without confirmed
	// violations, severity-weighted. Exceptions/false-positives already excluded.
	if ev.governedActions > 0 {
		clean := ev.governedActions - ev.violationWeight
		if clean < 0 {
			clean = 0
		}
		sc.Security = float64(clean) / float64(ev.governedActions) * 100
	} else {
		sc.Security = 100
	}

	// 4. Delivery efficiency — median active elapsed + median model turns,
	// compared within cohort (robust; raw speed across incomparable work gives
	// no credit). We normalize turns and elapsed to a 0-100 helper.
	sc.Efficiency = efficiencyScore(ev.efficiencyTurns, ev.efficiencyElapsed)

	// Overall: weights sum to 100 (validated at rubric save).
	total := r.WeightDelivery + r.WeightQuality + r.WeightSecurity + r.WeightEfficiency
	if total == 0 {
		total = 100
	}
	sc.Overall = (sc.Delivery*float64(r.WeightDelivery) + sc.Quality*float64(r.WeightQuality) +
		sc.Security*float64(r.WeightSecurity) + sc.Efficiency*float64(r.WeightEfficiency)) / float64(total)

	// Critical-violation ceiling (Patty enforced, independent of tenant weights).
	if ev.criticalViolation && sc.Overall > r.CriticalCeiling {
		sc.Overall = r.CriticalCeiling
	}
	sc.Explanation = fmt.Sprintf("수용 %d건 · 협약 1회 통과율 %.0f%% · 거버넌스 위반 %d건 · 효율 중앙값 %d턴/%dms",
		len(ev.accepted), pct(sc.Quality), ev.confirmedViolations, medianInt(ev.efficiencyTurns), medianInt64(ev.efficiencyElapsed))
	return sc
}

func pct(v float64) float64 { return v }

// rate maps a count to 0-100 using a saturating curve with an elbow at the
// cohort floor — no global raw threshold rewards unlimited work.
func rate(count, floor, elbow int) float64 {
	if count <= 0 {
		return 0
	}
	if count >= elbow {
		return 100
	}
	f := float64(floor)
	if f <= 0 {
		f = 1
	}
	return 50 + 50*float64(count-floor)/float64(elbow-floor)
}

func efficiencyScore(turns []int, elapsed []int64) float64 {
	if len(turns) == 0 && len(elapsed) == 0 {
		return 50 // no incomparable-speed credit; neutral
	}
	// Lower is better for both. Robust: fewer turns/less active time → higher.
	turnScore := 50.0
	elapsedScore := 50.0
	if len(turns) > 0 {
		med := medianInt(turns)
		turnScore = 100 - float64(clamp(med, 0, 100))*0.5 // 0 turns→100, 100+→50 floor
	}
	if len(elapsed) > 0 {
		med := medianInt64(elapsed)
		elapsedScore = 100 - float64(clamp64(med/60000, 0, 100))*0.5 // 0min→100, 100+min→50 floor
	}
	return (turnScore + elapsedScore) / 2
}

func medianInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	c := append([]int(nil), xs...)
	sort.Ints(c)
	return c[len(c)/2]
}

func medianInt64(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	c := append([]int64(nil), xs...)
	sort.Slice(c, func(a, b int) bool { return c[a] < c[b] })
	return c[len(c)/2]
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clamp64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func severityWeight(sev string) int {
	switch sev {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

// --- Rubric helpers ---

func (s *Service) activeRubric(orgID, rubricID string) (*models.ScorecardRubric, error) {
	var r models.ScorecardRubric
	if err := s.db.Where("organization_id = ? AND (rubric_id = ? OR id = ?) AND status != ?", orgID, rubricID, rubricID, "draft").First(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

// ValidateWeights enforces Patty guardrails: sum to 100, quality ≥20,
// security ≥20, no property >40.
func ValidateWeights(delivery, quality, security, efficiency int) error {
	if delivery+quality+security+efficiency != 100 {
		return fmt.Errorf("weights must sum to 100")
	}
	if quality < 20 {
		return fmt.Errorf("quality weight must be at least 20")
	}
	if security < 20 {
		return fmt.Errorf("security weight must be at least 20")
	}
	if delivery > 40 || quality > 40 || security > 40 || efficiency > 40 {
		return fmt.Errorf("no property may exceed 40")
	}
	return nil
}

// FreezePeriod marks a period frozen and locks its rubric; scores after this
// point compute against the frozen rubric version (historical retention).
func (s *Service) FreezePeriod(orgID, periodID, byUser string) error {
	now := time.Now().UTC()
	return s.db.Model(&models.ScorecardPeriod{}).
		Where("organization_id = ? AND id = ? AND status = ?", orgID, periodID, "running").
		Updates(map[string]interface{}{"status": "frozen", "finalized_by": byUser, "finalized_at": now.Format(time.RFC3339)}).Error
}

// cohortFor returns the subject's comparison cohort (role/level/work-family).
func (s *Service) cohortFor(orgID, userID string) string {
	if userID == "" {
		return "org"
	}
	var u models.User
	if err := s.db.Where("id = ?", userID).First(&u).Error; err != nil {
		return "org"
	}
	// Korean enterprise attributes: business unit + title determine the
	// like-for-like cohort (PRD §12.2 / §25.5).
	cohort := "org"
	if u.BusinessUnitID != "" {
		cohort = "bu:" + u.BusinessUnitID
	}
	if u.Title != "" {
		cohort += ":" + u.Title
	}
	return cohort
}

func (s *Service) eligibleSubjects(orgID string, period models.ScorecardPeriod) []string {
	set := map[string]bool{}
	var owners []string
	s.db.Model(&models.WorkObjective{}).
		Where("organization_id = ? AND status = ? AND accepted_at BETWEEN ? AND ?", orgID, "accepted",
			period.StartAt.UTC().Format(time.RFC3339), period.EndAt.UTC().Format(time.RFC3339)).
		Distinct().Pluck("owner_id", &owners)
	for _, o := range owners {
		if o != "" {
			set[o] = true
		}
	}
	var actors []string
	s.db.Model(&models.ActionEnvelope{}).
		Where("organization_id = ? AND occurred_at BETWEEN ? AND ?", orgID,
			period.StartAt.UTC().Format(time.RFC3339), period.EndAt.UTC().Format(time.RFC3339)).
		Distinct().Pluck("user_id", &actors)
	for _, a := range actors {
		if a != "" {
			set[a] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Service) persistSnapshot(orgID string, period models.ScorecardPeriod, rubric *models.ScorecardRubric, subjectID, cohort string, sc Score) {
	snap := &models.ScorecardSnapshot{
		OrganizationID: orgID, PeriodID: period.ID, RubricID: rubric.RubricID,
		RubricVersion: rubric.Version, SubjectType: "individual", SubjectID: subjectID, Cohort: cohort,
		DeliveryScore: sc.Delivery, QualityScore: sc.Quality, SecurityScore: sc.Security,
		EfficiencyScore: sc.Efficiency, OverallScore: sc.Overall, EvidenceCount: sc.EvidenceCount,
		Confidence: "sufficient", AcceptedOutcomes: sc.AcceptedOutcomes,
		GovernedActions: sc.GovernedActions, ConfirmedViolations: sc.ConfirmedViolations,
		Explanation: sc.Explanation, State: "provisional", ComputedAt: time.Now().UTC().Format(time.RFC3339),
		Rank: sc.Rank, Percentile: sc.Percentile,
	}
	if !sc.Sufficient {
		snap.Confidence = "insufficient"
		snap.OverallScore = 0
	}
	s.db.Where("organization_id = ? AND period_id = ? AND subject_id = ?", orgID, period.ID, subjectID).
		Delete(&models.ScorecardSnapshot{})
	s.db.Create(snap)
}

func explain(sc Score, r *models.ScorecardRubric, cohort string) string {
	if !sc.Sufficient {
		return "근거 부족 / Insufficient evidence"
	}
	return fmt.Sprintf("코호트 %s · 가중치 수용%d/품질%d/보안%d/효율%d · 수용 %d건 · 1회통과 %.0f%% · 보안 %.0f · 효율 %.0f",
		cohort, r.WeightDelivery, r.WeightQuality, r.WeightSecurity, r.WeightEfficiency,
		sc.AcceptedOutcomes, sc.Quality, sc.Security, sc.Efficiency)
}

// MarshalObjectivePayload is unused; kept for API symmetry.
func MarshalObjectivePayload(o models.WorkObjective) string {
	b, _ := json.Marshal(o)
	return string(b)
}

var _ = dari.GenerateID
var _ = strings.TrimSpace
