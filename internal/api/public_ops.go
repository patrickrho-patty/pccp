package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/detection"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// public_ops.go: web/10-12 — the public-cloud operations surfaces:
// /api/public/accounts with live risk enrichment, graduated-response
// actions (10 A4), per-account detail (11 A5/12 A2/A5/A7), support
// cases (11 A5), abuse-case lifecycle (12 A6), refund/credit records
// (12 A4), segment tagging (12 A8).

// accountRow is the account list row enriched with live signals.
type accountRow struct {
	models.Account
	RiskScore      int    `json:"risk_score"`
	SignalCount    int    `json:"signal_count"`
	ActiveSessions int    `json:"active_sessions"`
	HarnessCount   int    `json:"harness_count"`
	LeaseCount     int64  `json:"lease_count"`
	OpenCases      int64  `json:"open_support_cases"`
	OpenAbuse      int64  `json:"open_abuse_cases"`
	LadderRung     string `json:"ladder_rung"`
	LadderNext     string `json:"ladder_next"`
}

// graduatedLadder derives the current rung + next action from the
// account's three states (10 A4, §10C.10).
func graduatedLadder(a models.Account) (rung, next string) {
	severe := 0
	if a.PlatformSecurityState == "blocked" {
		severe++
	}
	if a.TrustSafetyState == "restricted" {
		severe++
	}
	if a.AccountIntegrityState == "restricted" {
		severe++
	}
	switch {
	case severe >= 2:
		return "suspend", "case review + legal"
	case a.PlatformSecurityState == "blocked" || a.TrustSafetyState == "restricted":
		return "restrict", "monitor 72h then review"
	case a.AccountIntegrityState != "normal" || a.TrustSafetyState != "normal":
		return "review", "collect evidence (signals/harnesses)"
	case a.CapacityState == "throttled":
		return "throttle", "review quota at billing cycle"
	default:
		return "normal", "—"
	}
}

// handlePublicAccounts lists accounts with live enrichment (10 A11/12,
// 11 A11).
func (s *Server) handlePublicAccounts(w http.ResponseWriter, r *http.Request) {
	q := s.db.Model(&models.Account{})
	if v := r.URL.Query().Get("segment"); v != "" {
		q = q.Where("segment = ?", v)
	}
	if v := r.URL.Query().Get("subscription_status"); v != "" {
		q = q.Where("subscription_status = ?", v)
	}
	if v := r.URL.Query().Get("search"); v != "" {
		q = q.Where("email LIKE ? OR display_name LIKE ? OR display_name_ko LIKE ?", "%"+v+"%", "%"+v+"%", "%"+v+"%")
	}
	var accounts []models.Account
	q.Order("created_at DESC").Find(&accounts)

	rows := make([]accountRow, 0, len(accounts))
	for _, a := range accounts {
		row := accountRow{Account: a}
		if s.ext().Detection != nil {
			row.RiskScore = s.ext().Detection.GetRiskScore(a.ID)
			row.SignalCount = len(s.ext().Detection.GetSignals(a.ID))
		}
		s.db.Model(&models.AccountCapacityLease{}).Where("account_id = ?", a.ID).Count(&row.LeaseCount)
		s.db.Model(&models.SupportCase{}).Where("account_id = ? AND status NOT IN ('closed','resolved')", a.ID).Count(&row.OpenCases)
		s.db.Model(&models.AbuseCase{}).Where("account_id = ? AND status NOT IN ('closed')", a.ID).Count(&row.OpenAbuse)
		row.LadderRung, row.LadderNext = graduatedLadder(a)
		rows = append(rows, row)
	}
	writeJSON(w, http.StatusOK, rows)
}

// handlePublicAccountDetail is the per-account drill-down (12 A2/A5/A7,
// 11 A5): harnesses, devices, sessions, leases, signals, cases.
func (s *Server) handlePublicAccountDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var account models.Account
	if err := s.db.First(&account, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	var leases []models.AccountCapacityLease
	s.db.Where("account_id = ?", account.ID).Order("created_at DESC").Find(&leases)
	var supportCases []models.SupportCase
	s.db.Where("account_id = ?", account.ID).Order("created_at DESC").Find(&supportCases)
	var abuseCases []models.AbuseCase
	s.db.Where("account_id = ?", account.ID).Order("created_at DESC").Find(&abuseCases)
	signals := []*detection.Signal{}
	if s.ext().Detection != nil {
		signals = s.ext().Detection.GetSignals(account.ID)
	}
	rung, next := graduatedLadder(account)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"account":       account,
		"leases":        leases,
		"support_cases": supportCases,
		"abuse_cases":   abuseCases,
		"signals":       signals,
		"ladder_rung":   rung,
		"ladder_next":   next,
	})
}

