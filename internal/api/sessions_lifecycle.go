package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// sessions_lifecycle.go: web/02 Sessions plan — consolidated detail (UX6),
// per-exchange decision log (B2), replay/fork (B6), cost breakdown (B7),
// visibility indicator (B8), bulk lifecycle (UX7).

// handleGetSessionDetail consolidates the inspector's fetches into one
// endpoint (UX6): session + usage + timeline + exchanges + provenance.
func (s *Server) handleGetSessionDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var actions []models.ActionEnvelope
	s.db.Where("session_id = ?", sess.SessionID).Order("occurred_at DESC").Limit(100).Find(&actions)
	var changeSets []models.ChangeSet
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at DESC").Find(&changeSets)
	var findings []models.SecurityFinding
	s.db.Where("session_id = ?", sess.SessionID).Order("occurred_at DESC").Find(&findings)
	var exchanges []models.PromptExchange
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at ASC").Find(&exchanges)
	var usage []models.UsageRecord
	s.db.Where("session_id = ?", sess.SessionID).Find(&usage)
	var spans []models.ProvenanceSpan
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at ASC").Find(&spans)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session":     sess,
		"actions":     actions,
		"change_sets": changeSets,
		"findings":    findings,
		"exchanges":   exchanges,
		"usage":       usage,
		"spans":       spans,
	})
}

// handleGetSessionDecisions returns the per-exchange policy decision log
// (B2): every exchange with its verdict + epoch, ordered chronologically.
func (s *Server) handleGetSessionDecisions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var exchanges []models.PromptExchange
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at ASC").Find(&exchanges)
	type decisionRow struct {
		ExchangeID     string `json:"exchange_id"`
		PolicyEpochID  string `json:"policy_epoch_id"`
		Verdict        string `json:"verdict"`
		ModelPackageID string `json:"model_package_id"`
		InputTokens    int    `json:"input_tokens"`
		OutputTokens   int    `json:"output_tokens"`
		At             string `json:"at"`
	}
	rows := make([]decisionRow, 0, len(exchanges))
	for _, ex := range exchanges {
		verdict := ex.VerdictResult
		if verdict == "" {
			verdict = "unrecorded"
		}
		rows = append(rows, decisionRow{
			ExchangeID: ex.ExchangeID, PolicyEpochID: ex.PolicyEpochID,
			Verdict: verdict, ModelPackageID: ex.ModelPackageID,
			InputTokens: ex.InputTokens, OutputTokens: ex.OutputTokens,
			At: ex.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sess.SessionID,
		"decisions":  rows,
	})
}

// handleGetSessionReplay reconstructs the session's governed event
// sequence for replay/fork (B6): spans + actions + change sets in one
// ordered timeline.
func (s *Server) handleGetSessionReplay(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	type timelineEvent struct {
		At      string      `json:"at"`
		Kind    string      `json:"kind"`
		Payload interface{} `json:"payload"`
	}
	var events []timelineEvent
	var spans []models.ProvenanceSpan
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at ASC").Find(&spans)
	for _, sp := range spans {
		events = append(events, timelineEvent{At: sp.CreatedAt.Format(time.RFC3339), Kind: "span", Payload: sp})
	}
	var actions []models.ActionEnvelope
	s.db.Where("session_id = ?", sess.SessionID).Order("occurred_at ASC").Find(&actions)
	for _, a := range actions {
		events = append(events, timelineEvent{At: a.OccurredAt, Kind: "action", Payload: a})
	}
	var changeSets []models.ChangeSet
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at ASC").Find(&changeSets)
	for _, cs := range changeSets {
		events = append(events, timelineEvent{At: cs.CreatedAt.Format(time.RFC3339), Kind: "changeset", Payload: cs})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At < events[j].At })
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session":     sess,
		"replayable":  true,
		"baseline_id": sess.BaselineID,
		"events":      events,
	})
}

// handleBulkSessions applies close/pause/terminate to many sessions (UX7).
func (s *Server) handleBulkSessions(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"` // close, pause, terminate
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids[] + action required")
		return
	}
	if len(req.IDs) > 500 {
		writeError(w, http.StatusBadRequest, "한 번에 최대 500개 · max 500 ids per bulk call")
		return
	}
	target := ""
	switch req.Action {
	case "close":
		target = "closed"
	case "pause":
		target = "paused"
	case "terminate":
		target = "terminated"
	default:
		writeError(w, http.StatusBadRequest, "action must be close|pause|terminate")
		return
	}
	var affected int64
	for _, id := range req.IDs {
		res := s.db.Model(&models.Session{}).
			Where("(id = ? OR session_id = ?) AND organization_id = ?", id, id, orgID).
			Update("status", target)
		affected += res.RowsAffected
		// Live-path propagation (B3): the relay enforces session status
		// on the next exchange; the realtime fan-out notifies watchers.
		s.ext().Realtime.NotifySessionUpdate(orgID, id, target)
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.sessions.bulk_" + req.Action, ActorType: "admin",
		Action: "bulk_" + req.Action, ResourceType: "session", ResourceID: "bulk",
		Details:    fmt.Sprintf(`{"ids":%d,"action":"%s"}`, len(req.IDs), req.Action),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"affected": affected})
}

// handleGetSessionVisibility returns the admin's visibility level for the
// session (B8): derived from the operator's scope vs the session's org —
// org admins get level A (full transcript), others level C (metadata).
func (s *Server) handleGetSessionVisibility(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	level := "C"
	labels := map[string]string{"A": "전체 열람 (전체 대화/코드)", "B": "부분 열람", "C": "메타데이터만", "D": "접근 제한"}
	role := getRole(r)
	if sess.OrganizationID == getOrgID(r) && (role == "admin" || role == "owner") {
		level = "A"
	} else if sess.OrganizationID == getOrgID(r) {
		level = "B"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sess.SessionID,
		"level":      level,
		"label":      labels[level],
	})
}
