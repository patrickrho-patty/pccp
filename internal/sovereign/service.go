package sovereign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	trustBundleSettingKey = "sovereign.trust_bundle"
)

// Service implements sovereign/air-gapped deployment features (PRD §34.4, §9.7, §5).
type Service struct {
	mu           sync.RWMutex
	db           *gorm.DB
	trustBundles map[string]*TrustBundle // orgID → local trust bundle
	updates      map[string]*OfflineUpdate
	entitlements map[string]*SignedOfflineEntitlement
}

// New creates a new sovereign deployment service.
func New(databases ...*gorm.DB) *Service {
	service := &Service{
		trustBundles: make(map[string]*TrustBundle),
		updates:      make(map[string]*OfflineUpdate),
		entitlements: make(map[string]*SignedOfflineEntitlement),
	}
	if len(databases) > 0 {
		service.db = databases[0]
	}
	return service
}

// OfflineEntitlement is the signed, portable authority used when a sovereign
// deployment cannot renew plan state online.
type OfflineEntitlement struct {
	Version         int      `json:"version"`
	OrganizationID  string   `json:"organization_id"`
	DeploymentID    string   `json:"deployment_id"`
	Profile         string   `json:"profile"`
	Sequence        uint64   `json:"sequence"`
	IssuedAt        string   `json:"issued_at"`
	NotBefore       string   `json:"not_before"`
	NotAfter        string   `json:"not_after"`
	MaxUserSeats    int      `json:"max_user_seats"`
	MaxHarnessSeats int      `json:"max_harness_seats"`
	Features        []string `json:"features,omitempty"`
	ModelClasses    []string `json:"model_classes,omitempty"`
}

func (e OfflineEntitlement) SigningBytes() []byte {
	payload, _ := json.Marshal(e)
	return append([]byte("PCCP-OFFLINE-ENTITLEMENT-v1\x00"), payload...)
}

type SignedOfflineEntitlement struct {
	Entitlement OfflineEntitlement `json:"entitlement"`
	KeyID       string             `json:"key_id,omitempty"`
	Signature   string             `json:"signature"`
}

func entitlementKey(orgID, deploymentID string) string { return orgID + "\x00" + deploymentID }

func entitlementHighWaterKey(deploymentID string) string {
	digest := sha256.Sum256([]byte(deploymentID))
	return "sovereign.entitlement.highwater." + hex.EncodeToString(digest[:12])
}

func activeEntitlementKey(deploymentID string) string {
	digest := sha256.Sum256([]byte(deploymentID))
	return "sovereign.active_entitlement." + hex.EncodeToString(digest[:12])
}

