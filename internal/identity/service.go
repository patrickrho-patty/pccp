package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/config"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sessionlifecycle"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service handles identity, enrollment, and authentication operations.
type Service struct {
	db          *gorm.DB
	ca          *dari.PeerCredentialIssuer
	revocations *CredentialRevocations
	lifecycle   *sessionlifecycle.Service
}

var (
	ErrUserNotFound          = errors.New("identity: user not found in organization")
	ErrUserNotActive         = errors.New("identity: operation requires an active user")
	ErrEnrollmentCodeBinding = errors.New("identity: enrollment code binding mismatch")
)

// New creates a new identity service. It initializes or loads the control
// plane's CA key pair for issuing PPCs.
func New(db *gorm.DB) (*Service, error) {
	caKey, err := keys.LoadOrCreate(db, "identity-ca")
	if err != nil {
		return nil, fmt.Errorf("identity: load CA key: %w", err)
	}
	ca, err := dari.LoadOrCreatePeerCredentialIssuer("pccp-ca", caKey)
	if err != nil {
		return nil, fmt.Errorf("identity: init CA: %w", err)
	}
	return &Service{db: db, ca: ca, revocations: newCredentialRevocations(db), lifecycle: sessionlifecycle.New(db)}, nil
}

func (s *Service) SetSessionLifecycle(lifecycle *sessionlifecycle.Service) {
	if lifecycle != nil {
		s.lifecycle = lifecycle
	}
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
	var user *models.User
	err := s.db.Transaction(func(tx *gorm.DB) error {
		created, err := s.CreateUserWithDB(tx, orgID, email, name, nameKo, authMethod, externalID)
		user = created
		return err
	})
	return user, err
}

// CreateUserWithDB creates a user using the caller's transaction. Callers that
// also persist profile fields, access grants, or audit evidence must use this
// form so no partially-created active identity can escape.
func (s *Service) CreateUserWithDB(db *gorm.DB, orgID, email, name, nameKo, authMethod, externalID string) (*models.User, error) {
	org, err := LockOrganizationForAdmission(db, orgID)
	if err != nil {
		return nil, err
	}
	user := &models.User{
		AuditBase: models.AuditBase{
			Base:           models.Base{},
			OrganizationID: orgID,
			Classification: "internal",
		},
		Email:      NormalizeEmail(email),
		Name:       name,
		NameKo:     nameKo,
		Status:     "active",
		AuthMethod: authMethod,
		ExternalID: externalID,
		Locale:     "ko-KR",
		Timezone:   "Asia/Seoul",
	}
	if _, err := createAdmittedUserWithLockedOrganization(db, *org, user, false); err != nil {
		return nil, fmt.Errorf("identity: create user: %w", err)
	}
	return user, nil
}

// NormalizeEmail is the canonical persisted and lookup form for human
// identities. Email local-part case distinctions are not supported because
// they make tenant identity and lifecycle enforcement ambiguous.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func (s *Service) requireActiveUser(orgID, userID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		_, err := LockActiveUser(tx, orgID, userID)
		return err
	})
}

// EnrollHarnessRequest contains the information needed to enroll a harness.
type EnrollHarnessRequest struct {
	OrganizationID    string `json:"organization_id"`
	UserID            string `json:"user_id"`
	HarnessID         string `json:"harness_id"`
	PublicKeyHex      string `json:"public_key_hex"`
	BinaryVersion     string `json:"binary_version"`
	BinaryHash        string `json:"binary_hash"`
	ExtensionVersion  string `json:"extension_version,omitempty"`
	CLIVersion        string `json:"cli_version,omitempty"`
	DeviceHostname    string `json:"device_hostname,omitempty"`
	DeviceOS          string `json:"device_os,omitempty"`
	DeviceOSVersion   string `json:"device_os_version,omitempty"`
	DeviceArch        string `json:"device_arch,omitempty"`
	MDMEnrolled       bool   `json:"mdm_enrolled,omitempty"`
	MDMPosture        string `json:"mdm_posture,omitempty"`
	NetworkZone       string `json:"network_zone,omitempty"`
	IPAddress         string `json:"ip_address,omitempty"`
	Attestation       string `json:"attestation,omitempty"`
	AttestedAt        string `json:"attested_at,omitempty"`
	BuildSignature    string `json:"build_signature,omitempty"`
	DeploymentProfile string `json:"-"`
	EnrollmentMode    string `json:"enrollment_mode,omitempty"`
	// EnrollmentCode is the one-time code issued to the operator
	// (harnesses B3); validated by the API layer before enrollment.
	EnrollmentCode string `json:"enrollment_code,omitempty"`
}

