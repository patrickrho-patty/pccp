package policy

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"gorm.io/gorm"
)

// Service manages policy epochs and capability leases.
type Service struct {
	db         *gorm.DB
	signingKey ed25519.PrivateKey
}

// New creates a new policy service.
func New(db *gorm.DB) (*Service, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("policy: generate signing key: %w", err)
	}
	return &Service{db: db, signingKey: priv}, nil
}

// SigningPublicKey returns the ed25519 public half of the lease/epoch
// signing key. The DARI listener transports it to enrolled harnesses
// in the AUTH_ACK payload so the connector can verify issued leases
// without a side channel.
func (s *Service) SigningPublicKey() ed25519.PublicKey {
	return s.signingKey.Public().(ed25519.PublicKey)
}

// CreatePolicyEpoch creates a new immutable policy epoch for an organization.
func (s *Service) CreatePolicyEpoch(orgID string, allowedModels []string, transitionMode string) (*models.PolicyEpoch, error) {
	// Get the next epoch number
	var maxEpoch models.PolicyEpoch
	s.db.Where("organization_id = ?", orgID).Order("epoch_number DESC").First(&maxEpoch)
	nextNum := uint64(1)
	if maxEpoch.ID != "" {
		nextNum = maxEpoch.EpochNumber + 1
	}

	modelsJSON := "[]"
	if len(allowedModels) > 0 {
		b, _ := json.Marshal(allowedModels)
		modelsJSON = string(b)
	}

	epoch := &models.PolicyEpoch{
		OrganizationID:    orgID,
		EpochID:           dari.GenerateID("epoch"),
		EpochNumber:       nextNum,
		OrgPolicyDigest:   s.computePolicyDigest(orgID, "org"),
		ModelPolicyDigest: s.computePolicyDigest(orgID, "model"),
		EngineVersion:     "1.0",
		AllowedModelsJSON: modelsJSON,
		TransitionMode:    transitionMode,
		EffectiveAt:       time.Now().Format(time.RFC3339),
		Status:            "active",
	}

	// Mark previous epoch as superseded
	if nextNum > 1 {
		s.db.Model(&models.PolicyEpoch{}).
			Where("organization_id = ? AND status = 'active'", orgID).
			Updates(map[string]interface{}{
				"status":         "superseded",
				"superseded_by":  epoch.EpochID,
			})
	}

	if err := s.db.Create(epoch).Error; err != nil {
		return nil, fmt.Errorf("policy: create epoch: %w", err)
	}
	return epoch, nil
}

// GetActiveEpoch returns the current active policy epoch for an organization.
func (s *Service) GetActiveEpoch(orgID string) (*models.PolicyEpoch, error) {
	var epoch models.PolicyEpoch
	if err := s.db.Where("organization_id = ? AND status = 'active'", orgID).
		Order("epoch_number DESC").First(&epoch).Error; err != nil {
		return nil, fmt.Errorf("policy: no active epoch for org %s", orgID)
	}
	return &epoch, nil
}

// IsModelAllowed checks if a model is allowed under the given policy epoch.
func (s *Service) IsModelAllowed(epochID, modelPackageID string) (bool, error) {
	var epoch models.PolicyEpoch
	if err := s.db.Where("epoch_id = ?", epochID).First(&epoch).Error; err != nil {
		return false, fmt.Errorf("policy: epoch not found")
	}
	var models []string
	json.Unmarshal([]byte(epoch.AllowedModelsJSON), &models)
	for _, m := range models {
		if m == modelPackageID {
			return true, nil
		}
	}
	return false, nil
}

// IssueCapabilityLease creates a signed capability lease for a session (DARI §22).
type IssueLeaseRequest struct {
	OrganizationID     string              `json:"organization_id"`
	SubjectPeerID      string              `json:"subject_peer_id"`
	UserID             string              `json:"user_id"`
	SessionID          string              `json:"session_id"`
	PolicyEpochID      string              `json:"policy_epoch_id"`
	AllowedModels      []string            `json:"allowed_models"`
	RepositoryScope    []map[string]string `json:"repository_scope,omitempty"`
	FilePathReadScope  []string            `json:"file_path_read_scope,omitempty"`
	FilePathWriteScope []string            `json:"file_path_write_scope,omitempty"`
	ToolClasses        []string            `json:"tool_classes,omitempty"`
	TokenBudget        int64               `json:"token_budget,omitempty"`
	Validity           time.Duration       `json:"validity"`
}

