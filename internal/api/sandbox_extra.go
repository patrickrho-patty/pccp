package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// sandbox_extra.go: web/15 — image allowlist (D). The allowlist is an
// org setting (JSON array of image refs); CreateSandbox enforces it
// fail-closed when configured.

const sandboxImageAllowlistKey = "sandbox.image_allowlist"

// handleSandboxImageAllowlist GETs/replaces the org's image allowlist.
func (s *Server) handleSandboxImageAllowlist(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"images":   s.sandboxImageAllowlist(orgID),
			"enforced": len(s.sandboxImageAllowlist(orgID)) > 0,
		})
	case http.MethodPut:
		var req struct {
			Images []string `json:"images"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		cleaned := make([]string, 0, len(req.Images))
		for _, img := range req.Images {
			if img = strings.TrimSpace(img); img != "" {
				cleaned = append(cleaned, img)
			}
		}
		raw, _ := json.Marshal(cleaned)
		// Upsert the org setting.
		var existing models.OrgSetting
		err := s.db.Where("organization_id = ? AND key = ?", orgID, sandboxImageAllowlistKey).First(&existing).Error
		if err != nil {
			s.db.Create(&models.OrgSetting{
				Base:           models.Base{ID: models.GenerateID("os")},
				OrganizationID: orgID,
				Key:            sandboxImageAllowlistKey,
				Value:          string(raw),
			})
		} else {
			s.db.Model(&existing).Update("value", string(raw))
		}
		s.db.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.sandbox.allowlist_updated", ActorType: "admin",
			Action: "update_image_allowlist", ResourceType: "organization", ResourceID: orgID,
			Details:    string(raw),
			Result:     "success",
			OccurredAt: time.Now().Format(time.RFC3339),
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "allowlist_replaced", "images": cleaned})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// sandboxImageAllowlist returns the org's configured allowlist.
func (s *Server) sandboxImageAllowlist(orgID string) []string {
	var setting models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key = ?", orgID, sandboxImageAllowlistKey).
		First(&setting).Error; err != nil {
		return nil
	}
	var images []string
	_ = json.Unmarshal([]byte(setting.Value), &images)
	return images
}