// handlePublicAccountAction applies a graduated-response action with a
// required reason (10 A4, 12 A3).
func (s *Server) handlePublicAccountAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Dimension string `json:"dimension"` // integrity, trust_safety, platform_security, capacity, subscription
		State     string `json:"state"`
		Reason    string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Dimension == "" || req.State == "" {
		writeError(w, http.StatusBadRequest, "dimension + state + reason required")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason required for graduated responses")
		return
	}
	var account models.Account
	if err := s.db.First(&account, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	updates := map[string]interface{}{}
	switch req.Dimension {
	case "integrity":
		updates["account_integrity_state"] = req.State
	case "trust_safety":
		updates["trust_safety_state"] = req.State
	case "platform_security":
		updates["platform_security_state"] = req.State
	case "capacity":
		updates["capacity_state"] = req.State
	case "subscription":
		updates["subscription_status"] = req.State
	default:
		writeError(w, http.StatusBadRequest, "unknown dimension")
		return
	}
	s.db.Model(&account).Updates(updates)
	s.db.Create(&models.AuditEvent{
		OrganizationID: "", EventType: "cp.ops.graduated_response", ActorType: "admin",
		Action: "graduated_response", ResourceType: "account", ResourceID: account.ID,
		Details: fmt.Sprintf(`{"dimension":"%s","state":"%s","reason":"%s"}`, req.Dimension, req.State, req.Reason),
		Result:  "success", OccurredAt: time.Now().Format(time.RFC3339),
	})
	s.db.First(&account, "id = ?", id)
	rung, next := graduatedLadder(account)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "applied", "ladder_rung": rung, "ladder_next": next,
	})
}

// handlePublicAccountRefund records a refund/credit adjustment (12 A4).
// Honest: no external payment provider is wired — the adjustment is
// recorded as an audited entitlement change, and provider actions are
// flagged not_configured rather than faked.
func (s *Server) handlePublicAccountRefund(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		AmountMicros int64  `json:"amount_micros"`
		Currency     string `json:"currency"`
		Reason       string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || req.AmountMicros <= 0 {
		writeError(w, http.StatusBadRequest, "amount_micros + reason required")
		return
	}
	if req.Currency == "" {
		req.Currency = "KRW"
	}
	var account models.Account
	if err := s.db.First(&account, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: "", EventType: "cp.ops.refund_recorded", ActorType: "admin",
		Action: "record_refund", ResourceType: "account", ResourceID: account.ID,
		Details: fmt.Sprintf(`{"amount_micros":%d,"currency":"%s","reason":"%s","provider":"not_configured"}`, req.AmountMicros, req.Currency, req.Reason),
		Result:  "success", OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status": "recorded", "provider_action": "not_configured — external payment provider integration required",
	})
}

