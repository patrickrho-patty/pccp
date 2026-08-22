package identity

import (
	"errors"
	"fmt"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

var (
	ErrExternalIdentityUnlinked  = errors.New("identity: external identity is not linked")
	ErrExternalIdentityAmbiguous = errors.New("identity: external identity link is ambiguous")
)

// ExternalIdentity is the canonical immutable provider namespace used by SSO,
// migration, and enrollment boundaries.
type ExternalIdentity struct {
	Issuer  string
	Subject string
}

const PublicAccountAuthMethod = "account"
const PublicAccountIssuer = "pccp:accounts-authority"

// PublicAccountIdentity is the single local user identity for every immutable
// login linked to one authoritative public account.
func PublicAccountIdentity(authorityID string) ExternalIdentity {
	return NormalizeExternalIdentity(PublicAccountIssuer, authorityID)
}

// NormalizeIssuer is the durable issuer-key contract shared by live SSO and
// migration flows. Issuer URL aliases that differ only by surrounding
// whitespace or trailing slashes must resolve to one immutable identity.
func NormalizeIssuer(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func NormalizeExternalSubject(raw string) string { return strings.TrimSpace(raw) }

func NormalizeExternalIdentity(issuer, subject string) ExternalIdentity {
	return ExternalIdentity{Issuer: NormalizeIssuer(issuer), Subject: NormalizeExternalSubject(subject)}
}

// FindUserByExternalIdentity resolves only an exact tenant, method, issuer,
// and subject tuple. Email is deliberately outside this immutable seam.
func FindUserByExternalIdentity(db *gorm.DB, orgID, authMethod string, external ExternalIdentity) (*models.User, error) {
	if db == nil || strings.TrimSpace(orgID) == "" || strings.TrimSpace(authMethod) == "" || external.Issuer == "" || external.Subject == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var user models.User
	err := db.Where("organization_id = ? AND auth_method = ? AND external_issuer = ? AND external_id = ?",
		orgID, strings.TrimSpace(authMethod), external.Issuer, external.Subject).First(&user).Error
	return &user, err
}

// FindActiveUserByLinkedTargetIdentity resolves a first-party identity only
// through an explicit governance-managed customer-identity reconciliation
// link. The target tuple is never inferred from email or a customer IdP/SCIM
// subject, and duplicate mappings fail closed.
func FindActiveUserByLinkedTargetIdentity(db *gorm.DB, orgID string, target ExternalIdentity) (*models.User, error) {
	if db == nil || strings.TrimSpace(orgID) == "" || target.Issuer == "" || target.Subject == "" {
		return nil, ErrExternalIdentityUnlinked
	}
	var links []models.SSOIdentityLink
	if err := db.Where(
		"organization_id = ? AND target_issuer = ? AND target_subject = ? AND status = ? AND patty_user_id <> ''",
		orgID, target.Issuer, target.Subject, models.SSOLinkStatusLinked,
	).Limit(2).Find(&links).Error; err != nil {
		return nil, fmt.Errorf("identity: resolve target identity link: %w", err)
	}
	if len(links) == 0 {
		return nil, ErrExternalIdentityUnlinked
	}
	if len(links) != 1 {
		return nil, ErrExternalIdentityAmbiguous
	}
	user, err := LockActiveUser(db, orgID, links[0].PattyUserID)
	if err != nil {
		return nil, fmt.Errorf("identity: linked external identity user is unavailable: %w", err)
	}
	return user, nil
}

// ResolveLinkedSourceIdentity is the shared fail-closed resolver for live SSO
// and migration bridge authentication. It never guesses by email.
func ResolveLinkedSourceIdentity(db *gorm.DB, orgID string, source ExternalIdentity) (*models.SSOIdentityLink, *models.User, error) {
	source = NormalizeExternalIdentity(source.Issuer, source.Subject)
	if db == nil || strings.TrimSpace(orgID) == "" || source.Issuer == "" || source.Subject == "" {
		return nil, nil, ErrExternalIdentityUnlinked
	}
	var links []models.SSOIdentityLink
	if err := db.Where("organization_id = ? AND legacy_issuer = ? AND legacy_subject = ?", orgID, source.Issuer, source.Subject).Limit(2).Find(&links).Error; err != nil {
		return nil, nil, fmt.Errorf("identity: resolve source identity link: %w", err)
	}
	if len(links) == 0 {
		return nil, nil, ErrExternalIdentityUnlinked
	}
	if len(links) != 1 || links[0].Status != models.SSOLinkStatusLinked || links[0].PattyUserID == "" {
		return &links[0], nil, ErrExternalIdentityAmbiguous
	}
	user, err := LockActiveUser(db, orgID, links[0].PattyUserID)
	if err != nil {
		return &links[0], nil, fmt.Errorf("identity: linked source identity user is unavailable: %w", err)
	}
	return &links[0], user, nil
}
