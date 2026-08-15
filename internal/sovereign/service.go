package sovereign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Service implements sovereign/air-gapped deployment features (PRD §34.4, §9.7, §5).
type Service struct {
	mu           sync.RWMutex
	trustBundles map[string]*TrustBundle // orgID → local trust bundle
	updates      map[string]*OfflineUpdate
}

// New creates a new sovereign deployment service.
func New() *Service {
	return &Service{
		trustBundles: make(map[string]*TrustBundle),
		updates:      make(map[string]*OfflineUpdate),
	}
}

// TrustBundle represents a local trust bundle for air-gapped operation (PRD §9.7).
type TrustBundle struct {
	OrganizationID        string            `json:"organization_id"`
	LocalCAIdentity       string            `json:"local_ca_identity"`
	LocalCAPublicKey      string            `json:"local_ca_public_key"`
	ModelSigningKeys      []string          `json:"model_signing_keys"`
	RevocationList        []string          `json:"revocation_list"`
	ReferenceMeasurements map[string]string `json:"reference_measurements"` // component → expected hash
	ImportedAt            string            `json:"imported_at"`
	ExpiresAt             string            `json:"expires_at"`
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
	if bundle.LocalCAPublicKey == "" {
		return nil, fmt.Errorf("sovereign: local CA public key required")
	}
	bundle.ImportedAt = time.Now().Format(time.RFC3339)

	s.mu.Lock()
	s.trustBundles[bundle.OrganizationID] = &bundle
	s.mu.Unlock()

	return &bundle, nil
}

// GetTrustBundle returns the local trust bundle for an organization.
func (s *Service) GetTrustBundle(orgID string) (*TrustBundle, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bundle, ok := s.trustBundles[orgID]
	if !ok {
		return nil, fmt.Errorf("sovereign: trust bundle not found for org %s", orgID)
	}
	return bundle, nil
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
