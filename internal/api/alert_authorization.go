package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

var validAlertPermissions = map[string]bool{
	AlertPermissionRead: true, AlertPermissionCreate: true, AlertPermissionTest: true,
	AlertPermissionRotate: true, AlertPermissionDisable: true, AlertPermissionDelete: true,
	"security.alert_endpoint.*": true,
}

type AlertAction string

const (
	alertPermissionPrefix             = "security.alert_endpoint."
	AlertActionRead       AlertAction = "read"
	AlertActionCreate     AlertAction = "create"
	AlertActionTest       AlertAction = "test"
	AlertActionRotate     AlertAction = "rotate"
	AlertActionDisable    AlertAction = "disable"
	AlertActionDelete     AlertAction = "delete"

	AlertPermissionRead    = alertPermissionPrefix + string(AlertActionRead)
	AlertPermissionCreate  = alertPermissionPrefix + string(AlertActionCreate)
	AlertPermissionTest    = alertPermissionPrefix + string(AlertActionTest)
	AlertPermissionRotate  = alertPermissionPrefix + string(AlertActionRotate)
	AlertPermissionDisable = alertPermissionPrefix + string(AlertActionDisable)
	AlertPermissionDelete  = alertPermissionPrefix + string(AlertActionDelete)
)

func (action AlertAction) Permission() string {
	return alertPermissionPrefix + string(action)
}

func hasAlertPermission(r *http.Request, permission string) bool {
	claims, ok := claimsFromCtx(r.Context())
	if !ok {
		return false
	}
	switch claims.Role {
	case "admin", "owner", "super_admin", "security_admin":
		return true
	case "viewer", "auditor", "security_viewer":
		if permission == AlertPermissionRead {
			return true
		}
	}
	for _, grant := range claims.Permissions {
		if grant == permission || grant == "security.alert_endpoint.*" {
			return true
		}
	}
	return false
}

func (s *Server) requireAlertPermission(w http.ResponseWriter, r *http.Request, action AlertAction, resourceID string) bool {
	permission := action.Permission()
	if hasAlertPermission(r, permission) {
		return true
	}
	if err := s.auditAlertAction(r, action, resourceID, "denied", map[string]interface{}{
		"reason_code": "authorization_denied",
		"permission":  permission,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "could not record authorization decision")
		return false
	}
	writeError(w, http.StatusForbidden, "insufficient permission")
	return false
}

func (s *Server) auditAlertAction(r *http.Request, action AlertAction, resourceID, result string, details map[string]interface{}) error {
	return s.auditAlertActionDB(s.db, r, action, resourceID, result, details)
}

func (s *Server) recordAlertAuditOrFail(w http.ResponseWriter, r *http.Request, action AlertAction, resourceID, result string, details map[string]interface{}) bool {
	if err := s.auditAlertAction(r, action, resourceID, result, details); err != nil {
		writeError(w, http.StatusInternalServerError, "could not record alert endpoint audit event")
		return false
	}
	return true
}

func (s *Server) auditAlertActionDB(db *gorm.DB, r *http.Request, action AlertAction, resourceID, result string, details map[string]interface{}) error {
	clean := make(map[string]interface{}, len(details))
	for key, value := range details {
		// Keep this audit boundary allowlisted. A future caller cannot add a
		// target/error/URL field and accidentally persist credential material.
		switch key {
		case "credential_id", "old_credential_id", "new_credential_id", "type", "count", "reason_code", "permission", "status_class", "http_status", "provider_revocation_required", "enabled", "rotation_required":
			clean[key] = value
		}
	}
	encoded, _ := json.Marshal(clean)
	return db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r),
		ActorID:        getActorID(r),
		ActorType:      "user",
		EventType:      "security.alert_endpoint." + strings.TrimSpace(string(action)),
		Action:         string(action),
		ResourceType:   "alert_endpoint",
		ResourceID:     resourceID,
		Result:         result,
		Details:        string(encoded),
		OccurredAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}).Error
}

func (s *Server) handlePutAlertOperatorPermissions(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromCtx(r.Context())
	if !ok || (claims.Role != "owner" && claims.Role != "super_admin") {
		writeError(w, http.StatusForbidden, "owner permission is required")
		return
	}
	var req struct {
		Email       string   `json:"email"`
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
	}
	if decodeJSON(r, &req) != nil || strings.TrimSpace(req.Email) == "" {
		writeError(w, http.StatusBadRequest, "email and permissions are required")
		return
	}
	email := strings.TrimSpace(req.Email)
	if req.Role == "" {
		req.Role = "security_operator"
	}
	if req.Role != "security_operator" && req.Role != "security_viewer" {
		writeError(w, http.StatusBadRequest, "role must be security_operator or security_viewer")
		return
	}
	seen := map[string]bool{}
	permissions := make([]string, 0, len(req.Permissions))
	for _, permission := range req.Permissions {
		if !validAlertPermissions[permission] {
			writeError(w, http.StatusBadRequest, "unsupported alert permission")
			return
		}
		if !seen[permission] {
			seen[permission] = true
			permissions = append(permissions, permission)
		}
	}
	resourceID := keymgmt.DomainFingerprint("DARI-ALERT-OPERATOR-v1", strings.ToLower(email))
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.auth.SetPermissionsWithDB(tx, getOrgID(r), email, req.Role, permissions); err != nil {
			return err
		}
		details, _ := json.Marshal(map[string]interface{}{"grant_count": len(permissions)})
		return tx.Create(&models.AuditEvent{
			OrganizationID: getOrgID(r), ActorID: getActorID(r), ActorType: "user",
			EventType: "security.alert_operator.permissions_updated", Action: "update_alert_operator_permissions",
			ResourceType: "alert_operator", ResourceID: resourceID, Result: "success",
			Details: string(details), OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		}).Error
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update operator permissions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "updated", "permissions": permissions})
}
