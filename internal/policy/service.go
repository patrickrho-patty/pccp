package policy

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keys"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service manages policy epochs and capability leases.
type Service struct {
	db         *gorm.DB
	signingKey ed25519.PrivateKey
}

// New creates a new policy service.
func New(db *gorm.DB) (*Service, error) {
	priv, err := keys.LoadOrCreate(db, "policy-issuer")
	if err != nil {
		return nil, fmt.Errorf("policy: load signing key: %w", err)
	}
	return &Service{db: db, signingKey: priv}, nil
}

// SigningPrivateKey returns the policy issuer's private signing key.
// Callers are the relay's in-process issuance paths only; never log it.
func (s *Service) SigningPrivateKey() ed25519.PrivateKey {
	return s.signingKey
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
		// Full-domain digests (06 A2): every policy domain commits its
		// enabled rules into the epoch — a change in ANY domain moves
		// the corresponding digest (empty domain = its zero digest).
		DLPSecurityDigest:     s.computePolicyDigest(orgID, "data"),
		ApprovalMatrixDigest:  s.computePolicyDigest(orgID, "tools"),
		RetentionPolicyDigest: s.computePolicyDigest(orgID, "session"),
		ProjectOverlayDigest:  s.computePolicyDigest(orgID, "project"),
		EngineVersion:         "1.0",
		AllowedModelsJSON:     modelsJSON,
		TransitionMode:        transitionMode,
		EffectiveAt:           time.Now().Format(time.RFC3339),
		Status:                "active",
	}

	// Supersede + create commit atomically: superseding before the new
	// epoch exists (or failing between the two statements) would leave
	// the org with zero active epochs — every governed session open
	// fail-closes. A transaction guarantees readers never observe the
	// intermediate state and a failed create rolls the supersede back.
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if nextNum > 1 {
			if err := tx.Model(&models.PolicyEpoch{}).
				Where("organization_id = ? AND status = 'active'", orgID).
				Updates(map[string]interface{}{
					"status":        "superseded",
					"superseded_by": epoch.EpochID,
				}).Error; err != nil {
				return err
			}
		}
		return tx.Create(epoch).Error
	})
	if err != nil {
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
	// ServicePrincipal is set only by in-process non-human transports such as
	// the verified browser binding. It is deliberately excluded from JSON so
	// the public lease endpoint cannot opt out of managed-user validation.
	ServicePrincipal bool `json:"-"`
}

// IssueCapabilityLease creates a signed capability lease.
func (s *Service) IssueCapabilityLease(req IssueLeaseRequest) (*models.CapabilityLease, error) {
	var lease *models.CapabilityLease
	err := s.db.Transaction(func(tx *gorm.DB) error {
		created, err := s.IssueCapabilityLeaseWithDB(tx, req)
		lease = created
		return err
	})
	return lease, err
}

// IssueCapabilityLeaseWithDB validates and persists a lease under the
// caller's transaction, serializing human capability issuance with lifecycle
// transitions through the managed user's row lock.
func (s *Service) IssueCapabilityLeaseWithDB(db *gorm.DB, req IssueLeaseRequest) (*models.CapabilityLease, error) {
	if err := validateLeaseBinding(db, req); err != nil {
		return nil, err
	}
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

	if err := db.Create(lease).Error; err != nil {
		return nil, fmt.Errorf("policy: create capability lease: %w", err)
	}
	return lease, nil
}

