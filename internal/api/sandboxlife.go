package api

// PAT-1452 hardened dev-environment lifecycle — API adapter over the
// sandboxlife engine: scoped lifecycle policies (strengthen-only), immutable
// signed templates, runner-pool registry, environment prepare/attach with
// single-writable concurrency, drift/quarantine/drain/destroy/reset actions,
// and the admin environment inventory. Conversation resume restores no
// environment state.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sandboxlife"
)

// handleSandboxLifePolicy lists or writes lifecycle policies.
func (s *Server) handleSandboxLifePolicy(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		var policies []models.SandboxLifecyclePolicy
		s.db.Where("organization_id = ?", orgID).Order("priority").Find(&policies)
		writeJSON(w, http.StatusOK, policies)
		return
	}
	var req sandboxlife.LifecyclePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	row, err := s.sandboxLife.SetPolicy(orgID, req, getActorID(r))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// handleSandboxLifeResolve returns the effective lifecycle for a target.
func (s *Server) handleSandboxLifeResolve(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	target := r.URL.Query().Get("target_id")
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "repository"
	}
	eff := s.sandboxLife.ResolveLifecycle(orgID, target, scope)
	writeJSON(w, http.StatusOK, eff)
}

// handleSandboxLifeTemplates lists/creates environment templates.
func (s *Server) handleSandboxLifeTemplates(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		var tpls []models.SandboxEnvironmentTemplate
		s.db.Where("organization_id = ?", orgID).Order("template_id, version DESC").Find(&tpls)
		writeJSON(w, http.StatusOK, tpls)
		return
	}
	var t models.SandboxEnvironmentTemplate
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	created, err := s.sandboxLife.CreateTemplate(orgID, t, getActorID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, created)
}

// handleSandboxLifeRunners lists/registers runner-pool capacity.
func (s *Server) handleSandboxLifeRunners(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		var runners []models.SandboxRunner
		s.db.Where("organization_id = ?", orgID).Find(&runners)
		writeJSON(w, http.StatusOK, runners)
		return
	}
	var rr models.SandboxRunner
	if err := json.NewDecoder(r.Body).Decode(&rr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	created, err := s.sandboxLife.RegisterRunner(orgID, rr, getActorID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, created)
}

// handleSandboxLifePrepare prepares/reattaches an environment for a session.
func (s *Server) handleSandboxLifePrepare(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	var req struct {
		UserID       string `json:"user_id"`
		RepositoryID string `json:"repository_id"`
		HarnessID    string `json:"harness_id,omitempty"`
		SessionID    string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.UserID == "" || req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "user_id and session_id required")
		return
	}
	res, err := s.sandboxLife.Prepare(orgID, req.UserID, req.RepositoryID, req.HarnessID, req.SessionID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleSandboxLifeConcurrency checks single-writable attachment.
func (s *Server) handleSandboxLifeConcurrency(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	envID := r.URL.Query().Get("environment_id")
	sessionID := r.URL.Query().Get("session_id")
	ok, reason := s.sandboxLife.IsSingleWritable(orgID, envID, sessionID)
	status := http.StatusOK
	if !ok {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]interface{}{"allowed": ok, "reason": reason})
}

// handleSandboxLifeAction runs destroy/drain/quarantine/reset on an env.
func (s *Server) handleSandboxLifeAction(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	envID := chi.URLParam(r, "id")
	action := chi.URLParam(r, "action")
	if orgID == "" || envID == "" || action == "" {
		writeError(w, http.StatusBadRequest, "organization context, environment id, and action required")
		return
	}

	if !requireGovernanceAdmin(w, r) {
		return
	}
	if err := s.sandboxLife.Action(orgID, envID, action, getActorID(r)); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "action": action, "environment_id": envID})
}

// handleSandboxLifeEnvironments lists the admin environment inventory.
func (s *Server) handleSandboxLifeEnvironments(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxLife.ListEnvironments(orgID))
}

// handleSandboxLifeDrift marks a persistent environment's drift.
func (s *Server) handleSandboxLifeDrift(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	envID := chi.URLParam(r, "id")
	var req struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	}
	if orgID == "" || envID == "" {
		writeError(w, http.StatusBadRequest, "organization context and environment id required")
		return
	}

	if !requireGovernanceAdmin(w, r) {
		return
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := s.sandboxLife.Drift(orgID, envID, req.Kind, req.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "drift_status": req.Kind})
}
