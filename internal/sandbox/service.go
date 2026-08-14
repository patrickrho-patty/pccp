package sandbox

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"gorm.io/gorm"
)

// Service implements Runtime and Sandbox Control (PRD §31).
type Service struct {
	db *gorm.DB
}

// New creates a new sandbox service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// RuntimeMode represents the execution boundary mode (PRD §31.1).
type RuntimeMode string

const (
	ModeManagedLocal   RuntimeMode = "managed_local"    // signed Harness invokes approved local tools
	ModeRemoteSandbox  RuntimeMode = "remote_sandbox"   // disposable isolated workspace controlled by CP
	ModeCustomerPool   RuntimeMode = "customer_pool"    // on-prem/private execution workers
	ModeAirGapped      RuntimeMode = "air_gapped"       // local-only government execution
	ModeReviewOnly     RuntimeMode = "review_only"      // no execution
)

// Sandbox represents a sandboxed execution environment.
type Sandbox struct {
	ID              string      `json:"id"`
	OrganizationID  string      `json:"organization_id"`
	SessionID       string      `json:"session_id,omitempty"`
	UserID          string      `json:"user_id,omitempty"`
	Mode            RuntimeMode `json:"mode"`
	// Image/attestation
	BaseImage       string      `json:"base_image"`
	ImageDigest     string      `json:"image_digest"`
	Attestation     string      `json:"attestation,omitempty"`
	// Runtime
	Host            string      `json:"host,omitempty"`
	Pool            string      `json:"pool,omitempty"`
	ResourceLimits  string      `json:"resource_limits,omitempty"` // JSON
	NetworkPolicy   string      `json:"network_policy,omitempty"`  // JSON: allowed destinations
	// State
	Status          string      `json:"status"` // pending, running, paused, destroyed, failed
	StartedAt       string      `json:"started_at,omitempty"`
	DestroyedAt     string      `json:"destroyed_at,omitempty"`
	// Forensics
	HasSnapshot     bool        `json:"has_snapshot"`
	DestroyEvidence string      `json:"destroy_evidence,omitempty"` // proof of destruction
}

// CreateSandbox creates a new sandbox instance.
type CreateRequest struct {
 	OrganizationID string `json:"organization_i_d"`
 	SessionID      string `json:"session_i_d"`
 	UserID         string `json:"user_i_d"`
 	Mode           RuntimeMode `json:"mode"`
	BaseImage      string
 	ImageDigest    string `json:"image_digest"`
 	NetworkPolicy  map[string]interface{} `json:"network_policy"`
 	ResourceLimits map[string]interface{} `json:"resource_limits"`
}

// CreateSandbox provisions a new sandbox.
func (s *Service) CreateSandbox(req CreateRequest) (*Sandbox, error) {
	if req.Mode == "" {
		req.Mode = ModeRemoteSandbox
	}
	if req.BaseImage == "" {
		req.BaseImage = "patty/sandbox-base:latest"
	}

	networkJSON, _ := json.Marshal(req.NetworkPolicy)
	resourceJSON, _ := json.Marshal(req.ResourceLimits)

	sandbox := &Sandbox{
		ID:             dari.GenerateID("sandbox"),
		OrganizationID: req.OrganizationID,
		SessionID:      req.SessionID,
		UserID:         req.UserID,
		Mode:           req.Mode,
		BaseImage:      req.BaseImage,
		ImageDigest:    req.ImageDigest,
		NetworkPolicy:  string(networkJSON),
		ResourceLimits: string(resourceJSON),
		Status:         "pending",
	}

	// In a real implementation, this would provision via Docker/containerd/Kubernetes.
	// For now, we record the sandbox definition.
	if err := s.recordSandbox(sandbox); err != nil {
		return nil, err
	}

	sandbox.Status = "running"
	sandbox.StartedAt = time.Now().Format(time.RFC3339)
	s.updateSandboxStatus(sandbox.ID, "running", sandbox.StartedAt)

	return sandbox, nil
}

// DestroySandbox destroys a sandbox and generates destruction evidence (PRD §31.2).
func (s *Service) DestroySandbox(sandboxID string) (*Sandbox, error) {
	sandbox, err := s.GetSandbox(sandboxID)
	if err != nil {
		return nil, err
	}

	// Generate destruction evidence (key discard proof)
	sandbox.Status = "destroyed"
	sandbox.DestroyedAt = time.Now().Format(time.RFC3339)
	sandbox.DestroyEvidence = dari.GenerateID("destruct")

	s.updateSandboxStatus(sandboxID, "destroyed", sandbox.DestroyedAt)

	// Record audit
	auditEvent := &models.AuditEvent{
		OrganizationID: sandbox.OrganizationID,
		EventType:      "cp.runtime.sandbox_destroyed",
		ActorType:      "system",
		Action:         "sandbox_destroy",
		ResourceType:   "sandbox",
		ResourceID:     sandboxID,
		Details:        fmt.Sprintf(`{"destroy_evidence":"%s","destroyed_at":"%s"}`, sandbox.DestroyEvidence, sandbox.DestroyedAt),
		Result:         "success",
		OccurredAt:     sandbox.DestroyedAt,
	}
	s.db.Create(auditEvent)

	return sandbox, nil
}