// IssueCapabilityLease creates a signed capability lease.
func (s *Service) IssueCapabilityLease(req IssueLeaseRequest) (*models.CapabilityLease, error) {
	allowedModels, _ := json.Marshal(req.AllowedModels)
	repoScope, _ := json.Marshal(req.RepositoryScope)
	readScope, _ := json.Marshal(req.FilePathReadScope)
	writeScope, _ := json.Marshal(req.FilePathWriteScope)
	tools, _ := json.Marshal(req.ToolClasses)

	filePathScope, _ := json.Marshal(map[string]interface{}{
		"read":  req.FilePathReadScope,
		"write": req.FilePathWriteScope,
	})

	// Truncate to whole seconds so the RFC3339 storage columns and the
	// signed/wire UnixMilli values agree exactly on roundtrip — the
	// connector recomputes the signature from the wire fields.
	now := time.Now().Truncate(time.Second)
	notAfter := now.Add(req.Validity).Truncate(time.Second)
	lease := &models.CapabilityLease{
		OrganizationID:       req.OrganizationID,
		LeaseID:              dari.GenerateID("lease"),
		SubjectPeerID:        req.SubjectPeerID,
		UserID:               req.UserID,
		SessionID:            req.SessionID,
		PolicyEpochID:        req.PolicyEpochID,
		AllowedModelPackages: string(allowedModels),
		RepositoryScope:      string(repoScope),
		FilePathScope:        string(filePathScope),
		ToolClasses:          string(tools),
		TokenBudget:          req.TokenBudget,
		ProtectionProfile:    "P0",
		NotBefore:            now.Format(time.RFC3339),
		NotAfter:             notAfter.Format(time.RFC3339),
		LeaseSequence:        1,
		IssuedAt:             now.Format(time.RFC3339),
		Status:               "active",
	}

	// CP signs the lease using COSE-Sign1 (DARI §22). The signed body
	// is the canonical, domain-separated, length-prefixed layout the
	// connector's LeaseVerifier recomputes (see canonical.go + the
	// cross-repo lease conformance suite) — it binds every scope field,
	// the token budget, the validity window, and the sequence number.
	canonical := CanonicalLeaseSigningBytes(LeaseSigningInput{
		LeaseID:            lease.LeaseID,
		SubjectPeerID:      lease.SubjectPeerID,
		UserID:             lease.UserID,
		SessionID:          lease.SessionID,
		PolicyEpochID:      lease.PolicyEpochID,
		AllowedModels:      req.AllowedModels,
		FilePathReadScope:  req.FilePathReadScope,
		FilePathWriteScope: req.FilePathWriteScope,
		ToolClasses:        req.ToolClasses,
		RepositoryScope:    req.RepositoryScope,
		TokenBudget:        lease.TokenBudget,
		NotBeforeUnixMs:    now.UnixMilli(),
		NotAfterUnixMs:     notAfter.UnixMilli(),
		LeaseSequence:      uint64(lease.LeaseSequence),
		IssuedAtUnixMs:     now.UnixMilli(),
	})
	sign1, err := dari.CreateCOSESign1(canonical, s.signingKey, []byte("pccp-policy"))
	if err != nil {
		return nil, fmt.Errorf("policy: sign lease: %w", err)
	}
	encoded, err := dari.EncodeCOSESign1(sign1)
	if err != nil {
		return nil, fmt.Errorf("policy: encode lease signature: %w", err)
	}
	lease.CPSignature = hex.EncodeToString(encoded)

	_ = readScope
	_ = writeScope

	if err := s.db.Create(lease).Error; err != nil {
		return nil, fmt.Errorf("policy: create capability lease: %w", err)
	}
	return lease, nil
}

// ValidateCapabilityLease checks whether a capability lease is valid.
func (s *Service) ValidateCapabilityLease(leaseID, peerID, sessionID string) (*models.CapabilityLease, error) {
	var lease models.CapabilityLease
	if err := s.db.Where("lease_id = ?", leaseID).First(&lease).Error; err != nil {
		return nil, fmt.Errorf("policy: lease not found")
	}
	if lease.Status != "active" {
		return nil, fmt.Errorf("policy: lease status is %s", lease.Status)
	}
	if lease.SubjectPeerID != peerID {
		return nil, fmt.Errorf("policy: lease peer mismatch")
	}
	if sessionID != "" && lease.SessionID != sessionID {
		return nil, fmt.Errorf("policy: lease session mismatch")
	}

	notAfter, _ := time.Parse(time.RFC3339, lease.NotAfter)
	if time.Now().After(notAfter) {
		s.db.Model(&lease).Update("status", "expired")
		return nil, fmt.Errorf("policy: lease expired")
	}

	return &lease, nil
}

// RevokeCapabilityLease revokes a lease.
func (s *Service) RevokeCapabilityLease(leaseID string) error {
	result := s.db.Model(&models.CapabilityLease{}).
		Where("lease_id = ?", leaseID).
		Update("status", "revoked")
	return result.Error
}

func (s *Service) computePolicyDigest(orgID, domain string) string {
	data := fmt.Sprintf("%s|%s|%d", orgID, domain, time.Now().UnixNano())
	h := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(h[:])
}
