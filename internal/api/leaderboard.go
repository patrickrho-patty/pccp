package api

// PAT-1440 evidence-backed leaderboard — API adapter over the leaderboard
// scoring engine. Rubric management with weight guardrails, review periods
// with freeze-at-finalization, score generation, corrections/disputes, and
// separated human finalization records. Tenant-isolated; never exposed in
// public profiles.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/leaderboard"
	"github.com/patrickrho-patty/pccp/internal/models"
)

const leaderboardIssuer = "leaderboard"

// --- Rubric ---

type rubricRequest struct {
	Name             string `json:"name"`
	NameKo           string `json:"name_ko"`
	WeightDelivery   int    `json:"weight_delivery"`
	WeightQuality    int    `json:"weight_quality"`
	WeightSecurity   int    `json:"weight_security"`
	WeightEfficiency int    `json:"weight_efficiency"`
}

func (s *Server) handleLeaderboardRubrics(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if r.Method == http.MethodGet {
		var rubrics []models.ScorecardRubric
		s.db.Where("organization_id = ?", orgID).Order("version DESC").Find(&rubrics)
		writeJSON(w, http.StatusOK, rubrics)
		return
	}
	var req rubricRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := leaderboard.ValidateWeights(req.WeightDelivery, req.WeightQuality, req.WeightSecurity, req.WeightEfficiency); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var max models.ScorecardRubric
	s.db.Where("organization_id = ?", orgID).Order("version DESC").First(&max)
	version := uint64(1)
	if max.ID != "" {
		version = max.Version + 1
	}
	rubric := &models.ScorecardRubric{
		OrganizationID: orgID, RubricID: dari.GenerateID("scr"),
		Version: version, Name: req.Name, NameKo: req.NameKo,
		WeightDelivery: req.WeightDelivery, WeightQuality: req.WeightQuality,
		WeightSecurity: req.WeightSecurity, WeightEfficiency: req.WeightEfficiency,
		CriticalCeiling: leaderboard.DefaultRubric.CriticalCeiling,
		MinAcceptedOutcomes: leaderboard.DefaultRubric.MinAcceptedOutcomes,
		MinGovernedActions:  leaderboard.DefaultRubric.MinGovernedActions,
		Status: "active", CreatedBy: getActorID(r),
		Supersedes: max.Version, EffectiveAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(rubric).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "leaderboard: "+err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, ActorID: getActorID(r), ActorType: "user",
		EventType: "cp.leaderboard.rubric", Action: "leaderboard.rubric",
		ResourceType: "scorecard_rubric", ResourceID: rubric.RubricID, Result: "created",
		Details: string(mustJSON(map[string]interface{}{"version": version, "weights": fmt.Sprintf("%d/%d/%d/%d", req.WeightDelivery, req.WeightQuality, req.WeightSecurity, req.WeightEfficiency)})),
	})
	writeJSON(w, http.StatusOK, rubric)
}

// --- Period ---

type periodRequest struct {
	Name       string `json:"name"`
	NameKo     string `json:"name_ko"`
	PeriodType string `json:"period_type"`
	StartAt    string `json:"start_at"`
	EndAt      string `json:"end_at"`
	RubricID   string `json:"rubric_id"`
}

func (s *Server) handleLeaderboardPeriods(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if r.Method == http.MethodGet {
		var periods []models.ScorecardPeriod
		s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Find(&periods)
		writeJSON(w, http.StatusOK, periods)
		return
	}
	var req periodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.PeriodType != "rolling_90d" && req.PeriodType != "fixed_quarter" {
		writeError(w, http.StatusBadRequest, "period_type must be rolling_90d|fixed_quarter")
		return
	}
	start, err1 := time.Parse(time.RFC3339, req.StartAt)
	end, err2 := time.Parse(time.RFC3339, req.EndAt)
	if err1 != nil || err2 != nil || !end.After(start) {
		writeError(w, http.StatusBadRequest, "start_at/end_at must be RFC3339 with end after start")
		return
	}
	period := &models.ScorecardPeriod{
		OrganizationID: orgID, PeriodID: dari.GenerateID("scp"),
		Name: req.Name, NameKo: req.NameKo, PeriodType: req.PeriodType,
		StartAt: start, EndAt: end, RubricID: req.RubricID, Status: "running",
	}
	if err := s.db.Create(period).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "leaderboard: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, period)
}

// handleLeaderboardFreeze freezes a period: scores at/after this point compute
// against the frozen rubric version; official snapshots no longer recompute.
func (s *Server) handleLeaderboardFreeze(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	periodID := chi.URLParam(r, "id")
	if orgID == "" || periodID == "" {
		writeError(w, http.StatusBadRequest, "organization context and period id required")
		return
	}
	var period models.ScorecardPeriod
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, periodID).First(&period).Error; err != nil {
		writeError(w, http.StatusNotFound, "period not found")
		return
	}
	if err := s.leaderboardSV.FreezePeriod(orgID, period.ID, getActorID(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "status": "frozen"})
}

