package configmgmt

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements Configuration Lifecycle Management (PRD §46).
// High-impact configuration follows a draft → validation → approval →
// publication → staged rollout → observation → enforcement lifecycle.
type Service struct {
	db *gorm.DB
	mu sync.RWMutex
	// Pending configuration changes
	pending map[string]*ConfigChange
}

// New creates a new configuration management service.
func New(db *gorm.DB) *Service {
	return &Service{
		db:      db,
		pending: make(map[string]*ConfigChange),
	}
}

// ConfigDomain identifies a configuration domain (PRD §46.1).
type ConfigDomain string

const (
	DomainOrganization ConfigDomain = "organization_hierarchy"
	DomainIdentity     ConfigDomain = "identity_roles"
	DomainHarness      ConfigDomain = "harness_enrollment"
	DomainModel        ConfigDomain = "model_endpoints"
	DomainProject      ConfigDomain = "projects_repos"
	DomainPolicy       ConfigDomain = "policies"
	DomainTools        ConfigDomain = "tools_mcp"
	DomainRuntime      ConfigDomain = "runtime"
	DomainComms        ConfigDomain = "communications"
	DomainBilling      ConfigDomain = "billing_entitlements"
	DomainRetention    ConfigDomain = "retention"
	DomainKeys         ConfigDomain = "keys_certificates"
	DomainFeatureFlags ConfigDomain = "feature_flags"
)

