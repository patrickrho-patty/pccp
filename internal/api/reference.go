package api

// PAT-1404 Patty Reference — API adapter over the reference retrieval engine.
// Governed source registry CRUD, bounded retrieval (resolve/search),
// signed package import → stage → activate → rollback, and deployment sync
// state. Governance (MCP policy, capability leases, allowlists, audit) is
// enforced by the existing policy paths.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/reference"
)

// --- Sources ---

func (s *Server) handleReferenceSources(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		var sources []models.ReferenceSource
		s.db.Where("organization_id = ?", orgID).Order("tier, name").Find(&sources)
		writeJSON(w, http.StatusOK, sources)
		return
	}
	var src models.ReferenceSource
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	created, err := s.refSV.RegisterSource(orgID, src)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, ActorID: getActorID(r), ActorType: "user", EventType: "cp.reference.source",
		Action: "reference.source", ResourceType: "reference_source", ResourceID: created.SourceID, Result: "saved",
	})
	writeJSON(w, http.StatusOK, created)
}

func (s *Server) handleReferenceSourceDelete(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	if orgID == "" || id == "" {
		writeError(w, http.StatusBadRequest, "organization context and source id required")
		return
	}

	if !requireGovernanceAdmin(w, r) {
		return
	}
	if err := s.refSV.RemoveSource(orgID, id, getActorID(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// --- Retrieval ---

func (s *Server) handleReferenceResolve(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q required")
		return
	}
	res, err := s.refSV.ResolveLibrary(orgID, q, r.URL.Query().Get("project_evidence"), r.URL.Query().Get("requested_version"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleReferenceSearch(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q required")
		return
	}
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &limit)
	}
	hits, err := s.refSV.SearchDocs(orgID, r.URL.Query().Get("library_id"), q,
		r.URL.Query().Get("version"), r.URL.Query().Get("locale"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query": q, "hits": hits, "count": len(hits), "budget_tokens": reference.MaxResultTokensPublic,
	})
}

// handleReferenceListVersions returns available chunk versions per source.
func (s *Server) handleReferenceListVersions(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	sourceID := r.URL.Query().Get("source_id")
	if orgID == "" || sourceID == "" {
		writeError(w, http.StatusBadRequest, "organization context and source_id required")
		return
	}
	var versions []string
	s.db.Model(&models.ReferenceChunk{}).Where("organization_id = ? AND source_id = ? AND version != ''", orgID, sourceID).
		Distinct().Order("version").Pluck("version", &versions)
	writeJSON(w, http.StatusOK, map[string]interface{}{"source_id": sourceID, "versions": versions})
}

// --- Packages (import / stage / activate / rollback) ---

func (s *Server) handleReferencePackages(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		var pkgs []models.ReferencePackage
		s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Limit(100).Find(&pkgs)
		writeJSON(w, http.StatusOK, pkgs)
		return
	}
	// POST: import (body = raw manifest bytes) with headers for signature/publisher.
	var req struct {
		Manifest     string `json:"manifest"`
		SignatureHex string `json:"signature_hex"`
		Publisher    string `json:"publisher"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.Manifest == "" {
		writeError(w, http.StatusBadRequest, "manifest required")
		return
	}
	pkg, err := s.refSV.ImportPackage(orgID, req.Publisher, req.SignatureHex, getActorID(r), []byte(req.Manifest))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "package_id": pkg.PackageID, "state": pkg.State})
}

func (s *Server) handleReferencePackageActivate(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	if orgID == "" || id == "" {
		writeError(w, http.StatusBadRequest, "organization context and package id required")
		return
	}

	if !requireGovernanceAdmin(w, r) {
		return
	}

	var req struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := s.refSV.ActivatePackage(orgID, id, getActorID(r), req.Note); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, ActorID: getActorID(r), ActorType: "user", EventType: "cp.reference.activate",
		Action: "reference.activate", ResourceType: "reference_package", ResourceID: id, Result: "activated",
		Details: string(mustJSON(map[string]interface{}{"note": req.Note})),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "state": "active"})
}

func (s *Server) handleReferencePackageRollback(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	if orgID == "" || id == "" {
		writeError(w, http.StatusBadRequest, "organization context and package id required")
		return
	}

	if !requireGovernanceAdmin(w, r) {
		return
	}
	if err := s.refSV.RollbackPackage(orgID, id, getActorID(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "state": "rolled_back"})
}

// --- Catalog / sync state ---

func (s *Server) handleReferenceCatalog(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		var state models.ReferenceCatalogState
		if err := s.db.Where("organization_id = ?", orgID).First(&state).Error; err != nil {
			state = models.ReferenceCatalogState{OrganizationID: orgID, Deployment: "onprem"}
		}
		var active models.ReferencePackage
		if state.ActivePackageID != "" {
			s.db.Where("organization_id = ? AND package_id = ?", orgID, state.ActivePackageID).First(&active)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"state": state, "active_package": active})
		return
	}
	var req struct {
		Deployment   string `json:"deployment"`
		SyncEnabled  bool   `json:"sync_enabled"`
		AutoActivate bool   `json:"auto_activate"`
		ChannelAllow string `json:"channel_allow,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	state := models.ReferenceCatalogState{
		Deployment: req.Deployment, SyncEnabled: req.SyncEnabled, AutoActivate: req.AutoActivate,
		ChannelAllow: req.ChannelAllow, LastSyncAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.refSV.SetCatalogState(orgID, state); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
