package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
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
	s.db.Where("organization_id = ? AND allowed_users LIKE ?", orgID, "%\""+id+"\"%").Find(&harnesses)
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
	if _, ok := s.requireMutableUser(w, r, id); !ok {
		return
	}
	var req struct {
		HarnessID string `json:"harness_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.HarnessID == "" {
		writeError(w, http.StatusBadRequest, "harness_id required")
		return
	}
	harness, err := s.identity.GrantUserHarness(orgID, id, req.HarnessID, getActorID(r))
	if err != nil {
		writeUserAccessMutationError(w, err, "harness not found")
		return
	}
	writeJSON(w, http.StatusOK, harness)
}

// handleRevokeUserHarness removes the developer binding from a harness (A2).
func (s *Server) handleRevokeUserHarness(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	harnessID := chi.URLParam(r, "harnessId")
	orgID := getOrgID(r)
	if _, ok := s.requireMutableUser(w, r, id); !ok {
		return
	}
	if err := s.identity.RevokeUserHarness(orgID, id, harnessID, getActorID(r)); err != nil {
		writeUserAccessMutationError(w, err, "harness not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func writeUserAccessMutationError(w http.ResponseWriter, err error, notFound string) {
	switch {
	case errors.Is(err, identity.ErrUserNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		writeError(w, http.StatusNotFound, notFound)
	case errors.Is(err, identity.ErrUserReadOnly):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// handleGetUserUsage returns the canonical, unit-safe ledger report for one
// user. It shares aggregation and currency rules with every other scope.
func (s *Server) handleGetUserUsage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if !requireUsagePermission(w, r, usageReadAction(r), "user", id) {
		return
	}
	var user models.User
	if err := s.db.WithContext(r.Context()).Where("id = ? AND organization_id = ?", id, orgID).First(&user).Error; err != nil {
		writeError(w, http.StatusNotFound, "사용자를 찾을 수 없습니다")
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	report, err := s.buildUsageReport(orgID, usageFilterFromRequest(r, usageFilter{UserID: id, Projection: usageProjectionLedger}), fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeUsageReportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// The canonical user lifecycle state machine lives in
// internal/models/user_lifecycle.go (PAT-1489) and is shared by every
// writer of users.status. Every surface — list rows, detail header, bulk
// actions, API validation — derives allowed actions from it.

// canAdministerUsers is a positive role allow-list for access-changing user
// administration. Unknown, member, audit-only, and empty roles fail closed.
func canAdministerUsers(role string) bool {
	switch role {
	case "owner", "admin", "super_admin":
		return true
	default:
		return false
	}
}

func sameUserEmail(operatorEmail, targetEmail string) bool {
	return operatorEmail != "" && strings.EqualFold(strings.TrimSpace(operatorEmail), strings.TrimSpace(targetEmail))
}

func claimsIdentifyUser(claims *identity.Claims, user *models.User) bool {
	if claims == nil {
		return false
	}
	if claims.Subject != "" {
		return claims.Subject == user.ID
	}
	return sameUserEmail(claims.Email, user.Email)
}

func canManageUser(r *http.Request, user *models.User) bool {
	if !canAdministerUsers(getRole(r)) {
		return false
	}
	if claims, ok := claimsFromCtx(r.Context()); ok && claimsIdentifyUser(claims, user) {
		return false
	}
	return user.Status != models.UserStatusOffboarded
}

func userLifecycleActions(r *http.Request, user *models.User) []string {
	if !canManageUser(r, user) {
		return []string{}
	}
	return models.UserLifecycleActions(user.Status)
}

func decorateUserForRequest(r *http.Request, user models.User, lastAdministrator bool) map[string]interface{} {
	row := map[string]interface{}{}
	encoded, _ := json.Marshal(user)
	_ = json.Unmarshal(encoded, &row)
	actions := userLifecycleActions(r, &user)
	if lastAdministrator && len(actions) > 0 {
		actions = []string{}
	}
	row["allowed_actions"] = actions
	row["can_manage"] = canManageUser(r, &user)
	row["lifecycle_denial_reason"] = lifecycleDenialReason(r, &user, lastAdministrator)
	return row
}

func lifecycleDenialReason(r *http.Request, user *models.User, lastAdministrator bool) string {
	if !canAdministerUsers(getRole(r)) {
		return "insufficient_role"
	}
	if claims, ok := claimsFromCtx(r.Context()); ok && claimsIdentifyUser(claims, user) {
		return "self_action"
	}
	if user.Status == models.UserStatusOffboarded {
		return "terminal_state"
	}
	if lastAdministrator {
		return "last_administrator"
	}
	return ""
}

func (s *Server) decorateSingleUser(r *http.Request, user models.User) map[string]interface{} {
	lastAdministrators, err := identity.LastOrganizationAdministratorIDs(s.db, []string{user.OrganizationID})
	// Relationship lookup failure must hide destructive actions. The mutation
	// endpoint re-checks transactionally, but the projection must never invite
	// an action whose last-admin safety it could not establish.
	return decorateUserForRequest(r, user, err != nil || lastAdministrators[user.ID])
}

// lifecycleGuard enforces the shared mutation rules (PAT-1489): only
// organization administrators may mutate lifecycle state, and operators never
// act on their own account. Returns false when the request is rejected.
func (s *Server) lifecycleGuard(w http.ResponseWriter, r *http.Request, user *models.User, action, reason string) bool {
	if !canAdministerUsers(getRole(r)) {
		s.recordLifecycleDenied(r, user.ID, action, reason, "insufficient_role")
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return false
	}
	if claims, ok := claimsFromCtx(r.Context()); ok && claimsIdentifyUser(claims, user) {
		s.recordLifecycleDenied(r, user.ID, action, reason, "self_target")
		writeError(w, http.StatusBadRequest, "cannot change your own account lifecycle")
		return false
	}
	return true
}

func (s *Server) recordLifecycleDenied(r *http.Request, userID, action, reason, denial string) {
	details, _ := json.Marshal(map[string]string{"reason": reason, "denial": denial})
	_ = s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.user.lifecycle_denied", ActorID: getActorID(r), ActorType: "admin",
		Action: action, ResourceType: "user", ResourceID: userID, Details: string(details), Result: "failure",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	}).Error
}

func (s *Server) requireMutableUser(w http.ResponseWriter, r *http.Request, userID string) (*models.User, bool) {
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return nil, false
	}
	var user models.User
	if err := s.db.Where("id = ? AND organization_id = ?", userID, getOrgID(r)).First(&user).Error; err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return nil, false
	}
	if claims, ok := claimsFromCtx(r.Context()); ok && claimsIdentifyUser(claims, &user) {
		writeError(w, http.StatusBadRequest, "cannot change your own access relationships")
		return nil, false
	}
	if user.Status == models.UserStatusOffboarded {
		writeError(w, http.StatusConflict, "offboarded users are read-only")
		return nil, false
	}
	return &user, true
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
	action := models.UserActionSuspend
	if to == models.UserStatusActive {
		action = models.UserActionResume
	}
	reason, ok := decodeRequiredReason(w, r)
	if !ok {
		s.recordLifecycleDenied(r, id, action, "", "invalid_or_missing_reason")
		return
	}
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		s.recordLifecycleDenied(r, id, action, reason, "target_not_found")
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if !s.lifecycleGuard(w, r, &user, action, reason) {
		return
	}
	result, err := identity.TransitionUserLifecycle(s.db, identity.UserLifecycleMutation{
		OrganizationID:   orgID,
		UserID:           id,
		To:               to,
		Reason:           reason,
		ActorID:          getActorID(r),
		ActorType:        "admin",
		SessionLifecycle: s.sessionLifecycle,
	})
	if err != nil {
		if result != nil && errors.Is(err, identity.ErrLifecycleCleanup) {
			response := s.decorateSingleUser(r, result.User)
			response["closed_sessions"] = result.ClosedSessions
			response["revoked_leases"] = result.RevokedLeases
			response["cleanup_failures"] = result.SessionCleanupFailures
			response["warning"] = "lifecycle transition committed; cleanup requires retry"
			writeJSON(w, http.StatusOK, response)
			return
		}
		s.recordLifecycleDenied(r, id, action, reason, err.Error())
		writeUserLifecycleError(w, err)
		return
	}
	response := s.decorateSingleUser(r, result.User)
	response["closed_sessions"] = result.ClosedSessions
	response["revoked_leases"] = result.RevokedLeases
	writeJSON(w, http.StatusOK, response)
}

// handleOffboardUser runs the OffboardingCase workflow (B2): closes
// sessions, revokes harnesses, packages evidence, and confirms zero
// remaining access.
func (s *Server) handleOffboardUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	reason, ok := decodeRequiredReason(w, r)
	if !ok {
		s.recordLifecycleDenied(r, id, models.UserActionOffboard, "", "invalid_or_missing_reason")
		return
	}
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		s.recordLifecycleDenied(r, id, models.UserActionOffboard, reason, "target_not_found")
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if !s.lifecycleGuard(w, r, &user, models.UserActionOffboard, reason) {
		return
	}
	result, err := identity.TransitionUserLifecycle(s.db, identity.UserLifecycleMutation{
		OrganizationID:   orgID,
		UserID:           id,
		To:               models.UserStatusOffboarded,
		Reason:           reason,
		ActorID:          getActorID(r),
		ActorType:        "admin",
		SessionLifecycle: s.sessionLifecycle,
	})
	if err != nil {
		if result != nil && errors.Is(err, identity.ErrLifecycleCleanup) {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"status": result.User.Status, "closed_sessions": result.ClosedSessions,
				"revoked_leases": result.RevokedLeases, "cleanup_failures": result.SessionCleanupFailures,
				"warning": "offboarding committed; cleanup requires retry",
			})
			return
		}
		s.recordLifecycleDenied(r, id, models.UserActionOffboard, reason, err.Error())
		writeUserLifecycleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":                      result.User.Status,
		"closed_sessions":             result.ClosedSessions,
		"revoked_harnesses":           result.RevokedHarnesses,
		"revoked_leases":              result.RevokedLeases,
		"revoked_devices":             result.RevokedDevices,
		"revoked_enrollments":         result.RevokedEnrollments,
		"revoked_entitlements":        result.RevokedEntitlements,
		"revoked_project_memberships": result.RevokedProjectMemberships,
		"revoked_console_access":      result.RevokedConsoleAccess,
		"remaining_active":            result.RemainingAccess,
		"evidence": map[string]interface{}{
			"reason":           reason,
			"offboarding_date": result.User.OffboardingDate,
			"access_removed":   result.RemainingAccess == 0,
		},
	})
}

func writeUserLifecycleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrLifecycleUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, identity.ErrLifecycleInvalid), errors.Is(err, identity.ErrLifecycleStateChanged),
		errors.Is(err, identity.ErrLifecycleLastAdmin), errors.Is(err, identity.ErrLifecycleAccessRemain):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "lifecycle update failed")
	}
}

// handleImportUsersCSV provisions developers from CSV (B7): dry-run by
// default; apply=true commits. Idempotent on email within org.
func (s *Server) handleImportUsersCSV(w http.ResponseWriter, r *http.Request) {
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
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
	if _, ok := s.requireMutableUser(w, r, id); !ok {
		return
	}
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
	requested := make([]identity.RoleAssignment, 0, len(req.Roles))
	for _, ro := range req.Roles {
		if ro.RoleID == "" {
			writeError(w, http.StatusBadRequest, "role_id is required")
			return
		}
		if ro.Scope == "" {
			ro.Scope = "org"
		}
		requested = append(requested, identity.RoleAssignment{RoleID: ro.RoleID, Scope: ro.Scope, ScopeID: ro.ScopeID})
	}
	if err := s.identity.ReplaceUserRoles(orgID, id, requested, getActorID(r)); err != nil {
		if errors.Is(err, identity.ErrUserReadOnly) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
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
	user, ok := s.requireMutableUser(w, r, id)
	if !ok {
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
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := identity.LockMutableUser(tx, orgID, id); err != nil {
			return err
		}
		if err := tx.Model(&models.User{}).Where("id = ? AND organization_id = ?", id, orgID).
			Update("contractor_info", string(raw)).Error; err != nil {
			return err
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.user.contractor_updated", ActorID: getActorID(r), ActorType: "admin",
			Action: "update_contractor", ResourceType: "user", ResourceID: id,
			Details:    string(raw),
			Result:     "success",
			OccurredAt: time.Now().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		if errors.Is(err, identity.ErrUserReadOnly) {
			writeError(w, http.StatusConflict, "offboarded users are read-only")
		} else {
			writeError(w, http.StatusInternalServerError, "contractor profile update failed")
		}
		return
	}
	if err := s.db.First(user, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "contractor profile reload failed")
		return
	}
	writeJSON(w, http.StatusOK, s.decorateSingleUser(r, *user))
}

var _ = io.EOF
