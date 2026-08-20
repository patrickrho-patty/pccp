package api

// PAT-1442 SSO Keycloak→Authentik migration — API adapter over the
// ssomigrate compatibility service: immutable identity links, bridge
// authentication (never copies Keycloak tokens — always issues a new session),
// idempotent discovery manifests, reconciliation reports, and wave sign-off.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/ssomigrate"
)

// handleSSOMigrateLinks lists registered identity links.
func (s *Server) handleSSOMigrateLinks(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var links []models.SSOIdentityLink
	s.db.Where("organization_id = ?", orgID).Order("legacy_issuer, legacy_subject").Find(&links)
	writeJSON(w, http.StatusOK, links)
}

// handleSSOMigrateLink creates/updates an immutable identity mapping.
func (s *Server) handleSSOMigrateLink(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var req ssomigrate.IdentityLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	link, err := s.ssoMigrate.LinkIdentity(orgID, req, getActorID(r))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, ActorID: getActorID(r), ActorType: "user", EventType: "cp.sso.link",
		Action: "sso.link", ResourceType: "sso_identity_link", ResourceID: link.ID, Result: "linked",
		Details: string(ssomigrate.Marshal(map[string]interface{}{"legacy_issuer": req.LegacyIssuer, "legacy_subject": req.LegacySubject, "patty_user_id": req.PattyUserID})),
	})
	writeJSON(w, http.StatusOK, link)
}

// handleSSOMigrateBridge resolves a legacy identity and issues a NEW session
// decision — never copying a Keycloak token. Fails closed on ambiguity.
func (s *Server) handleSSOMigrateBridge(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var req ssomigrate.BridgeEvent
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	row, err := s.ssoMigrate.BridgeLegacy(orgID, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !row.NewSessionIssued {
		writeJSON(w, http.StatusUnprocessableEntity, row)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// handleSSOMigrateManifests lists manifests or imports a discovery snapshot.
func (s *Server) handleSSOMigrateManifests(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if r.Method == http.MethodGet {
		var manifests []models.SSOMigrationManifest
		s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Limit(50).Find(&manifests)
		writeJSON(w, http.StatusOK, manifests)
		return
	}
	var req struct {
		Name     string                    `json:"name"`
		Source   string                    `json:"source"`
		Wave     int                       `json:"wave"`
		ImportID string                    `json:"import_id"`
		Items    []ssomigrate.ManifestItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.ImportID == "" || len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "import_id and items[] required")
		return
	}
	m, err := s.ssoMigrate.ImportManifest(orgID, req.Name, req.Source, req.Wave, req.ImportID, getActorID(r), req.Items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// handleSSOMigrateReconcile produces the reconciliation report for a manifest.
func (s *Server) handleSSOMigrateReconcile(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	manifestID := chi.URLParam(r, "id")
	if orgID == "" || manifestID == "" {
		writeError(w, http.StatusBadRequest, "organization context and manifest id required")
		return
	}
	m, err := s.ssoMigrate.Reconcile(orgID, manifestID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// Include the item list for review.
	var items []models.SSOMigrationItem
	s.db.Where("manifest_id = ?", m.ManifestID).Order("kind, legacy_key").Find(&items)
	writeJSON(w, http.StatusOK, map[string]interface{}{"manifest": m, "items": items})
}

// handleSSOMigrateWaves lists/creates migration waves and signs off.
func (s *Server) handleSSOMigrateWaves(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if r.Method == http.MethodGet {
		var waves []models.SSOMigrationWave
		s.db.Where("organization_id = ?", orgID).Order("wave").Find(&waves)
		writeJSON(w, http.StatusOK, waves)
		return
	}
	var req struct {
		ManifestID string   `json:"manifest_id"`
		Wave       int      `json:"wave"`
		Name       string   `json:"name"`
		OwnerID    string   `json:"owner_id"`
		Apps       []string `json:"apps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	apps, _ := json.Marshal(req.Apps)
	wave := &models.SSOMigrationWave{
		OrganizationID: orgID, ManifestID: req.ManifestID, Wave: req.Wave,
		Name: req.Name, OwnerID: req.OwnerID, Apps: string(apps), Status: "planned",
	}
	if err := s.db.Create(wave).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wave)
}

// handleSSOMigrateWaveSignOff marks a wave signed off by its app owner.
func (s *Server) handleSSOMigrateWaveSignOff(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	var req struct {
		RollbackWindow string `json:"rollback_window"`
	}
	if orgID == "" || id == "" {
		writeError(w, http.StatusBadRequest, "organization context and wave id required")
		return
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := s.ssoMigrate.SignOffWave(orgID, id, getActorID(r), req.RollbackWindow); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "status": "signed_off"})
}
