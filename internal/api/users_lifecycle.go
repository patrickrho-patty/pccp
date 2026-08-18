package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// users_lifecycle.go: web/01 Users plan — developer harness binding (A2),
// enrollment codes (A3), seat usage (A4), structured contractors (A5),
// offboarding workflow (B2), server-side list query (B3), developer
// entitlement (B5), per-developer usage (B6), CSV provisioning (B7),
// SSO connection status (B8).

// handleListUserHarnesses lists the developer's harnesses from the real
// bindings (Harness.AllowedUsers + Device.UserID), not via sessions (A2).
func (s *Server) handleListUserHarnesses(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	var harnesses []models.Harness
	s.db.Where("organization_id = ?", orgID).Find(&harnesses)
	var bound []models.Harness
	for _, h := range harnesses {
		allowed := parseAllowedUsers(h.AllowedUsers)
		for _, uid := range allowed {
			if uid == id {
				bound = append(bound, h)
				break
			}
		}
	}
	// Also include harnesses bound via device owner.
	if len(bound) == 0 || true {
		var byDevice []models.Harness
		s.db.Joins("JOIN devices ON devices.id = harnesses.device_id").
			Where("harnesses.organization_id = ? AND devices.user_id = ?", orgID, id).
			Find(&byDevice)
		seen := map[string]bool{}
		for _, h := range bound {
			seen[h.ID] = true
		}
		for _, h := range byDevice {
			if !seen[h.ID] {
				bound = append(bound, h)
			}
		}
	}
	writeJSON(w, http.StatusOK, bound)
}

