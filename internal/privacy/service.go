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
 	AdminID        string `json:"admin_i_d"`
 	OrganizationID string `json:"organization_i_d"`
 	TargetUserID   string `json:"target_user_i_d"`
 	TargetSession  string `json:"target_session"`
 	Level          VisibilityLevel `json:"level"`
 	Purpose        string `json:"purpose"`
 	Justification  string `json:"justification"`
}

// AccessDecision determines whether content access is permitted.
type AccessDecision struct {
	Allowed     bool   `json:"allowed"`
	Level       VisibilityLevel `json:"level"`
	Reason      string `json:"reason"`
	Redacted    bool   `json:"redacted"`
	Conditions  []string `json:"conditions,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
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
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.legal.hold_activated",
		ActorID:        placedBy,
		ActorType:      "admin",
		Action:         "legal_hold_activate",
		Details:        fmt.Sprintf(`{"reason":"%s"}`, reason),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(audit).Error
}

// ReleaseLegalHold releases a legal hold.
func (s *Service) ReleaseLegalHold(orgID, reason, releasedBy string) error {
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.legal.hold_released",
		ActorID:        releasedBy,
		ActorType:      "admin",
		Action:         "legal_hold_release",
		Details:        fmt.Sprintf(`{"reason":"%s"}`, reason),
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
	DataType       string `json:"data_type"`
	RetentionDays  int    `json:"retention_days"`
	Action         string `json:"action"` // delete, anonymize, archive
	Profile        string `json:"profile"` // standard, enhanced, maximum
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
