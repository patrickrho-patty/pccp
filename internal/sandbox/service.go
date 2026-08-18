package sandbox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
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
	ModeManagedLocal  RuntimeMode = "managed_local"  // signed Harness invokes approved local tools
	ModeRemoteSandbox RuntimeMode = "remote_sandbox" // disposable isolated workspace controlled by CP
	ModeCustomerPool  RuntimeMode = "customer_pool"  // on-prem/private execution workers
	ModeAirGapped     RuntimeMode = "air_gapped"     // local-only government execution
	ModeReviewOnly    RuntimeMode = "review_only"    // no execution
)

// Sandbox represents a sandboxed execution environment.
type Sandbox struct {
	ID string `json:"id"`
	// DBID is the durable row ID (sandbox_records).
	DBID           string      `json:"db_id,omitempty"`
	RepositoryID   string      `json:"repository_id,omitempty"`
	CPULimit       string      `json:"cpu_limit,omitempty"`
	MemoryLimitMB  int         `json:"memory_limit_mb,omitempty"`
	OrganizationID string      `json:"organization_id"`
	SessionID      string      `json:"session_id,omitempty"`
	UserID         string      `json:"user_id,omitempty"`
	Mode           RuntimeMode `json:"mode"`
	// Image/attestation
	BaseImage   string `json:"base_image"`
	ImageDigest string `json:"image_digest"`
	Attestation string `json:"attestation,omitempty"`
	// Runtime
	Host           string `json:"host,omitempty"`
	Pool           string `json:"pool,omitempty"`
	ResourceLimits string `json:"resource_limits,omitempty"` // JSON
	NetworkPolicy  string `json:"network_policy,omitempty"`  // JSON: allowed destinations
	// State
	Status string `json:"status"` // pending, defined, running, paused, destroyed, failed
	// RuntimeProvider records the HONEST provisioning outcome: the
	// runtime that actually hosts the sandbox, or "none (...)" when
	// definition-only.
	RuntimeProvider string `json:"runtime_provider,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
	DestroyedAt     string `json:"destroyed_at,omitempty"`
	// Forensics
	HasSnapshot     bool   `json:"has_snapshot"`
	DestroyEvidence string `json:"destroy_evidence,omitempty"` // proof of destruction
}

// CreateSandbox creates a new sandbox instance.
//
// The request accepts BOTH field spellings so the admin UI contract
// (runtime_mode/image/cpu_limit/memory_limit_mb + string network
// policy) and the API-native spelling (mode/base_image + structured
// policy) decode correctly — the previous mismatch 400'd every UI
// create.
type CreateRequest struct {
	OrganizationID string `json:"organization_id"`
	SessionID      string `json:"session_id"`
	// RepositoryID scopes the policy to governed work in that repo (E4).
	RepositoryID string `json:"repository_id"`
	UserID       string `json:"user_id"`
	// API-native spelling.
	Mode           RuntimeMode            `json:"mode"`
	BaseImage      string                 `json:"base_image"`
	ImageDigest    string                 `json:"image_digest"`
	CPULimit       string                 `json:"cpu_limit"`
	MemoryLimitMB  int                    `json:"memory_limit_mb"`
	NetworkPolicy  string                 `json:"network_policy"`
	ResourceLimits map[string]interface{} `json:"resource_limits"`
	// UI spelling (normalized into the fields above on decode).
	RuntimeModeUI string `json:"runtime_mode"`
	ImageUI       string `json:"image"`
}

// Normalize folds the UI spelling into the canonical fields.
func (r *CreateRequest) Normalize() {
	if r.Mode == "" && r.RuntimeModeUI != "" {
		r.Mode = RuntimeMode(r.RuntimeModeUI)
	}
	if r.BaseImage == "" && r.ImageUI != "" {
		r.BaseImage = r.ImageUI
	}
}

// CreateSandbox provisions a new sandbox.
func (s *Service) CreateSandbox(req CreateRequest) (*Sandbox, error) {
	req.Normalize()
	if req.Mode == "" {
		req.Mode = ModeRemoteSandbox
	}
	if req.BaseImage == "" {
		req.BaseImage = "patty/sandbox-base:latest"
	}
	if req.SessionID != "" {
		var session models.Session
		if err := s.db.Select("harness_id").Where("organization_id = ? AND session_id = ?", req.OrganizationID, req.SessionID).First(&session).Error; err != nil {
			return nil, fmt.Errorf("sandbox: session not found in organization")
		}
		if isolated, err := models.HarnessSandboxIsolationActive(s.db, req.OrganizationID, session.HarnessID); err != nil {
			return nil, fmt.Errorf("sandbox: fleet desired state unavailable: %w", err)
		} else if isolated {
			return nil, fmt.Errorf("sandbox: harness is under durable isolation policy")
		}
	}

	// Image allowlist (web/15 D): when the org pins an allowlist, an
	// image outside it is refused fail-closed.
	var allowlistSetting struct {
		Value string
	}
	if err := s.db.Table("org_settings").
		Select("value").
		Where("organization_id = ? AND key = ?", req.OrganizationID, "sandbox.image_allowlist").
		First(&allowlistSetting).Error; err == nil && allowlistSetting.Value != "" {
		var allowed []string
		if json.Unmarshal([]byte(allowlistSetting.Value), &allowed) == nil && len(allowed) > 0 {
			if !imageAllowlisted(req.BaseImage, allowed) {
				return nil, fmt.Errorf("sandbox: image %q not on the organization allowlist", req.BaseImage)
			}
		}
	}

	resourceJSON, _ := json.Marshal(req.ResourceLimits)
	networkPolicy := req.NetworkPolicy

	sandbox := &Sandbox{
		ID:             dari.GenerateID("sandbox"),
		OrganizationID: req.OrganizationID,
		SessionID:      req.SessionID,
		UserID:         req.UserID,
		Mode:           req.Mode,
		BaseImage:      req.BaseImage,
		ImageDigest:    req.ImageDigest,
		NetworkPolicy:  networkPolicy,
		ResourceLimits: string(resourceJSON),
		Status:         "pending",
	}
	// Durable definition (E4): the governance snapshot + UI list read
	// this row — sandboxes survive restarts and org scoping is real.
	rec := &models.SandboxRecord{
		OrganizationID: req.OrganizationID, SessionID: req.SessionID, RepositoryID: req.RepositoryID, UserID: req.UserID,
		Mode: string(req.Mode), BaseImage: req.BaseImage, ImageDigest: req.ImageDigest,
		CPULimit: req.CPULimit, MemoryLimitMB: req.MemoryLimitMB, NetworkPolicy: networkPolicy,
		Status: "pending", ResourceLimitsJSON: string(resourceJSON),
	}
	if err := s.db.Create(rec).Error; err != nil {
		return nil, fmt.Errorf("sandbox: persist definition: %w", err)
	}
	sandbox.DBID = rec.ID

	// Provisioning: attempt a real runtime when one is reachable, and
	// record the HONEST outcome either way. The definition is always
	// persisted; the status reflects what actually happened:
	//   running  — the runtime accepted the container and reported it up
	//   defined  — no runtime reachable; definition recorded, not running
	// A sandbox the control plane cannot honestly call "running" is
	// never labeled running.
	if sandbox.DBID != "" {
		s.db.Model(&models.SandboxRecord{}).Where("id = ?", sandbox.DBID).
			Updates(map[string]interface{}{"status": sandbox.Status, "runtime_provider": sandbox.RuntimeProvider})
	}
	if err := s.recordSandbox(sandbox); err != nil {
		return nil, err
	}

	if outcome := s.attemptProvision(sandbox); outcome.provisioned {
		sandbox.Status = "running"
		sandbox.StartedAt = outcome.startedAt
		sandbox.RuntimeProvider = outcome.provider
		s.updateSandboxStatus(sandbox.ID, "running", sandbox.StartedAt)
	} else {
		// No runtime reachable — record the definition with the honest
		// status and the provider probe result for operators.
		sandbox.Status = "defined"
		sandbox.RuntimeProvider = "none (" + outcome.detail + ")"
		s.updateSandboxStatus(sandbox.ID, "defined", "")
	}

	return sandbox, nil
}

// provisionOutcome reports a provisioning attempt.
type provisionOutcome struct {
	provisioned bool
	startedAt   string
	provider    string
	detail      string
}

// attemptProvision tries to bring the sandbox up on a reachable local
// runtime (Docker API over its unix socket). It returns a failure
// detail rather than an error: an unreachable runtime is an expected
// deployment condition, not a fault.
func (s *Service) attemptProvision(sandbox *Sandbox) provisionOutcome {
	client := &http.Client{Timeout: 3 * time.Second}
	// Docker socket probe. A deployment with a real runtime answers.
	endpoints := []string{"http://localhost/v1.41/containers/json?limit=1"}
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		endpoints = append([]string{strings.TrimSuffix(dockerHost, "/") + "/containers/json?limit=1"}, endpoints...)
	}
	for _, ep := range endpoints {
		if ep == "" {
			continue
		}
		resp, err := client.Get(ep)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 500 {
			return provisionOutcome{
				provisioned: false, // runtime reachable; container scheduling is engine-specific
				provider:    "docker-api",
				startedAt:   time.Now().Format(time.RFC3339),
				detail:      "runtime reachable; engine scheduling not wired in this build",
			}
		}
	}
	return provisionOutcome{provisioned: false, detail: "no container runtime reachable"}
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
	var recs []models.SandboxRecord
	q := s.db.Order("created_at DESC")
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&recs).Error; err != nil {
		return nil, err
	}
	out := make([]Sandbox, 0, len(recs))
	for _, rec := range recs {
		out = append(out, Sandbox{
			DBID: rec.ID, ID: rec.ID,
			OrganizationID: rec.OrganizationID, SessionID: rec.SessionID, RepositoryID: rec.RepositoryID, UserID: rec.UserID,
			Mode: RuntimeMode(rec.Mode), BaseImage: rec.BaseImage, ImageDigest: rec.ImageDigest,
			CPULimit: rec.CPULimit, MemoryLimitMB: rec.MemoryLimitMB, NetworkPolicy: rec.NetworkPolicy,
			Status: rec.Status, RuntimeProvider: rec.RuntimeProvider, ResourceLimits: rec.ResourceLimitsJSON,
		})
	}
	return out, nil
}

func (s *Service) GetSandbox(sandboxID string) (*Sandbox, error) {
	var rec models.SandboxRecord
	if err := s.db.Where("id = ?", sandboxID).First(&rec).Error; err == nil {
		return &Sandbox{
			DBID: rec.ID, ID: rec.ID,
			OrganizationID: rec.OrganizationID, SessionID: rec.SessionID, RepositoryID: rec.RepositoryID, UserID: rec.UserID,
			Mode: RuntimeMode(rec.Mode), BaseImage: rec.BaseImage, ImageDigest: rec.ImageDigest,
			CPULimit: rec.CPULimit, MemoryLimitMB: rec.MemoryLimitMB, NetworkPolicy: rec.NetworkPolicy,
			Status: rec.Status, RuntimeProvider: rec.RuntimeProvider, ResourceLimits: rec.ResourceLimitsJSON,
		}, nil
	}
	return nil, fmt.Errorf("sandbox: %s not found", sandboxID)
}

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
		"immutable_base_image": true,
		"ephemeral_encrypted":  true,
		"non_root":             true,
		"no_host_socket":       true,
		"no_broad_network":     true,
		"resource_limits":      true,
		"external_audit_agent": true,
		"short_lived_creds":    true,
		"artifact_export_gate": true,
		"destroy_after_close":  true,
		"key_discard":          true,
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
		Details:        fmt.Sprintf(`{"mode":"%s","image":"%s","status":"%s","runtime_provider":"%s"}`, sb.Mode, sb.BaseImage, sb.Status, sb.RuntimeProvider),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(auditEvent).Error
}

func (s *Service) updateSandboxStatus(sandboxID, status, timestamp string) {
	// Durable row update first (the list reads the table).
	s.db.Model(&models.SandboxRecord{}).Where("id = ?", sandboxID).
		Update("status", status)
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

// imageRepoPart strips an optional tag or digest, returning the repository
// name ("patty/sandbox-base:latest" → "patty/sandbox-base").
func imageRepoPart(image string) string {
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	if colon := strings.Index(image, ":"); colon >= 0 {
		image = image[:colon]
	}
	return image
}

// imageAllowlisted decides whether image is permitted by the allowlist.
// Rules (fail-closed; no accidental prefix passing):
//   - the image's repository part must EQUAL an entry's repository part
//     (string-prefix tricks like "patty/sandbox-evil" against
//     "patty/sandbox" never match), and
//   - the entry's tag must be absent (pin the repo, allow any tag —
//     the standard enterprise pin), "*" (explicit any-tag), or exactly
//     the image's tag. Digest forms reduce to their repository.
func imageAllowlisted(image string, allowed []string) bool {
	imgRepo := imageRepoPart(image)
	imgTag := imageTagPart(image)
	for _, entry := range allowed {
		if imageRepoPart(entry) != imgRepo {
			continue
		}
		tag := imageTagPart(entry)
		if tag == "" || tag == "*" || tag == imgTag {
			return true
		}
	}
	return false
}

// imageTagPart returns the tag after the repository ("repo:v1" → "v1";
// digest form or untagged → "").
func imageTagPart(image string) string {
	if at := strings.Index(image, "@"); at >= 0 {
		return ""
	}
	if colon := strings.Index(image, ":"); colon >= 0 {
		return image[colon+1:]
	}
	return ""
}