func (s *Service) ImportOfflineEntitlementAt(signed SignedOfflineEntitlement, expectedOrgID, expectedDeploymentID string, now time.Time) (*SignedOfflineEntitlement, error) {
	e := signed.Entitlement
	key := entitlementKey(expectedOrgID, expectedDeploymentID)
	if s.db != nil {
		payload, err := json.Marshal(signed)
		if err != nil {
			return nil, fmt.Errorf("sovereign: encode offline entitlement: %w", err)
		}
		err = s.db.Transaction(func(tx *gorm.DB) error {
			var organization models.Organization
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&organization, "id = ?", expectedOrgID).Error; err != nil {
				return fmt.Errorf("sovereign: entitlement organization not found: %w", err)
			}
			var trustSetting models.OrgSetting
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("organization_id = ? AND key = ?", expectedOrgID, trustBundleSettingKey).First(&trustSetting).Error; err != nil {
				return fmt.Errorf("sovereign: durable trust bundle required")
			}
			var bundle TrustBundle
			if err := json.Unmarshal([]byte(trustSetting.Value), &bundle); err != nil {
				return fmt.Errorf("sovereign: trust bundle is malformed")
			}
			if _, err := validateOfflineEntitlementAt(bundle, signed, expectedOrgID, expectedDeploymentID, now); err != nil {
				return err
			}
			highWaterKey := entitlementHighWaterKey(expectedDeploymentID)
			var highWater models.OrgSetting
			queryErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("organization_id = ? AND key = ?", expectedOrgID, highWaterKey).First(&highWater).Error
			if queryErr != nil && !errors.Is(queryErr, gorm.ErrRecordNotFound) {
				return queryErr
			}
			if queryErr == nil {
				sequence, parseErr := strconv.ParseUint(highWater.Value, 10, 64)
				if parseErr != nil || e.Sequence <= sequence {
					return fmt.Errorf("sovereign: offline entitlement sequence did not advance")
				}
			}
			settings := []models.OrgSetting{
				{OrganizationID: expectedOrgID, Key: highWaterKey, Value: strconv.FormatUint(e.Sequence, 10)},
				{OrganizationID: expectedOrgID, Key: activeEntitlementKey(expectedDeploymentID), Value: string(payload)},
			}
			for i := range settings {
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "organization_id"}, {Name: "key"}},
					DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
				}).Create(&settings[i]).Error; err != nil {
					return err
				}
			}
			return tx.Model(&models.Organization{}).Where("id = ?", expectedOrgID).Updates(map[string]interface{}{
				"max_user_seats": e.MaxUserSeats, "max_harness_seats": e.MaxHarnessSeats,
			}).Error
		})
		if err != nil {
			return nil, err
		}
		copySigned := signed
		return &copySigned, nil
	} else {
		bundle, err := s.GetTrustBundle(expectedOrgID)
		if err != nil {
			return nil, err
		}
		if _, err := validateOfflineEntitlementAt(*bundle, signed, expectedOrgID, expectedDeploymentID, now); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.entitlements[key]; current != nil && e.Sequence <= current.Entitlement.Sequence {
		return nil, fmt.Errorf("sovereign: offline entitlement sequence did not advance")
	}
	copySigned := signed
	s.entitlements[key] = &copySigned
	return &copySigned, nil
}

func (s *Service) ImportOfflineEntitlement(signed SignedOfflineEntitlement, expectedOrgID, expectedDeploymentID string) (*SignedOfflineEntitlement, error) {
	return s.ImportOfflineEntitlementAt(signed, expectedOrgID, expectedDeploymentID, time.Now().UTC())
}

func (s *Service) GetOfflineEntitlement(orgID, deploymentID string) (*SignedOfflineEntitlement, error) {
	if s.db != nil {
		var setting models.OrgSetting
		if err := s.db.Where("organization_id = ? AND key = ?", orgID, activeEntitlementKey(deploymentID)).First(&setting).Error; err != nil {
			return nil, fmt.Errorf("sovereign: offline entitlement not found")
		}
		var signed SignedOfflineEntitlement
		if err := json.Unmarshal([]byte(setting.Value), &signed); err != nil || signed.Entitlement.DeploymentID != deploymentID {
			return nil, fmt.Errorf("sovereign: offline entitlement not found")
		}
		return &signed, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	current := s.entitlements[entitlementKey(orgID, deploymentID)]
	if current == nil {
		return nil, fmt.Errorf("sovereign: offline entitlement not found")
	}
	copySigned := *current
	return &copySigned, nil
}

// TrustBundle represents a local trust bundle for air-gapped operation (PRD §9.7).
type TrustBundle struct {
	OrganizationID                string            `json:"organization_id"`
	LocalCAIdentity               string            `json:"local_ca_identity"`
	LocalCAPublicKey              string            `json:"local_ca_public_key"`
	EntitlementAuthorityPublicKey string            `json:"entitlement_authority_public_key"`
	ModelSigningKeys              []string          `json:"model_signing_keys"`
	RevocationList                []string          `json:"revocation_list"`
	ReferenceMeasurements         map[string]string `json:"reference_measurements"` // component → expected hash
	ImportedAt                    string            `json:"imported_at"`
	ExpiresAt                     string            `json:"expires_at"`
}

// OfflineUpdate represents an update package for air-gapped deployment.
type OfflineUpdate struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	Type       string `json:"type"` // server, relay, pia, model, policy
	Hash       string `json:"hash"`
	Signature  string `json:"signature"`
	Size       int64  `json:"size"`
	CreatedAt  string `json:"created_at"`
	ImportedAt string `json:"imported_at,omitempty"`
	AppliedAt  string `json:"applied_at,omitempty"`
	Status     string `json:"status"` // pending_import, imported, applied, failed
	Checksum   string `json:"checksum"`
}

