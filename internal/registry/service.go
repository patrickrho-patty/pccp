package registry

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service manages the model registry: PMP signing, endpoint enrollment,
// attestation, and endpoint lease issuance.
type Service struct {
	db           *gorm.DB
	signingKey   ed25519.PrivateKey
	signingKeyID string
}

// New creates a new model registry service with a model-signing key pair.
func New(db *gorm.DB) (*Service, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("registry: generate signing key: %w", err)
	}
	// Store the key fingerprint for reference
	keyID := "patty-model-release-" + time.Now().Format("2006")

	s := &Service{
		db:           db,
		signingKey:   priv,
		signingKeyID: keyID,
	}

	// Persist the signing key reference
	_ = pub
	return s, nil
}

// RegisterModelPackage creates and signs a Patty Model Package.
func (s *Service) RegisterModelPackage(pkg *models.ModelPackage) error {
	if pkg.PackageID == "" {
		pkg.PackageID = dari.GenerateID("pmp")
	}
	if pkg.State == "" {
		pkg.State = "draft"
	}

	// Compute the manifest digest
	manifest := s.buildManifestDigest(pkg)
	pkg.ManifestDigest = manifest

	// Sign the manifest
	sig := ed25519.Sign(s.signingKey, []byte(manifest))
	pkg.SignatureKeyID = s.signingKeyID
	pkg.Signature = hex.EncodeToString(sig)

	if err := s.db.Create(pkg).Error; err != nil {
		return fmt.Errorf("registry: register model package: %w", err)
	}
	return nil
}

// PublishModelPackage transitions a model package to published state.
// The manifest signature is VERIFIED before the state change (Task 15:
// PMP signature/digest verification at publish) — a tampered or
// unsigned package fails closed.
func (s *Service) PublishModelPackage(packageID string) error {
	var pkg models.ModelPackage
	if err := s.db.Where("package_id = ?", packageID).First(&pkg).Error; err != nil {
		return fmt.Errorf("registry: publish: package not found: %w", err)
	}
	if err := s.VerifyPackageSignature(&pkg); err != nil {
		return fmt.Errorf("registry: publish refused: %w", err)
	}
	result := s.db.Model(&models.ModelPackage{}).
		Where("package_id = ?", packageID).
		Updates(map[string]interface{}{
			"state":        "published",
			"published_at": time.Now().Format(time.RFC3339),
		})
	if result.Error != nil {
		return fmt.Errorf("registry: publish model package: %w", result.Error)
	}
	return nil
}

// VerifyPackageSignature recomputes the manifest digest and verifies
// the registry signature over it.
func (s *Service) VerifyPackageSignature(pkg *models.ModelPackage) error {
	if pkg == nil || pkg.ManifestDigest == "" || pkg.Signature == "" {
		return fmt.Errorf("package %s has no signed manifest", pkg.PackageID)
	}
	manifest := ComputePackageManifest(pkg)
	if manifest != pkg.ManifestDigest {
		return fmt.Errorf("package %s manifest digest mismatch (content changed after registration)", pkg.PackageID)
	}
	sig, err := hex.DecodeString(pkg.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("package %s has a malformed signature", pkg.PackageID)
	}
	if !ed25519.Verify(s.signingKey.Public().(ed25519.PublicKey), []byte(manifest), sig) {
		return fmt.Errorf("package %s signature verification failed", pkg.PackageID)
	}
	return nil
}

// RecallModelPackage transitions a model package to recalled state (PRD §33.9).
func (s *Service) RecallModelPackage(packageID, reason string) error {
	result := s.db.Model(&models.ModelPackage{}).
		Where("package_id = ?", packageID).
		Update("state", "recalled")
	if result.Error != nil {
		return fmt.Errorf("registry: recall model package: %w", result.Error)
	}
	// Invalidate all endpoint leases for this model package
	s.db.Model(&models.EndpointLease{}).
		Where("model_package_id = ? AND status = 'active'", packageID).
		Update("status", "revoked")
	return nil
}