func validateLeaseBinding(db *gorm.DB, req IssueLeaseRequest) error {
	if strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.SubjectPeerID) == "" || strings.TrimSpace(req.PolicyEpochID) == "" {
		return fmt.Errorf("policy: organization_id, subject_peer_id, and policy_epoch_id are required")
	}
	if req.Validity <= 0 {
		return fmt.Errorf("policy: positive validity is required")
	}
	var epoch models.PolicyEpoch
	if err := db.Where("organization_id = ? AND epoch_id = ? AND status = ?", req.OrganizationID, req.PolicyEpochID, "active").First(&epoch).Error; err != nil {
		return fmt.Errorf("policy: active epoch not found in organization")
	}
	if !req.ServicePrincipal {
		if strings.TrimSpace(req.UserID) == "" {
			return fmt.Errorf("policy: managed user_id is required")
		}
		if _, err := identity.LockActiveUser(db, req.OrganizationID, req.UserID); err != nil {
			return fmt.Errorf("policy: user binding rejected: %w", err)
		}
	}

	var session models.Session
	sessionErr := db.Where("organization_id = ? AND session_id = ?", req.OrganizationID, req.SessionID).First(&session).Error
	if sessionErr == nil {
		if !models.SessionIsLive(session.Status) || session.UserID != req.UserID || session.HarnessID != req.SubjectPeerID {
			return fmt.Errorf("policy: session identity binding mismatch")
		}
		return nil
	}
	if sessionErr != nil && !errors.Is(sessionErr, gorm.ErrRecordNotFound) {
		return sessionErr
	}

	// Native DARI and browser transports mint before a console Session row is
	// available, so the authenticated harness binding is the authority there.
	if req.ServicePrincipal {
		var harness models.Harness
		if err := db.Where("organization_id = ? AND harness_id = ? AND status IN ?", req.OrganizationID, req.SubjectPeerID, []string{"enrolled", "active"}).First(&harness).Error; err != nil {
			return fmt.Errorf("policy: enrolled subject peer not found in organization")
		}
		return nil
	}
	if err := identity.ValidateActiveHarnessUserBinding(db, req.OrganizationID, req.SubjectPeerID, req.UserID); err != nil {
		return fmt.Errorf("policy: subject peer binding rejected: %w", err)
	}
	return nil
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

// computePolicyDigest derives the content digest of an org's ACTIVE
// rule set for a domain. The digest covers the actual rules that will
// be enforced — ID, scope, and canonical config — sorted
// deterministically so an identical rule set always yields the
// identical digest, and any rule change (create/enable/disable/edit)
// changes it. An org with no rules in the domain digests the empty
// set (a stable, honest value), not a timestamp.
func (s *Service) computePolicyDigest(orgID, domain string) string {
	var rules []models.PolicyRule
	s.db.Where("organization_id = ? AND domain = ? AND enabled = ?", orgID, domain, true).
		Order("id ASC").Find(&rules)

	h := sha256.New()
	fmt.Fprintf(h, "DARI-POLICY-DIGEST-v1\x00")
	fmt.Fprintf(h, "org=%s;domain=%s;rules=%d\x00", orgID, domain, len(rules))
	for _, r := range rules {
		fmt.Fprintf(h, "rule=%s;scope=%s/%s;config=%s\x00", r.ID, r.Scope, r.ScopeName, canonicalJSONString(r.ConfigJSON))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// canonicalJSONString normalizes a stored JSON config so key-order
// differences in storage do not change the digest. Unparseable
// configs digest as their literal bytes (a change is a change).
func canonicalJSONString(s string) string {
	if s == "" {
		return "{}"
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		return s
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		return s
	}
	return string(out)
}

// --- Rule lifecycle (policy C1 §46.2) ---

// ApproveRule publishes a draft rule into the active policy: the rule
// is approved and the org's epoch is rebuilt so enforcement matches
// the authored set.
func (s *Service) ApproveRule(orgID, ruleID string) (*models.PolicyEpoch, error) {
	result := s.db.Model(&models.PolicyRule{}).
		Where("id = ? AND organization_id = ? AND status = ?", ruleID, orgID, "draft").
		Update("status", "approved")
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("policy: rule not found or not in draft")
	}
	s.recordAudit(orgID, "cp.policy.rule_approved", "admin", "policy_rule", ruleID, "rule approved")
	return s.RebuildEpochFromRules(orgID, "immediate", false)
}

// RejectRule denies a draft rule (removes it).
func (s *Service) RejectRule(orgID, ruleID string) error {
	return s.db.Where("id = ? AND organization_id = ?", ruleID, orgID).
		Delete(&models.PolicyRule{}).Error
}

// BulkSetRules enables/disables many rules at once and rebuilds the
// epoch once (policy UX12).
func (s *Service) BulkSetRules(orgID string, ids []string, enabled bool) (*models.PolicyEpoch, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("policy: no rule ids")
	}
	if err := s.db.Model(&models.PolicyRule{}).
		Where("organization_id = ? AND id IN ?", orgID, ids).
		Update("enabled", enabled).Error; err != nil {
		return nil, err
	}
	return s.RebuildEpochFromRules(orgID, "immediate", false)
}
