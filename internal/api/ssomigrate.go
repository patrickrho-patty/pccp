package api

// PAT-1564 SSO realm-to-realm migration — API adapter over the
// ssomigrate compatibility service: immutable identity links, bridge
// authentication (never copies Keycloak tokens — always issues a new session),
// idempotent discovery manifests, reconciliation reports, and wave sign-off.

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/ssomigrate"
)

const (
	ssoMigrationWaveBodyLimit    = 64 << 10
	ssoMigrationWaveMaxApps      = 200
	ssoMigrationWaveStringLimit  = 256
	ssoMigrationRollbackMaxBytes = 2048
	ssoMigrationListDefaultLimit = 50
	ssoMigrationListMaxLimit     = 200
)

type ssoMigrationPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

func ssoMigrationPageLimit(r *http.Request) int {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		return ssoMigrationListDefaultLimit
	}
	if limit > ssoMigrationListMaxLimit {
		return ssoMigrationListMaxLimit
	}
	return limit
}

// handleSSOMigrateLinks lists registered identity links.
func (s *Server) handleSSOMigrateLinks(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	var links []models.SSOIdentityLink
	limit, cursor := ssoMigrationPageLimit(r), strings.TrimSpace(r.URL.Query().Get("cursor"))
	query := s.db.Where("organization_id = ?", orgID)
	if cursor != "" {
		query = query.Where("id > ?", cursor)
	}
	if err := query.Order("id").Limit(limit + 1).Find(&links).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "could not list identity links")
		return
	}
	hasMore, next := len(links) > limit, ""
	if hasMore {
		links = links[:limit]
		next = links[len(links)-1].ID
	}
	writeJSON(w, http.StatusOK, ssoMigrationPage[models.SSOIdentityLink]{Items: links, NextCursor: next, HasMore: hasMore})
}

// handleSSOMigrateLink creates/updates an immutable identity mapping.
func (s *Server) handleSSOMigrateLink(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	var req ssomigrate.IdentityLinkRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	link, err := s.ssoMigrate.LinkIdentity(orgID, req, getActorID(r))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.governanceAudit(orgID, r, "cp.sso.link", "sso.link", "sso_identity_link", link.ID, "linked",
		map[string]interface{}{"legacy_issuer": req.LegacyIssuer, "legacy_subject": req.LegacySubject, "patty_user_id": req.PattyUserID})
	writeJSON(w, http.StatusOK, link)
}

// handleSSOMigrateBridge resolves a legacy identity and issues a NEW session
// decision — never copying a Keycloak token. Fails closed on ambiguity.
func (s *Server) handleSSOMigrateBridge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ssomigrate.BridgeEvent
		Code     string `json:"code"`
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	var token string
	var row *models.SSOMigrationBridgeEvent
	if !redeemSSOHandoff(w, r, s.ext(), "sso-bridge", req.Code, req.Provider, func(tx *gorm.DB, handoff *models.SSOLoginHandoff, user *models.User) error {
		requested := identity.NormalizeExternalIdentity(req.LegacyIssuer, req.LegacySubject)
		if subtle.ConstantTimeCompare([]byte(requested.Issuer), []byte(handoff.SourceIssuer)) != 1 ||
			subtle.ConstantTimeCompare([]byte(requested.Subject), []byte(handoff.SourceSubject)) != 1 {
			return fmt.Errorf("SSO bridge source identity does not match the verified login")
		}
		var bridgeErr error
		row, bridgeErr = s.ssoMigrate.BridgeLegacyWithDB(tx, handoff.OrganizationID, req.BridgeEvent,
			identity.ExternalIdentity{Issuer: handoff.SourceIssuer, Subject: handoff.SourceSubject}, user, func(tx *gorm.DB, locked *models.User) error {
				issued, issueErr := s.auth.IssueTokenForLockedUserWithDB(tx, locked, "member")
				if issueErr != nil {
					return issueErr
				}
				token = issued
				return nil
			})
		return bridgeErr
	}) {
		return
	}
	if !row.NewSessionIssued {
		// Fail-closed decision with a readable, never-secret message so the
		// client can show why no session was issued (unlinked/ambiguous/
		// disabled → recoverable support flow, never guessed).
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":              "브리지 인증 실패(클로즈드): " + row.Decision + " — 수동 해결이 필요합니다",
			"decision":           row.Decision,
			"legacy_issuer":      row.LegacyIssuer,
			"legacy_subject":     row.LegacySubject,
			"new_session_issued": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		*models.SSOMigrationBridgeEvent
		Token string `json:"token"`
	}{SSOMigrationBridgeEvent: row, Token: token})
}

