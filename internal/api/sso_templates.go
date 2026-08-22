package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/patrickrho-patty/pccp/internal/sso"
)

func (s *Server) handleSSOTemplates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"templates": sso.IdPTemplates()})
}

func (s *Server) handleApplySSOTemplate(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(chi.URLParam(r, "id"))
	if orgID == "" || orgID != getOrgID(r) {
		writeError(w, http.StatusForbidden, "organization context does not match target")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	var req struct {
		TemplateID   string               `json:"template_id"`
		Input        sso.IdPTemplateInput `json:"config"`
		ClientSecret string               `json:"client_secret"`
		SPPrivateKey string               `json:"saml_sp_private_key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg, err := s.ext().SSO.ApplyOrganizationIdPTemplate(orgID, getActorID(r), sso.ApplyIdPTemplateRequest{
		TemplateID: req.TemplateID, Input: req.Input, ClientSecret: req.ClientSecret, SPPrivateKey: req.SPPrivateKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"configuration": cfg})
}
