package keymgmt

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Service implements Cryptography and Key Management (PRD §36).
type Service struct {
	mu       sync.RWMutex
	keyStore map[string]*KeyEntry
}

// KeyDomain identifies a cryptographic key domain (PRD §36.1).
type KeyDomain string

const (
	DomainTransport    KeyDomain = "transport"     // TLS/QUIC transport keys
	DomainPeerIdentity KeyDomain = "peer_identity" // PPC signing
	DomainModelSigning KeyDomain = "model_signing" // PMP signing
	DomainEvidence     KeyDomain = "evidence"      // evidence receipt signing
	DomainLease        KeyDomain = "lease"         // capability/endpoint lease signing
	DomainModelDecrypt KeyDomain = "model_decrypt" // model package decryption
	DomainCommsE2EE    KeyDomain = "comms_e2ee"    // communication end-to-end encryption
)

// KeyEntry represents a managed key.
type KeyEntry struct {
	ID         string
	Domain     KeyDomain
	Algorithm  string // ed25519, aes-256-gcm
	PublicKey  []byte
	PrivateKey []byte // never exported in production (HSM/KMS)
	CreatedAt  time.Time
	ExpiresAt  time.Time
	RotatedFrom string // previous key ID if rotated
	Status     string  // active, rotated, revoked
}

// New creates a new key management service.
func New() *Service {
	return &Service{
		keyStore: make(map[string]*KeyEntry),
	}
}

// GenerateKey generates a new Ed25519 key pair for a domain.
func (s *Service) GenerateKey(domain KeyDomain, validity time.Duration) (*KeyEntry, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("keymgmt: generate key: %w", err)
	}

	now := time.Now()
	entry := &KeyEntry{
		ID:         generateKeyID(domain),
		Domain:     domain,
		Algorithm:  "ed25519",
		PublicKey:  pub,
		PrivateKey: priv,
		CreatedAt:  now,
		ExpiresAt:  now.Add(validity),
		Status:     "active",
	}

	s.mu.Lock()
	s.keyStore[entry.ID] = entry
	s.mu.Unlock()

	return entry, nil
}

// GetKey retrieves a key by ID.
func (s *Service) GetKey(keyID string) (*KeyEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.keyStore[keyID]
	if !ok {
		return nil, fmt.Errorf("keymgmt: key %s not found", keyID)
	}
	return entry, nil
}

// GetActiveKey returns the active key for a domain.
func (s *Service) GetActiveKey(domain KeyDomain) (*KeyEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, entry := range s.keyStore {
		if entry.Domain == domain && entry.Status == "active" {
			if time.Now().Before(entry.ExpiresAt) {
				return entry, nil
			}
		}
	}
	return nil, fmt.Errorf("keymgmt: no active key for domain %s", domain)
}

// RotateKey generates a new key for a domain and marks the old one as rotated.
func (s *Service) RotateKey(domain KeyDomain, validity time.Duration) (*KeyEntry, error) {
	// Get current active key
	oldKey, _ := s.GetActiveKey(domain)

	// Generate new key
	newKey, err := s.GenerateKey(domain, validity)
	if err != nil {
		return nil, err
	}

	// Mark old key as rotated
	if oldKey != nil {
		s.mu.Lock()
		oldKey.Status = "rotated"
		newKey.RotatedFrom = oldKey.ID
		s.mu.Unlock()
	}

	return newKey, nil
}

// RevokeKey revokes a key.
func (s *Service) RevokeKey(keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.keyStore[keyID]
	if !ok {
		return fmt.Errorf("keymgmt: key %s not found", keyID)
	}
	entry.Status = "revoked"
	return nil
}

// ListKeys returns all keys for a domain.
func (s *Service) ListKeys(domain KeyDomain) []*KeyEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*KeyEntry
	for _, entry := range s.keyStore {
		if domain == "" || entry.Domain == domain {
			result = append(result, entry)
		}
	}
	return result
}

// KeyThumbprint computes a SHA-256 thumbprint of a public key.
func KeyThumbprint(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:])
}

// EnterpriseKeyOptions defines enterprise key deployment options (PRD §36.2).
type EnterpriseKeyOptions struct {
	UseHSM         bool   `json:"use_hsm"`
	UseKMS         bool   `json:"use_kms"`
	KMSProvider    string `json:"kms_provider,omitempty"` // aws-kms, gcp-kms, azure-keyvault
	KeyEscrow      bool   `json:"key_escrow"`
	RotationPeriod string `json:"rotation_period"` // e.g. "90d"
}

// DefaultEnterpriseKeyOptions returns default enterprise key options.
func DefaultEnterpriseKeyOptions() EnterpriseKeyOptions {
	return EnterpriseKeyOptions{
		UseHSM:         false,
		UseKMS:         false,
		RotationPeriod: "90d",
	}
}

// SovereignKeyOptions returns government/sovereign key options (PRD §36.2).
func SovereignKeyOptions() EnterpriseKeyOptions {
	return EnterpriseKeyOptions{
		UseHSM:         true,
		UseKMS:         true,
		KMSProvider:    "local-hsm", // customer-local HSM
		KeyEscrow:      false,
		RotationPeriod: "30d",
	}
}

func generateKeyID(domain KeyDomain) string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("key_%s_%s", domain, hex.EncodeToString(b))
}
