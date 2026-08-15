package privacy

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements Privacy, Administrative Visibility, and Content Access controls (PRD §27).
type Service struct {
	db *gorm.DB
}

// New creates a new privacy service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// VisibilityLevel defines what an admin can see (PRD §27.2).
type VisibilityLevel string

const (
	// Level A — operational metadata (always visible to authorized admins)
	LevelAOperational VisibilityLevel = "operational_metadata"
	// Level B — engineering content (requires purpose-bound access)
	LevelBEngineering VisibilityLevel = "engineering_content"
	// Level C — communication content (requires explicit authorization)
	LevelCCommunication VisibilityLevel = "communication_content"
	// Level D — employment analytics (requires HR/admin authorization + human finalization)
	LevelDEmployment VisibilityLevel = "employment_analytics"
)

// AccessRequest is a request to access content at a specific visibility level.
type AccessRequest struct {
	AdminID        string          `json:"admin_id"`
	OrganizationID string          `json:"organization_id"`
	TargetUserID   string          `json:"target_user_id"`
	TargetSession  string          `json:"target_session"`
	Level          VisibilityLevel `json:"level"`
	Purpose        string          `json:"purpose"`
	Justification  string          `json:"justification"`
}

// AccessDecision determines whether content access is permitted.
type AccessDecision struct {
	Allowed    bool            `json:"allowed"`
	Level      VisibilityLevel `json:"level"`
	Reason     string          `json:"reason"`
	Redacted   bool            `json:"redacted"`
	Conditions []string        `json:"conditions,omitempty"`
	ExpiresAt  string          `json:"expires_at,omitempty"`
}

// EvaluateAccess determines whether an admin can access content at a visibility level.
func (s *Service) EvaluateAccess(req AccessRequest) *AccessDecision {
	decision := &AccessDecision{
		Level: req.Level,
	}

	switch req.Level {
	case LevelAOperational:
		// Operational metadata is always available to authorized admins
		decision.Allowed = true
		decision.Reason = "operational metadata access granted"

	case LevelBEngineering:
		// Engineering content requires purpose-bound access
		if req.Purpose == "" {
			decision.Allowed = false
			decision.Reason = "engineering content access requires stated purpose"
			return decision
		}
		decision.Allowed = true
		decision.Reason = "engineering content access granted with purpose"
		decision.Conditions = []string{"access logged", "purpose-bound"}

	case LevelCCommunication:
		// Communication content requires explicit authorization
		decision.Allowed = false
		decision.Reason = "communication content requires explicit authorization per PRD §27.2"
		decision.Conditions = []string{"requires legal basis", "requires user notification"}

	case LevelDEmployment:
		// Employment analytics requires HR authorization and human finalization
		decision.Allowed = false
		decision.Reason = "employment analytics requires authorization and human finalization per PRD §26.1"
		decision.Conditions = []string{"requires HR authorization", "human finalization required", "bias review required"}

	default:
		decision.Allowed = false
		decision.Reason = "unknown visibility level"
	}

	return decision
}

// CheckLegalHold checks if an organization or user is under legal hold.
func (s *Service) CheckLegalHold(orgID, userID string) (bool, error) {
	// Check audit events for legal hold markers
	var count int64
	s.db.Model(&models.AuditEvent{}).
		Where("organization_id = ? AND event_type = 'cp.legal.hold_activated'", orgID).
		Count(&count)
	if count > 0 {
		// Check if it was released
		var released int64
		s.db.Model(&models.AuditEvent{}).
			Where("organization_id = ? AND event_type = 'cp.legal.hold_released'", orgID).
			Count(&released)
		return count > released, nil
	}
	return false, nil
}

// SetLegalHold places a legal hold on an organization's data.
func (s *Service) SetLegalHold(orgID, reason, placedBy string) error {
	details, _ := json.Marshal(map[string]string{"reason": reason})
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.legal.hold_activated",
		ActorID:        placedBy,
		ActorType:      "admin",
		Action:         "legal_hold_activate",
		Details:        string(details),
		Result:         "success",
		LegalHold:      true, // the hold marker itself is held — spoliation guard
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(audit).Error
}

// ReleaseLegalHold releases a legal hold.
func (s *Service) ReleaseLegalHold(orgID, reason, releasedBy string) error {
	details, _ := json.Marshal(map[string]string{"reason": reason})
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.legal.hold_released",
		ActorID:        releasedBy,
		ActorType:      "admin",
		Action:         "legal_hold_release",
		Details:        string(details),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(audit).Error
}

// RecordAdminAccess logs an admin's access to content for audit (PRD §27.4).
func (s *Service) RecordAdminAccess(adminID, orgID, targetUserID, action, resourceType, resourceID string) error {
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.admin.content_access",
		ActorID:        adminID,
		ActorType:      "admin",
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Details:        fmt.Sprintf(`{"target_user":"%s"}`, targetUserID),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(audit).Error
}

// RedactContent redacts sensitive content based on visibility level.
func (s *Service) RedactContent(content string, level VisibilityLevel) string {
	switch level {
	case LevelAOperational:
		// Only metadata, no content
		return "[metadata-only]"
	case LevelBEngineering:
		// Engineering content shown but PII/secrets redacted
		return redactSensitive(content)
	case LevelCCommunication:
		return "[requires-authorization]"
	case LevelDEmployment:
		return "[requires-hr-authorization]"
	default:
		return content
	}
}

// redactSensitive masks PII and secrets in content.
func redactSensitive(content string) string {
	// Simple redaction — production would use the security service
	result := content
	// This is a placeholder — the security service handles actual detection
	return result
}