// handleLeaderboardGenerate computes provisional scores for a period.
func (s *Server) handleLeaderboardGenerate(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	periodID := chi.URLParam(r, "id")
	if orgID == "" || periodID == "" {
		writeError(w, http.StatusBadRequest, "organization context and period id required")
		return
	}
	var period models.ScorecardPeriod
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, periodID).First(&period).Error; err != nil {
		writeError(w, http.StatusNotFound, "period not found")
		return
	}
	if period.Status == "frozen" || period.Status == "finalized" {
		writeError(w, http.StatusConflict, "period is frozen/finalized — scores are immutable")
		return
	}
	scores, err := s.leaderboardSV.GeneratePeriodScores(orgID, period.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"generated": len(scores), "scores": scores})
}

// handleLeaderboardList returns persisted snapshots for a period with filters.
func (s *Server) handleLeaderboardList(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	periodID := r.URL.Query().Get("period_id")
	if orgID == "" || periodID == "" {
		writeError(w, http.StatusBadRequest, "organization context and period_id required")
		return
	}
	q := s.db.Where("organization_id = ? AND period_id = ?", orgID, periodID)
	if subject := r.URL.Query().Get("subject_id"); subject != "" {
		q = q.Where("subject_id = ?", subject)
	}
	if conf := r.URL.Query().Get("confidence"); conf != "" {
		q = q.Where("confidence = ?", conf)
	}
	var snaps []models.ScorecardSnapshot
	q.Order("CASE WHEN confidence = 'insufficient' THEN 1 ELSE 0 END, rank").Find(&snaps)
	writeJSON(w, http.StatusOK, snaps)
}

// handleLeaderboardObjective persists a predeclared work objective. Work class,
// size band, owner/attribution, and acceptance policy arrive BEFORE meaningful
// work; changing class after observing the outcome is an audited correction.
func (s *Server) handleLeaderboardObjective(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var req struct {
		ObjectiveID      string `json:"objective_id"`
		WorkType         string `json:"work_type"`
		SizeBand         string `json:"size_band"`
		OwnerID          string `json:"owner_id"`
		TeamID           string `json:"team_id,omitempty"`
		ProjectID        string `json:"project_id,omitempty"`
		CoalesceKey      string `json:"coalesce_key,omitempty"`
		StartedAt        string `json:"started_at"`
		AcceptedAt       string `json:"accepted_at"`
		Status           string `json:"status"`
		PassedFirstGate  bool   `json:"passed_first_gate"`
		AvoidedRework    bool   `json:"avoided_rework"`
		NoRegression     bool   `json:"no_regression"`
		ModelTurns       int    `json:"model_turns"`
		ActiveElapsedMs  int64  `json:"active_elapsed_ms"`
		ApprovalWaitMs   int64  `json:"approval_wait_ms,omitempty"`
		QueryQueueWaitMs int64  `json:"query_queue_wait_ms,omitempty"`
		UserAwayMs       int64  `json:"user_away_ms,omitempty"`
		AcceptanceGate   string `json:"acceptance_gate,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.ObjectiveID == "" || req.OwnerID == "" {
		writeError(w, http.StatusBadRequest, "objective_id and owner_id required")
		return
	}
	switch req.WorkType {
	case "defect", "feature", "refactor", "security", "documentation":
	default:
		writeError(w, http.StatusBadRequest, "work_type must be defect|feature|refactor|security|documentation")
		return
	}
	switch req.SizeBand {
	case "small", "medium", "large":
	default:
		writeError(w, http.StatusBadRequest, "size_band must be small|medium|large")
		return
	}
	switch req.Status {
	case "open", "accepted", "rejected", "reverted":
	default:
		writeError(w, http.StatusBadRequest, "status must be open|accepted|rejected|reverted")
		return
	}
	obj := &models.WorkObjective{
		OrganizationID: orgID, ObjectiveID: req.ObjectiveID, WorkType: req.WorkType,
		SizeBand: req.SizeBand, OwnerID: req.OwnerID, TeamID: req.TeamID, ProjectID: req.ProjectID,
		CoalesceKey: req.CoalesceKey, Status: req.Status, AcceptanceGate: req.AcceptanceGate,
		PassedFirstGate: req.PassedFirstGate, AvoidedRework: req.AvoidedRework, NoRegression: req.NoRegression,
		ModelTurns: req.ModelTurns, ActiveElapsedMs: req.ActiveElapsedMs,
		ApprovalWaitMs: req.ApprovalWaitMs, QueryQueueWaitMs: req.QueryQueueWaitMs, UserAwayMs: req.UserAwayMs,
		SourcedBy: "api",
	}
	if t, err := time.Parse(time.RFC3339, req.StartedAt); err == nil {
		obj.StartedAt = t
	}
	if t, err := time.Parse(time.RFC3339, req.AcceptedAt); err == nil {
		obj.AcceptedAt = t
	}
	// Upsert by objective id.
	var existing models.WorkObjective
	if err := s.db.Where("organization_id = ? AND objective_id = ?", orgID, req.ObjectiveID).First(&existing).Error; err == nil {
		if existing.WorkType != req.WorkType || existing.SizeBand != req.SizeBand {
			// Class change after outcome observed — audited as a correction.
			models.CreateAuditEvent(s.db, &models.AuditEvent{
				OrganizationID: orgID, ActorID: getActorID(r), ActorType: "user",
				EventType: "cp.leaderboard.correction", Action: "leaderboard.objective_reclassify",
				ResourceType: "work_objective", ResourceID: obj.ObjectiveID, Result: "review",
				Details: string(mustJSON(map[string]interface{}{"from": existing.WorkType + "/" + existing.SizeBand, "to": req.WorkType + "/" + req.SizeBand})),
			})
		}
		obj.ID = existing.ID
		if err := s.db.Model(&existing).Updates(map[string]interface{}{
			"work_type": req.WorkType, "size_band": req.SizeBand, "status": req.Status,
			"accepted_at": req.AcceptedAt, "model_turns": req.ModelTurns,
			"active_elapsed_ms": req.ActiveElapsedMs, "passed_first_gate": req.PassedFirstGate,
			"avoided_rework": req.AvoidedRework, "no_regression": req.NoRegression,
			"approval_wait_ms": req.ApprovalWaitMs, "query_queue_wait_ms": req.QueryQueueWaitMs, "user_away_ms": req.UserAwayMs,
		}).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "leaderboard: "+err.Error())
			return
		}
	} else {
		if err := s.db.Create(obj).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "leaderboard: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "objective_id": req.ObjectiveID, "reclassified": existing.ID != ""})
}

// handleLeaderboardCorrection submits/decides a correction or dispute.
func (s *Server) handleLeaderboardCorrection(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var req struct {
		PeriodID    string `json:"period_id"`
		SubjectID   string `json:"subject_id"`
		Kind        string `json:"kind"`
		Reason      string `json:"reason"`
		EvidenceRef string `json:"evidence_ref,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.PeriodID == "" || req.SubjectID == "" {
		writeError(w, http.StatusBadRequest, "period_id and subject_id required")
		return
	}
	if req.Kind != "correction" && req.Kind != "dispute" && req.Kind != "appeal" {
		writeError(w, http.StatusBadRequest, "kind must be correction|dispute|appeal")
		return
	}
	corr := &models.ScorecardCorrection{
		OrganizationID: orgID, PeriodID: req.PeriodID, SubjectID: req.SubjectID,
		Kind: req.Kind, Reason: req.Reason, EvidenceRef: req.EvidenceRef,
		Status: "open", ByUserID: getActorID(r),
	}
	if err := s.db.Create(corr).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "leaderboard: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, corr)
}