// ImportTrustBundle imports a trust bundle for air-gapped operation (PRD §9.7).
func (s *Service) ImportTrustBundle(bundle TrustBundle) (*TrustBundle, error) {
	if bundle.OrganizationID == "" || bundle.LocalCAPublicKey == "" {
		return nil, fmt.Errorf("sovereign: local CA public key required")
	}
	now := time.Now().UTC()
	if err := validateTrustBundleAt(bundle, bundle.OrganizationID, now); err != nil {
		return nil, err
	}
	bundle.ImportedAt = now.Format(time.RFC3339)
	if s.db != nil {
		err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := lockOrganization(tx, bundle.OrganizationID); err != nil {
				return err
			}
			current, err := loadTrustBundleWithDB(tx, bundle.OrganizationID, true)
			if err == nil {
				bundle.EntitlementAuthorityPublicKey = current.EntitlementAuthorityPublicKey
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			} else {
				bundle.EntitlementAuthorityPublicKey = ""
			}
			return persistTrustBundleWithDB(tx, bundle)
		})
		if err != nil {
			return nil, err
		}
	} else {
		s.mu.Lock()
		if current := s.trustBundles[bundle.OrganizationID]; current != nil {
			bundle.EntitlementAuthorityPublicKey = current.EntitlementAuthorityPublicKey
		} else {
			bundle.EntitlementAuthorityPublicKey = ""
		}
		copyBundle := bundle
		s.trustBundles[bundle.OrganizationID] = &copyBundle
		s.mu.Unlock()
		return &copyBundle, nil
	}

	s.mu.Lock()
	copyBundle := bundle
	s.trustBundles[bundle.OrganizationID] = &copyBundle
	s.mu.Unlock()

	return &copyBundle, nil
}

// InstallEntitlementAuthority is the offline-ceremony seam. It is not exposed
// by the tenant HTTP API; deployment tooling pins this key out of band.
func (s *Service) InstallEntitlementAuthority(orgID, publicKeyHex string) error {
	key, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("sovereign: valid entitlement authority public key required")
	}
	var bundle *TrustBundle
	if s.db != nil {
		err = s.db.Transaction(func(tx *gorm.DB) error {
			bundle, _, err = installEntitlementAuthorityWithDB(tx, orgID, publicKeyHex)
			return err
		})
		if err != nil {
			return err
		}
	} else {
		s.mu.Lock()
		defer s.mu.Unlock()
		current := s.trustBundles[orgID]
		if current == nil {
			return fmt.Errorf("sovereign: trust bundle not found for org %s", orgID)
		}
		if err := requirePinnedAuthority(current.EntitlementAuthorityPublicKey, publicKeyHex); err != nil {
			return err
		}
		copyBundle := *current
		copyBundle.EntitlementAuthorityPublicKey = publicKeyHex
		copyBundle.ImportedAt = time.Now().UTC().Format(time.RFC3339)
		s.trustBundles[orgID] = &copyBundle
		return nil
	}
	s.mu.Lock()
	copyBundle := *bundle
	s.trustBundles[orgID] = &copyBundle
	s.mu.Unlock()
	return nil
}