// parseAllowedUsers decodes the JSON user-id array on a harness.
func parseAllowedUsers(raw string) []string {
	var out []string
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// handleGrantUserHarness binds a harness to a developer (A2).
func (s *Server) handleGrantUserHarness(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		HarnessID string `json:"harness_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.HarnessID == "" {
		writeError(w, http.StatusBadRequest, "harness_id required")
		return
	}
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	var harness models.Harness
	if err := s.db.First(&harness, "id = ? AND organization_id = ?", req.HarnessID, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "harness not found")
		return
	}
	allowed := parseAllowedUsers(harness.AllowedUsers)
	for _, uid := range allowed {
		if uid == id {
			writeJSON(w, http.StatusOK, harness)
			return
		}
	}
	allowed = append(allowed, id)
	raw, _ := json.Marshal(allowed)
	s.db.Model(&harness).Update("allowed_users", string(raw))
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.user.harness_granted", ActorType: "admin",
		Action: "grant_harness", ResourceType: "user", ResourceID: id,
		Details:    fmt.Sprintf(`{"harness_id":"%s"}`, req.HarnessID),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	s.db.First(&harness, "id = ? AND organization_id = ?", req.HarnessID, orgID)
	writeJSON(w, http.StatusOK, harness)
}

// handleRevokeUserHarness removes the developer binding from a harness (A2).
func (s *Server) handleRevokeUserHarness(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	harnessID := chi.URLParam(r, "harnessId")
	orgID := getOrgID(r)
	var harness models.Harness
	if err := s.db.First(&harness, "id = ? AND organization_id = ?", harnessID, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "harness not found")
		return
	}
	allowed := parseAllowedUsers(harness.AllowedUsers)
	kept := allowed[:0]
	for _, uid := range allowed {
		if uid != id {
			kept = append(kept, uid)
		}
	}
	raw, _ := json.Marshal(kept)
	s.db.Model(&harness).Update("allowed_users", string(raw))
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.user.harness_revoked", ActorType: "admin",
		Action: "revoke_harness", ResourceType: "user", ResourceID: id,
		Details:    fmt.Sprintf(`{"harness_id":"%s"}`, harnessID),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// handleGetUserUsage returns the canonical, unit-safe ledger report for one
// user. It shares aggregation and currency rules with every other scope.
func (s *Server) handleGetUserUsage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var user models.User
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&user).Error; err != nil {
		writeError(w, http.StatusNotFound, "사용자를 찾을 수 없습니다")
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	report, err := s.buildUsageReport(orgID, usageFilter{UserID: id}, fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// The canonical user lifecycle state machine lives in
// internal/models/user_lifecycle.go (PAT-1489) and is shared by every
// writer of users.status. Every surface — list rows, detail header, bulk
// actions, API validation — derives allowed actions from it.

// lifecycleGuard enforces the shared mutation rules (PAT-1489): viewers
// never mutate lifecycle state, and operators never act on their own
// account. Returns false when the request has been rejected.
func lifecycleGuard(w http.ResponseWriter, r *http.Request, user *models.User) bool {
	if getRole(r) == "viewer" {
		writeError(w, http.StatusForbidden, "viewers cannot change user lifecycle state")
		return false
	}
	if claims, ok := claimsFromCtx(r.Context()); ok && claims.Email != "" && claims.Email == user.Email {
		writeError(w, http.StatusBadRequest, "cannot change your own account lifecycle")
		return false
	}
	return true
}

// decodeRequiredReason enforces the shared mutation contract (PAT-1489):
// a well-formed body with a non-empty reason. Returns false when the
// request has been rejected.
func decodeRequiredReason(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return "", false
	}
	return req.Reason, true
}

// handleSuspendUser transitions active → suspended via a dedicated,
// audited endpoint (PAT-1489).
func (s *Server) handleSuspendUser(w http.ResponseWriter, r *http.Request) {
	s.transitionUserStatus(w, r, "suspended")
}

// handleResumeUser transitions suspended → active (재활성화) (PAT-1489).
func (s *Server) handleResumeUser(w http.ResponseWriter, r *http.Request) {
	s.transitionUserStatus(w, r, "active")
}

// transitionUserStatus applies one lifecycle move with full validation:
// org-scoped lookup, reason required, RBAC + self-action guards, and an
// atomic conditional update keyed on the CURRENT persisted state — so a
// stale page or a racing request can never force an invalid transition.
func (s *Server) transitionUserStatus(w http.ResponseWriter, r *http.Request, to string) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	reason, ok := decodeRequiredReason(w, r)
	if !ok {
		return
	}
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if !lifecycleGuard(w, r, &user) {
		return
	}
	if !models.UserTransitionAllowed(user.Status, to) {
		writeError(w, http.StatusConflict, fmt.Sprintf("invalid lifecycle transition: %s → %s", user.Status, to))
		return
	}
	from := user.Status
	eventType, action := "cp.user.suspended", "suspend_user"
	if to == "active" {
		eventType, action = "cp.user.resumed", "resume_user"
	}
	// The conditional WHERE on the prior status makes the read-validate-write
	// window race-free: a concurrent mover wins or loses atomically.
	var updated int64
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.User{}).
			Where("id = ? AND organization_id = ? AND status = ?", id, orgID, from).
			Update("status", to)
		if res.Error != nil {
			return res.Error
		}
		updated = res.RowsAffected
		if updated == 0 {
			return nil
		}
		details, mErr := json.Marshal(map[string]string{"from": from, "to": to, "reason": reason})
		if mErr != nil {
			return mErr
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: eventType, ActorType: "admin",
			Action: action, ResourceType: "user", ResourceID: id,
			Details:    string(details),
			Result:     "success",
			OccurredAt: time.Now().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lifecycle update failed")
		return
	}
	if updated == 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("invalid lifecycle transition: %s → %s (state changed concurrently)", from, to))
		return
	}
	user.Status = to
	writeJSON(w, http.StatusOK, user)
}

// handleOffboardUser runs the OffboardingCase workflow (B2): closes
// sessions, revokes harnesses, packages evidence, and confirms zero
// remaining access.
func (s *Server) handleOffboardUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	reason, ok := decodeRequiredReason(w, r)
	if !ok {
		return
	}
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if !lifecycleGuard(w, r, &user) {
		return
	}
	if !models.UserTransitionAllowed(user.Status, "offboarded") {
		writeError(w, http.StatusConflict, fmt.Sprintf("invalid lifecycle transition: %s → offboarded", user.Status))
		return
	}
	// Claim the transition atomically BEFORE any cascade, so a racing
	// offboard loses here (409) without side effects.
	res := s.db.Model(&models.User{}).
		Where("id = ? AND organization_id = ? AND status = ?", id, orgID, user.Status).
		Update("status", "offboarded")
	if res.Error != nil {
		writeError(w, http.StatusInternalServerError, "offboard update failed")
		return
	}
	if res.RowsAffected == 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("invalid lifecycle transition: %s → offboarded (state changed concurrently)", user.Status))
		return
	}

	// 1. Sessions: close active/idle/paused. RowsAffected counts exactly
	// the sessions this run closed.
	sessRes := s.db.Model(&models.Session{}).
		Where("user_id = ? AND status IN ('active','idle','paused','pending')", id).
		Update("status", "terminated")
	closedSessions := sessRes.RowsAffected

	// 2. Harnesses: remove the user from AllowedUsers; revoke if empty.
	var harnesses []models.Harness
	s.db.Where("organization_id = ?", orgID).Find(&harnesses)
	revoked := 0
	for _, h := range harnesses {
		allowed := parseAllowedUsers(h.AllowedUsers)
		var kept []string
		for _, uid := range allowed {
			if uid != id {
				kept = append(kept, uid)
			}
		}
		if len(kept) == len(allowed) {
			continue
		}
		if len(kept) == 0 {
			s.db.Model(&h).Updates(map[string]interface{}{
				"status": "revoked", "revocation_reason": "user offboarded",
			})
			revoked++
		} else {
			raw, _ := json.Marshal(kept)
			s.db.Model(&h).Update("allowed_users", string(raw))
		}
	}

	// 3. Record the offboarding date (status was claimed atomically above).
	s.db.Model(&user).Update("offboarding_date", time.Now().Format("2006-01-02"))

	// 4. Audit + evidence package.
	offboardDetails, _ := json.Marshal(map[string]interface{}{
		"reason": reason, "closed_sessions": closedSessions, "revoked_harnesses": revoked,
	})
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.user.offboarded", ActorType: "admin",
		Action: "offboard_user", ResourceType: "user", ResourceID: id,
		Details:    string(offboardDetails),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})

	// 5. Confirm access removal.
	var remaining int64
	s.db.Model(&models.Session{}).
		Where("user_id = ? AND status NOT IN ('closed','terminated')", id).Count(&remaining)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "offboarded",
		"closed_sessions":   closedSessions,
		"revoked_harnesses": revoked,
		"remaining_active":  remaining,
		"evidence": map[string]interface{}{
			"reason":           reason,
			"offboarding_date": time.Now().Format("2006-01-02"),
			"access_removed":   remaining == 0,
		},
	})
}

// handleImportUsersCSV provisions developers from CSV (B7): dry-run by
// default; apply=true commits. Idempotent on email within org.
func (s *Server) handleImportUsersCSV(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	apply := r.URL.Query().Get("apply") == "true"
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "multipart field 'file' required")
		return
	}
	defer file.Close()
	// Bounded intake: a malformed/giant upload must not balloon memory.
	const csvMaxBytes = 5 << 20 // 5 MiB
	limited := io.LimitReader(file, csvMaxBytes+1)
	reader := csv.NewReader(limited)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		writeError(w, http.StatusBadRequest, "csv parse: "+err.Error())
		return
	}
	if len(records) > 50_000 {
		writeError(w, http.StatusBadRequest, "csv row cap (50,000) exceeded")
		return
	}
	type row struct {
		Line   int    `json:"line"`
		Email  string `json:"email"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	var rows []row
	imported := 0
	for i, rec := range records {
		if i == 0 {
			continue // header
		}
		if len(rec) < 2 {
			rows = append(rows, row{Line: i + 1, Error: "missing columns"})
			continue
		}
		email := strings.TrimSpace(rec[0])
		name := strings.TrimSpace(rec[1])
		rw := row{Line: i + 1, Email: email, Name: name, Status: "dry-run"}
		if email == "" || !strings.Contains(email, "@") {
			rw.Error = "invalid email"
			rows = append(rows, rw)
			continue
		}
		var existing models.User
		if s.db.Where("email = ? AND organization_id = ?", email, orgID).First(&existing).Error == nil {
			rw.Status = "exists"
			rows = append(rows, rw)
			continue
		}
		if apply {
			if _, err := s.identity.CreateUser(orgID, email, name, "", "scim", ""); err != nil {
				rw.Error = err.Error()
			} else {
				rw.Status = "imported"
				imported++
			}
		} else {
			rw.Status = "would-import"
			imported++
		}
		rows = append(rows, rw)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"dry_run":  !apply,
		"imported": imported,
		"rows":     rows,
	})
}

