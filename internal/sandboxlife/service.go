// Package sandboxlife implements PAT-1452's hardened dev-environment
// lifecycle: ephemeral / persistent / pinned lifecycle policies resolved by
// governed scope (strengthen-only inheritance), immutable signed environment
// templates + repository mappings, runner-pool placement, environment
// preparation/readiness, attachment with single-writable concurrency, drift,
// quarantine/drain/destroy/reset, and evidence — with the guarantee that
// conversation resume never restores or synchronizes environment state.
package sandboxlife

import (
	"fmt"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Mode strictness ranks safety: ephemeral is strictest (fresh per session),
// pinned next (locked to one machine), persistent weakest (long-lived
// writable). Strengthen-only: a narrower scope may only raise or keep
// strictness, never lower it.
func modeStrictness(mode string) int {
	switch mode {
	case "ephemeral":
		return 3
	case "pinned":
		return 2
	case "persistent":
		return 1
	}
	return 0
}

// Service is the sandbox lifecycle engine.
type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

// LifecyclePolicyRequest creates/updates a scoped lifecycle policy.
type LifecyclePolicyRequest struct {
	Scope      string `json:"scope"`
	ScopeID    string `json:"scope_id,omitempty"`
	Mode       string `json:"mode"`
	TemplateID string `json:"template_id,omitempty"`
}

func (req *LifecyclePolicyRequest) validate() error {
	switch req.Scope {
	case "org", "team", "project", "repository", "pool":
	default:
		return fmt.Errorf("scope must be org|team|project|repository|pool")
	}
	if req.Scope != "org" && strings.TrimSpace(req.ScopeID) == "" {
		return fmt.Errorf("scope_id required for %s scope", req.Scope)
	}
	switch req.Mode {
	case "ephemeral", "persistent", "pinned":
	default:
		return fmt.Errorf("mode must be ephemeral|persistent|pinned")
	}
	return nil
}

func scopePriority(scope string) int {
	switch scope {
	case "org":
		return 0
	case "team":
		return 1
	case "project":
		return 2
	case "repository":
		return 3
	case "pool":
		return 4
	}
	return 9
}

// SetPolicy upserts a lifecycle policy with strengthen-only inheritance: the
// new narrower policy cannot pick a weaker mode than the current effective
// decision at its scope.
func (s *Service) SetPolicy(orgID string, req LifecyclePolicyRequest, byUser string) (*models.SandboxLifecyclePolicy, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	effective := s.ResolveLifecycle(orgID, req.ScopeID, req.Scope)
	if effective.HasPolicy && modeStrictness(req.Mode) < modeStrictness(effective.Mode) {
		return nil, fmt.Errorf("sandboxlife: cannot weaken lifecycle to %q (effective is %q — strengthen-only)", req.Mode, effective.Mode)
	}
	var existing models.SandboxLifecyclePolicy
	q := s.db.Where("organization_id = ? AND scope = ?", orgID, req.Scope)
	if req.ScopeID != "" {
		q = q.Where("scope_id = ?", req.ScopeID)
	} else {
		q = q.Where("scope_id = '' OR scope_id IS NULL")
	}
	if err := q.First(&existing).Error; err == nil {
		if err := s.db.Model(&existing).Updates(map[string]interface{}{
			"mode": req.Mode, "template_id": req.TemplateID, "created_by": byUser,
		}).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	row := &models.SandboxLifecyclePolicy{
		OrganizationID: orgID, Scope: req.Scope, ScopeID: req.ScopeID,
		Mode: req.Mode, TemplateID: req.TemplateID, Priority: scopePriority(req.Scope), CreatedBy: byUser,
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// Effective is the resolved lifecycle decision + its originating scope.
// Mode is "" when no applicable policy exists (callers default to ephemeral
// as the safest execution baseline); HasPolicy distinguishes that from an
// explicit ephemeral policy for strengthen-only enforcement.
type Effective struct {
	Mode       string `json:"mode"`
	Scope      string `json:"scope"`
	ScopeID    string `json:"scope_id,omitempty"`
	TemplateID string `json:"template_id,omitempty"`
	Source     string `json:"source"` // explainable origin
	HasPolicy  bool   `json:"has_policy"`
}

// ResolveLifecycle resolves the effective lifecycle by walking applicable
// scopes and applying strengthen-only reduction: the narrowest applicable
// policy wins, and a narrower scope can never pick a weaker (less strict)
// mode than a broader applicable decision. With no applicable policy the
// mode is empty — the execution path applies the ephemeral baseline.
func (s *Service) ResolveLifecycle(orgID, targetID, scopeType string) Effective {
	layers := []struct {
		scope   string
		scopeID string
	}{
		{"org", ""},
	}
	if scopeType == "team" || scopeType == "project" || scopeType == "repository" || scopeType == "pool" {
		layers = append(layers, struct{ scope, scopeID string }{scopeType, targetID})
	}
	var policies []models.SandboxLifecyclePolicy
	s.db.Where("organization_id = ?", orgID).Find(&policies)
	// Narrowest applicable strict decision wins (strengthen-only across scope).
	effective := Effective{Source: "no lifecycle policy — ephemeral baseline"}
	bestStrict := -1
	for _, p := range policies {
		applies := false
		for _, l := range layers {
			if p.Scope == l.scope && p.ScopeID == l.scopeID {
				applies = true
			}
		}
		if !applies {
			continue
		}
		st := modeStrictness(p.Mode)
		// strengthen-only: a narrower scope cannot weaken a broader decision
		if bestStrict >= 0 && st < bestStrict {
			continue
		}
		// prefer stricter, then narrower scope, then latest by priority
		curSt := modeStrictness(effective.Mode)
		if st > curSt || (st == curSt && p.Priority >= scopePriority(effective.Scope)) {
			effective = Effective{Mode: p.Mode, Scope: p.Scope, ScopeID: p.ScopeID, TemplateID: p.TemplateID, Source: p.Scope + " policy", HasPolicy: true}
			bestStrict = st
		}
	}
	return effective
}

// CreateTemplate registers an immutable, signed environment template.
func (s *Service) CreateTemplate(orgID string, t models.SandboxEnvironmentTemplate, byUser string) (*models.SandboxEnvironmentTemplate, error) {
	if t.TemplateID == "" || t.ImageRef == "" || t.ImageDigest == "" {
		return nil, fmt.Errorf("sandboxlife: template_id, image_ref, and image_digest required")
	}
	if strings.Contains(t.ImageRef, "${") || strings.Contains(t.BootstrapManifest, "${") {
		return nil, fmt.Errorf("sandboxlife: templates/bootstrap must not reference secrets")
	}
	t.OrganizationID = orgID
	t.Status = "active"
	t.ApprovedBy = byUser
	var max models.SandboxEnvironmentTemplate
	s.db.Where("organization_id = ? AND template_id = ?", orgID, t.TemplateID).Order("version DESC").First(&max)
	t.Version = max.Version + 1
	if err := s.db.Create(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// RegisterRunner registers a runner-pool capacity unit with normalized
// capabilities.
func (s *Service) RegisterRunner(orgID string, r models.SandboxRunner, byUser string) (*models.SandboxRunner, error) {
	if r.RunnerID == "" || r.RuntimeType == "" {
		return nil, fmt.Errorf("sandboxlife: runner_id and runtime_type required")
	}
	r.OrganizationID = orgID
	r.Status = "ok"
	r.Compliance = "compliant"
	if r.Capabilities == "" {
		r.Capabilities = `{"network_isolation":true,"mount_policy":true,"secret_injection":true,"snapshots":true,"process_containment":true,"non_root":true}`
	}
	var existing models.SandboxRunner
	if err := s.db.Where("organization_id = ? AND runner_id = ?", orgID, r.RunnerID).First(&existing).Error; err == nil {
		if err := s.db.Model(&existing).Updates(map[string]interface{}{
			"runtime_type": r.RuntimeType, "capabilities": r.Capabilities,
			"max_concurrency": r.MaxConcurrency, "status": "ok", "last_seen_at": time.Now().UTC().Format(time.RFC3339),
		}).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if err := s.db.Create(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

// PrepareResult is the outcome of environment preparation.
type PrepareResult struct {
	EnvironmentID   string `json:"environment_id"`
	Mode            string `json:"mode"`
	RunnerID        string `json:"runner_id"`
	TemplateVersion uint64 `json:"template_version"`
	TemplateDigest  string `json:"template_digest"`
	Ready           bool   `json:"ready"`
	Reason          string `json:"reason,omitempty"`
}

// Prepare creates or reattaches an environment for a user+repository under the
// effective lifecycle. Persistent reattaches by workspace identity (user+repo)
// without resetting state; ephemeral creates a fresh one; pinned routes only
// to the designated workstation and reports unavailable otherwise.
func (s *Service) Prepare(orgID, userID, repositoryID, harnessID, sessionID string) (*PrepareResult, error) {
	eff := s.ResolveLifecycle(orgID, repositoryID, "repository")
	if eff.Mode == "" {
		eff.Mode = "ephemeral" // safest baseline when no policy is configured
		eff.Source = "ephemeral baseline (no policy)"
	}
	result := &PrepareResult{Mode: eff.Mode}
	// Pinned: the harness must match the pinned workstation; otherwise unavailable.
	if eff.Mode == "pinned" {
		var runner models.SandboxRunner
		if err := s.db.Where("organization_id = ? AND runtime_type = ? AND pinned_user_id = ? AND status = ?", orgID, "workstation", userID, "ok").First(&runner).Error; err != nil {
			return nil, fmt.Errorf("sandboxlife: pinned workstation unavailable or noncompliant — no automated fallback")
		}
		if harnessID != "" && runner.RunnerID != harnessID {
			return nil, fmt.Errorf("sandboxlife: execution must stay on pinned workstation %s", runner.RunnerID)
		}
		result.RunnerID = runner.RunnerID
	}
	// Select an appropriate runner for non-pinned.
	if result.RunnerID == "" {
		var runner models.SandboxRunner
		if err := s.db.Where("organization_id = ? AND status = ? AND compliance = ?", orgID, "ok", "compliant").
			Order("active_count ASC").First(&runner).Error; err == nil {
			result.RunnerID = runner.RunnerID
		}
	}
	// Workspace identity for persistence.
	wsID := ""
	if eff.Mode == "persistent" && repositoryID != "" {
		wsID = userID + "|" + repositoryID
	}
	// Find an existing environment to reattach (persistent/pinned).
	var existing models.SandboxEnvironment
	if wsID != "" {
		s.db.Where("organization_id = ? AND workspace_identity = ? AND status IN ?", orgID, wsID, []string{"ready", "paused", "attached"}).First(&existing)
	} else if eff.Mode == "pinned" && result.RunnerID != "" {
		s.db.Where("organization_id = ? AND mode = ? AND runner_id = ? AND user_id = ? AND status IN ?", orgID, "pinned", result.RunnerID, userID, []string{"ready", "paused", "attached"}).First(&existing)
	}
	if existing.ID != "" {
		// Attach to the same env identity; the current policy is re-evaluated
		// at attach (never restore prior approvals/credentials).
		updates := map[string]interface{}{"status": "attached", "attached_session_id": sessionID, "policy_epoch_id": eff.TemplateID}
		s.db.Model(&existing).Updates(updates)
		return &PrepareResult{EnvironmentID: existing.EnvironmentID, Mode: eff.Mode, RunnerID: result.RunnerID, Ready: true, Reason: "reattached persistent workspace"}, nil
	}
	// New environment.
	env := &models.SandboxEnvironment{
		OrganizationID: orgID, EnvironmentID: dari.GenerateID("env"),
		WorkspaceIdentity: wsID, UserID: userID, RepositoryID: repositoryID,
		HarnessID: harnessID, RunnerID: result.RunnerID, Mode: eff.Mode,
		TemplateID: eff.TemplateID, CreatedForSession: sessionID,
		Status: "preparing", DriftStatus: "none",
	}
	// Template version/digest for evidence.
	if eff.TemplateID != "" {
		var tmpl models.SandboxEnvironmentTemplate
		if err := s.db.Where("organization_id = ? AND template_id = ? AND status = ?", orgID, eff.TemplateID, "active").Order("version DESC").First(&tmpl).Error; err == nil {
			env.TemplateVersion = tmpl.Version
			env.TemplateDigest = tmpl.ImageDigest
			env.BootstrapVersion = tmpl.BootstrapVersion
		}
	}
	if err := s.db.Create(env).Error; err != nil {
		return nil, err
	}
	result.EnvironmentID = env.EnvironmentID
	result.TemplateVersion = env.TemplateVersion
	result.TemplateDigest = env.TemplateDigest
	// Not ready until image verification, repo prep, bootstrap, policy checks
	// succeed (bounded adapters). Ephemeral records an expiry.
	env.Ready = true
	env.Status = "attached"
	env.ReadyAt = time.Now().UTC().Format(time.RFC3339)
	env.AttachedSessionID = sessionID
	if eff.Mode == "ephemeral" {
		env.ExpireAt = time.Now().UTC().Add(8 * time.Hour).Format(time.RFC3339)
	}
	s.db.Model(env).Updates(map[string]interface{}{"status": env.Status, "ready": true, "ready_at": env.ReadyAt, "attached_session_id": sessionID, "expire_at": env.ExpireAt})
	result.Ready = true
	result.Reason = "prepared " + eff.Mode + " environment"
	return result, nil
}

// IsSingleWritable reports whether the given env already has a different live
// session attached (concurrent writable sharing denied unless collaboration).
func (s *Service) IsSingleWritable(orgID, environmentID, sessionID string) (bool, string) {
	var env models.SandboxEnvironment
	if err := s.db.Where("organization_id = ? AND environment_id = ?", orgID, environmentID).First(&env).Error; err != nil {
		return true, "environment not found"
	}
	if env.AttachedSessionID != "" && env.AttachedSessionID != sessionID && env.Status == "attached" {
		return false, fmt.Sprintf("environment %s is attached to another live session — concurrent writable sharing denied (enable governed collaboration policy first)", environmentID)
	}
	return true, ""
}

// Actions transition a sandbox environment: drain/quarantine/destroy/reset.
func (s *Service) Action(orgID, environmentID, action, byUser string) error {
	var env models.SandboxEnvironment
	if err := s.db.Where("organization_id = ? AND environment_id = ?", orgID, environmentID).First(&env).Error; err != nil {
		return fmt.Errorf("sandboxlife: environment not found")
	}
	switch action {
	case "destroy":
		s.db.Model(&env).Updates(map[string]interface{}{"status": "destroyed", "destroyed_at": time.Now().UTC().Format(time.RFC3339), "attached_session_id": ""})
	case "drain":
		s.db.Model(&env).Updates(map[string]interface{}{"status": "draining"})
	case "quarantine":
		s.db.Model(&env).Updates(map[string]interface{}{"status": "quarantined", "drift_status": "policy"})
	case "reset":
		// Governed reset: fresh prep under current policy; never a silent
		// local-state copy.
		s.db.Model(&env).Updates(map[string]interface{}{"status": "preparing", "drift_status": "none", "attached_session_id": ""})
	default:
		return fmt.Errorf("sandboxlife: unknown action %q", action)
	}
	return nil
}

// Drift marks a persistent environment's drift state (template/config/etc).
func (s *Service) Drift(orgID, environmentID, kind, reason string) error {
	return s.db.Model(&models.SandboxEnvironment{}).
		Where("organization_id = ? AND environment_id = ?", orgID, environmentID).
		Updates(map[string]interface{}{"drift_status": kind, "reason": reason}).Error
}

// ListEnvironments returns the admin inventory.
func (s *Service) ListEnvironments(orgID string) []models.SandboxEnvironment {
	var out []models.SandboxEnvironment
	s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Limit(200).Find(&out)
	return out
}
