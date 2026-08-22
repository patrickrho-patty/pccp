// Package ssomigrate implements the realm-to-realm SSO migration compatibility
// layer: immutable issuer+subject identity links, idempotent discovery
// manifests, deterministic reconciliation, controlled waves, and a bridge that
// resolves a source identity to exactly one Patty user and issues a NEW session.
// It never copies access/refresh tokens or password data. The persisted legacy_*
// field names are retained for wire and database compatibility.
package ssomigrate

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	legacy := identity.NormalizeExternalIdentity(req.LegacyIssuer, req.LegacySubject)
	req.LegacyIssuer, req.LegacySubject = legacy.Issuer, legacy.Subject
	target := identity.NormalizeExternalIdentity(req.TargetIssuer, req.TargetSubject)
	req.TargetIssuer, req.TargetSubject = target.Issuer, target.Subject
	if req.LegacyIssuer == "" || req.LegacySubject == "" || req.PattyUserID == "" {
		return nil, fmt.Errorf("ssomigrate: legacy_issuer, legacy_subject, and patty_user_id required")
	}
	if (req.TargetIssuer == "") != (req.TargetSubject == "") {
		return nil, fmt.Errorf("ssomigrate: target_issuer and target_subject must be supplied together")
	}
	var result *models.SSOIdentityLink
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var organization models.Organization
		if err := tx.Select("id").First(&organization, "id = ?", orgID).Error; err != nil {
			return fmt.Errorf("ssomigrate: organization not found: %w", err)
		}
		if _, err := identity.LockActiveUser(tx, orgID, req.PattyUserID); err != nil {
			return fmt.Errorf("ssomigrate: target user is not active in organization: %w", err)
		}
		if req.TargetIssuer != "" {
			var targetConflict models.SSOIdentityLink
			targetErr := tx.Where(
				"organization_id = ? AND target_issuer = ? AND target_subject = ? AND NOT (legacy_issuer = ? AND legacy_subject = ?)",
				orgID, req.TargetIssuer, req.TargetSubject, req.LegacyIssuer, req.LegacySubject,
			).First(&targetConflict).Error
			if targetErr == nil {
				return fmt.Errorf("ssomigrate: target identity is already reconciled from another source identity")
			}
			if !errors.Is(targetErr, gorm.ErrRecordNotFound) {
				return fmt.Errorf("ssomigrate: lookup target identity: %w", targetErr)
			}
		}
		var existing models.SSOIdentityLink
		lookupErr := tx.Where("organization_id = ? AND legacy_issuer = ? AND legacy_subject = ?",
			orgID, req.LegacyIssuer, req.LegacySubject).First(&existing).Error
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			link := &models.SSOIdentityLink{
				OrganizationID: orgID, LegacyIssuer: req.LegacyIssuer, LegacySubject: req.LegacySubject,
				TargetIssuer: req.TargetIssuer, TargetSubject: req.TargetSubject,
				PattyUserID: req.PattyUserID, Status: models.SSOLinkStatusLinked,
				ResolvedBy: byUser, ResolvedAt: time.Now().UTC().Format(time.RFC3339),
				ResolutionNote: req.Note,
			}
			if err := tx.Create(link).Error; err != nil {
				return err
			}
			result = link
			return nil
		}
		if lookupErr != nil {
			return fmt.Errorf("ssomigrate: lookup legacy identity: %w", lookupErr)
		}
		// An ambiguity/unlinked placeholder carries no user binding. A
		// governance admin may resolve it exactly once through this path.
		if existing.PattyUserID == "" && (existing.Status == models.SSOLinkStatusAmbiguous || existing.Status == models.SSOLinkStatusUnlinked) {
			updates := map[string]interface{}{
				"patty_user_id": req.PattyUserID,
				"target_issuer": req.TargetIssuer, "target_subject": req.TargetSubject,
				"status": models.SSOLinkStatusLinked, "resolved_by": byUser,
				"resolved_at": time.Now().UTC().Format(time.RFC3339), "resolution_note": req.Note,
			}
			updated := tx.Model(&models.SSOIdentityLink{}).
				Where("id = ? AND organization_id = ? AND patty_user_id = '' AND status IN ?", existing.ID, orgID,
					[]string{models.SSOLinkStatusAmbiguous, models.SSOLinkStatusUnlinked}).Updates(updates)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return fmt.Errorf("ssomigrate: identity placeholder was resolved concurrently; reload and retry")
			}
			if err := tx.First(&existing, "id = ?", existing.ID).Error; err != nil {
				return err
			}
			result = &existing
			return nil
		}
		// Same Patty user → update target metadata (idempotent link).
		if existing.PattyUserID == req.PattyUserID {
			updates := map[string]interface{}{
				"target_issuer": req.TargetIssuer, "target_subject": req.TargetSubject,
				"status": models.SSOLinkStatusLinked, "resolved_by": byUser,
				"resolved_at": time.Now().UTC().Format(time.RFC3339), "resolution_note": req.Note,
			}
			if err := tx.Model(&existing).Updates(updates).Error; err != nil {
				return err
			}
			result = &existing
			return nil
		}
		return fmt.Errorf("ssomigrate: legacy identity already linked to user %s — ambiguous, resolve manually", existing.PattyUserID)
	})
	return result, err
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