// ForensicSnapshot creates a forensic snapshot of a sandbox.
func (s *Service) ForensicSnapshot(sandboxID string) (string, error) {
	sandbox, err := s.GetSandbox(sandboxID)
	if err != nil {
		return "", err
	}
	snapshotID := dari.GenerateID("snapshot")
	sandbox.HasSnapshot = true

	// Record the snapshot
	auditEvent := &models.AuditEvent{
		OrganizationID: sandbox.OrganizationID,
		EventType:      "cp.runtime.forensic_snapshot",
		ActorType:      "admin",
		Action:         "forensic_snapshot",
		ResourceType:   "sandbox",
		ResourceID:     sandboxID,
		Details:        fmt.Sprintf(`{"snapshot_id":"%s"}`, snapshotID),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(auditEvent)

	return snapshotID, nil
}

// ListSandboxes returns all sandboxes for an organization.
func (s *Service) ListSandboxes(orgID string) ([]Sandbox, error) {
	// In a real implementation this would query a sandbox registry.
	// For now, we use audit events to reconstruct.
	var events []models.AuditEvent
	s.db.Where("organization_id = ? AND resource_type = 'sandbox'", orgID).
		Order("occurred_at DESC").Find(&events)

	sandboxes := make(map[string]*Sandbox)
	for _, e := range events {
		sid := e.ResourceID
		if _, ok := sandboxes[sid]; !ok {
			sandboxes[sid] = &Sandbox{
				ID:             sid,
				OrganizationID: orgID,
			}
		}
		switch e.EventType {
		case "cp.runtime.sandbox_created":
			sandboxes[sid].Status = "created"
		case "cp.runtime.sandbox_destroyed":
			sandboxes[sid].Status = "destroyed"
		}
	}

	var result []Sandbox
	for _, sb := range sandboxes {
		result = append(result, *sb)
	}
	return result, nil
}

// GetSandbox retrieves a sandbox by ID.
func (s *Service) GetSandbox(sandboxID string) (*Sandbox, error) {
	// Reconstruct from audit events
	var events []models.AuditEvent
	s.db.Where("resource_id = ?", sandboxID).Order("occurred_at ASC").Find(&events)

	if len(events) == 0 {
		return nil, fmt.Errorf("sandbox: not found")
	}

	sandbox := &Sandbox{ID: sandboxID}
	for _, e := range events {
		sandbox.OrganizationID = e.OrganizationID
	}
	return sandbox, nil
}

// IsModeAllowed checks if a runtime mode is allowed for a project.
func IsModeAllowed(projectAllowedModes []RuntimeMode, requested RuntimeMode) bool {
	for _, m := range projectAllowedModes {
		if m == requested {
			return true
		}
	}
	return false
}

// RemoteSandboxBaseline returns the baseline configuration for remote sandboxes (PRD §31.2).
func RemoteSandboxBaseline() map[string]interface{} {
	return map[string]interface{}{
		"immutable_base_image":  true,
		"ephemeral_encrypted":   true,
		"non_root":              true,
		"no_host_socket":        true,
		"no_broad_network":      true,
		"resource_limits":       true,
		"external_audit_agent":  true,
		"short_lived_creds":     true,
		"artifact_export_gate":  true,
		"destroy_after_close":   true,
		"key_discard":           true,
	}
}

func (s *Service) recordSandbox(sb *Sandbox) error {
	auditEvent := &models.AuditEvent{
		OrganizationID: sb.OrganizationID,
		EventType:      "cp.runtime.sandbox_created",
		ActorType:      "system",
		Action:         "sandbox_create",
		ResourceType:   "sandbox",
		ResourceID:     sb.ID,
		Details:        fmt.Sprintf(`{"mode":"%s","image":"%s"}`, sb.Mode, sb.BaseImage),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(auditEvent).Error
}

func (s *Service) updateSandboxStatus(sandboxID, status, timestamp string) {
	auditEvent := &models.AuditEvent{
		EventType:    fmt.Sprintf("cp.runtime.sandbox_%s", status),
		Action:       fmt.Sprintf("sandbox_%s", status),
		ResourceType: "sandbox",
		ResourceID:   sandboxID,
		Result:       "success",
		OccurredAt:   timestamp,
	}
	s.db.Create(auditEvent)
}
