package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/fleet"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// fleet_extra.go: web/09 — bulk actions (A5), change freeze (A3),
// force-version (A4), per-harness action history (A6), forensic
// snapshot bundle (A7), approvals queue (A9), impact preview + scoped
// lockdown (A11), server-side inventory query + live status (A12).

// handleFleetBulkAction applies one action to a filtered selection (A5).
func (s *Server) handleFleetBulkAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action      string   `json:"action"`
		Reason      string   `json:"reason"`
		TargetIDs   []string `json:"harness_ids"`
		PerformedBy string   `json:"performed_by"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Action == "" || req.Reason == "" || len(req.TargetIDs) == 0 {
		writeError(w, http.StatusBadRequest, "action + reason + harness_ids[] required")
		return
	}
	orgID := getOrgID(r)
	executed, failed := 0, 0
	var failures []string
	for _, hid := range req.TargetIDs {
		err := s.fleet.PerformAction(fleet.ActionRequest{
			OrganizationID: orgID,
			HarnessID:      hid,
			Action:         fleet.FleetAction(req.Action),
			Reason:         req.Reason,
			PerformedBy:    req.PerformedBy,
		})
		if err != nil {
			failed++
			failures = append(failures, hid+": "+err.Error())
			continue
		}
		executed++
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.fleet.bulk_action", ActorType: "admin",
		Action: req.Action, ResourceType: "harness", ResourceID: "bulk",
		Details:    fmt.Sprintf(`{"reason":"%s","executed":%d,"failed":%d}`, req.Reason, executed, failed),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"executed": executed, "failed": failed, "failures": failures,
	})
}

// handleFleetChangeFreeze activates a repo-scoped change freeze (A3).
func (s *Server) handleFleetChangeFreeze(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		Reason         string   `json:"reason"`
		ReasonKo       string   `json:"reason_ko"`
		AffectedRepos  []string `json:"affected_repos"`
		AllowedActions []string `json:"allowed_actions"`
		InitiatedBy    string   `json:"initiated_by"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason required")
		return
	}
	freeze, err := s.ext().Korean.InitiateChangeFreeze(orgID, req.Reason, req.ReasonKo, req.AffectedRepos, req.AllowedActions, req.InitiatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, freeze)
}

// handleFleetForceVersion blocks harnesses below a version floor (A4).
func (s *Server) handleFleetForceVersion(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		MinVersion  string `json:"min_version"`
		ReleaseRing string `json:"release_ring"`
		Deadline    string `json:"deadline"`
		Reason      string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MinVersion == "" {
		writeError(w, http.StatusBadRequest, "min_version required")
		return
	}
	if err := s.ext().Korean.SetForcedHarnessVersion(orgID, req.MinVersion, req.ReleaseRing, req.Deadline, req.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.fleet.forced_version", ActorType: "admin",
		Action: "force_harness_version", ResourceType: "organization", ResourceID: orgID,
		Details:    fmt.Sprintf(`{"min_version":"%s","reason":"%s"}`, req.MinVersion, req.Reason),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "version_floor_set", "min_version": req.MinVersion})
}

// handleFleetActionHistory lists fleet actions (all or per harness) (A6).
func (s *Server) handleFleetActionHistory(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.AuditEvent{}).
		Where("organization_id = ? AND (action LIKE 'fleet.%' OR action LIKE 'cp.fleet%' OR event_type LIKE 'cp.fleet.%' OR action IN ('revoke_harness_certificate','quarantine_device','emergency_lockdown','terminate_session','force_harness_version'))", orgID)
	if v := r.URL.Query().Get("harness_id"); v != "" {
		q = q.Where("resource_id = ?", v)
	}
	if v := r.URL.Query().Get("action"); v != "" {
		q = q.Where("action = ?", v)
	}
	var events []models.AuditEvent
	q.Order("created_at DESC").Limit(200).Find(&events)
	writeJSON(w, http.StatusOK, events)
}

// handleFleetSnapshot downloads a forensic evidence bundle for a
// harness (A7): harness + device + sessions + findings + approvals +
// action trail, as an attachment.
func (s *Server) handleFleetSnapshot(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	harnessID := chi.URLParam(r, "id")
	var harness models.Harness
	if err := s.db.Where("(id = ? OR harness_id = ?) AND organization_id = ?", harnessID, harnessID, orgID).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "harness not found")
		return
	}
	var device models.Device
	s.db.Where("id = ?", harness.DeviceID).First(&device)
	var sessions []models.Session
	s.db.Where("harness_id = ?", harness.HarnessID).Find(&sessions)
	var findings []models.SecurityFinding
	s.db.Where("session_id IN (?)", s.db.Model(&models.Session{}).Select("session_id").Where("harness_id = ?", harness.HarnessID)).Find(&findings)
	var approvals []models.Approval
	s.db.Where("session_id IN (?)", s.db.Model(&models.Session{}).Select("session_id").Where("harness_id = ?", harness.HarnessID)).Find(&approvals)
	var trail []models.AuditEvent
	s.db.Where("organization_id = ? AND resource_id = ?", orgID, harness.ID).Order("created_at DESC").Limit(100).Find(&trail)

	bundle := map[string]interface{}{
		"collected_at": time.Now().Format(time.RFC3339),
		"harness":      harness,
		"device":       device,
		"sessions":     sessions,
		"findings":     findings,
		"approvals":    approvals,
		"action_trail": trail,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=harness-%s-forensic.json", harness.HarnessID[:12]))
	writeJSON(w, http.StatusOK, bundle)
}