// handleGetUserEntitlements returns a developer's entitlement assignments.
func (s *Server) handleGetUserEntitlements(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	assignments, roles, err := s.identity.UserEntitlements(orgID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"assignments": assignments,
		"roles":       roles,
	})
}

// handlePutUserEntitlements replaces a developer's entitlement set.
func (s *Server) handlePutUserEntitlements(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		Roles []struct {
			RoleID  string `json:"role_id"`
			Scope   string `json:"scope"`
			ScopeID string `json:"scope_id"`
		} `json:"roles"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Revoke all current assignments, then grant the requested set.
	s.db.Where("organization_id = ? AND user_id = ?", orgID, id).Delete(&models.UserRole{})
	for _, ro := range req.Roles {
		if ro.RoleID == "" {
			continue
		}
		if ro.Scope == "" {
			ro.Scope = "org"
		}
		if _, err := s.identity.AssignUserRole(orgID, id, ro.RoleID, ro.Scope, ro.ScopeID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	assignments, roles, err := s.identity.UserEntitlements(orgID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"assignments": assignments,
		"roles":       roles,
	})
}

// handleListRoles returns the org's entitlement roles (B5).
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	roles, err := s.identity.ListRoles(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

// handleUserSSOStatus surfaces SSO connection state (B8).
func (s *Server) handleUserSSOStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"auth_method":   user.AuthMethod,
		"external_id":   user.ExternalID,
		"connected":     user.ExternalID != "" && user.AuthMethod != "" && user.AuthMethod != "local",
		"last_login_at": user.LastLoginAt,
		"mfa_enrolled":  user.MFAEnrolled,
	})
}

// handleContractorProfile validates and stores the structured contractor
// record (A5). The caller PATCHes via handleUpdateUser; this endpoint is
// the typed variant.
func (s *Server) handleContractorProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	var profile identity.ContractorProfile
	if err := decodeJSON(r, &profile); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if profile.ContractStart != "" && profile.ContractEnd != "" && profile.ContractEnd < profile.ContractStart {
		writeError(w, http.StatusBadRequest, "contract_end precedes contract_start")
		return
	}
	raw, _ := json.Marshal(profile)
	user.ContractorInfo = string(raw)
	s.db.Save(&user)
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.user.contractor_updated", ActorType: "admin",
		Action: "update_contractor", ResourceType: "user", ResourceID: id,
		Details:    string(raw),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, user)
}

var _ = io.EOF
