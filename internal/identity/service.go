package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/patrickrho-patty/pccp/internal/config"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service handles identity, enrollment, and authentication operations.
type Service struct {
	db          *gorm.DB
	ca          *dari.PeerCredentialIssuer
	revocations *CredentialRevocations
}

// New creates a new identity service. It initializes or loads the control
// plane's CA key pair for issuing PPCs.
func New(db *gorm.DB) (*Service, error) {
	ca, err := dari.NewPeerCredentialIssuer("pccp-ca")
	if err != nil {
		return nil, fmt.Errorf("identity: init CA: %w", err)
	}
	return &Service{db: db, ca: ca, revocations: newCredentialRevocations(db)}, nil
}

// CACAPublicKey returns the CA's public key (hex-encoded).
func (s *Service) CAPublicKeyHex() string {
	return hex.EncodeToString(s.ca.PublicKey)
}

// CAIssuerID returns the credential issuer identifier ("pccp-ca").
func (s *Service) CAIssuerID() string { return s.ca.IssuerID }

// CAPublicKeyRaw returns the CA's ed25519 public key bytes. The DARI
// listener builds its trust bundle from this.
func (s *Service) CAPublicKeyRaw() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), s.ca.PublicKey...)
}

// CreateOrganization creates a new organization.
func (s *Service) CreateOrganization(name, nameKo, slug, profile string) (*models.Organization, error) {
	org := &models.Organization{
		Name:    name,
		NameKo:  nameKo,
		Slug:    slug,
		Profile: profile,
		Status:  "active",
		Type:    profile,
	}
	if err := s.db.Create(org).Error; err != nil {
		return nil, fmt.Errorf("identity: create org: %w", err)
	}
	return org, nil
}

// CreateUser creates a new user in an organization.
func (s *Service) CreateUser(orgID, email, name, nameKo, authMethod, externalID string) (*models.User, error) {
	user := &models.User{
		AuditBase: models.AuditBase{
			Base:           models.Base{},
			OrganizationID: orgID,
			Classification: "internal",
		},
		Email:      email,
		Name:       name,
		NameKo:     nameKo,
		Status:     "active",
		AuthMethod: authMethod,
		ExternalID: externalID,
		Locale:     "ko-KR",
		Timezone:   "Asia/Seoul",
	}
	if err := s.db.Create(user).Error; err != nil {
		return nil, fmt.Errorf("identity: create user: %w", err)
	}
	return user, nil
}

// EnrollHarnessRequest contains the information needed to enroll a harness.
type EnrollHarnessRequest struct {
	OrganizationID   string `json:"organization_id"`
	UserID           string `json:"user_id"`
	HarnessID        string `json:"harness_id"`
	PublicKeyHex     string `json:"public_key_hex"`
	BinaryVersion    string `json:"binary_version"`
	BinaryHash       string `json:"binary_hash"`
	ExtensionVersion string `json:"extension_version,omitempty"`
	CLIVersion       string `json:"cli_version,omitempty"`
	DeviceHostname   string `json:"device_hostname,omitempty"`
	DeviceOS         string `json:"device_os,omitempty"`
	DeviceOSVersion  string `json:"device_os_version,omitempty"`
	DeviceArch       string `json:"device_arch,omitempty"`
	EnrollmentMode   string `json:"enrollment_mode,omitempty"`
	// EnrollmentCode is the one-time code issued to the operator
	// (harnesses B3); validated by the API layer before enrollment.
	EnrollmentCode string `json:"enrollment_code,omitempty"`
}