// handleFleetApprovals surfaces the pending approvals queue (A9).
func (s *Server) handleFleetApprovals(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.Approval{}).Where("organization_id = ?", orgID)
	if v := r.URL.Query().Get("status"); v != "" {
		q = q.Where("decision = ?", v)
	} else {
		q = q.Where("decision = 'pending'")
	}
	var approvals []models.Approval
	q.Order("created_at DESC").Limit(100).Find(&approvals)
	writeJSON(w, http.StatusOK, approvals)
}

// handleFleetImpactPreview counts what a scoped lockdown would hit (A11).
func (s *Server) handleFleetImpactPreview(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	action := r.URL.Query().Get("action")
	scope := r.URL.Query().Get("scope")
	if action == "" {
		action = "emergency_lockdown"
	}
	harnesses := int64(0)
	sessions := int64(0)
	switch action {
	case "emergency_lockdown":
		switch scope {
		case "project":
			if pid := r.URL.Query().Get("project_id"); pid != "" {
				s.db.Model(&models.Session{}).Where("organization_id = ? AND project_id = ? AND status = 'active'", orgID, pid).Count(&sessions)
				s.db.Model(&models.Harness{}).Where("organization_id = ?", orgID).Count(&harnesses)
			} else {
				s.db.Model(&models.Harness{}).Where("organization_id = ?", orgID).Count(&harnesses)
				s.db.Model(&models.Session{}).Where("organization_id = ? AND status = 'active'", orgID).Count(&sessions)
			}
		default: // org
			s.db.Model(&models.Harness{}).Where("organization_id = ?", orgID).Count(&harnesses)
			s.db.Model(&models.Session{}).Where("organization_id = ? AND status = 'active'", orgID).Count(&sessions)
		}
	case "quarantine_device", "revoke_harness_certificate":
		s.db.Model(&models.Harness{}).Where("organization_id = ? AND status != 'revoked'", orgID).Count(&harnesses)
		s.db.Model(&models.Session{}).Where("organization_id = ? AND status = 'active'", orgID).Count(&sessions)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"action": action, "scope": scope,
		"affected_harnesses": harnesses, "affected_sessions": sessions,
	})
}

// handleFleetStatus is the live-refresh heartbeat (A12): counts + max
// heartbeat age so the UI can show staleness honestly.
func (s *Server) handleFleetStatus(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var total, active, quarantined int64
	s.db.Model(&models.Harness{}).Where("organization_id = ?", orgID).Count(&total)
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status = 'enrolled'", orgID).Count(&active)
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status = 'quarantined'", orgID).Count(&quarantined)
	var stale int64
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND (last_heartbeat IS NULL OR last_heartbeat = '' OR last_heartbeat < ?)", orgID, time.Now().Add(-10*time.Minute).Format(time.RFC3339)).Count(&stale)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total": total, "active": active, "quarantined": quarantined,
		"stale_heartbeats": stale, "checked_at": time.Now().Format(time.RFC3339),
	})
}

// handleFleetInventoryQuery extends the inventory with server-side
// filters + pagination (A12). Backward-compatible: no params = full list.
func (s *Server) handleFleetInventoryQuery(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	inventory, err := s.fleet.GetFleetInventory(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Filters (A12): status, risk, version, search.
	status := r.URL.Query().Get("status")
	risk := r.URL.Query().Get("risk")
	version := r.URL.Query().Get("version")
	search := r.URL.Query().Get("search")
	filtered := inventory[:0]
	for _, item := range inventory {
		if status != "" && item.Harness.Status != status {
			continue
		}
		if risk != "" && item.Harness.RiskState != risk {
			continue
		}
		if version != "" && item.Harness.BinaryVersion != version && !(version == "stale" && (item.Harness.BinaryVersion == "" || item.Harness.LastHeartbeat == "")) {
			continue
		}
		if search != "" {
			name := item.Harness.Name + " " + item.Harness.HarnessID
			if item.User != nil {
				name += " " + item.User.Email + " " + item.User.Name
			}
			if !containsFold(name, search) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page > 0 && size > 0 {
		total := len(filtered)
		start := (page - 1) * size
		if start > total {
			start = total
		}
		end := start + size
		if end > total {
			end = total
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": filtered[start:end], "total": total, "page": page, "size": size,
		})
		return
	}
	writeJSON(w, http.StatusOK, filtered)
}

func containsFold(haystack, needle string) bool {
	return len(needle) == 0 || len(haystack) >= len(needle) && indexFold(haystack, needle) >= 0
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

var _ = json.Marshal
