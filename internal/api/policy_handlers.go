package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
)

// --- Epoch diff (policy B3) ---

func (s *Server) handleEpochDiff(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	against := r.URL.Query().Get("against")
	orgID := getOrgID(r)
	var epochA, epochB models.PolicyEpoch
	if err := s.db.Where("epoch_id = ? AND organization_id = ?", id, orgID).First(&epochA).Error; err != nil {
		writeError(w, http.StatusNotFound, "epoch not found")
		return
	}
	if err := s.db.Where("epoch_id = ? AND organization_id = ?", against, orgID).First(&epochB).Error; err != nil {
		writeError(w, http.StatusNotFound, "comparison epoch not found")
		return
	}
	diff, err := s.policy.EpochDiff(&epochA, &epochB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

// --- Acknowledgement campaign (policy C2, §33.6) ---

func (s *Server) handleRequireEpochAck(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if err := s.policy.SetRequiresAck(orgID, id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ack_required"})
}

func (s *Server) handleListEpochAcks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	acks, err := s.policy.ListAcks(orgID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, acks)
}

func (s *Server) handleAckEpoch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	claims, ok := claimsFromCtx(r.Context())
	if !ok || strings.TrimSpace(claims.Subject) == "" {
		writeError(w, http.StatusForbidden, "policy acknowledgement requires an immutable user subject")
		return
	}
	userID := strings.TrimSpace(claims.Subject)
	if req.UserID != "" && req.UserID != userID {
		writeError(w, http.StatusForbidden, "users may acknowledge policy only for their own immutable subject")
		return
	}
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if err := s.policy.AckEpoch(orgID, id, userID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "acked"})
}

// --- Effective policy resolver (policy B1) ---

func (s *Server) handleEffectivePolicy(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	projectID := r.URL.Query().Get("project_id")
	repoID := r.URL.Query().Get("repo_id")
	effective, err := s.policy.EffectivePolicy(orgID, projectID, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, effective)
}

// --- Policy packs (policy B2/UX12) ---

func (s *Server) handleListPolicyPacks(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	packs, err := s.policy.ListPacks(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, packs)
}

func (s *Server) handleCreatePolicyPack(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		Name    string   `json:"name"`
		NameKo  string   `json:"name_ko"`
		Version string   `json:"version"`
		Profile string   `json:"profile"`
		RuleIDs []string `json:"rule_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	pack, err := s.policy.CreatePackFromRules(orgID, req.Name, req.NameKo, req.Version, req.Profile, req.RuleIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pack)
}

func (s *Server) handleImportPolicyPack(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var doc map[string]interface{}
	if err := decodeJSON(r, &doc); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pack, err := s.policy.ImportPack(orgID, doc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pack)
}

func (s *Server) handleExportPolicyPack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	doc, err := s.policy.ExportPack(orgID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleAssignPolicyPack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		Scope   string `json:"scope"`
		ScopeID string `json:"scope_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.policy.AssignPack(orgID, id, req.Scope, req.ScopeID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
}

// --- Templates (policy UX2) ---