// PreparedHarnessEnrollment contains proofs and a PPC prepared before the
// short organization enrollment transaction. Its fields are private so only
// this package can construct a commit-capable value.
type PreparedHarnessEnrollment struct {
	request           EnrollHarnessRequest
	credential        *dari.PeerCredential
	policyFingerprint [32]byte
}

// PrepareHarnessEnrollment performs public-key parsing, policy proof
// verification, and PPC issuance without holding organization or seat locks.
func (s *Service) PrepareHarnessEnrollment(req EnrollHarnessRequest) (*PreparedHarnessEnrollment, error) {
	if strings.TrimSpace(req.UserID) == "" {
		return nil, fmt.Errorf("%w: user_id is required", ErrUserNotFound)
	}
	pubBytes, err := hex.DecodeString(req.PublicKeyHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("identity: invalid public key: %w", err)
	}
	var org models.Organization
	if err := s.db.Where("id = ?", req.OrganizationID).First(&org).Error; err != nil {
		return nil, fmt.Errorf("identity: load enrollment organization: %w", err)
	}
	req.DeploymentProfile = org.Profile
	policy, err := LoadEnrollmentPolicy(s.db, org)
	if err != nil {
		return nil, err
	}
	if err := ValidateEnrollmentPolicy(policy, req); err != nil {
		return nil, err
	}
	fingerprint, err := EnrollmentPolicyFingerprint(org.Profile, policy)
	if err != nil {
		return nil, err
	}
	credential, err := s.ca.Issue(dari.IssueRequest{
		SubjectPeerID: req.HarnessID, Organization: req.OrganizationID, Profile: dari.ProfileHarness,
		PublicKey: ed25519.PublicKey(pubBytes), Validity: 90 * 24 * time.Hour,
		RevocationAuthority: s.ca.IssuerID, AllowedProtocolVersions: []uint8{1}, BuildChannel: "stable",
	})
	if err != nil {
		return nil, fmt.Errorf("identity: issue PPC: %w", err)
	}
	return &PreparedHarnessEnrollment{request: req, credential: credential, policyFingerprint: fingerprint}, nil
}

// EnrollHarness enrolls a new harness instance and issues a PPC.
func (s *Service) EnrollHarness(req EnrollHarnessRequest) (*models.Harness, *dari.PeerCredential, error) {
	prepared, err := s.PrepareHarnessEnrollment(req)
	if err != nil {
		return nil, nil, err
	}
	var harness *models.Harness
	var cred *dari.PeerCredential
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var org models.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&org, "id = ?", req.OrganizationID).Error; err != nil {
			return err
		}
		var err error
		harness, cred, err = s.EnrollPreparedHarnessWithDB(tx, org, prepared)
		return err
	})
	return harness, cred, err
}