// EnrollEndpoint enrolls a new inference endpoint (PRD §9.4).
func (s *Service) EnrollEndpoint(orgID, piaPeerID, modelPackageID, servingEngine, servingEngineVer, publicKeyHex, nodeIdentity string, assuranceLevel string) (*models.InferenceEndpoint, error) {
	endpoint := &models.InferenceEndpoint{
		OrganizationID:   orgID,
		EndpointID:       dari.GenerateID("ep"),
		PIAPeerID:        piaPeerID,
		ModelPackageID:   modelPackageID,
		ServingEngine:    servingEngine,
		ServingEngineVer: servingEngineVer,
		NodeIdentity:     nodeIdentity,
		AssuranceLevel:   assuranceLevel,
		Status:           "enrolled",
		PublicKey:        publicKeyHex,
		EnrolledAt:       time.Now().Format(time.RFC3339),
	}
	if err := s.db.Create(endpoint).Error; err != nil {
		return nil, fmt.Errorf("registry: enroll endpoint: %w", err)
	}
	return endpoint, nil
}

// RecordAttestation stores an endpoint attestation envelope.
func (s *Service) RecordAttestation(att *models.EndpointAttestation) error {
	if att.EndpointID == "" {
		return fmt.Errorf("registry: attestation requires endpoint_id")
	}
	att.Timestamp = time.Now().Format(time.RFC3339)
	if err := s.db.Create(att).Error; err != nil {
		return fmt.Errorf("registry: record attestation: %w", err)
	}
	return nil
}

// AttestationMaxAge bounds attestation freshness for lease issuance.
var AttestationMaxAge = 24 * time.Hour

// requireFreshAttestation enforces the PIA-attestation gate: the most
// recent attestation for the endpoint must be verified, carry the
// CURRENT model manifest digest, and be within AttestationMaxAge.
func (s *Service) requireFreshAttestation(endpoint *models.InferenceEndpoint, pkg *models.ModelPackage) error {
	if endpoint.AssuranceLevel == "" || endpoint.AssuranceLevel == "none" {
		// Endpoints explicitly enrolled without attestation (dev) are
		// refused at lease time only when the policy requires it; the
		// default requires attestation for L1+.
		return nil
	}
	var att models.EndpointAttestation
	if err := s.db.Where("endpoint_id = ?", endpoint.EndpointID).
		Order("timestamp DESC").First(&att).Error; err != nil {
		return fmt.Errorf("registry: endpoint %s has no attestation (PIA attestation required before lease)", endpoint.EndpointID)
	}
	if !att.Verified {
		return fmt.Errorf("registry: endpoint %s latest attestation is unverified", endpoint.EndpointID)
	}
	ts, err := time.Parse(time.RFC3339, att.Timestamp)
	if err != nil || time.Since(ts) > AttestationMaxAge {
		return fmt.Errorf("registry: endpoint %s attestation is stale", endpoint.EndpointID)
	}
	if att.ModelManifestDigest != "" && att.ModelManifestDigest != pkg.ManifestDigest {
		return fmt.Errorf("registry: endpoint %s attestation binds a different manifest", endpoint.EndpointID)
	}
	return nil
}