// BridgeLegacyWithDB joins bridge resolution to an existing verified login
// transaction. verifiedUser must be the exact active user that the one-time
// IdP handoff locked.
func (s *Service) BridgeLegacyWithDB(
	tx *gorm.DB,
	orgID string,
	ev BridgeEvent,
	verifiedSource identity.ExternalIdentity,
	verifiedUser *models.User,
	complete func(tx *gorm.DB, user *models.User) error,
) (*models.SSOMigrationBridgeEvent, error) {
	legacy := identity.NormalizeExternalIdentity(ev.LegacyIssuer, ev.LegacySubject)
	ev.LegacyIssuer, ev.LegacySubject = legacy.Issuer, legacy.Subject
	verifiedSource = identity.NormalizeExternalIdentity(verifiedSource.Issuer, verifiedSource.Subject)
	if ev.LegacyIssuer == "" || ev.LegacySubject == "" {
		return nil, fmt.Errorf("ssomigrate: issuer and subject required")
	}
	if verifiedSource.Issuer == "" || verifiedSource.Subject == "" || verifiedSource != legacy {
		return nil, fmt.Errorf("ssomigrate: requested bridge identity does not match verified login source")
	}
	if verifiedUser == nil {
		return nil, fmt.Errorf("ssomigrate: verified login user required")
	}
	// New session is issued for the resolved Patty user on the TARGET side; the
	// legacy Keycloak token is never copied. If any ambiguity/unlink/disabled,
	// we fail closed (recoverable support flow), never guess.
	link, active, lookupErr := identity.ResolveLinkedSourceIdentity(tx, orgID, legacy)
	decision := models.SSOBridgeDecisionUnlinked
	pattyUser := ""
	if lookupErr == nil {
		switch link.Status {
		case models.SSOLinkStatusLinked:
			if active.ID != verifiedUser.ID || active.OrganizationID != orgID || active.Status != models.UserStatusActive {
				return nil, fmt.Errorf("ssomigrate: verified login subject does not match legacy identity link")
			}
			if complete == nil {
				return nil, fmt.Errorf("ssomigrate: linked bridge requires transactional session issuance")
			}
			if err := complete(tx, active); err != nil {
				return nil, fmt.Errorf("ssomigrate: issue fresh target session: %w", err)
			}
			decision = models.SSOBridgeDecisionLinkedSession
			pattyUser = link.PattyUserID
		case models.SSOLinkStatusDisabled:
			decision = models.SSOBridgeDecisionDisabled
		default:
			decision = models.SSOBridgeDecisionAmbiguous
		}
	} else if errors.Is(lookupErr, identity.ErrExternalIdentityAmbiguous) && link != nil {
		if link.Status == models.SSOLinkStatusDisabled {
			decision = models.SSOBridgeDecisionDisabled
		} else {
			decision = models.SSOBridgeDecisionAmbiguous
		}
	} else if !errors.Is(lookupErr, identity.ErrExternalIdentityUnlinked) {
		return nil, fmt.Errorf("ssomigrate: resolve legacy identity: %w", lookupErr)
	}
	row := &models.SSOMigrationBridgeEvent{
		OrganizationID: orgID, ManifestID: ev.ManifestID,
		LegacyIssuer: ev.LegacyIssuer, LegacySubject: ev.LegacySubject,
		Decision: decision, PattyUserID: pattyUser,
		NewSessionIssued: decision == models.SSOBridgeDecisionLinkedSession, Note: ev.Note,
	}
	if err := tx.Create(row).Error; err != nil {
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
func (s *Service) ImportManifest(orgID, name, source, sourceIssuer string, wave int, importID, byUser string, items []ManifestItem) (*models.SSOMigrationManifest, error) {
	if len(items) == 0 || len(items) > 5000 {
		return nil, fmt.Errorf("ssomigrate: manifest items must contain 1..5000 records")
	}
	source = strings.TrimSpace(source)
	sourceIssuer = identity.NormalizeIssuer(sourceIssuer)
	if sourceIssuer == "" {
		return nil, fmt.Errorf("ssomigrate: canonical source_issuer required")
	}
	var organization models.Organization
	if err := s.db.Select("id").First(&organization, "id = ?", orgID).Error; err != nil {
		return nil, fmt.Errorf("ssomigrate: organization not found: %w", err)
	}
	rows := make([]models.SSOMigrationItem, 0, len(items))
	for _, it := range items {
		rows = append(rows, models.SSOMigrationItem{
			OrganizationID: orgID,
			Kind:           it.Kind,
			LegacyKey:      it.LegacyKey,
			LegacyName:     it.LegacyName,
			Criticality:    it.Criticality,
			Protocol:       it.Protocol,
			Disposition:    it.Disposition,
			TestPlan:       it.TestPlan,
			RollbackPlan:   it.RollbackPlan,
			Notes:          it.Notes,
			Status:         "discovered",
		})
	}
	var manifest models.SSOMigrationManifest
	inventoryDigest := migrationInventoryDigest(name, source, sourceIssuer, wave, rows)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		candidate := models.SSOMigrationManifest{
			OrganizationID: orgID, ManifestID: dari.GenerateID("ssom"),
			Name: name, Source: source, SourceIssuer: sourceIssuer, Wave: wave, ImportID: importID,
			InventoryDigest: inventoryDigest,
			CreatedBy:       byUser, ItemCount: len(items), SourceCount: len(items), Status: models.SSOManifestStatusInventory,
		}
		created := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "organization_id"}, {Name: "import_id"}},
			DoNothing: true,
		}).Create(&candidate)
		if created.Error != nil {
			return created.Error
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("organization_id = ? AND import_id = ?", orgID, importID).First(&manifest).Error; err != nil {
			return fmt.Errorf("ssomigrate: load tenant manifest import: %w", err)
		}
		if created.RowsAffected == 0 {
			if manifest.InventoryDigest != "" && manifest.InventoryDigest == inventoryDigest {
				return nil
			}
			var existingItems []models.SSOMigrationItem
			if err := tx.Where("organization_id = ? AND manifest_id = ?", orgID, manifest.ManifestID).
				Find(&existingItems).Error; err != nil {
				return fmt.Errorf("ssomigrate: load existing manifest inventory: %w", err)
			}
			if migrationInventoryDigest(manifest.Name, manifest.Source, manifest.SourceIssuer, manifest.Wave, existingItems) == inventoryDigest {
				return tx.Model(&manifest).Update("inventory_digest", inventoryDigest).Error
			}
			var signedOff int64
			if err := tx.Model(&models.SSOMigrationWave{}).
				Where("organization_id = ? AND manifest_id = ? AND status = ?", orgID, manifest.ManifestID, models.SSOWaveStatusSignedOff).
				Count(&signedOff).Error; err != nil {
				return err
			}
			if signedOff > 0 {
				return fmt.Errorf("ssomigrate: signed-off manifest inventory is immutable")
			}
			if err := tx.Where("organization_id = ? AND manifest_id = ?", orgID, manifest.ManifestID).Delete(&models.SSOMigrationItem{}).Error; err != nil {
				return err
			}
		}
		for i := range rows {
			rows[i].ManifestID = manifest.ManifestID
		}
		if err := tx.CreateInBatches(rows, 250).Error; err != nil {
			return err
		}
		return tx.Model(&manifest).Updates(map[string]interface{}{
			"name": name, "source": source, "source_issuer": sourceIssuer, "wave": wave,
			"inventory_digest": inventoryDigest,
			"item_count":       len(items), "source_count": len(items), "status": models.SSOManifestStatusInventory,
			"target_count": 0, "linked_count": 0, "ambiguous_count": 0, "excluded_count": 0, "conflict_count": 0,
		}).Error
	})
	return &manifest, err
}