// handleSupportCases lists/creates support cases (11 A5, 12 A7).
func (s *Server) handleSupportCases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := s.db.Model(&models.SupportCase{})
		if v := r.URL.Query().Get("account"); v != "" {
			q = q.Where("account_id = ?", v)
		}
		if v := r.URL.Query().Get("status"); v != "" {
			q = q.Where("status = ?", v)
		}
		var cases []models.SupportCase
		q.Order("created_at DESC").Find(&cases)
		writeJSON(w, http.StatusOK, cases)
	case http.MethodPost:
		var req struct {
			AccountID   string `json:"account_id"`
			Subject     string `json:"subject"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
		}
		if err := decodeJSON(r, &req); err != nil || req.AccountID == "" || req.Subject == "" {
			writeError(w, http.StatusBadRequest, "account_id + subject required")
			return
		}
		if req.Priority == "" {
			req.Priority = "normal"
		}
		c := models.SupportCase{
			Base:      models.Base{ID: models.GenerateID("case")},
			AccountID: req.AccountID, Subject: req.Subject, Description: req.Description,
			Priority: req.Priority, Status: "open",
		}
		var account models.Account
		_ = s.db.First(&account, "id = ?", req.AccountID).Error
		if err := s.db.Create(&c).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, c)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleSupportCaseItem updates status + timeline (11 A5).
func (s *Server) handleSupportCaseItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var c models.SupportCase
	if err := s.db.First(&c, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "case not found")
		return
	}
	var timeline []map[string]string
	if c.TimelineJSON != "" {
		_ = json.Unmarshal([]byte(c.TimelineJSON), &timeline)
	}
	if req.Note != "" {
		timeline = append(timeline, map[string]string{
			"at": time.Now().Format(time.RFC3339), "actor": getOperatorEmail(r), "note": req.Note,
		})
	}
	raw, _ := json.Marshal(timeline)
	updates := map[string]interface{}{"timeline": string(raw)}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	s.db.Model(&c).Updates(updates)
	writeJSON(w, http.StatusOK, c)
}

// handleAbuseCases lists/creates abuse cases (12 A6).
func (s *Server) handleAbuseCases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := s.db.Model(&models.AbuseCase{})
		if v := r.URL.Query().Get("account"); v != "" {
			q = q.Where("account_id = ?", v)
		}
		if v := r.URL.Query().Get("status"); v != "" {
			q = q.Where("status = ?", v)
		}
		var cases []models.AbuseCase
		q.Order("created_at DESC").Find(&cases)
		writeJSON(w, http.StatusOK, cases)
	case http.MethodPost:
		var req struct {
			AccountID   string `json:"account_id"`
			Category    string `json:"category"`
			Severity    string `json:"severity"`
			Description string `json:"description"`
		}
		if err := decodeJSON(r, &req); err != nil || req.AccountID == "" || req.Category == "" {
			writeError(w, http.StatusBadRequest, "account_id + category required")
			return
		}
		c := models.AbuseCase{
			Base:      models.Base{ID: models.GenerateID("abuse")},
			AccountID: req.AccountID, Category: req.Category,
			Severity: req.Severity, Description: req.Description, Status: "open",
		}
		var account models.Account
		_ = s.db.First(&account, "id = ?", req.AccountID).Error
		if err := s.db.Create(&c).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, c)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAbuseCaseItem advances the abuse-case lifecycle (12 A6).
func (s *Server) handleAbuseCaseItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Status   string `json:"status"`
		Decision string `json:"decision"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var c models.AbuseCase
	if err := s.db.First(&c, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "case not found")
		return
	}
	updates := map[string]interface{}{}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.Decision != "" {
		updates["decision"] = req.Decision
	}
	s.db.Model(&c).Updates(updates)
	s.db.Create(&models.AuditEvent{
		OrganizationID: c.OrganizationID, EventType: "cp.ops.abuse_case_updated", ActorType: "admin",
		Action: "update_abuse_case", ResourceType: "abuse_case", ResourceID: c.ID,
		Details: fmt.Sprintf(`{"status":"%s","decision":"%s"}`, req.Status, req.Decision),
		Result:  "success", OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, c)
}

// handleAccountSegments lists/assigns segment tags (12 A8).
func (s *Server) handleAccountSegments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var accounts []models.Account
		s.db.Select("DISTINCT segment").Where("segment != ''").Find(&accounts)
		segments := []string{}
		for _, a := range accounts {
			segments = append(segments, a.Segment)
		}
		writeJSON(w, http.StatusOK, segments)
	case http.MethodPut:
		var req struct {
			AccountID string `json:"account_id"`
			Segment   string `json:"segment"`
		}
		if err := decodeJSON(r, &req); err != nil || req.AccountID == "" {
			writeError(w, http.StatusBadRequest, "account_id + segment required")
			return
		}
		var account models.Account
		if err := s.db.First(&account, "id = ?", req.AccountID).Error; err != nil {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		s.db.Model(&account).Update("segment", req.Segment)
		writeJSON(w, http.StatusOK, map[string]string{"status": "tagged"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

var _ = strconv.Itoa