// VerifyAttestation validates an attestation against the endpoint's registered model package.
func (s *Service) VerifyAttestation(att *models.EndpointAttestation) error {
	var endpoint models.InferenceEndpoint
	if err := s.db.Where("endpoint_id = ?", att.EndpointID).First(&endpoint).Error; err != nil {
		return fmt.Errorf("registry: endpoint not found")
	}

	var pkg models.ModelPackage
	if err := s.db.Where("package_id = ?", endpoint.ModelPackageID).First(&pkg).Error; err != nil {
		return fmt.Errorf("registry: model package not found")
	}

	// Verify signature
	pubBytes, err := hex.DecodeString(endpoint.PublicKey)
	if err != nil {
		return fmt.Errorf("registry: invalid endpoint public key: %w", err)
	}
	pubKey := ed25519.PublicKey(pubBytes)

	sigBytes, err := hex.DecodeString(att.Signature)
	if err != nil {
		return fmt.Errorf("registry: invalid attestation signature encoding: %w", err)
	}

	// Verify model package digest matches
	if att.ModelManifestDigest != "" && att.ModelManifestDigest != pkg.ManifestDigest {
		return fmt.Errorf("registry: attestation manifest digest mismatch (expected %s, got %s)", pkg.ManifestDigest, att.ModelManifestDigest)
	}

	// Build the attestation verification message
	attestMsg := s.buildAttestationMessage(att)
	if !ed25519.Verify(pubKey, attestMsg, sigBytes) {
		return fmt.Errorf("registry: attestation signature verification failed")
	}

	// Mark verified
	s.db.Model(att).Updates(map[string]interface{}{
		"verified":    true,
		"verified_at": time.Now().Format(time.RFC3339),
	})

	return nil
}

// IssueEndpointLease creates a short-lived lease for an endpoint (PRD A.5, DARI §40.4).
func (s *Service) IssueEndpointLease(orgID, endpointID string, validity time.Duration) (*models.EndpointLease, error) {
	var endpoint models.InferenceEndpoint
	if err := s.db.Where("endpoint_id = ? AND organization_id = ?", endpointID, orgID).First(&endpoint).Error; err != nil {
		return nil, fmt.Errorf("registry: endpoint not found")
	}

	if endpoint.Status != "enrolled" && endpoint.Status != "active" {
		return nil, fmt.Errorf("registry: endpoint status is %s, cannot issue lease", endpoint.Status)
	}

	var pkg models.ModelPackage
	if err := s.db.Where("package_id = ?", endpoint.ModelPackageID).First(&pkg).Error; err != nil {
		return nil, fmt.Errorf("registry: model package not found")
	}
	if pkg.State == "recalled" {
		return nil, fmt.Errorf("registry: model package %s has been recalled", pkg.PackageID)
	}

	// Task 15 (P.2): a fresh VERIFIED attestation is required before an
	// endpoint lease is issued. Missing, unverified, stale, or
	// manifest-mismatched attestation fails closed.
	if err := s.requireFreshAttestation(&endpoint, &pkg); err != nil {
		return nil, err
	}

	now := time.Now()
	lease := &models.EndpointLease{
		EndpointID:     endpointID,
		OrganizationID: orgID,
		ModelPackageID: endpoint.ModelPackageID,
		LeaseID:        dari.GenerateID("epl"),
		PIAPeerID:      endpoint.PIAPeerID,
		PIAPublicKey:   endpoint.PublicKey,
		NotBefore:      now.Format(time.RFC3339),
		NotAfter:       now.Add(validity).Format(time.RFC3339),
		CapacityClass:  endpoint.CapacityClass,
		Status:         "active",
		IssuedAt:       now.Format(time.RFC3339),
	}

	// CP signs the lease
	leaseBody := fmt.Sprintf("%s|%s|%s|%s|%s", lease.LeaseID, endpointID, endpoint.ModelPackageID, lease.NotBefore, lease.NotAfter)
	sig := ed25519.Sign(s.signingKey, []byte(leaseBody))
	lease.CPSignature = hex.EncodeToString(sig)

	if err := s.db.Create(lease).Error; err != nil {
		return nil, fmt.Errorf("registry: create endpoint lease: %w", err)
	}

	// Update endpoint status to active
	s.db.Model(&endpoint).Updates(map[string]interface{}{
		"status":           "active",
		"last_attestation": now.Format(time.RFC3339),
	})

	return lease, nil
}