func migrationInventoryDigest(name, source, sourceIssuer string, wave int, items []models.SSOMigrationItem) string {
	itemDigests := make([]string, len(items))
	for i := range items {
		itemDigests[i] = migrationItemInventoryDigest(items[i])
	}
	sort.Strings(itemDigests)
	metadata, _ := json.Marshal(struct {
		Name         string
		Source       string
		SourceIssuer string
		Wave         int
	}{Name: name, Source: source, SourceIssuer: sourceIssuer, Wave: wave})
	digest := sha256.New()
	_, _ = digest.Write(metadata)
	for _, itemDigest := range itemDigests {
		_, _ = digest.Write([]byte(itemDigest))
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

func migrationItemInventoryDigest(item models.SSOMigrationItem) string {
	fields := [...]string{
		item.Kind, item.LegacyKey, item.LegacyName, item.Criticality, item.Protocol,
		item.Disposition, item.TestPlan, item.RollbackPlan, item.Notes,
	}
	encoded, _ := json.Marshal(fields)
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:])
}

type ReconciliationReport struct {
	Manifest *models.SSOMigrationManifest `json:"manifest"`
	Items    []models.SSOMigrationItem    `json:"items"`
}

// Reconcile produces one transactionally consistent manifest and item report.
func (s *Service) Reconcile(orgID, manifestID string) (*ReconciliationReport, error) {
	var manifest models.SSOMigrationManifest
	var items []models.SSOMigrationItem
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("organization_id = ? AND manifest_id = ?", orgID, manifestID).First(&manifest).Error; err != nil {
			return fmt.Errorf("ssomigrate: manifest not found")
		}
		if err := tx.Where("organization_id = ? AND manifest_id = ?", orgID, manifest.ManifestID).
			Order("kind, legacy_key").Find(&items).Error; err != nil {
			return fmt.Errorf("ssomigrate: load manifest items: %w", err)
		}
		if len(items) != manifest.ItemCount {
			return fmt.Errorf("ssomigrate: manifest item count mismatch: have %d, expected %d", len(items), manifest.ItemCount)
		}
		type linkProjection struct {
			LegacyIssuer  string
			LegacySubject string
			Status        string
		}
		userSubjects := make([]string, 0, len(items))
		seenSubjects := make(map[string]struct{}, len(items))
		for _, item := range items {
			if item.Kind == models.SSOMigrationItemKindUser && item.Disposition != models.SSOMigrationDispositionRetire {
				if _, seen := seenSubjects[item.LegacyKey]; !seen {
					seenSubjects[item.LegacyKey] = struct{}{}
					userSubjects = append(userSubjects, item.LegacyKey)
				}
			}
		}
		bySubject := make(map[string][]linkProjection, len(userSubjects))
		sourceIssuer := identity.NormalizeIssuer(manifest.SourceIssuer)
		if sourceIssuer == "" {
			return fmt.Errorf("ssomigrate: manifest has no canonical source issuer; re-import it with source_issuer")
		}
		if len(userSubjects) > 0 {
			query := tx.Model(&models.SSOIdentityLink{}).
				Where("organization_id = ? AND legacy_issuer = ? AND legacy_subject IN ?", orgID, sourceIssuer, userSubjects)
			var links []linkProjection
			if err := query.Select("legacy_issuer, legacy_subject, status").Find(&links).Error; err != nil {
				return fmt.Errorf("ssomigrate: load identity links: %w", err)
			}
			for _, link := range links {
				bySubject[link.LegacySubject] = append(bySubject[link.LegacySubject], link)
			}
		}

		linked, ambiguous, excluded, conflicts, target := 0, 0, 0, 0, 0
		for _, item := range items {
			if item.Kind != models.SSOMigrationItemKindUser {
				if migrationDispositionComplete(item.Disposition) {
					target++
				} else {
					conflicts++
				}
				continue
			}
			if item.Disposition == models.SSOMigrationDispositionRetire {
				excluded++
				target++
				continue
			}
			candidates := bySubject[item.LegacyKey]
			if len(candidates) == 0 {
				conflicts++
				continue
			}
			if len(candidates) > 1 {
				ambiguous++
				continue
			}
			switch candidates[0].Status {
			case models.SSOLinkStatusLinked:
				linked++
				target++
			case models.SSOLinkStatusDisabled:
				excluded++
				target++
			default:
				ambiguous++
			}
		}
		manifest.SourceCount = len(items)
		manifest.TargetCount = target
		manifest.LinkedCount = linked
		manifest.AmbiguousCount = ambiguous
		manifest.ExcludedCount = excluded
		manifest.ConflictCount = conflicts
		manifest.Status = models.SSOManifestStatusReconciled
		if target == len(items) && ambiguous == 0 && conflicts == 0 {
			manifest.Status = models.SSOManifestStatusWaveReady
		}
		manifest.ReconciledAt = time.Now().UTC().Format(time.RFC3339)
		return tx.Model(&manifest).Updates(map[string]interface{}{
			"source_count": len(items), "target_count": target,
			"linked_count": linked, "ambiguous_count": ambiguous, "excluded_count": excluded,
			"conflict_count": conflicts, "status": manifest.Status, "reconciled_at": manifest.ReconciledAt,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &ReconciliationReport{Manifest: &manifest, Items: items}, nil
}

func migrationDispositionComplete(disposition string) bool {
	switch disposition {
	case models.SSOMigrationDispositionKeep, models.SSOMigrationDispositionCompatBridge, models.SSOMigrationDispositionRetire:
		return true
	default:
		return false
	}
}

// SignOffWave advances a wave to signed off (application-owner sign-off).
func (s *Service) SignOffWave(orgID, waveID, byUser string, rollbackWindow string) error {
	if strings.TrimSpace(byUser) == "" {
		return fmt.Errorf("ssomigrate: sign-off actor required")
	}
	if strings.TrimSpace(rollbackWindow) == "" {
		return fmt.Errorf("ssomigrate: rollback window required")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var wave models.SSOMigrationWave
		if err := tx.Where("organization_id = ? AND id = ?", orgID, waveID).First(&wave).Error; err != nil {
			return fmt.Errorf("ssomigrate: wave not found")
		}
		var manifest models.SSOMigrationManifest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND manifest_id = ?", orgID, wave.ManifestID).First(&manifest).Error; err != nil {
			return fmt.Errorf("ssomigrate: migration manifest not found")
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ? AND manifest_id = ?", orgID, waveID, manifest.ManifestID).First(&wave).Error; err != nil {
			return fmt.Errorf("ssomigrate: wave changed during sign-off")
		}
		if manifest.Status != models.SSOManifestStatusWaveReady || manifest.SourceCount != manifest.ItemCount ||
			manifest.SourceCount != manifest.TargetCount || manifest.AmbiguousCount != 0 || manifest.ConflictCount != 0 {
			return fmt.Errorf("ssomigrate: reconciliation parity required before wave sign-off")
		}
		updates := map[string]interface{}{
			"status": models.SSOWaveStatusSignedOff, "sign_off_by": byUser,
			"sign_off_at": time.Now().UTC().Format(time.RFC3339), "rollback_window": rollbackWindow,
		}
		updated := tx.Model(&models.SSOMigrationWave{}).
			Where("organization_id = ? AND id = ? AND status != ?", orgID, waveID, models.SSOWaveStatusSignedOff).Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return fmt.Errorf("ssomigrate: wave was already signed off")
		}
		return nil
	})
}

// Marshal is a small helper to keep JSON payloads explicit.
func Marshal(v interface{}) []byte { b, _ := json.Marshal(v); return b }