// handleLeaderboardReview records a human finalization decision SEPARATE from
// any score. PCCP never emits an automatic promotion recommendation.
func (s *Server) handleLeaderboardReview(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var req struct {
		PeriodID  string `json:"period_id"`
		SubjectID string `json:"subject_id"`
		Decision  string `json:"decision"`
		Rationale string `json:"rationale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.PeriodID == "" || req.SubjectID == "" || req.Decision == "" {
		writeError(w, http.StatusBadRequest, "period_id, subject_id, and decision required")
		return
	}
	if req.Decision != "documented" && req.Decision != "retain" && req.Decision != "promotion_review" {
		writeError(w, http.StatusBadRequest, "decision must be documented|retain|promotion_review (never an automatic action)")
		return
	}
	review := &models.ScorecardReview{
		OrganizationID: orgID, PeriodID: req.PeriodID, SubjectID: req.SubjectID,
		ReviewerID: getActorID(r), Decision: req.Decision, Rationale: req.Rationale,
		ReviewedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(review).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "leaderboard: "+err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, ActorID: getActorID(r), ActorType: "user",
		EventType: "cp.leaderboard.reviewed", Action: "leaderboard.reviewed",
		ResourceType: "scorecard_review", ResourceID: review.ID, Result: "saved",
		Details: string(mustJSON(map[string]interface{}{"decision": req.Decision, "period_id": req.PeriodID, "subject_id": req.SubjectID})),
	})
	writeJSON(w, http.StatusOK, review)
}

// handleLeaderboardExport streams a finalized period's snapshots + reviews as
// JSON for audit export (same role authorization as the API itself).
func (s *Server) handleLeaderboardExport(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	periodID := r.URL.Query().Get("period_id")
	if orgID == "" || periodID == "" {
		writeError(w, http.StatusBadRequest, "organization context and period_id required")
		return
	}
	var snaps []models.ScorecardSnapshot
	s.db.Where("organization_id = ? AND period_id = ?", orgID, periodID).Find(&snaps)
	var reviews []models.ScorecardReview
	s.db.Where("organization_id = ? AND period_id = ?", orgID, periodID).Find(&reviews)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"snapshots":   snaps, "reviews": reviews,
	})
}
