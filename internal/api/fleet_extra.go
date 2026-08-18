package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/fleet"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// fleet_extra.go: web/09 — bulk actions (A5), change freeze (A3),
// force-version (A4), per-harness action history (A6), forensic
// snapshot bundle (A7), approvals queue (A9), impact preview + scoped
// lockdown (A11), server-side inventory query + live status (A12).

// handleFleetBulkAction applies one action to a filtered selection (A5).
var fleetBulkConcurrency = make(chan struct{}, 16)

func (s *Server) handleFleetBulkAction(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
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
	if req.Action == "" || strings.TrimSpace(req.Reason) == "" || len(req.TargetIDs) == 0 {
		writeError(w, http.StatusBadRequest, "action + reason + harness_ids[] required")
		return
	}
	if len(req.TargetIDs) > 32 {
		writeError(w, http.StatusBadRequest, "max 32 harnesses per synchronous bulk action")
		return
	}
	action := fleet.FleetAction(req.Action)
	if !fleet.IsBulkHarnessScopedAction(action) {
		writeError(w, http.StatusBadRequest, "unsupported harness-scoped action; use the security lockdown workflow for organization containment")
		return
	}
	seen := make(map[string]struct{}, len(req.TargetIDs))
	deduplicated := make([]string, 0, len(req.TargetIDs))
	for _, id := range req.TargetIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		deduplicated = append(deduplicated, id)
	}
	if len(deduplicated) == 0 {
		writeError(w, http.StatusBadRequest, "at least one non-empty harness_id is required")
		return
	}
	req.TargetIDs = deduplicated
	orgID := getOrgID(r)
	req.PerformedBy = getActorID(r)
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		writeError(w, http.StatusBadRequest, "Idempotency-Key header is required and must be at most 128 characters")
		return
	}
	digestTargets := append([]string(nil), req.TargetIDs...)
	sort.Strings(digestTargets)
	digestPayload, _ := json.Marshal(map[string]interface{}{"action": req.Action, "reason": req.Reason, "harness_ids": digestTargets})
	requestDigest := fmt.Sprintf("%x", sha256.Sum256(digestPayload))
	operation := models.FleetBulkOperation{
		OrganizationID: orgID, IdempotencyKey: idempotencyKey, RequestDigest: requestDigest,
		Status: "running", LeaseExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	var reserved bool
	reservationErr := s.db.Transaction(func(tx *gorm.DB) error {
		reservation := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&operation)
		if reservation.Error != nil {
			return reservation.Error
		}
		reserved = reservation.RowsAffected == 1
		if !reserved {
			return nil
		}
		rows := make([]models.FleetBulkTargetOutcome, 0, len(req.TargetIDs))
		for _, harnessID := range req.TargetIDs {
			rows = append(rows, models.FleetBulkTargetOutcome{
				OrganizationID: orgID, OperationID: operation.ID, HarnessID: harnessID, Result: "pending",
			})
		}
		return tx.Create(&rows).Error
	})
	if reservationErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to reserve bulk operation")
		return
	}
	if !reserved {
		var existing models.FleetBulkOperation
		if err := s.db.Where("organization_id = ? AND idempotency_key = ?", orgID, idempotencyKey).First(&existing).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve bulk operation")
			return
		}
		if existing.RequestDigest != requestDigest {
			writeError(w, http.StatusConflict, "Idempotency-Key was already used for a different request")
			return
		}
		if existing.Status == "complete" && existing.ResponseJSON != "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Idempotent-Replay", "true")
			w.WriteHeader(existing.HTTPStatus)
			_, _ = w.Write([]byte(existing.ResponseJSON))
			return
		}
		leaseExpired := (!existing.LeaseExpiresAt.IsZero() && time.Now().UTC().After(existing.LeaseExpiresAt)) ||
			(existing.LeaseExpiresAt.IsZero() && time.Since(existing.UpdatedAt) > 5*time.Minute)
		if existing.Status == "running" && leaseExpired {
			responseJSON, recovered := s.recoverExpiredFleetBulkOperation(existing)
			if recovered {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Idempotent-Replay", "true")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(responseJSON))
				return
			}
		}
		writeError(w, http.StatusConflict, "bulk operation is already running")
		return
	}
	results := make([]map[string]string, len(req.TargetIDs))
	jobs := make(chan int)
	ctx := r.Context()
	var workers sync.WaitGroup
	workerCount := 8
	if len(req.TargetIDs) < workerCount {
		workerCount = len(req.TargetIDs)
	}
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				hid := req.TargetIDs[index]
				select {
				case fleetBulkConcurrency <- struct{}{}:
				case <-ctx.Done():
					results[index] = map[string]string{"harness_id": hid, "result": "failed", "error": "request cancelled before execution"}
					s.db.Model(&models.FleetBulkTargetOutcome{}).Where("operation_id = ? AND harness_id = ? AND result = ?", operation.ID, hid, "pending").Updates(map[string]interface{}{"result": "failed", "error": "request cancelled before execution"})
					continue
				}
				claimed := s.db.Model(&models.FleetBulkTargetOutcome{}).
					Where("operation_id = ? AND harness_id = ? AND result = ?", operation.ID, hid, "pending").
					Update("result", "running")
				if claimed.Error != nil || claimed.RowsAffected != 1 {
					<-fleetBulkConcurrency
					results[index] = map[string]string{"harness_id": hid, "result": "failed", "error": "durable target claim failed; action was not executed"}
					continue
				}
				err := s.fleet.PerformAction(fleet.ActionRequest{
					Context:        ctx,
					OrganizationID: orgID,
					HarnessID:      hid,
					Action:         fleet.FleetAction(req.Action),
					Reason:         req.Reason,
					PerformedBy:    req.PerformedBy,
				})
				<-fleetBulkConcurrency
				if err != nil {
					result := "failed"
					var executionErr *fleet.ActionExecutionError
					if errors.As(err, &executionErr) && (executionErr.LocalApplied || executionErr.RelayDelivered) {
						result = "partial"
					}
					results[index] = map[string]string{"harness_id": hid, "result": result, "error": err.Error()}
					s.db.Model(&models.FleetBulkTargetOutcome{}).Where("operation_id = ? AND harness_id = ? AND result = ?", operation.ID, hid, "running").Updates(map[string]interface{}{"result": result, "error": err.Error()})
					continue
				}
				results[index] = map[string]string{"harness_id": hid, "result": "executed"}
				s.db.Model(&models.FleetBulkTargetOutcome{}).Where("operation_id = ? AND harness_id = ? AND result = ?", operation.ID, hid, "running").Updates(map[string]interface{}{"result": "executed", "error": ""})
			}
		}()
	}