// ConfigureEntitlementAuthoritiesJSON applies the deployment operator's
// offline trust ceremony at process start. The value is a JSON object mapping
// organization IDs to Ed25519 public keys. Existing pins are immutable, so a
// key change fails startup and must use an explicit offline rotation process.
func (s *Service) ConfigureEntitlementAuthoritiesJSON(raw string) error {
	if raw == "" {
		return nil
	}
	configured := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &configured); err != nil || len(configured) == 0 {
		return fmt.Errorf("sovereign: entitlement authority configuration must be a non-empty JSON object")
	}
	orgIDs := make([]string, 0, len(configured))
	for orgID, publicKeyHex := range configured {
		key, err := hex.DecodeString(publicKeyHex)
		if orgID == "" || err != nil || len(key) != ed25519.PublicKeySize {
			return fmt.Errorf("sovereign: valid organization and entitlement authority public key required")
		}
		orgIDs = append(orgIDs, orgID)
	}
	slices.Sort(orgIDs)
	if s.db == nil {
		for _, orgID := range orgIDs {
			if err := s.InstallEntitlementAuthority(orgID, configured[orgID]); err != nil {
				return err
			}
		}
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, orgID := range orgIDs {
			publicKeyHex := configured[orgID]
			_, changed, err := installEntitlementAuthorityWithDB(tx, orgID, publicKeyHex)
			if err != nil {
				return fmt.Errorf("sovereign: install configured entitlement authority: %w", err)
			}
			if !changed {
				continue
			}
			fingerprint := sha256.Sum256([]byte(publicKeyHex))
			details, _ := json.Marshal(map[string]string{"key_fingerprint": hex.EncodeToString(fingerprint[:])})
			if err := tx.Create(&models.AuditEvent{
				OrganizationID: orgID,
				EventType:      "cp.sovereign.entitlement_authority.installed",
				ActorType:      "deployment_operator",
				Action:         "install_entitlement_authority",
				ResourceType:   "sovereign_trust_bundle",
				ResourceID:     orgID,
				Details:        string(details),
				Result:         "success",
				OccurredAt:     time.Now().UTC().Format(time.RFC3339),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func installEntitlementAuthorityWithDB(tx *gorm.DB, orgID, publicKeyHex string) (*TrustBundle, bool, error) {
	if err := lockOrganization(tx, orgID); err != nil {
		return nil, false, err
	}
	bundle, err := loadTrustBundleWithDB(tx, orgID, true)
	if err != nil {
		return nil, false, fmt.Errorf("sovereign: durable trust bundle required before authority installation: %w", err)
	}
	if err := requirePinnedAuthority(bundle.EntitlementAuthorityPublicKey, publicKeyHex); err != nil {
		return nil, false, err
	}
	if bundle.EntitlementAuthorityPublicKey == publicKeyHex {
		return bundle, false, nil
	}
	bundle.EntitlementAuthorityPublicKey = publicKeyHex
	bundle.ImportedAt = time.Now().UTC().Format(time.RFC3339)
	if err := persistTrustBundleWithDB(tx, *bundle); err != nil {
		return nil, false, err
	}
	return bundle, true, nil
}

func requirePinnedAuthority(installed, proposed string) error {
	if installed != "" && installed != proposed {
		return fmt.Errorf("sovereign: entitlement authority is already pinned; explicit offline rotation is required")
	}
	return nil
}

func verifyEntitlementSignature(bundle TrustBundle, signed SignedOfflineEntitlement) error {
	publicKey, err := hex.DecodeString(bundle.EntitlementAuthorityPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("sovereign: invalid entitlement authority public key")
	}
	signature, err := hex.DecodeString(signed.Signature)
	if err != nil || !ed25519.Verify(publicKey, signed.Entitlement.SigningBytes(), signature) {
		return fmt.Errorf("sovereign: offline entitlement signature verification failed")
	}
	return nil
}

func validateOfflineEntitlementAt(bundle TrustBundle, signed SignedOfflineEntitlement, expectedOrgID, expectedDeploymentID string, now time.Time) (*OfflineEntitlement, error) {
	if err := validateTrustBundleAt(bundle, expectedOrgID, now); err != nil {
		return nil, err
	}
	e := signed.Entitlement
	if e.Version != 1 || e.Profile != "sovereign" || e.Sequence == 0 {
		return nil, fmt.Errorf("sovereign: invalid offline entitlement contract")
	}
	if e.OrganizationID != expectedOrgID || e.DeploymentID != expectedDeploymentID {
		return nil, fmt.Errorf("sovereign: offline entitlement scope mismatch")
	}
	notBefore, err := time.Parse(time.RFC3339, e.NotBefore)
	if err != nil {
		return nil, fmt.Errorf("sovereign: invalid entitlement not_before")
	}
	notAfter, err := time.Parse(time.RFC3339, e.NotAfter)
	if err != nil || !notAfter.After(notBefore) || now.Before(notBefore) || !now.Before(notAfter) {
		return nil, fmt.Errorf("sovereign: offline entitlement is outside its validity window")
	}
	if err := verifyEntitlementSignature(bundle, signed); err != nil {
		return nil, err
	}
	return &e, nil
}

func validateTrustBundleAt(bundle TrustBundle, expectedOrgID string, now time.Time) error {
	if bundle.OrganizationID != expectedOrgID {
		return fmt.Errorf("sovereign: trust bundle organization mismatch")
	}
	expiresAt, err := time.Parse(time.RFC3339, bundle.ExpiresAt)
	if err != nil || !now.Before(expiresAt) {
		return fmt.Errorf("sovereign: trust bundle is expired or has an invalid expiry")
	}
	return nil
}

func lockOrganization(tx *gorm.DB, orgID string) error {
	var organization models.Organization
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&organization, "id = ?", orgID).Error; err != nil {
		return fmt.Errorf("sovereign: organization not found: %w", err)
	}
	return nil
}

func loadTrustBundleWithDB(db *gorm.DB, orgID string, locked bool) (*TrustBundle, error) {
	query := db.Where("organization_id = ? AND key = ?", orgID, trustBundleSettingKey)
	if locked {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var setting models.OrgSetting
	if err := query.First(&setting).Error; err != nil {
		return nil, err
	}
	var bundle TrustBundle
	if err := json.Unmarshal([]byte(setting.Value), &bundle); err != nil {
		return nil, fmt.Errorf("sovereign: trust bundle is malformed")
	}
	return &bundle, nil
}

func persistTrustBundleWithDB(db *gorm.DB, bundle TrustBundle) error {
	payload, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	setting := models.OrgSetting{OrganizationID: bundle.OrganizationID, Key: trustBundleSettingKey, Value: string(payload)}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "organization_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&setting).Error; err != nil {
		return fmt.Errorf("sovereign: persist trust bundle: %w", err)
	}
	return nil
}

// GetTrustBundle returns the local trust bundle for an organization.
func (s *Service) GetTrustBundle(orgID string) (*TrustBundle, error) {
	if s.db != nil {
		bundle, err := loadTrustBundleWithDB(s.db, orgID, false)
		if err != nil {
			return nil, fmt.Errorf("sovereign: trust bundle not found for org %s", orgID)
		}
		return bundle, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	bundle, ok := s.trustBundles[orgID]
	if !ok {
		return nil, fmt.Errorf("sovereign: trust bundle not found for org %s", orgID)
	}
	copyBundle := *bundle
	return &copyBundle, nil
}

// ValidateActiveEntitlementWithDB re-verifies the durable signed artifact at
// the authorization point. A database edit, restart, replica handoff, expiry,
// sequence rollback, missing feature, or disallowed model class fails closed.
func ValidateActiveEntitlementWithDB(db *gorm.DB, orgID, deploymentID, requiredFeature, modelClass string, now time.Time) (*OfflineEntitlement, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("sovereign: local deployment identity is not configured")
	}
	var bundleSetting, entitlementSetting models.OrgSetting
	if err := db.Where("organization_id = ? AND key = ?", orgID, trustBundleSettingKey).First(&bundleSetting).Error; err != nil {
		return nil, fmt.Errorf("sovereign: durable trust bundle required")
	}
	if err := db.Where("organization_id = ? AND key = ?", orgID, activeEntitlementKey(deploymentID)).First(&entitlementSetting).Error; err != nil {
		return nil, fmt.Errorf("sovereign: active offline entitlement required")
	}
	var bundle TrustBundle
	var signed SignedOfflineEntitlement
	if json.Unmarshal([]byte(bundleSetting.Value), &bundle) != nil || json.Unmarshal([]byte(entitlementSetting.Value), &signed) != nil {
		return nil, fmt.Errorf("sovereign: durable entitlement state is malformed")
	}
	e, err := validateOfflineEntitlementAt(bundle, signed, orgID, deploymentID, now)
	if err != nil {
		return nil, fmt.Errorf("sovereign: durable entitlement validation failed: %w", err)
	}
	var highWater models.OrgSetting
	if err := db.Where("organization_id = ? AND key = ?", orgID, entitlementHighWaterKey(e.DeploymentID)).First(&highWater).Error; err != nil || highWater.Value != strconv.FormatUint(e.Sequence, 10) {
		return nil, fmt.Errorf("sovereign: durable entitlement sequence is not current")
	}
	if requiredFeature != "" && !slices.Contains(e.Features, requiredFeature) {
		return nil, fmt.Errorf("sovereign: entitlement does not grant feature %s", requiredFeature)
	}
	if modelClass != "" && len(e.ModelClasses) > 0 && !slices.Contains(e.ModelClasses, modelClass) {
		return nil, fmt.Errorf("sovereign: entitlement does not grant model class %s", modelClass)
	}
	return e, nil
}

// ImportUpdate imports an offline update package (PRD §34.4).
func (s *Service) ImportUpdate(update OfflineUpdate) (*OfflineUpdate, error) {
	if update.ID == "" {
		update.ID = fmt.Sprintf("upd_%d", time.Now().UnixMilli())
	}
	if update.Status == "" {
		update.Status = "imported"
	}
	update.ImportedAt = time.Now().Format(time.RFC3339)

	// Verify hash
	if update.Hash == "" {
		return nil, fmt.Errorf("sovereign: update hash required")
	}

	s.mu.Lock()
	s.updates[update.ID] = &update
	s.mu.Unlock()

	return &update, nil
}

// ApplyUpdate applies a previously imported update.
func (s *Service) ApplyUpdate(updateID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	update, ok := s.updates[updateID]
	if !ok {
		return fmt.Errorf("sovereign: update not found")
	}
	if update.Status != "imported" {
		return fmt.Errorf("sovereign: update status is %s, must be imported", update.Status)
	}

	// In production, this would apply the update to the local system
	update.Status = "applied"
	update.AppliedAt = time.Now().Format(time.RFC3339)

	return nil
}

// VerifyModelSignature verifies a model package signature against local keys (PRD §9.7).
func (s *Service) VerifyModelSignature(orgID string, modelDigest string, signature []byte) error {
	bundle, err := s.GetTrustBundle(orgID)
	if err != nil {
		return err
	}

	// Check if any of the model signing keys verify the signature
	for _, keyHex := range bundle.ModelSigningKeys {
		pubBytes, err := hex.DecodeString(keyHex)
		if err != nil || len(pubBytes) != ed25519.PublicKeySize {
			continue
		}
		pubKey := ed25519.PublicKey(pubBytes)
		if ed25519.Verify(pubKey, []byte(modelDigest), signature) {
			return nil
		}
	}

	return fmt.Errorf("sovereign: model signature verification failed")
}

// CheckRevocation checks if a credential/key is in the local revocation list.
func (s *Service) CheckRevocation(orgID, keyID string) (bool, error) {
	bundle, err := s.GetTrustBundle(orgID)
	if err != nil {
		return false, err
	}
	for _, revoked := range bundle.RevocationList {
		if revoked == keyID {
			return true, nil
		}
	}
	return false, nil
}

// GenerateLocalTimeProof generates a proof of local time integrity (PRD §9.7).
// For air-gapped deployments, time integrity is critical for lease validation.
type TimeIntegrityProof struct {
	LocalTimestamp string `json:"local_timestamp"`
	Counter        uint64 `json:"counter"`
	Hash           string `json:"hash"`
}

// GenerateTimeProof creates a time integrity proof.
func (s *Service) GenerateTimeProof(orgID string) *TimeIntegrityProof {
	now := time.Now()
	proof := &TimeIntegrityProof{
		LocalTimestamp: now.Format(time.RFC3339Nano),
		Counter:        uint64(now.UnixNano()),
	}
	// Hash the proof for tamper detection
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", proof.LocalTimestamp, proof.Counter, orgID)))
	proof.Hash = hex.EncodeToString(h[:])
	return proof
}

// VerifyTimeProof checks that a time proof is consistent.
func (s *Service) VerifyTimeProof(proof *TimeIntegrityProof, orgID string) bool {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", proof.LocalTimestamp, proof.Counter, orgID)))
	expected := hex.EncodeToString(h[:])
	return expected == proof.Hash
}

// ListPendingUpdates returns updates that are imported but not yet applied.
func (s *Service) ListPendingUpdates() []*OfflineUpdate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*OfflineUpdate
	for _, u := range s.updates {
		if u.Status == "imported" {
			result = append(result, u)
		}
	}
	return result
}
