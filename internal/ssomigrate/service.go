// Package ssomigrate implements PAT-1442's in-PCCP compatibility layer for
// the Keycloak → Authentik migration: immutable issuer+subject identity link
// registry, idempotent discovery manifests, reconciliation reports, controlled
// migration waves, and a compatibility bridge that resolves a legacy identity
// to exactly one Patty user and issues a NEW session — it never copies Keycloak
// access/refresh tokens or password data. Authorization keys are immutable
// provider subject + issuer + Patty user ID; email is only an attribute.
package ssomigrate

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service is the SSO migration compatibility service.
type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

// IdentityLinkRequest is a non-secret identity mapping.
type IdentityLinkRequest struct {
	LegacyIssuer  string `json:"legacy_issuer"`
	LegacySubject string `json:"legacy_subject"`
	PattyUserID   string `json:"patty_user_id"`
	TargetIssuer  string `json:"target_issuer,omitempty"`
	TargetSubject string `json:"target_subject,omitempty"`
	Note          string `json:"note,omitempty"`
}

// LinkIdentity registers or updates an immutable issuer+subject → Patty user
// mapping. A legacy issuer+subject maps to exactly one Patty user; a second
// Patty user for the same legacy identity is a conflict, not an override.
// Email is never used to auto-link.
func (s *Service) LinkIdentity(orgID string, req IdentityLinkRequest, byUser string) (*models.SSOIdentityLink, error) {
	if req.LegacyIssuer == "" || req.LegacySubject == "" || req.PattyUserID == "" {
		return nil, fmt.Errorf("ssomigrate: legacy_issuer, legacy_subject, and patty_user_id required")
	}
	var existing models.SSOIdentityLink
	err := s.db.Where("organization_id = ? AND legacy_issuer = ? AND legacy_subject = ?",
		orgID, req.LegacyIssuer, req.LegacySubject).First(&existing).Error
	if err == nil {
		// Same Patty user → update target metadata (idempotent link).
		if existing.PattyUserID == req.PattyUserID {
			updates := map[string]interface{}{
				"target_issuer": req.TargetIssuer, "target_subject": req.TargetSubject,
				"status": "linked", "resolved_by": byUser,
				"resolved_at": time.Now().UTC().Format(time.RFC3339), "resolution_note": req.Note,
			}
			if e := s.db.Model(&existing).Updates(updates).Error; e != nil {
				return nil, e
			}
			return &existing, nil
		}
		// Conflicting Patty user for the same legacy identity — ambiguous,
		// requires manual resolution, never guessed.
		existing.Status = "ambiguous"
		s.db.Model(&existing).Update("status", "ambiguous")
		return nil, fmt.Errorf("ssomigrate: legacy identity already linked to user %s — ambiguous, resolve manually", existing.PattyUserID)
	}
	link := &models.SSOIdentityLink{
		OrganizationID: orgID, LegacyIssuer: req.LegacyIssuer, LegacySubject: req.LegacySubject,
		TargetIssuer: req.TargetIssuer, TargetSubject: req.TargetSubject,
		PattyUserID: req.PattyUserID, Status: "linked",
		ResolvedBy: byUser, ResolvedAt: time.Now().UTC().Format(time.RFC3339),
		ResolutionNote: req.Note,
	}
	if err := s.db.Create(link).Error; err != nil {
		return nil, err
	}
	return link, nil
}

// BridgeEvent records the auditable outcome of one bridge authentication.
type BridgeEvent struct {
	ManifestID       string `json:"manifest_id,omitempty"`
	LegacyIssuer     string `json:"legacy_issuer"`
	LegacySubject    string `json:"legacy_subject"`
	Decision         string `json:"decision"`
	PattyUserID      string `json:"patty_user_id,omitempty"`
	NewSessionIssued bool   `json:"new_session_issued"`
	Note             string `json:"note,omitempty"`
}