sendBulkJobs:
	for index := range req.TargetIDs {
		select {
		case jobs <- index:
		case <-ctx.Done():
			for pending := index; pending < len(req.TargetIDs); pending++ {
				results[pending] = map[string]string{"harness_id": req.TargetIDs[pending], "result": "failed", "error": "request cancelled before execution"}
				s.db.Model(&models.FleetBulkTargetOutcome{}).Where("operation_id = ? AND harness_id = ? AND result = ?", operation.ID, req.TargetIDs[pending], "pending").Updates(map[string]interface{}{"result": "failed", "error": "request cancelled before execution"})
			}
			break sendBulkJobs
		}
	}
	close(jobs)
	workers.Wait()
	executed, partial, failed := 0, 0, 0
	var failures []string
	var persistenceErr error
	for _, outcome := range results {
		switch outcome["result"] {
		case "failed":
			failed++
			failures = append(failures, outcome["harness_id"]+": "+outcome["error"])
		case "partial":
			partial++
			failures = append(failures, outcome["harness_id"]+": "+outcome["error"])
		default:
			executed++
		}
	}
	result := "success"
	if partial > 0 || (failed > 0 && executed > 0) {
		result = "partial"
	} else if failed > 0 {
		result = "failure"
	}
	summary, err := json.Marshal(map[string]interface{}{"reason": req.Reason, "executed": executed, "partial": partial, "failed": failed})
	if err != nil {
		persistenceErr = err
	}
	if persistenceErr == nil {
		if err := models.CreateAuditEvent(s.db, &models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.fleet.bulk_action", ActorID: req.PerformedBy, ActorType: "admin",
			Action: req.Action, ResourceType: "harness", ResourceID: "bulk",
			Details:    string(summary),
			Result:     result,
			OccurredAt: time.Now().Format(time.RFC3339),
		}); err != nil {
			persistenceErr = err
		}
	}
	response := map[string]interface{}{
		"executed": executed, "partial": partial, "failed": failed, "failures": failures, "outcomes": results,
	}
	statusCode := http.StatusOK
	if persistenceErr != nil {
		statusCode = http.StatusInternalServerError
		response["error"] = "bulk actions ran, but one or more durable audit outcomes could not be persisted"
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode bulk operation response")
		return
	}
	finalized := s.db.Model(&models.FleetBulkOperation{}).Where("id = ? AND status = ?", operation.ID, "running").Updates(map[string]interface{}{
		"status": "complete", "response_json": string(responseJSON), "http_status": statusCode,
	})
	if finalized.Error != nil || finalized.RowsAffected != 1 {
		writeError(w, http.StatusInternalServerError, "failed to finalize bulk operation")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(responseJSON)
}