// handleSSOMigrateManifests lists manifests or imports a discovery snapshot.
func (s *Server) handleSSOMigrateManifests(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		var manifests []models.SSOMigrationManifest
		s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Limit(50).Find(&manifests)
		writeJSON(w, http.StatusOK, manifests)
		return
	}
	var req struct {
		Name         string                    `json:"name"`
		Source       string                    `json:"source"`
		SourceIssuer string                    `json:"source_issuer"`
		Wave         int                       `json:"wave"`
		ImportID     string                    `json:"import_id"`
		Items        []ssomigrate.ManifestItem `json:"items"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.ImportID == "" || strings.TrimSpace(req.SourceIssuer) == "" || len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "import_id, source_issuer, and items[] required")
		return
	}
	if len(req.Items) > 5000 {
		writeError(w, http.StatusRequestEntityTooLarge, "manifest supports at most 5000 items")
		return
	}
	m, err := s.ssoMigrate.ImportManifest(orgID, req.Name, req.Source, req.SourceIssuer, req.Wave, req.ImportID, getActorID(r), req.Items)
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
	if !requireGovernanceAdmin(w, r) {
		return
	}
	report, err := s.ssoMigrate.Reconcile(orgID, manifestID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleSSOMigrateWaves lists/creates migration waves and signs off.
func (s *Server) handleSSOMigrateWaves(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		var waves []models.SSOMigrationWave
		limit, cursor := ssoMigrationPageLimit(r), strings.TrimSpace(r.URL.Query().Get("cursor"))
		query := s.db.Where("organization_id = ?", orgID)
		if cursor != "" {
			query = query.Where("id > ?", cursor)
		}
		if err := query.Order("id").Limit(limit + 1).Find(&waves).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "could not list migration waves")
			return
		}
		hasMore, next := len(waves) > limit, ""
		if hasMore {
			waves = waves[:limit]
			next = waves[len(waves)-1].ID
		}
		writeJSON(w, http.StatusOK, ssoMigrationPage[models.SSOMigrationWave]{Items: waves, NextCursor: next, HasMore: hasMore})
		return
	}
	var req struct {
		ManifestID string   `json:"manifest_id"`
		Wave       int      `json:"wave"`
		Name       string   `json:"name"`
		OwnerID    string   `json:"owner_id"`
		Apps       []string `json:"apps"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ssoMigrationWaveBodyLimit)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := validateSSOMigrationWaveRequest(req.ManifestID, req.Name, req.OwnerID, req.Apps); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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

	if !requireGovernanceAdmin(w, r) {
		return
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if len(req.RollbackWindow) > ssoMigrationRollbackMaxBytes {
		writeError(w, http.StatusBadRequest, "rollback_window exceeds 2048 bytes")
		return
	}
	if err := s.ssoMigrate.SignOffWave(orgID, id, getActorID(r), req.RollbackWindow); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "status": "signed_off"})
}

func validateSSOMigrationWaveRequest(manifestID, name, ownerID string, apps []string) error {
	for field, value := range map[string]string{"manifest_id": manifestID, "name": name, "owner_id": ownerID} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
		if len(value) > ssoMigrationWaveStringLimit {
			return fmt.Errorf("%s exceeds %d bytes", field, ssoMigrationWaveStringLimit)
		}
	}
	if len(apps) == 0 || len(apps) > ssoMigrationWaveMaxApps {
		return fmt.Errorf("apps must contain 1..%d entries", ssoMigrationWaveMaxApps)
	}
	for _, app := range apps {
		if strings.TrimSpace(app) == "" || len(app) > ssoMigrationWaveStringLimit {
			return fmt.Errorf("each app must contain 1..%d bytes", ssoMigrationWaveStringLimit)
		}
	}
	return nil
}