// EnrollPreparedHarnessWithDB persists a prepared device/harness/credential
// graph under the caller's organization, enrollment-code, and seat locks.
func (s *Service) EnrollPreparedHarnessWithDB(tx *gorm.DB, org models.Organization, prepared *PreparedHarnessEnrollment) (*models.Harness, *dari.PeerCredential, error) {
	if prepared == nil || prepared.credential == nil {
		return nil, nil, fmt.Errorf("identity: prepared enrollment is required")
	}
	req := prepared.request
	if org.ID != req.OrganizationID {
		return nil, nil, fmt.Errorf("identity: prepared enrollment organization mismatch")
	}
	if err := RequireHarnessSeatWithDB(tx, org); err != nil {
		return nil, nil, err
	}
	if _, err := LockActiveUser(tx, req.OrganizationID, req.UserID); err != nil {
		return nil, nil, err
	}
	policy, err := LoadEnrollmentPolicy(tx, org)
	if err != nil {
		return nil, nil, err
	}
	fingerprint, err := EnrollmentPolicyFingerprint(org.Profile, policy)
	if err != nil {
		return nil, nil, err
	}
	if fingerprint != prepared.policyFingerprint {
		return nil, nil, denyEnrollment("organization enrollment policy changed; retry enrollment")
	}
	now := time.Now().Format(time.RFC3339)
	device := &models.Device{
		OrganizationID: req.OrganizationID, UserID: req.UserID,
		Hostname: req.DeviceHostname, OS: req.DeviceOS, OSVersion: req.DeviceOSVersion,
		Arch: req.DeviceArch, MDMEnrolled: req.MDMEnrolled, MDMPosture: req.MDMPosture,
		NetworkZone: req.NetworkZone, IPAddress: req.IPAddress,
		PublicKey: req.PublicKeyHex, Status: "active", FirstSeen: now, LastSeen: now,
	}
	if err := tx.Create(device).Error; err != nil {
		return nil, nil, fmt.Errorf("identity: create device: %w", err)
	}
	cred := prepared.credential
	credentialDigest := dari.ComputeObjectDigest(dari.ObjTypePeerCredential, cred.SignedCredential)
	harness := &models.Harness{
		OrganizationID: req.OrganizationID, DeviceID: device.ID, HarnessID: req.HarnessID,
		BinaryVersion: req.BinaryVersion, BinaryHash: req.BinaryHash, ExtensionVersion: req.ExtensionVersion,
		CLIVersion: req.CLIVersion, PublicKey: req.PublicKeyHex, CredentialJSON: hex.EncodeToString(cred.SignedCredential),
		CredentialDigest: credentialDigest.String(), BuildChannel: "stable", PolicyProfile: "enterprise",
		LicenseState: "active", Status: "enrolled", EnrollmentMode: req.EnrollmentMode,
		EnrolledAt: now, LastHeartbeat: now, NetworkZone: req.NetworkZone, AllowedUsers: fmt.Sprintf(`["%s"]`, req.UserID),
	}
	if strings.TrimSpace(req.Attestation) != "" {
		harness.LastAttestation = now
	}
	if err := tx.Create(harness).Error; err != nil {
		return nil, nil, fmt.Errorf("identity: create harness: %w", err)
	}
	if err := s.recordAuditWithDB(tx, req.OrganizationID, "harness.enrolled", "system", req.UserID, "harness", harness.ID,
		fmt.Sprintf("Harness %s enrolled by user %s", req.HarnessID, req.UserID)); err != nil {
		return nil, nil, err
	}
	return harness, cred, nil
}