func (s *Server) handleListPolicyTemplates(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	// Seed the built-in catalog on first read (idempotent).
	s.policy.SeedTemplates(orgID, policyTemplateSeed())
	templates, err := s.policy.ListTemplates(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (s *Server) handleSavePolicyTemplate(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		TemplateID  string          `json:"template_id"`
		Domain      string          `json:"domain"`
		Name        string          `json:"name"`
		NameEn      string          `json:"nameEn"`
		Description string          `json:"desc"`
		Config      json.RawMessage `json:"config"`
		Version     string          `json:"version"`
		Enabled     *bool           `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TemplateID == "" || req.Domain == "" {
		writeError(w, http.StatusBadRequest, "template_id and domain are required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	tpl := &models.PolicyTemplate{
		TemplateID: req.TemplateID, Domain: req.Domain, Name: req.Name,
		NameEn: req.NameEn, Description: req.Description,
		ConfigJSON: string(req.Config), Version: req.Version, Enabled: enabled,
	}
	if tpl.Version == "" {
		tpl.Version = "1"
	}
	saved, err := s.policy.SaveTemplate(orgID, tpl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeletePolicyTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if err := s.policy.DeleteTemplate(orgID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Exceptions (policy C5, §33.8) ---

func (s *Server) handleListPolicyExceptions(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	// PAT-1506: support a ranked queue (priority by severity then age).
	if r.URL.Query().Get("ranked") == "true" {
		exceptions, err := s.policy.ListExceptionsRanked(orgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, exceptions)
		return
	}
	exceptions, err := s.policy.ListExceptions(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, exceptions)
}

func (s *Server) handleGetPolicyException(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	ex, err := s.policy.GetException(orgID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "예외를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, ex)
}

func (s *Server) handleCreatePolicyException(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		Scope                 string              `json:"scope"`
		ScopeID               string              `json:"scope_id"`
		ScopeName             string              `json:"scopeName"`
		Reason                string              `json:"reason"`
		RequestedBy           string              `json:"requested_by"`
		RuleIDs               []string            `json:"rule_ids"`
		JustificationKo       string              `json:"justification_ko"`
		Evidence              []map[string]string `json:"evidence"`
		CompensatingControls  string              `json:"compensating_controls"`
		ResourceDestination   string              `json:"resource_destination"`
		SeverityLabel         string              `json:"severity_label"`
		CurrentRuleValues     []map[string]string `json:"current_rule_values"`
		ProposedRuleValues    []map[string]string `json:"proposed_rule_values"`
		Conditions            []map[string]string `json:"conditions"`
		RequestedStart        string              `json:"requested_start"`
		ExpiresAt             string              `json:"expires_at"`
		RequiredApproverRoles []string            `json:"required_approver_roles"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ex, err := s.policy.CreateException(orgID, policy.ExceptionInput{
		Scope: req.Scope, ScopeID: req.ScopeID, ScopeName: req.ScopeName,
		Reason: req.Reason, RequestedBy: req.RequestedBy, RuleIDs: req.RuleIDs,
		JustificationKo:      req.JustificationKo,
		Evidence:             req.Evidence,
		CompensatingControls: req.CompensatingControls,
		ResourceDestination:  req.ResourceDestination,
		SeverityLabel:        req.SeverityLabel,
		CurrentRuleValues:    req.CurrentRuleValues,
		ProposedRuleValues:   req.ProposedRuleValues,
		Conditions:           req.Conditions,
		RequestedStart:       req.RequestedStart,
		ExpiresAt:            req.ExpiresAt,
		RequiredApproverRoles: req.RequiredApproverRoles,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ex)
}

func (s *Server) handleDecidePolicyException(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		Approve         bool                `json:"approve"`
		DecidedBy       string              `json:"decided_by"`
		DecidedByRole   string              `json:"decided_by_role"`
		Reason          string              `json:"reason"`
		Conditions      []map[string]string `json:"conditions"`
		PublishNewEpoch bool                `json:"publish_new_epoch"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ex, err := s.policy.DecideException(orgID, id, policy.ExceptionDecision{
		Approve: req.Approve, DecidedBy: req.DecidedBy, DecidedByRole: req.DecidedByRole,
		Reason: req.Reason, Conditions: req.Conditions, PublishNewEpoch: req.PublishNewEpoch,
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ex)
}

func (s *Server) handleRevokePolicyException(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		DecidedBy string `json:"decided_by"`
		Reason    string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ex, err := s.policy.RevokeException(orgID, id, req.DecidedBy, req.Reason)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ex)
}

// --- Rule lifecycle (policy C1 §46.2 / UX12) ---

func (s *Server) handleApprovePolicyRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	epoch, err := s.policy.ApproveRule(orgID, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "approved", "epoch": epoch})
}

func (s *Server) handleRejectPolicyRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if err := s.policy.RejectRule(orgID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func (s *Server) handleBulkPolicyRules(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		IDs     []string `json:"ids"`
		Enabled bool     `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	epoch, err := s.policy.BulkSetRules(orgID, req.IDs, req.Enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "bulk_updated", "epoch": epoch})
}

// --- Helpers ---

// getActorID extracts an actor identity from the claims (user id when
// present, else the operator email).
func getActorID(r *http.Request) string {
	if claims, ok := claimsFromCtx(r.Context()); ok {
		if claims.Subject != "" {
			return claims.Subject
		}
		return claims.Email
	}
	return ""
}