func (s *Server) recoverExpiredFleetBulkOperation(operation models.FleetBulkOperation) (string, bool) {
	responseJSON := ""
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var locked models.FleetBulkOperation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND organization_id = ?", operation.ID, operation.OrganizationID).First(&locked).Error; err != nil {
			return err
		}
		leaseExpired := (!locked.LeaseExpiresAt.IsZero() && time.Now().UTC().After(locked.LeaseExpiresAt)) ||
			(locked.LeaseExpiresAt.IsZero() && time.Since(locked.UpdatedAt) > 5*time.Minute)
		if locked.Status != "running" || !leaseExpired {
			return gorm.ErrInvalidData
		}
		if err := tx.Model(&models.FleetBulkTargetOutcome{}).Where("operation_id = ? AND result = ?", locked.ID, "running").Updates(map[string]interface{}{
			"result": "indeterminate", "error": "worker lease expired after execution began; verify harness state before issuing a new action",
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.FleetBulkTargetOutcome{}).Where("operation_id = ? AND result = ?", locked.ID, "pending").Updates(map[string]interface{}{
			"result": "failed", "error": "worker lease expired before execution",
		}).Error; err != nil {
			return err
		}
		var rows []models.FleetBulkTargetOutcome
		if err := tx.Where("operation_id = ?", locked.ID).Order("harness_id ASC").Find(&rows).Error; err != nil {
			return err
		}
		outcomes := make([]map[string]string, 0, len(rows))
		executed, partial, failed := 0, 0, 0
		for _, row := range rows {
			outcomes = append(outcomes, map[string]string{"harness_id": row.HarnessID, "result": row.Result, "error": row.Error})
			switch row.Result {
			case "executed":
				executed++
			case "partial", "indeterminate":
				partial++
			default:
				failed++
			}
		}
		body, err := json.Marshal(map[string]interface{}{
			"executed": executed, "partial": partial, "failed": failed, "outcomes": outcomes,
			"error": "the prior worker lease expired; indeterminate targets were not retried automatically",
		})
		if err != nil {
			return err
		}
		responseJSON = string(body)
		updated := tx.Model(&models.FleetBulkOperation{}).Where("id = ? AND status = ?", locked.ID, "running").Updates(map[string]interface{}{
			"status": "complete", "response_json": responseJSON, "http_status": http.StatusConflict,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrInvalidData
		}
		return nil
	})
	return responseJSON, err == nil
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
	var device models.Device
	var sessions []models.Session
	var findings []models.SecurityFinding
	var approvals []models.Approval
	var trail []models.AuditEvent
	var sessionTotal, findingTotal, approvalTotal int64
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("(id = ? OR harness_id = ?) AND organization_id = ?", harnessID, harnessID, orgID).First(&harness).Error; err != nil {
			return err
		}
		if harness.DeviceID != "" {
			if err := tx.Where("id = ? AND organization_id = ?", harness.DeviceID, orgID).First(&device).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		sessionQuery := tx.Model(&models.Session{}).Where("harness_id = ? AND organization_id = ?", harness.HarnessID, orgID)
		if err := sessionQuery.Count(&sessionTotal).Error; err != nil {
			return err
		}
		if err := sessionQuery.Order("created_at DESC").Limit(500).Find(&sessions).Error; err != nil {
			return err
		}
		sessionIDs := make([]string, 0, len(sessions))
		for _, session := range sessions {
			sessionIDs = append(sessionIDs, session.SessionID)
		}
		if len(sessionIDs) > 0 {
			findingQuery := tx.Model(&models.SecurityFinding{}).Where("organization_id = ? AND session_id IN ?", orgID, sessionIDs)
			if err := findingQuery.Count(&findingTotal).Error; err != nil {
				return err
			}
			if err := findingQuery.Order("created_at DESC").Limit(1000).Find(&findings).Error; err != nil {
				return err
			}
			approvalQuery := tx.Model(&models.Approval{}).Where("organization_id = ? AND session_id IN ?", orgID, sessionIDs)
			if err := approvalQuery.Count(&approvalTotal).Error; err != nil {
				return err
			}
			if err := approvalQuery.Order("created_at DESC").Limit(1000).Find(&approvals).Error; err != nil {
				return err
			}
		}
		return tx.Where("organization_id = ? AND resource_id IN ?", orgID, []string{harness.ID, harness.HarnessID}).Order("created_at DESC").Limit(200).Find(&trail).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "harness not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	bundle := map[string]interface{}{
		"collected_at": time.Now().Format(time.RFC3339),
		"harness":      harness,
		"device":       device,
		"sessions":     sessions,
		"findings":     findings,
		"approvals":    approvals,
		"action_trail": trail,
		"limits": map[string]interface{}{
			"sessions": 500, "findings": 1000, "approvals": 1000, "action_trail": 200,
			"sessions_total": sessionTotal, "findings_total": findingTotal, "approvals_total": approvalTotal,
			"truncated": sessionTotal > int64(len(sessions)) || findingTotal > int64(len(findings)) || approvalTotal > int64(len(approvals)),
		},
	}
	shortID := harness.HarnessID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	shortID = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, shortID)
	if shortID == "" {
		shortID = "unknown"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=harness-%s-forensic.json", shortID))
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
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	orgID := getOrgID(r)
	action := r.URL.Query().Get("action")
	scope := r.URL.Query().Get("scope")
	if action == "" {
		action = "emergency_lockdown"
	}
	if action == "emergency_lockdown" {
		s.handleSecurityLockdownImpact(w, r)
		return
	}
	if scope == "" {
		scope = "org"
	}
	if scope != "org" && scope != "project" {
		writeError(w, http.StatusBadRequest, "scope must be org or project")
		return
	}
	if action != "quarantine_device" && action != "revoke_harness_certificate" {
		writeError(w, http.StatusBadRequest, "unsupported fleet action")
		return
	}
	projectID := r.URL.Query().Get("project_id")
	if scope == "project" {
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "project scope requires project_id")
			return
		}
		var projectCount int64
		if err := s.db.Model(&models.Project{}).Where("id = ? AND organization_id = ?", projectID, orgID).Count(&projectCount).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if projectCount != 1 {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
	}
	harnesses := int64(0)
	sessions := int64(0)
	nonTerminal := models.SessionNonTerminalStatuses()
	switch action {
	case "quarantine_device", "revoke_harness_certificate":
		if scope == "project" {
			writeError(w, http.StatusBadRequest, "project scope is supported only for emergency_lockdown")
			return
		}
		if err := s.db.Model(&models.Harness{}).Where("organization_id = ? AND status != 'revoked'", orgID).Count(&harnesses).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.db.Model(&models.Session{}).Where("organization_id = ? AND status IN ?", orgID, nonTerminal).Count(&sessions).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"action": action, "scope": scope, "project_id": projectID,
		"affected_harnesses": harnesses, "affected_sessions": sessions,
	})
}

// handleFleetStatus is the live-refresh heartbeat (A12): counts + max
// heartbeat age so the UI can show staleness honestly.
func (s *Server) handleFleetStatus(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var total, active, quarantined int64
	s.db.Model(&models.Harness{}).Where("organization_id = ?", orgID).Count(&total)
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status IN ?", orgID, models.HarnessPermittedStatuses()).Count(&active)
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
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	inventory, total, err := s.fleet.GetFleetInventoryPage(orgID, fleet.InventoryQuery{
		Status: r.URL.Query().Get("status"), Risk: r.URL.Query().Get("risk"),
		Version: r.URL.Query().Get("version"), HarnessID: r.URL.Query().Get("harness_id"),
		Search: r.URL.Query().Get("search"), Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": inventory, "total": total, "page": page, "size": size,
	})
}