// VerifyHarnessAuth verifies that a harness is enrolled and has a valid credential.
func (s *Service) VerifyHarnessAuth(harnessID string, signature, message []byte) (*models.Harness, error) {
	var harness models.Harness
	if err := s.db.Where("harness_id = ?", harnessID).First(&harness).Error; err != nil {
		return nil, fmt.Errorf("identity: harness not found: %w", err)
	}
	if !models.HarnessStatusPermitted(harness.Status) {
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

// HarnessRenewalSigningBytes is the public proof-of-possession contract for a
// silent PPC renewal. The current credential digest makes a captured request
// single-use because a successful renewal replaces that credential.
func HarnessRenewalSigningBytes(harnessID, credentialSHA256, signedAt string) []byte {
	return []byte("pccp-harness-credential-renewal-v1\x00" + harnessID + "\x00" + credentialSHA256 + "\x00" + signedAt)
}

// RenewHarnessCredential replaces an enrolled Harness PPC under a row lock,
// revokes the superseded serial, and records the credential lifecycle event.
func (s *Service) RenewHarnessCredential(harnessID, credentialSHA256 string) (*dari.PeerCredential, error) {
	return s.RenewHarnessCredentialWithAdmission(harnessID, credentialSHA256, nil)
}

// RenewHarnessCredentialWithAdmission evaluates caller-supplied account and
// user policy under the same Harness row-lock transaction as PPC rotation.
func (s *Service) RenewHarnessCredentialWithAdmission(harnessID, credentialSHA256 string, admit func(*gorm.DB, *models.Organization, *models.User, *models.Harness) error) (*dari.PeerCredential, error) {
	var renewed *dari.PeerCredential
	var oldSerial string
	var epoch uint64
	var snapshot models.Harness
	if err := s.db.Where("harness_id = ?", harnessID).First(&snapshot).Error; err != nil {
		return nil, fmt.Errorf("identity: renew Harness credential: %w", err)
	}
	var allowedUsers []string
	_ = json.Unmarshal([]byte(snapshot.AllowedUsers), &allowedUsers)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		org, err := LockOrganizationForAdmission(tx, snapshot.OrganizationID)
		if err != nil {
			return err
		}
		var user *models.User
		if len(allowedUsers) > 0 && strings.TrimSpace(allowedUsers[0]) != "" {
			user, err = LockActiveUser(tx, org.ID, allowedUsers[0])
			if err != nil {
				return err
			}
		}
		var harness models.Harness
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("harness_id = ? AND organization_id = ?", harnessID, org.ID).First(&harness).Error; err != nil {
			return err
		}
		if harness.AllowedUsers != snapshot.AllowedUsers {
			return errors.New("identity: Harness user binding changed during renewal")
		}
		if !models.HarnessStatusPermitted(harness.Status) {
			return fmt.Errorf("identity: harness status is %s", harness.Status)
		}
		if admit != nil {
			if err := admit(tx, org, user, &harness); err != nil {
				return err
			}
		}
		oldRaw, err := hex.DecodeString(harness.CredentialJSON)
		if err != nil || len(oldRaw) == 0 {
			return errors.New("identity: stored Harness credential is invalid")
		}
		digest := sha256.Sum256(oldRaw)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), strings.TrimSpace(credentialSHA256)) {
			return errors.New("identity: Harness credential renewal replay or digest mismatch")
		}
		sign1, err := dari.DecodeCOSESign1(oldRaw)
		if err != nil {
			return fmt.Errorf("identity: decode current Harness credential: %w", err)
		}
		current, err := dari.DecodePeerCredential(sign1.Payload)
		if err != nil {
			return fmt.Errorf("identity: decode current Harness credential body: %w", err)
		}
		publicKey, err := hex.DecodeString(harness.PublicKey)
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return errors.New("identity: stored Harness public key is invalid")
		}
		renewed, err = s.ca.Issue(dari.IssueRequest{
			SubjectPeerID: harness.HarnessID, Organization: harness.OrganizationID, Profile: current.PeerProfile,
			PublicKey: ed25519.PublicKey(publicKey), Validity: 90 * 24 * time.Hour,
			RevocationAuthority: s.ca.IssuerID, AllowedProtocolVersions: append([]uint8(nil), current.AllowedProtocolVersions...),
			BuildChannel: current.BuildChannel, DeploymentZone: current.DeploymentZone,
		})
		if err != nil {
			return fmt.Errorf("identity: issue renewed Harness credential: %w", err)
		}
		newDigest := dari.ComputeObjectDigest(dari.ObjTypePeerCredential, renewed.SignedCredential)
		if err := tx.Model(&models.Harness{}).Where("id = ?", harness.ID).Updates(map[string]interface{}{
			"credential_json": hex.EncodeToString(renewed.SignedCredential), "credential_digest": newDigest.String(),
			"last_heartbeat": time.Now().UTC().Format(time.RFC3339),
		}).Error; err != nil {
			return err
		}
		oldSerial = current.Serial
		epoch = s.revocations.reserveEpoch()
		if oldSerial != "" {
			if err := tx.Create(&models.CredentialRevocationRecord{
				Serial: oldSerial, RevokedEpoch: epoch, Reason: "credential_renewed", RevokedAtRFC: time.Now().UTC().Format(time.RFC3339Nano),
			}).Error; err != nil {
				return fmt.Errorf("identity: persist superseded credential revocation: %w", err)
			}
		}
		return s.recordAuditWithDB(tx, harness.OrganizationID, "harness.credential_renewed", "harness", harness.HarnessID, "harness", harness.ID, "silent proof-of-possession renewal")
	})
	if err != nil {
		return nil, fmt.Errorf("identity: renew Harness credential: %w", err)
	}
	s.revocations.applyCommitted(oldSerial, epoch)
	return renewed, nil
}

// RevokeHarness revokes a harness enrollment.
type RevocationAppliedError struct{ Cause error }

func (e *RevocationAppliedError) Error() string { return e.Cause.Error() }
func (e *RevocationAppliedError) Unwrap() error { return e.Cause }

func (s *Service) RevokeHarness(orgID, harnessID, reason string) error {
	return s.RevokeHarnessByActor(orgID, harnessID, reason, "")
}