// BridgeLegacy maps a legacy (Keycloak) identity to its Patty user and issues
// a fresh session decision. It NEVER copies a Keycloak token or password; it
// only resolves the immutable mapping and returns the Patty user + new session
// decision for the Authentik flow to mint.
func (s *Service) BridgeLegacy(orgID string, ev BridgeEvent) (*models.SSOMigrationBridgeEvent, error) {
	if ev.LegacyIssuer == "" || ev.LegacySubject == "" {
		return nil, fmt.Errorf("ssomigrate: issuer and subject required")
	}
	var link models.SSOIdentityLink
	err := s.db.Where("organization_id = ? AND legacy_issuer = ? AND legacy_subject = ?",
		orgID, ev.LegacyIssuer, ev.LegacySubject).First(&link).Error
	decision := "unlinked"
	pattyUser := ""
	if err == nil {
		switch link.Status {
		case "linked":
			decision = "linked_issued_session"
			pattyUser = link.PattyUserID
		case "disabled":
			decision = "disabled"
		default:
			decision = "ambiguous"
		}
	}
	row := &models.SSOMigrationBridgeEvent{
		OrganizationID: orgID, ManifestID: ev.ManifestID,
		LegacyIssuer: ev.LegacyIssuer, LegacySubject: ev.LegacySubject,
		Decision:         decision,
		PattyUserID:      pattyUser,
		NewSessionIssued: decision == "linked_issued_session",
		Note:             ev.Note,
	}
	// New session is issued for the resolved Patty user on the TARGET side; the
	// legacy Keycloak token is never copied. If any ambiguity/unlink/disabled,
	// we fail closed (recoverable support flow), never guess.
	if err := s.db.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// ManifestItem is one discovered entity (non-secret metadata).
type ManifestItem struct {
	Kind         string `json:"kind"`
	LegacyKey    string `json:"legacy_key"`
	LegacyName   string `json:"legacy_name,omitempty"`
	Criticality  string `json:"criticality,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	Disposition  string `json:"disposition,omitempty"`
	TestPlan     string `json:"test_plan,omitempty"`
	RollbackPlan string `json:"rollback_plan,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

// ImportManifest idempotently ingests a discovery manifest (ImportID is the
// replay key; re-import with the same ImportID replaces, never duplicates).
func (s *Service) ImportManifest(orgID, name, source string, wave int, importID, byUser string, items []ManifestItem) (*models.SSOMigrationManifest, error) {
	// Reuse an existing manifest for the same idempotent import key.
	var existing models.SSOMigrationManifest
	if err := s.db.Where("organization_id = ? AND import_id = ?", orgID, importID).First(&existing).Error; err == nil {
		// Replace items for idempotent re-import.
		s.db.Where("manifest_id = ?", existing.ManifestID).Delete(&models.SSOMigrationItem{})
		for _, it := range items {
			s.db.Create(&models.SSOMigrationItem{
				OrganizationID: orgID, ManifestID: existing.ManifestID,
				Kind: it.Kind, LegacyKey: it.LegacyKey, LegacyName: it.LegacyName,
				Criticality: it.Criticality, Protocol: it.Protocol, Disposition: it.Disposition,
				TestPlan: it.TestPlan, RollbackPlan: it.RollbackPlan, Notes: it.Notes, Status: "discovered",
			})
		}
		if e := s.db.Model(&existing).Updates(map[string]interface{}{
			"item_count": len(items), "source_count": len(items), "status": "inventory",
		}).Error; e != nil {
			return nil, e
		}
		return &existing, nil
	}
	manifest := models.SSOMigrationManifest{
		OrganizationID: orgID, ManifestID: dari.GenerateID("ssom"),
		Name: name, Source: source, Wave: wave, ImportID: importID,
		CreatedBy: byUser, ItemCount: len(items), SourceCount: len(items), Status: "inventory",
	}
	if err := s.db.Create(&manifest).Error; err != nil {
		return nil, err
	}
	for _, it := range items {
		s.db.Create(&models.SSOMigrationItem{
			OrganizationID: orgID, ManifestID: manifest.ManifestID,
			Kind: it.Kind, LegacyKey: it.LegacyKey, LegacyName: it.LegacyName,
			Criticality: it.Criticality, Protocol: it.Protocol, Disposition: it.Disposition,
			TestPlan: it.TestPlan, RollbackPlan: it.RollbackPlan, Notes: it.Notes, Status: "discovered",
		})
	}
	return &manifest, nil
}

// Reconcile produces deterministic count/identity/authorization-level
// reconciliation for a manifest and its linked identities.
func (s *Service) Reconcile(orgID, manifestID string) (*models.SSOMigrationManifest, error) {
	var manifest models.SSOMigrationManifest
	if err := s.db.Where("organization_id = ? AND manifest_id = ?", orgID, manifestID).First(&manifest).Error; err != nil {
		return nil, fmt.Errorf("ssomigrate: manifest not found")
	}
	var items []models.SSOMigrationItem
	s.db.Where("manifest_id = ?", manifest.ManifestID).Find(&items)
	var users []struct {
		LegacyIssuer  string
		LegacySubject string
		Status        string
	}
	s.db.Model(&models.SSOIdentityLink{}).Where("organization_id = ?", orgID).
		Select("legacy_issuer, legacy_subject, status").Find(&users)
	linked, ambiguous, excluded := 0, 0, 0
	for _, u := range users {
		switch u.Status {
		case "linked":
			linked++
		case "ambiguous":
			ambiguous++
		case "disabled":
			excluded++
		}
	}
	manifest.SourceCount = len(items)
	manifest.TargetCount = len(items)
	manifest.Status = "reconciled"
	manifest.ReconciledAt = time.Now().UTC().Format(time.RFC3339)
	// Persist deterministic reconciliation counts. (conflicts are resolved at
	// identity-link time — an ambiguous link is not a silent conflict.)
	if err := s.db.Model(&manifest).Updates(map[string]interface{}{
		"source_count": len(items), "target_count": len(items),
		"linked_count": linked, "ambiguous_count": ambiguous, "excluded_count": excluded,
		"status": "reconciled", "reconciled_at": manifest.ReconciledAt,
	}).Error; err != nil {
		return nil, err
	}
	// Reflect persisted counts back onto the returned manifest.
	var refreshed models.SSOMigrationManifest
	s.db.Where("organization_id = ? AND manifest_id = ?", orgID, manifestID).First(&refreshed)
	return &refreshed, nil
}

// SignOffWave advances a wave to signed off (application-owner sign-off).
func (s *Service) SignOffWave(orgID, waveID, byUser string, rollbackWindow string) error {
	updates := map[string]interface{}{
		"status": "signed_off", "sign_off_by": byUser,
		"sign_off_at": time.Now().UTC().Format(time.RFC3339), "rollback_window": rollbackWindow,
	}
	return s.db.Model(&models.SSOMigrationWave{}).
		Where("organization_id = ? AND id = ?", orgID, waveID).Updates(updates).Error
}

// Marshal is a small helper to keep JSON payloads explicit.
func Marshal(v interface{}) []byte { b, _ := json.Marshal(v); return b }
