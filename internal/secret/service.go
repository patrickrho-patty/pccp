package secret

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements the Secret Broker (PRD §17.5).
// The model should not receive raw long-lived credentials. Secret access uses
// short-lived scoped credentials that are injected directly to the approved
// process/connection and excluded from prompt/context and logs.
type Service struct {
	db *gorm.DB
	mu sync.RWMutex
	// activeCredentials tracks issued short-lived credentials
	activeCredentials map[string]*ScopedCredential
}

// ScopedCredential is a short-lived, purpose-bound credential.
type ScopedCredential struct {
	ID            string    `json:"id"`
	OrganizationID string   `json:"organization_id"`
	SessionID     string    `json:"session_id"`
	HarnessID     string    `json:"harness_id"`
	// Target
	SecretRef     string    `json:"secret_ref"`     // reference to the source secret (e.g. "vault://path/to/secret")
	TargetService string    `json:"target_service"`  // e.g. "postgres", "github-api"
	Operation     string    `json:"operation"`       // e.g. "migrate", "deploy"
	// Credential
	CredentialType string   `json:"credential_type"` // token, password, key
	CredentialValue string  `json:"-"`               // NEVER serialized to JSON/logs
	CredentialHash string   `json:"credential_hash"`  // hash for audit
	// Scoping
	Scopes        []string  `json:"scopes"`          // e.g. ["repo:read", "db:migrate"]
	AllowedFromIP string    `json:"allowed_from_ip,omitempty"`
	// Lifecycle
	IssuedAt      time.Time `json:"issued_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Revoked       bool      `json:"revoked"`
	UsedAt        *time.Time `json:"used_at,omitempty"`
	// Audit
	IssuedBy      string    `json:"issued_by"`
	PurposeRecord string    `json:"purpose_record"`
}

// New creates a new secret broker service.
func New(db *gorm.DB) *Service {
	return &Service{
		db:                db,
		activeCredentials: make(map[string]*ScopedCredential),
	}
}

// IssueRequest is a request to issue a short-lived scoped credential.
type IssueRequest struct {
	OrganizationID string   `json:"organization_id"`
	SessionID      string   `json:"session_id"`
	HarnessID      string   `json:"harness_id,omitempty"`
	SecretRef      string   `json:"secret_ref"`
	TargetService  string   `json:"target_service,omitempty"`
	Operation      string   `json:"operation,omitempty"`
	Scopes         []string `json:"scopes,omitempty"`
	ValiditySecs   int      `json:"validity_secs,omitempty"`
	IssuedBy       string   `json:"issued_by,omitempty"`
}

// Issue creates a short-lived scoped credential and returns it.
// The credential value must be injected directly to the approved process and
// must NOT appear in prompt/context or normal logs.
func (s *Service) Issue(req IssueRequest) (*ScopedCredential, error) {
	if req.SecretRef == "" {
		return nil, fmt.Errorf("secret: secret_ref is required")
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("secret: session_id is required (credentials are session-bound)")
	}

	validity := time.Duration(req.ValiditySecs) * time.Second
	if validity == 0 {
		validity = 5 * time.Minute // default 5-minute credential
	}

	// Generate a scoped credential value
	credValue := generateScopedToken()
	credHash := hashCredential(credValue)

	cred := &ScopedCredential{
		ID:              generateCredID(),
		OrganizationID:  req.OrganizationID,
		SessionID:       req.SessionID,
		HarnessID:       req.HarnessID,
		SecretRef:       req.SecretRef,
		TargetService:   req.TargetService,
		Operation:       req.Operation,
		CredentialType:  "token",
		CredentialValue: credValue,
		CredentialHash:  credHash,
		Scopes:          req.Scopes,
		IssuedAt:        time.Now(),
		ExpiresAt:       time.Now().Add(validity),
		IssuedBy:        req.IssuedBy,
		PurposeRecord:   fmt.Sprintf("session=%s operation=%s target=%s", req.SessionID, req.Operation, req.TargetService),
	}

	s.mu.Lock()
	s.activeCredentials[cred.ID] = cred
	s.mu.Unlock()

	// Record issuance in audit (hash only, never the value)
	s.recordAudit(req.OrganizationID, "secret.issued", cred.ID,
		fmt.Sprintf(`{"target":"%s","operation":"%s","scopes":%v,"expires":"%s","hash":"%s"}`,
			req.TargetService, req.Operation, req.Scopes, cred.ExpiresAt.Format(time.RFC3339), credHash))

	return cred, nil
}

// Validate checks whether a credential is currently valid.
func (s *Service) Validate(credID, sessionID string) (*ScopedCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cred, ok := s.activeCredentials[credID]
	if !ok {
		return nil, fmt.Errorf("secret: credential not found")
	}
	if cred.Revoked {
		return nil, fmt.Errorf("secret: credential revoked")
	}
	if time.Now().After(cred.ExpiresAt) {
		return nil, fmt.Errorf("secret: credential expired")
	}
	if cred.SessionID != sessionID {
		return nil, fmt.Errorf("secret: credential session mismatch")
	}
	return cred, nil
}

// Revoke immediately revokes a credential.
func (s *Service) Revoke(orgID, credID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cred, ok := s.activeCredentials[credID]
	if !ok {
		return fmt.Errorf("secret: credential not found")
	}
	cred.Revoked = true

	s.recordAudit(orgID, "secret.revoked", credID,
		fmt.Sprintf(`{"reason":"%s","hash":"%s"}`, reason, cred.CredentialHash))

	return nil
}

// ExpireAllForSession revokes all credentials for a session (called on session close).
func (s *Service) ExpireAllForSession(orgID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, cred := range s.activeCredentials {
		if cred.SessionID == sessionID {
			cred.Revoked = true
			s.recordAudit(orgID, "secret.session_expired", cred.ID,
				fmt.Sprintf(`{"session":"%s"}`, sessionID))
		}
	}
}

// CleanupExpired removes expired credentials from the active set.
func (s *Service) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, cred := range s.activeCredentials {
		if now.After(cred.ExpiresAt) || cred.Revoked {
			delete(s.activeCredentials, id)
		}
	}
}

// GetCredentialValue returns the raw credential value.
// This should ONLY be called when injecting into an approved process/connection.
// It must NEVER be logged or included in prompt/context.
func (s *Service) GetCredentialValue(credID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cred, ok := s.activeCredentials[credID]
	if !ok {
		return "", fmt.Errorf("secret: credential not found")
	}
	if cred.Revoked {
		return "", fmt.Errorf("secret: credential revoked")
	}
	if time.Now().After(cred.ExpiresAt) {
		return "", fmt.Errorf("secret: credential expired")
	}

	// Mark as used
	now := time.Now()
	cred.UsedAt = &now

	return cred.CredentialValue, nil
}

func (s *Service) recordAudit(orgID, action, resourceID, details string) {
	event := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp." + action,
		ActorType:      "system",
		Action:         action,
		ResourceType:   "scoped_credential",
		ResourceID:     resourceID,
		Details:        details,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(event)
}

func generateScopedToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "pccp-scoped-" + hex.EncodeToString(b)
}

func hashCredential(cred string) string {
	// Simple hash for audit (never reversible)
	h := make([]byte, 16)
	for i := 0; i < len(cred) && i < 32; i++ {
		h[i%16] ^= cred[i]
	}
	return "h:" + hex.EncodeToString(h)
}

func generateCredID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "cred_" + hex.EncodeToString(b)
}
