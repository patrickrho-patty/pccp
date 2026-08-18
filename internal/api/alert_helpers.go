package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/security"
	"gorm.io/gorm"
)

const alertTestCooldown = time.Minute

var (
	errAlertTestRateLimited = errors.New("alert test rate limited")
	errAlertEndpointChanged = errors.New("alert endpoint changed")
)

func isAcceptableAlertTarget(providerType, raw string) bool {
	return security.ValidateAlertTarget(providerType, raw) == nil
}

func (s *Server) finishAlertTest(r *http.Request, ep models.AlertEndpoint, at time.Time, status, result string, details map[string]interface{}) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&models.AlertEndpoint{}).
			Where("id = ? AND organization_id = ? AND target = ? AND target_enc = ? AND target_kek_id = ? AND target_binding_version = ? AND credential_id = ?",
				ep.ID, ep.OrganizationID, ep.Target, ep.TargetEnc, ep.TargetKEKID, ep.TargetBindingVersion, ep.CredentialID).
			Updates(map[string]interface{}{"last_test_at": at.UTC(), "last_test_status": status})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errAlertEndpointChanged
		}
		return s.auditAlertActionDB(tx, r, AlertActionTest, ep.ID, result, details)
	})
}

func normalizeAlertSeverities(values []string) ([]string, bool) {
	if values == nil {
		return []string{}, true
	}
	allowed := map[string]bool{"info": true, "low": true, "medium": true, "high": true, "critical": true}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if !allowed[value] {
			return nil, false
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out, true
}

func applyAlertRotation(tx *gorm.DB, ep models.AlertEndpoint, enc, kekID, credentialID string, bindingVersion int, rotatedAt time.Time, enable *bool) (bool, error) {
	values := map[string]interface{}{
		"target": "", "target_enc": enc, "target_kek_id": kekID,
		"target_binding_version": bindingVersion, "credential_id": credentialID,
		"rotation_required": false, "last_rotated_at": rotatedAt.UTC(),
	}
	if enable != nil {
		values["enabled"] = *enable
	}
	result := tx.Model(&models.AlertEndpoint{}).
		Where("id = ? AND organization_id = ? AND target = ? AND target_enc = ? AND target_kek_id = ? AND target_binding_version = ? AND credential_id = ? AND (last_test_status IS NULL OR last_test_status <> ? OR last_test_at IS NULL OR last_test_at <= ?)",
			ep.ID, ep.OrganizationID, ep.Target, ep.TargetEnc, ep.TargetKEKID, ep.TargetBindingVersion, ep.CredentialID, "pending", rotatedAt.UTC().Add(-alertTestCooldown)).
		Updates(values)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return false, errAlertEndpointChanged
	}
	var current struct{ Enabled bool }
	if err := tx.Model(&models.AlertEndpoint{}).Select("enabled").Where("id = ? AND organization_id = ?", ep.ID, ep.OrganizationID).Scan(&current).Error; err != nil {
		return false, err
	}
	return current.Enabled, nil
}

// reserveAlertTest performs the rate-limit decision atomically in the shared
// database, so concurrent API replicas cannot each issue a test delivery.
func (s *Server) reserveAlertTest(r *http.Request, ep models.AlertEndpoint, now time.Time) error {
	now = now.UTC()
	return s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.AlertEndpoint{}).
			Where("id = ? AND organization_id = ? AND target = ? AND target_enc = ? AND target_kek_id = ? AND target_binding_version = ? AND credential_id = ? AND (last_test_at IS NULL OR last_test_at <= ?)",
				ep.ID, ep.OrganizationID, ep.Target, ep.TargetEnc, ep.TargetKEKID, ep.TargetBindingVersion, ep.CredentialID, now.Add(-alertTestCooldown)).
			Updates(map[string]interface{}{"last_test_at": now, "last_test_status": "pending"})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var current models.AlertEndpoint
			if err := tx.Select("target, target_enc, target_kek_id, target_binding_version, credential_id").
				Where("id = ? AND organization_id = ?", ep.ID, ep.OrganizationID).First(&current).Error; err != nil {
				return errAlertEndpointChanged
			}
			if current.Target != ep.Target || current.TargetEnc != ep.TargetEnc || current.TargetKEKID != ep.TargetKEKID ||
				current.TargetBindingVersion != ep.TargetBindingVersion || current.CredentialID != ep.CredentialID {
				return errAlertEndpointChanged
			}
			return errAlertTestRateLimited
		}
		return s.auditAlertActionDB(tx, r, AlertActionTest, ep.ID, "started", map[string]interface{}{
			"reason_code": "test_reserved", "credential_id": ep.CredentialID,
		})
	})
}