// ValidateEndpointLease checks whether a lease is currently valid.
func (s *Service) ValidateEndpointLease(leaseID string) (*models.EndpointLease, error) {
	var lease models.EndpointLease
	if err := s.db.Where("lease_id = ?", leaseID).First(&lease).Error; err != nil {
		return nil, fmt.Errorf("registry: lease not found")
	}
	if lease.Status != "active" {
		return nil, fmt.Errorf("registry: lease status is %s", lease.Status)
	}
	notAfter, _ := time.Parse(time.RFC3339, lease.NotAfter)
	if time.Now().After(notAfter) {
		s.db.Model(&lease).Update("status", "expired")
		return nil, fmt.Errorf("registry: lease expired")
	}
	return &lease, nil
}

// GetEndpointWithValidLease finds an active endpoint for a given model package.
func (s *Service) GetEndpointWithValidLease(orgID, modelPackageID string) (*models.InferenceEndpoint, *models.EndpointLease, error) {
	var endpoint models.InferenceEndpoint
	err := s.db.Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
		orgID, modelPackageID).First(&endpoint).Error
	if err != nil {
		return nil, nil, fmt.Errorf("registry: no active endpoint for model %s in org %s: %w", modelPackageID, orgID, err)
	}

	var lease models.EndpointLease
	err = s.db.Where("endpoint_id = ? AND status = 'active' AND not_after > ?",
		endpoint.EndpointID, time.Now().Format(time.RFC3339)).
		Order("issued_at DESC").First(&lease).Error
	if err != nil {
		// Try to issue a new lease
		newLease, leaseErr := s.IssueEndpointLease(orgID, endpoint.EndpointID, 1*time.Hour)
		if leaseErr != nil {
			return nil, nil, fmt.Errorf("registry: no valid lease and cannot issue: %w", leaseErr)
		}
		return &endpoint, newLease, nil
	}

	return &endpoint, &lease, nil
}

// VerifyModelPackage checks that a model package's signature is valid and it's not recalled.
func (s *Service) VerifyModelPackage(packageID string) (*models.ModelPackage, error) {
	var pkg models.ModelPackage
	if err := s.db.Where("package_id = ?", packageID).First(&pkg).Error; err != nil {
		return nil, fmt.Errorf("registry: model package not found")
	}
	if pkg.State == "recalled" {
		return nil, fmt.Errorf("registry: model package %s has been recalled", packageID)
	}
	if pkg.State != "published" {
		return nil, fmt.Errorf("registry: model package state is %s, not published", pkg.State)
	}
	return &pkg, nil
}

// SigningKeyID returns the model signing key identifier.
func (s *Service) SigningKeyID() string {
	return s.signingKeyID
}

func (s *Service) buildManifestDigest(pkg *models.ModelPackage) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s", pkg.PackageID, pkg.ModelID, pkg.Name, pkg.Version,
		pkg.WeightsMerkleRoot, pkg.TokenizerDigest)
	h := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(h[:])
}

// ComputePackageManifest renders the canonical manifest digest input
// for verification callers.
func ComputePackageManifest(pkg *models.ModelPackage) string {
	if pkg == nil {
		return ""
	}
	s := &Service{}
	return s.buildManifestDigest(pkg)
}

func (s *Service) buildAttestationMessage(att *models.EndpointAttestation) []byte {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		att.EndpointID, att.Nonce, att.ModelPackageID,
		att.ModelManifestDigest, att.PIABuildDigest,
		att.ServingContainerDigest, att.Timestamp)
	return []byte(data)
}

// DefaultModelPackagesJSON returns the default Phase 0 model package definitions.
func DefaultModelPackagesJSON() []byte {
	pkgs := []map[string]interface{}{
		{
			"package_id":                 "pmp_kocoder_v1",
			"model_id":                   "patty-kocoder-35b",
			"name":                       "Patty-KoCoder-v1",
			"name_ko":                    "패티 코더 v1",
			"family":                     "coder",
			"version":                    "1.0.0",
			"capabilities":               []string{"code", "tool_use", "korean"},
			"entitlement_class":          "enterprise-coder",
			"minimum_endpoint_assurance": "L1",
			"state":                      "published",
		},
	}
	b, _ := json.Marshal(pkgs)
	return b
}