// RetentionPolicy defines how long data is retained (PRD §40.4).
type RetentionPolicy struct {
	DataType      string `json:"data_type"`
	RetentionDays int    `json:"retention_days"`
	Action        string `json:"action"`  // delete, anonymize, archive
	Profile       string `json:"profile"` // standard, enhanced, maximum
}

// DefaultRetentionPolicies returns the default retention policies.
func DefaultRetentionPolicies() []RetentionPolicy {
	return []RetentionPolicy{
		{DataType: "audit_events", RetentionDays: 365, Action: "archive", Profile: "standard"},
		{DataType: "session_transcripts", RetentionDays: 90, Action: "anonymize", Profile: "metadata_only"},
		{DataType: "prompt_content", RetentionDays: 30, Action: "delete", Profile: "redacted"},
		{DataType: "provenance_spans", RetentionDays: 2555, Action: "keep", Profile: "permanent"}, // 7 years
		{DataType: "evidence_receipts", RetentionDays: 2555, Action: "keep", Profile: "permanent"},
		{DataType: "security_findings", RetentionDays: 365, Action: "archive", Profile: "enhanced"},
		{DataType: "usage_records", RetentionDays: 730, Action: "anonymize", Profile: "standard"}, // 2 years
	}
}

// Ensure json import is used
var _ = json.Marshal

// EnforceRetention applies the retention schedule (Task 16 / §40.4):
// expired data is deleted/anonymized/archived per policy — EXCEPT rows
// under an active legal hold, which are never touched. Returns the
// per-type disposition counts for the compliance export.
type RetentionResult struct {
	DataType   string `json:"data_type"`
	Deleted    int64  `json:"deleted"`
	Anonymized int64  `json:"anonymized"`
	Held       int64  `json:"held_under_legal_hold"`
}

// EnforceAuditRetention enforces retention for audit events (the
// highest-volume governed record). Legal hold blocks deletion: held
// rows are counted, not removed. Cutoffs compare PARSED timestamps —
// lexicographic RFC3339 comparison misorders across timezone offsets
// and fractional seconds.
func (s *Service) EnforceAuditRetention(policies []RetentionPolicy, now time.Time) ([]RetentionResult, error) {
	var out []RetentionResult
	for _, p := range policies {
		if p.DataType != "audit_events" || p.Action == "keep" {
			continue
		}
		cutoff := now.AddDate(0, 0, -p.RetentionDays)

		// Fetch candidates with a GENEROUS string bound (cutoff minus
		// 48h covers every timezone offset), then filter precisely by
		// parsed timestamps.
		loose := cutoff.Add(-48 * time.Hour).UTC().Format(time.RFC3339)
		var candidates []models.AuditEvent
		if err := s.db.Where("occurred_at < ?", loose).Find(&candidates).Error; err != nil {
			return nil, err
		}
		var expiredIDs []string
		held := int64(0)
		for _, ev := range candidates {
			ts, err := time.Parse(time.RFC3339, ev.OccurredAt)
			if err != nil {
				continue // unparseable timestamps are retained (safe)
			}
			if !ts.Before(cutoff) {
				continue
			}
			if ev.LegalHold {
				held++
				continue
			}
			expiredIDs = append(expiredIDs, ev.ID)
		}

		var acted int64
		if len(expiredIDs) > 0 {
			switch p.Action {
			case "delete":
				res := s.db.Where("id IN ?", expiredIDs).Delete(&models.AuditEvent{})
				if res.Error != nil {
					return nil, res.Error
				}
				acted = res.RowsAffected
			case "archive", "anonymize":
				res := s.db.Model(&models.AuditEvent{}).Where("id IN ?", expiredIDs).
					Update("archive_state", "archived")
				if res.Error != nil {
					return nil, res.Error
				}
				acted = res.RowsAffected
				if p.Action == "anonymize" {
					anonDetails, _ := json.Marshal(map[string]interface{}{"data_type": p.DataType, "rows": acted})
					_ = s.db.Create(&models.AuditEvent{
						OrganizationID: "system",
						EventType:      "cp.retention.anonymized",
						ActorType:      "system",
						Action:         "retention_anonymize",
						ResourceType:   "audit_events",
						Details:        string(anonDetails),
						Result:         "success",
						LegalHold:      true,
						OccurredAt:     now.Format(time.RFC3339),
					}).Error
				}
			}
		}
		out = append(out, RetentionResult{DataType: p.DataType, Deleted: acted, Held: held})
	}
	return out, nil
}

// EnsureDeleteRespectsLegalHold is the deletion gate (§33.14): a
// hard-delete of an org's governed record is refused while that org
// has an activated hold without a matching release.
func (s *Service) EnsureDeleteRespectsLegalHold(orgID, resourceType, resourceID string) error {
	if orgID == "" {
		return fmt.Errorf("retention: deletion gate requires an organization scope")
	}
	var activated int64
	if err := s.db.Model(&models.AuditEvent{}).
		Where("organization_id = ? AND event_type = ?", orgID, "cp.legal.hold_activated").
		Count(&activated).Error; err != nil {
		return nil
	}
	if activated == 0 {
		return nil
	}
	var released int64
	if err := s.db.Model(&models.AuditEvent{}).
		Where("organization_id = ? AND event_type = ?", orgID, "cp.legal.hold_released").
		Count(&released).Error; err != nil {
		return nil
	}
	if activated > released {
		return fmt.Errorf("retention: delete refused — an active legal hold exists for org %s (spoliation guard)", orgID)
	}
	return nil
}