// EnrollHarness enrolls a new harness instance and issues a PPC.
func (s *Service) EnrollHarness(req EnrollHarnessRequest) (*models.Harness, *dari.PeerCredential, error) {
	// Parse the public key
	pubBytes, err := hex.DecodeString(req.PublicKeyHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return nil, nil, fmt.Errorf("identity: invalid public key: %w", err)
	}

	// Create device record
	device := &models.Device{
		OrganizationID: req.OrganizationID,
		Hostname:       req.DeviceHostname,
		OS:             req.DeviceOS,
		OSVersion:      req.DeviceOSVersion,
		Arch:           req.DeviceArch,
		PublicKey:      req.PublicKeyHex,
		Status:         "active",
		FirstSeen:      time.Now().Format(time.RFC3339),
		LastSeen:       time.Now().Format(time.RFC3339),
	}
	if err := s.db.Create(device).Error; err != nil {
		return nil, nil, fmt.Errorf("identity: create device: %w", err)
	}

	// Issue PPC
	pubKey := ed25519.PublicKey(pubBytes)
	cred, err := s.ca.Issue(dari.IssueRequest{
		SubjectPeerID:           req.HarnessID,
		Organization:            req.OrganizationID,
		Profile:                 dari.ProfileHarness,
		PublicKey:               pubKey,
		Validity:                90 * 24 * time.Hour, // 90-day validity
		RevocationAuthority:     s.ca.IssuerID,
		AllowedProtocolVersions: []uint8{1},
		BuildChannel:            "stable",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("identity: issue PPC: %w", err)
	}

	credentialHex := hex.EncodeToString(cred.SignedCredential)
	credentialDigest := dari.ComputeObjectDigest(dari.ObjTypePeerCredential, cred.SignedCredential)

	// Create harness record
	harness := &models.Harness{
		OrganizationID:   req.OrganizationID,
		DeviceID:         device.ID,
		HarnessID:        req.HarnessID,
		BinaryVersion:    req.BinaryVersion,
		BinaryHash:       req.BinaryHash,
		ExtensionVersion: req.ExtensionVersion,
		CLIVersion:       req.CLIVersion,
		PublicKey:        req.PublicKeyHex,
		CredentialJSON:   credentialHex,
		CredentialDigest: credentialDigest.String(),
		BuildChannel:     "stable",
		PolicyProfile:    "enterprise",
		LicenseState:     "active",
		Status:           "enrolled",
		EnrollmentMode:   req.EnrollmentMode,
		EnrolledAt:       time.Now().Format(time.RFC3339),
		LastHeartbeat:    time.Now().Format(time.RFC3339),
	}
	harness.AllowedUsers = fmt.Sprintf(`["%s"]`, req.UserID)

	if err := s.db.Create(harness).Error; err != nil {
		return nil, nil, fmt.Errorf("identity: create harness: %w", err)
	}

	// Record audit event
	s.recordAudit(req.OrganizationID, "harness.enrolled", "system", "harness", harness.ID,
		fmt.Sprintf("Harness %s enrolled by user %s", req.HarnessID, req.UserID))

	return harness, cred, nil
}

// VerifyHarnessAuth verifies that a harness is enrolled and has a valid credential.
func (s *Service) VerifyHarnessAuth(harnessID string, signature, message []byte) (*models.Harness, error) {
	var harness models.Harness
	if err := s.db.Where("harness_id = ?", harnessID).First(&harness).Error; err != nil {
		return nil, fmt.Errorf("identity: harness not found: %w", err)
	}
	if harness.Status != "enrolled" && harness.Status != "active" {
		return nil, fmt.Errorf("identity: harness status is %s, not enrolled/active", harness.Status)
	}

	pubBytes, err := hex.DecodeString(harness.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("identity: invalid harness public key: %w", err)
	}
	pubKey := ed25519.PublicKey(pubBytes)

	if !dari.VerifyEd25519(pubKey, message, signature) {
		return nil, errors.New("identity: signature verification failed")
	}

	return &harness, nil
}

// RevokeHarness revokes a harness enrollment.
func (s *Service) RevokeHarness(orgID, harnessID, reason string) error {
	var harness models.Harness
	if err := s.db.Where("harness_id = ? AND organization_id = ?", harnessID, orgID).First(&harness).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("identity: harness %s not found in org %s", harnessID, orgID)
		}
		return fmt.Errorf("identity: load harness for revocation: %w", err)
	}

	serial := credentialSerial(harness.CredentialJSON)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Harness{}).
			Where("harness_id = ? AND organization_id = ?", harnessID, orgID).
			Updates(map[string]interface{}{
				"status":            "revoked",
				"revocation_reason": reason,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Model(&models.Session{}).
			Where("harness_id = ? AND status IN ?", harnessID, []string{"pending", "active", "idle"}).
			Updates(map[string]interface{}{
				"status":    "terminated",
				"closed_at": time.Now().Format(time.RFC3339),
			}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("identity: harness %s not found in org %s", harnessID, orgID)
	}
	if err != nil {
		return fmt.Errorf("identity: revoke harness: %w", err)
	}
	s.revocations.revoke(serial, reason)
	s.recordAudit(orgID, "harness.revoked", "admin", "harness", harnessID, reason)
	return nil
}

// CredentialHexForHarness returns the stored COSE-Sign1 credential hex
// for an enrolled harness (operational tooling; the HTTP enroll
// handler returns the same value at enrollment time).
func (s *Service) CredentialHexForHarness(harnessID string) (string, error) {
	var harness models.Harness
	if err := s.db.Where("harness_id = ?", harnessID).First(&harness).Error; err != nil {
		return "", fmt.Errorf("identity: harness %s not found: %w", harnessID, err)
	}
	return harness.CredentialJSON, nil
}

// RevocationSnapshot returns the current monotonic epoch and revoked serials.
func (s *Service) RevocationSnapshot() (uint64, map[string]uint64) {
	return s.revocations.snapshot()
}

func credentialSerial(encoded string) string {
	credential, err := hex.DecodeString(encoded)
	if err != nil {
		return ""
	}
	sign1, err := dari.DecodeCOSESign1(credential)
	if err != nil {
		return ""
	}
	decoded, err := dari.DecodePeerCredential(sign1.Payload)
	if err != nil {
		return ""
	}
	return decoded.Serial
}

// CreateProject creates a project under an organization.
func (s *Service) CreateProject(orgID, name, nameKo, slug string, allowedModels []string) (*models.Project, error) {
	modelsJSON := "[]"
	if len(allowedModels) > 0 {
		b, _ := json.Marshal(allowedModels)
		modelsJSON = string(b)
	}
	proj := &models.Project{
		AuditBase: models.AuditBase{
			OrganizationID: orgID,
		},
		Name:                name,
		NameKo:              nameKo,
		Slug:                slug,
		Status:              "active",
		AllowedModelClasses: modelsJSON,
	}
	if err := s.db.Create(proj).Error; err != nil {
		return nil, fmt.Errorf("identity: create project: %w", err)
	}
	return proj, nil
}

// RegisterRepository registers a repository under a project.
func (s *Service) RegisterRepository(orgID, projectID, name, fullName, defaultBranch, sensitivity string) (*models.Repository, error) {
	repo := &models.Repository{
		AuditBase: models.AuditBase{
			OrganizationID: orgID,
			ProjectID:      projectID,
		},
		Name:          name,
		FullName:      fullName,
		SCMType:       "git",
		DefaultBranch: defaultBranch,
		Sensitivity:   sensitivity,
		Status:        "active",
	}
	if err := s.db.Create(repo).Error; err != nil {
		return nil, fmt.Errorf("identity: create repository: %w", err)
	}
	return repo, nil
}

// CreateBaseline creates a repository baseline (PRD §18.3).
func (s *Service) CreateBaseline(orgID, repoID, branch, commitSHA, commitMsg, authorName, authorEmail, committedAt, treeDigest, sessionID string) (*models.RepoBaseline, error) {
	baseline := &models.RepoBaseline{
		RepositoryID:  repoID,
		Branch:        branch,
		CommitSHA:     commitSHA,
		CommitMessage: commitMsg,
		AuthorName:    authorName,
		AuthorEmail:   authorEmail,
		CommittedAt:   committedAt,
		TreeDigest:    treeDigest,
		OrgID:         orgID,
		CreatedBy:     sessionID,
	}
	if err := s.db.Create(baseline).Error; err != nil {
		return nil, fmt.Errorf("identity: create baseline: %w", err)
	}
	return baseline, nil
}

// sessionTTLOverrides reads the per-deployment TTL overrides (web/02
// A3): unset falls back to the documented 8h/30m defaults.
func sessionTTLOverrides() (sessionTTL, idleTTL int) {
	return config.SessionTTLs()
}

// protectionProfileFor derives the session protection profile (web/02
// A3): repo sensitivity classes (§19) map to P0/P1/P2; the default P0
// applies when the repo carries no declared sensitivity.
func protectionProfileFor(repoID string) string {
	switch {
	case strings.Contains(repoID, "sensitive"), strings.Contains(repoID, "pii"), strings.Contains(repoID, "payment"):
		return "P2"
	case strings.Contains(repoID, "internal"):
		return "P1"
	default:
		return "P0"
	}
}

// OpenSession creates a working session.
func (s *Service) OpenSession(orgID, harnessID, userID, projectID, repoID, branch, baselineID, title, purpose, modelClass string) (*models.Session, error) {
	sessionID := dari.GenerateID("ses")
	sessionTTL, idleTTL := sessionTTLOverrides()
	sess := &models.Session{
		AuditBase: models.AuditBase{
			OrganizationID: orgID,
			ProjectID:      projectID,
		},
		HarnessID:         harnessID,
		UserID:            userID,
		RepositoryID:      repoID,
		Branch:            branch,
		BaselineID:        baselineID,
		SessionID:         sessionID,
		TaskPurpose:       purpose,
		Title:             title,
		Status:            "active",
		ModelClass:        modelClass,
		ProtectionProfile: protectionProfileFor(repoID),
		SessionTTL:        sessionTTL,
		IdleTTL:           idleTTL,
		OpenedAt:          time.Now().Format(time.RFC3339),
		LastActivityAt:    time.Now().Format(time.RFC3339),
	}
	if err := s.db.Create(sess).Error; err != nil {
		return nil, fmt.Errorf("identity: open session: %w", err)
	}
	return sess, nil
}

// CloseSession marks a session as closed.
func (s *Service) CloseSession(sessionID string) error {
	result := s.db.Model(&models.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]interface{}{
			"status":    "closed",
			"closed_at": time.Now().Format(time.RFC3339),
		})
	return result.Error
}

func (s *Service) recordAudit(orgID, action, actorType, resourceType, resourceID, details string) {
	event := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      action,
		ActorType:      actorType,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Details:        details,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(event) // best-effort audit
}

// GenerateEnrollmentCode creates a one-time enrollment code.
func (s *Service) GenerateEnrollmentCode(orgID, userID string, validity time.Duration) (string, error) {
	codeBytes := make([]byte, 32)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", fmt.Errorf("identity: generate code: %w", err)
	}
	code := hex.EncodeToString(codeBytes)

	ec := &models.EnrollmentCode{
		OrganizationID: orgID,
		Code:           code,
		UserID:         userID,
		ExpiresAt:      time.Now().Add(validity).Format(time.RFC3339),
		Used:           false,
	}
	if err := s.db.Create(ec).Error; err != nil {
		return "", fmt.Errorf("identity: save enrollment code: %w", err)
	}
	return code, nil
}

// DB exposes the underlying gorm handle (hierarchy admin tooling).
func (s *Service) DB() *gorm.DB { return s.db }