func (s *Service) RevokeHarnessByActor(orgID, harnessID, reason, actorID string) error {
	var harness models.Harness
	var outcomes []sessionlifecycle.Outcome
	var serial string
	var epoch uint64
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("harness_id = ? AND organization_id = ?", harnessID, orgID).First(&harness).Error; err != nil {
			return err
		}
		if harness.Status == "revoked" {
			serial = credentialSerial(harness.CredentialJSON)
			if serial != "" {
				var record models.CredentialRevocationRecord
				if err := tx.Where("serial = ?", serial).First(&record).Error; err != nil {
					return fmt.Errorf("identity: revoked harness is missing its credential ledger entry: %w", err)
				}
				epoch = record.RevokedEpoch
			}
			return nil
		}
		serial = credentialSerial(harness.CredentialJSON)
		updated := tx.Model(&models.Harness{}).Where("id = ? AND organization_id = ? AND status = ?", harness.ID, orgID, harness.Status).
			Updates(map[string]interface{}{"status": "revoked", "revocation_reason": reason})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrInvalidData
		}
		var err error
		outcomes, err = s.lifecycle.TransitionScopeInTransaction(tx, sessionlifecycle.Scope{OrganizationID: orgID, HarnessID: harnessID, ForceTerminal: true, ActorType: "admin"}, "terminated", "harness_revoked", reason, "")
		if err != nil {
			return fmt.Errorf("identity: terminate harness sessions: %w", err)
		}
		epoch = s.revocations.reserveEpoch()
		if serial != "" {
			if err := tx.Create(&models.CredentialRevocationRecord{Serial: serial, RevokedEpoch: epoch, Reason: reason, RevokedAtRFC: time.Now().UTC().Format(time.RFC3339Nano)}).Error; err != nil {
				return fmt.Errorf("identity: persist credential revocation: %w", err)
			}
		}
		return s.recordAuditWithDB(tx, orgID, "harness.revoked", "admin", actorID, "harness", harnessID, reason)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("identity: harness %s not found in org %s", harnessID, orgID)
	}
	if err != nil {
		return fmt.Errorf("identity: revoke harness: %w", err)
	}
	if harness.Status == "revoked" {
		s.revocations.applyCommitted(serial, epoch)
		return nil
	}
	s.revocations.applyCommitted(serial, epoch)
	finalized, finalizeErr := s.lifecycle.FinalizeTransitions(orgID, outcomes, "terminated", "harness_revoked", reason, "", "admin")
	if finalizeErr != nil {
		return &RevocationAppliedError{Cause: fmt.Errorf("identity: revocation committed but cleanup failed: %w", finalizeErr)}
	}
	for _, outcome := range finalized {
		if outcome.Result != sessionlifecycle.ResultUpdated || len(outcome.CleanupFailures) > 0 {
			return &RevocationAppliedError{Cause: fmt.Errorf("identity: revocation committed but session %s cleanup was incomplete (%s): %s", outcome.RequestedID, outcome.Result, outcome.Error)}
		}
	}
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
	var sess *models.Session
	err := s.db.Transaction(func(tx *gorm.DB) error {
		created, err := s.OpenSessionWithDB(tx, orgID, harnessID, userID, projectID, repoID, branch, baselineID, title, purpose, modelClass)
		sess = created
		return err
	})
	return sess, err
}

// OpenSessionWithDB creates a session under the caller's transaction. The
// lifecycle row lock must be held through any subsequent lease and audit writes.
func (s *Service) OpenSessionWithDB(db *gorm.DB, orgID, harnessID, userID, projectID, repoID, branch, baselineID, title, purpose, modelClass string) (*models.Session, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: user_id is required", ErrUserNotFound)
	}
	if _, err := LockActiveUser(db, orgID, userID); err != nil {
		return nil, err
	}
	sess := newSession(orgID, harnessID, userID, projectID, repoID, branch, baselineID, title, purpose, modelClass)
	if err := db.Create(sess).Error; err != nil {
		return nil, fmt.Errorf("identity: open session: %w", err)
	}
	return sess, nil
}