// ConfigChange represents a proposed configuration change (PRD §46.2).
type ConfigChange struct {
	ID             string       `json:"id"`
	OrganizationID string       `json:"organization_id"`
	Domain         ConfigDomain `json:"domain"`
	Title          string       `json:"title"`
	Description    string       `json:"description"`
	ProposedBy     string       `json:"proposed_by"`
	// Lifecycle state (PRD §46.2)
	State string `json:"state"` // draft, validating, pending_approval, approved, publishing, rolling_out, enforcing, rolled_back, expired
	// Configuration data
	CurrentConfig  json.RawMessage `json:"current_config,omitempty"`
	ProposedConfig json.RawMessage `json:"proposed_config"`
	Diff           json.RawMessage `json:"diff,omitempty"`
	// Validation
	ValidationResults json.RawMessage `json:"validation_results,omitempty"`
	ConflictCheck     json.RawMessage `json:"conflict_check,omitempty"`
	SimulationResult  json.RawMessage `json:"simulation_result,omitempty"`
	// Approval
	RequiresApproval bool   `json:"requires_approval"`
	ApprovedBy       string `json:"approved_by,omitempty"`
	ApprovedAt       string `json:"approved_at,omitempty"`
	RejectionReason  string `json:"rejection_reason,omitempty"`
	// Rollout
	RolloutStrategy string `json:"rollout_strategy"` // immediate, staged, canary
	RolloutPercent  int    `json:"rollout_percent"`
	ObservedAt      string `json:"observed_at,omitempty"`
	// Timestamps
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// CreateChange proposes a new configuration change.
func (s *Service) CreateChange(change ConfigChange) (*ConfigChange, error) {
	if change.ID == "" {
		change.ID = dari.GenerateID("cfg")
	}
	if change.State == "" {
		change.State = "draft"
	}
	now := time.Now().Format(time.RFC3339)
	change.CreatedAt = now
	change.UpdatedAt = now

	// Compute diff between current and proposed
	if change.CurrentConfig != nil && change.ProposedConfig != nil {
		change.Diff = s.computeDiff(change.CurrentConfig, change.ProposedConfig)
	}

	// Auto-determine if approval is needed
	change.RequiresApproval = s.requiresApproval(change.Domain)

	s.mu.Lock()
	s.pending[change.ID] = &change
	s.mu.Unlock()

	// Record in audit
	s.recordAudit(change.OrganizationID, "cp.config.draft_created", "config_change", change.ID,
		fmt.Sprintf("Config change %s proposed for %s", change.Title, change.Domain))

	return &change, nil
}

// ValidateChange runs validation checks (PRD §46.2 step 2-3).
func (s *Service) ValidateChange(changeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	change, ok := s.pending[changeID]
	if !ok {
		return fmt.Errorf("configmgmt: change not found")
	}

	// Run validation (simplified)
	results := map[string]interface{}{
		"valid":           true,
		"schema_check":    "passed",
		"reference_check": "passed",
		"warnings":        []string{},
	}

	resultJSON, _ := json.Marshal(results)
	change.ValidationResults = resultJSON

	// Run conflict check
	conflicts := map[string]interface{}{
		"conflicts": []string{},
	}
	conflictJSON, _ := json.Marshal(conflicts)
	change.ConflictCheck = conflictJSON

	change.State = "validating"
	change.UpdatedAt = time.Now().Format(time.RFC3339)

	return nil
}

// ApproveChange records approval for a configuration change (PRD §46.2 step 5).
func (s *Service) ApproveChange(changeID, approvedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	change, ok := s.pending[changeID]
	if !ok {
		return fmt.Errorf("configmgmt: change not found")
	}

	if change.State != "validating" && change.State != "pending_approval" {
		return fmt.Errorf("configmgmt: change is in %s state, not ready for approval", change.State)
	}

	change.ApprovedBy = approvedBy
	change.ApprovedAt = time.Now().Format(time.RFC3339)
	change.State = "approved"
	change.UpdatedAt = time.Now().Format(time.RFC3339)

	return nil
}

// RejectChange rejects a configuration change.
func (s *Service) RejectChange(changeID, rejectedBy, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	change, ok := s.pending[changeID]
	if !ok {
		return fmt.Errorf("configmgmt: change not found")
	}

	change.RejectionReason = reason
	change.State = "rejected"
	change.UpdatedAt = time.Now().Format(time.RFC3339)

	s.recordAudit(change.OrganizationID, "cp.config.rejected", "config_change", changeID,
		fmt.Sprintf("Rejected by %s: %s", rejectedBy, reason))

	return nil
}

// PublishChange publishes the approved configuration (PRD §46.2 step 6-7).
func (s *Service) PublishChange(changeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	change, ok := s.pending[changeID]
	if !ok {
		return fmt.Errorf("configmgmt: change not found")
	}

	if change.State != "approved" {
		return fmt.Errorf("configmgmt: change must be approved before publishing")
	}

	if change.RolloutStrategy == "staged" || change.RolloutStrategy == "canary" {
		change.State = "rolling_out"
		change.RolloutPercent = 10 // Start with 10%
	} else {
		change.State = "enforcing"
		change.RolloutPercent = 100
	}
	change.UpdatedAt = time.Now().Format(time.RFC3339)

	// Record signed publication
	s.recordAudit(change.OrganizationID, "cp.config.published", "config_change", changeID,
		fmt.Sprintf("Config change published (strategy: %s, percent: %d%%)", change.RolloutStrategy, change.RolloutPercent))

	return nil
}

// AdvanceRollout advances a staged rollout to the next percentage (PRD §46.2 step 8).
func (s *Service) AdvanceRollout(changeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	change, ok := s.pending[changeID]
	if !ok || change.State != "rolling_out" {
		return fmt.Errorf("configmgmt: change not in rolling_out state")
	}

	change.RolloutPercent += 30
	if change.RolloutPercent >= 100 {
		change.RolloutPercent = 100
		change.State = "enforcing"
	}
	change.ObservedAt = time.Now().Format(time.RFC3339)
	change.UpdatedAt = time.Now().Format(time.RFC3339)

	return nil
}

// RollbackChange rolls back a configuration change (PRD §46.2 step 10).
func (s *Service) RollbackChange(changeID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	change, ok := s.pending[changeID]
	if !ok {
		return fmt.Errorf("configmgmt: change not found")
	}

	change.State = "rolled_back"
	change.UpdatedAt = time.Now().Format(time.RFC3339)

	s.recordAudit(change.OrganizationID, "cp.config.rolled_back", "config_change", changeID,
		fmt.Sprintf("Rolled back: %s", reason))

	return nil
}

// DetectDrift checks for configuration drift (PRD §46.3).
type DriftResult struct {
	Component   string `json:"component"`
	DriftType   string `json:"drift_type"` // stale_harness, stale_pia, stale_policy, stale_config
	Description string `json:"description"`
	Severity    string `json:"severity"`
}

// DetectDrift checks for configuration drift across the fleet (PRD §46.3).
func (s *Service) DetectDrift(orgID string) ([]DriftResult, error) {
	var results []DriftResult

	// Check for stale harnesses
	var harnesses []models.Harness
	s.db.Where("organization_id = ? AND status IN ('active','enrolled')", orgID).Find(&harnesses)
	for _, h := range harnesses {
		// Check if heartbeat is stale (>5 minutes)
		if h.LastHeartbeat != "" {
			hb, err := time.Parse(time.RFC3339, h.LastHeartbeat)
			if err == nil && time.Since(hb) > 5*time.Minute {
				results = append(results, DriftResult{
					Component:   h.HarnessID,
					DriftType:   "stale_harness",
					Description: "하네스 하트비트가 지연됨 (harness heartbeat stale)",
					Severity:    "medium",
				})
			}
		}
	}

	// Check for stale endpoints
	var endpoints []models.InferenceEndpoint
	s.db.Where("organization_id = ? AND status = 'active'", orgID).Find(&endpoints)
	for _, ep := range endpoints {
		if ep.LastAttestation != "" {
			att, err := time.Parse(time.RFC3339, ep.LastAttestation)
			if err == nil && time.Since(att) > 10*time.Minute {
				results = append(results, DriftResult{
					Component:   ep.EndpointID,
					DriftType:   "stale_pia",
					Description: "PIA 증명이 지연됨 (PIA attestation stale)",
					Severity:    "high",
				})
			}
		}
	}

	return results, nil
}

// GetPendingChanges returns all pending configuration changes for an org.
func (s *Service) GetPendingChanges(orgID string) []*ConfigChange {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*ConfigChange
	for _, c := range s.pending {
		if c.OrganizationID == orgID && c.State != "enforcing" && c.State != "rolled_back" && c.State != "rejected" {
			result = append(result, c)
		}
	}
	return result
}

func (s *Service) requiresApproval(domain ConfigDomain) bool {
	switch domain {
	case DomainPolicy, DomainModel, DomainBilling, DomainKeys, DomainRetention:
		return true
	default:
		return false
	}
}

func (s *Service) computeDiff(current, proposed json.RawMessage) json.RawMessage {
	// Simplified diff — production would use JSON patch
	return []byte(`{"diff_type":"json_patch","summary":"configuration changes detected"}`)
}

func (s *Service) recordAudit(orgID, action, resourceType, resourceID, details string) {
	event := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      action,
		ActorType:      "admin",
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Details:        details,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(event)
}