// OpenProtocolSessionWithDB persists the authority binding supplied by an
// authenticated DARI SESSION_OPEN. Unlike the console path, the protocol has
// already minted the session ID locally, so that exact ID must become the
// canonical control-plane row before a lease can authorize AI_OPEN.
func (s *Service) OpenProtocolSessionWithDB(db *gorm.DB, orgID, harnessID, userID, sessionID, repoID, branch, modelClass string) (*models.Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("identity: protocol session_id is required")
	}
	if _, err := LockActiveUser(db, orgID, userID); err != nil {
		return nil, err
	}
	if err := ValidateActiveHarnessUserBinding(db, orgID, harnessID, userID); err != nil {
		return nil, err
	}
	var existing models.Session
	if err := db.Where("session_id = ?", sessionID).First(&existing).Error; err == nil {
		if existing.OrganizationID != orgID || existing.HarnessID != harnessID || existing.UserID != userID || !models.SessionIsLive(existing.Status) {
			return nil, fmt.Errorf("identity: protocol session authority binding mismatch")
		}
		now := time.Now().UTC().Format(time.RFC3339)
		updates := map[string]interface{}{
			"repository_id":    repoID,
			"branch":           branch,
			"model_class":      modelClass,
			"last_activity_at": now,
		}
		if err := db.Model(&models.Session{}).Where("id = ? AND organization_id = ?", existing.ID, orgID).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("identity: refresh protocol session: %w", err)
		}
		existing.RepositoryID = repoID
		existing.Branch = branch
		existing.ModelClass = modelClass
		existing.LastActivityAt = now
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("identity: check protocol session: %w", err)
	}
	sess := newSession(orgID, harnessID, userID, "", repoID, branch, "", "", "", modelClass)
	sess.SessionID = sessionID
	if err := db.Create(sess).Error; err != nil {
		return nil, fmt.Errorf("identity: open protocol session: %w", err)
	}
	return sess, nil
}

func newSession(orgID, harnessID, userID, projectID, repoID, branch, baselineID, title, purpose, modelClass string) *models.Session {
	sessionID := dari.GenerateID("ses")
	sessionTTL, idleTTL := sessionTTLOverrides()
	return &models.Session{
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
}

func (s *Service) recordAudit(orgID, action, actorType, resourceType, resourceID, details string) {
	_ = s.recordAuditWithDB(s.db, orgID, action, actorType, "", resourceType, resourceID, details)
}

func (s *Service) recordAuditWithDB(db *gorm.DB, orgID, eventType, actorType, actorID, resourceType, resourceID, details string) error {
	event := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      eventType,
		ActorID:        actorID,
		ActorType:      actorType,
		Action:         eventType,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Details:        details,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return db.Create(event).Error
}

// GenerateEnrollmentCode creates a one-time enrollment code.
func (s *Service) GenerateEnrollmentCode(orgID, userID string, validity time.Duration) (string, error) {
	var code string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		code, err = s.GenerateEnrollmentCodeWithDB(tx, orgID, userID, validity)
		return err
	})
	return code, err
}

func (s *Service) GenerateEnrollmentCodeWithDB(tx *gorm.DB, orgID, userID string, validity time.Duration) (string, error) {
	return s.generateEnrollmentCodeWithDB(tx, orgID, userID, "", "", "admin", validity)
}

// GenerateBoundEnrollmentCodeWithDB issues the public bootstrap grant. Unlike
// an administrator code, it is bound to one Harness ID and one Ed25519 key.
func (s *Service) GenerateBoundEnrollmentCodeWithDB(tx *gorm.DB, orgID, userID, harnessID, publicKeyHex string, validity time.Duration) (string, error) {
	publicKey, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return "", fmt.Errorf("identity: invalid public enrollment key")
	}
	if strings.TrimSpace(harnessID) == "" {
		return "", fmt.Errorf("identity: public enrollment harness_id is required")
	}
	digest := sha256.Sum256(publicKey)
	return s.generateEnrollmentCodeWithDB(tx, orgID, userID, harnessID, hex.EncodeToString(digest[:]), "public", validity)
}

func (s *Service) generateEnrollmentCodeWithDB(tx *gorm.DB, orgID, userID, harnessID, publicKeyHash, purpose string, validity time.Duration) (string, error) {
	codeBytes := make([]byte, 32)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", fmt.Errorf("identity: generate code: %w", err)
	}
	code := hex.EncodeToString(codeBytes)

	ec := &models.EnrollmentCode{
		OrganizationID: orgID,
		Code:           code,
		UserID:         userID,
		HarnessID:      harnessID,
		PublicKeyHash:  publicKeyHash,
		Purpose:        purpose,
		ExpiresAt:      time.Now().Add(validity).Format(time.RFC3339),
		Used:           false,
	}
	if _, err := LockActiveUser(tx, orgID, userID); err != nil {
		return "", err
	}
	if err := tx.Create(ec).Error; err != nil {
		return "", fmt.Errorf("identity: save enrollment code: %w", err)
	}
	return code, nil
}

// DB exposes the underlying gorm handle (hierarchy admin tooling).
func (s *Service) DB() *gorm.DB { return s.db }
