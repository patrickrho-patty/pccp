package sso

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	dsig "github.com/russellhaering/goxmldsig"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ssoFlowLifetime       = 10 * time.Minute
	ssoHandoffLifetime    = 2 * time.Minute
	maxSAMLResponseBase64 = 1 << 20
	maxSAMLResponseXML    = 768 << 10
)

var ErrInvalidLoginHandoff = errors.New("sso: login handoff is missing, expired, bound to another browser, or already used")

// OrganizationSSOConfig is the server-authoritative SSO configuration stored
// on an organization. Browser requests select an organization only; they never
// supply IdP endpoints, client IDs, redirect URIs, keys, or state.
type OrganizationSSOConfig struct {
	Provider         string          `json:"provider"`
	Mode             string          `json:"mode"`
	Issuer           string          `json:"issuer"`
	ClientID         string          `json:"client_id"`
	ClientSecret     string          `json:"client_secret,omitempty"` // legacy input; rejected by validation
	ClientSecretRef  string          `json:"client_secret_ref"`
	AuthorizationURL string          `json:"authorization_url"`
	TokenURL         string          `json:"token_url"`
	RedirectURI      string          `json:"redirect_uri"`
	JWKS             json.RawMessage `json:"jwks"`
	IDPMetadata      string          `json:"saml_idp_metadata"`
	SPEntityID       string          `json:"saml_sp_entity_id"`
	ACSURL           string          `json:"saml_acs_url"`
	SPPrivateKeyPEM  string          `json:"saml_sp_private_key,omitempty"` // legacy input; rejected by validation
	SPPrivateKeyRef  string          `json:"saml_sp_private_key_ref"`
	SPCertificatePEM string          `json:"saml_sp_certificate"`
}

type OIDCLoginResult struct {
	OrganizationID string
	Issuer         string
	User           *OIDCUserInfo
	BrowserBinding string
	ConfigDigest   string
}

type SAMLLoginResult struct {
	OrganizationID string
	Issuer         string
	User           *SAMLResponse
	BrowserBinding string
	ConfigDigest   string
}

// ValidateProviderReadiness proves that every active organization's configured
// SSO mode is complete and that each referenced credential can be decrypted by
// the provider this process will serve with. A deployment must not advertise a
// healthy API while its only login path is unusable.
func ValidateProviderReadiness(db *gorm.DB, provider keymgmt.KeyProvider) error {
	var organizations []models.Organization
	if err := db.Where("status = ? AND sso_config <> ''", "active").Find(&organizations).Error; err != nil {
		return fmt.Errorf("sso readiness: list configured organizations: %w", err)
	}
	svc := New(db, "")
	svc.SetKeyProvider(provider)
	for _, org := range organizations {
		var declared struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal([]byte(org.SSOConfig), &declared); err != nil {
			return fmt.Errorf("sso readiness: organization %s has invalid configuration", org.ID)
		}
		mode := strings.ToLower(strings.TrimSpace(declared.Mode))
		if mode != "oidc" && mode != "saml" {
			return fmt.Errorf("sso readiness: organization %s has unsupported SSO mode %q", org.ID, declared.Mode)
		}
		_, cfg, _, err := svc.loadOrganizationSSOConfig(org.ID, mode)
		if err != nil {
			return fmt.Errorf("sso readiness: organization %s: %w", org.ID, err)
		}
		if mode == "oidc" {
			if err := validateOIDCConfig(cfg); err != nil {
				return fmt.Errorf("sso readiness: organization %s: %w", org.ID, err)
			}
			if _, err := parseOIDCJWKS(cfg.JWKS); err != nil {
				return fmt.Errorf("sso readiness: organization %s: %w", org.ID, err)
			}
		} else if _, err := samlServiceProvider(cfg); err != nil {
			return fmt.Errorf("sso readiness: organization %s: %w", org.ID, err)
		}
	}
	return nil
}

func (s *Service) BeginOIDCLogin(orgRef, browserBinding string) (string, error) {
	if strings.TrimSpace(browserBinding) == "" {
		return "", fmt.Errorf("sso: browser binding required")
	}
	if err := s.cleanupExpiredSSOTransactions(); err != nil {
		return "", err
	}
	org, cfg, digest, err := s.loadOrganizationSSOConfig(orgRef, "oidc")
	if err != nil {
		return "", err
	}
	if err := validateOIDCConfig(cfg); err != nil {
		return "", err
	}
	if _, err := parseOIDCJWKS(cfg.JWKS); err != nil {
		return "", err
	}
	state, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	nonce, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	verifier, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	challenge := sha256.Sum256([]byte(verifier))
	flow := models.SSOAuthFlow{
		OrganizationID: org.ID,
		Provider:       "oidc",
		StateHash:      hashSSOState(state),
		Nonce:          nonce,
		PKCEVerifier:   verifier,
		ConfigDigest:   digest,
		BrowserBinding: hashSSOState(browserBinding),
		ExpiresAt:      time.Now().UTC().Add(ssoFlowLifetime),
	}
	if err := s.db.Create(&flow).Error; err != nil {
		return "", fmt.Errorf("sso: persist OIDC login transaction: %w", err)
	}
	authorizationURL, _ := url.Parse(cfg.AuthorizationURL)
	params := authorizationURL.Query()
	params.Set("response_type", "code")
	params.Set("client_id", cfg.ClientID)
	params.Set("redirect_uri", cfg.RedirectURI)
	params.Set("scope", "openid profile email")
	params.Set("state", state)
	params.Set("nonce", nonce)
	params.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	params.Set("code_challenge_method", "S256")
	authorizationURL.RawQuery = params.Encode()
	return authorizationURL.String(), nil
}

func (s *Service) CompleteOIDCLogin(ctx context.Context, code, state, browserBinding string) (*OIDCLoginResult, error) {
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("sso: authorization code required")
	}
	flow, err := s.consumeSSOFlow("oidc", state, browserBinding)
	if err != nil {
		return nil, err
	}
	org, cfg, digest, err := s.loadOrganizationSSOConfig(flow.OrganizationID, "oidc")
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(digest), []byte(flow.ConfigDigest)) != 1 {
		return nil, fmt.Errorf("sso: organization SSO configuration changed during login")
	}
	if err := validateOIDCConfig(cfg); err != nil {
		return nil, err
	}
	jwks, err := parseOIDCJWKS(cfg.JWKS)
	if err != nil {
		return nil, err
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {cfg.RedirectURI},
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"code_verifier": {flow.PKCEVerifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("sso: token request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sso: token endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("sso: token endpoint rejected the authorization code (HTTP %d)", resp.StatusCode)
	}
	var tokens OIDCTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("sso: decode token response: %w", err)
	}
	if tokens.IDToken == "" {
		return nil, fmt.Errorf("sso: token response carries no id_token")
	}
	info, err := parseOIDCIDToken(tokens.IDToken, cfg.Issuer, cfg.ClientID, flow.Nonce, jwks)
	if err != nil {
		return nil, err
	}
	return &OIDCLoginResult{
		OrganizationID: org.ID, Issuer: cfg.Issuer, User: info,
		BrowserBinding: flow.BrowserBinding, ConfigDigest: flow.ConfigDigest,
	}, nil
}

func (s *Service) BeginSAMLLogin(orgRef, browserBinding string) (string, error) {
	if strings.TrimSpace(browserBinding) == "" {
		return "", fmt.Errorf("sso: browser binding required")
	}
	if err := s.cleanupExpiredSSOTransactions(); err != nil {
		return "", err
	}
	org, cfg, digest, err := s.loadOrganizationSSOConfig(orgRef, "saml")
	if err != nil {
		return "", err
	}
	sp, err := samlServiceProvider(cfg)
	if err != nil {
		return "", err
	}
	state, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	idpURL := sp.GetSSOBindingLocation(saml.HTTPRedirectBinding)
	if idpURL == "" {
		return "", fmt.Errorf("sso: SAML metadata has no HTTP-Redirect SSO endpoint")
	}
	authn, err := sp.MakeAuthenticationRequest(idpURL, saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return "", fmt.Errorf("sso: create SAML authentication request: %w", err)
	}
	flow := models.SSOAuthFlow{
		OrganizationID: org.ID,
		Provider:       "saml",
		StateHash:      hashSSOState(state),
		RequestID:      authn.ID,
		ConfigDigest:   digest,
		BrowserBinding: hashSSOState(browserBinding),
		ExpiresAt:      time.Now().UTC().Add(ssoFlowLifetime),
	}
	if err := s.db.Create(&flow).Error; err != nil {
		return "", fmt.Errorf("sso: persist SAML login transaction: %w", err)
	}
	redirect, err := authn.Redirect(state, sp)
	if err != nil {
		return "", fmt.Errorf("sso: encode SAML authentication request: %w", err)
	}
	return redirect.String(), nil
}

func (s *Service) CompleteSAMLLogin(encodedResponse, relayState, browserBinding string) (*SAMLLoginResult, error) {
	if len(encodedResponse) == 0 || len(encodedResponse) > maxSAMLResponseBase64 {
		return nil, fmt.Errorf("sso: SAML response size is invalid")
	}
	flow, err := s.consumeSSOFlow("saml", relayState, browserBinding)
	if err != nil {
		return nil, err
	}
	org, cfg, digest, err := s.loadOrganizationSSOConfig(flow.OrganizationID, "saml")
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(digest), []byte(flow.ConfigDigest)) != 1 {
		return nil, fmt.Errorf("sso: organization SSO configuration changed during login")
	}
	sp, err := samlServiceProvider(cfg)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(encodedResponse)
	if err != nil {
		return nil, fmt.Errorf("sso: decode SAML response: %w", err)
	}
	if len(decoded) == 0 || len(decoded) > maxSAMLResponseXML {
		return nil, fmt.Errorf("sso: decoded SAML response size is invalid")
	}
	assertion, err := sp.ParseXMLResponse(decoded, []string{flow.RequestID}, sp.AcsURL)
	if err != nil {
		return nil, fmt.Errorf("sso: SAML response verification failed")
	}
	if err := validateSAMLAssertionBindings(assertion, cfg, flow, time.Now().UTC()); err != nil {
		return nil, err
	}
	if assertion.Subject == nil || assertion.Subject.NameID == nil || strings.TrimSpace(assertion.Subject.NameID.Value) == "" {
		return nil, fmt.Errorf("sso: SAML response carries no authenticated subject")
	}
	const persistentNameID = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	nameID := assertion.Subject.NameID
	if nameID.Format != persistentNameID {
		return nil, fmt.Errorf("sso: SAML subject must use a persistent NameID")
	}
	if nameID.NameQualifier != "" && nameID.NameQualifier != cfg.Issuer {
		return nil, fmt.Errorf("sso: SAML subject NameQualifier mismatch")
	}
	if nameID.SPNameQualifier != "" && nameID.SPNameQualifier != cfg.SPEntityID {
		return nil, fmt.Errorf("sso: SAML subject SPNameQualifier mismatch")
	}
	user := &SAMLResponse{
		UserID:     assertion.Subject.NameID.Value,
		Issuer:     cfg.Issuer,
		Attributes: make(map[string]string),
	}
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			if len(attribute.Values) == 0 {
				continue
			}
			value := attribute.Values[0].Value
			user.Attributes[attribute.Name] = value
			attributeName := attribute.Name
			if attribute.FriendlyName != "" {
				attributeName = attribute.FriendlyName
			}
			switch attributeName {
			case "email", "EmailAddress", "mail", "urn:oid:0.9.2342.19200300.100.1.3":
				user.Email = value
			case "name", "DisplayName", "cn", "urn:oid:2.5.4.3":
				user.Name = value
			case "nameKo", "koreanName":
				user.NameKo = value
			}
		}
	}
	if strings.TrimSpace(user.Email) == "" {
		return nil, fmt.Errorf("sso: SAML assertion carries no email attribute")
	}
	return &SAMLLoginResult{
		OrganizationID: org.ID, Issuer: cfg.Issuer, User: user,
		BrowserBinding: flow.BrowserBinding, ConfigDigest: flow.ConfigDigest,
	}, nil
}

func (s *Service) loadOrganizationSSOConfig(orgRef, mode string) (*models.Organization, OrganizationSSOConfig, string, error) {
	return s.loadOrganizationSSOConfigWithDB(s.db, orgRef, mode, false)
}

func (s *Service) loadOrganizationSSOConfigWithDB(db *gorm.DB, orgRef, mode string, lock bool) (*models.Organization, OrganizationSSOConfig, string, error) {
	orgRef = strings.TrimSpace(orgRef)
	if orgRef == "" {
		return nil, OrganizationSSOConfig{}, "", fmt.Errorf("sso: organization required")
	}
	var org models.Organization
	query := db
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Where("id = ? AND status = ?", orgRef, "active").First(&org).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = query.Where("slug = ? AND status = ?", orgRef, "active").First(&org).Error
	}
	if err != nil {
		return nil, OrganizationSSOConfig{}, "", fmt.Errorf("sso: organization is not available for SSO")
	}
	var cfg OrganizationSSOConfig
	if strings.TrimSpace(org.SSOConfig) == "" || json.Unmarshal([]byte(org.SSOConfig), &cfg) != nil {
		return nil, OrganizationSSOConfig{}, "", fmt.Errorf("sso: organization SSO configuration is invalid")
	}
	if strings.ToLower(strings.TrimSpace(cfg.Mode)) != mode {
		return nil, OrganizationSSOConfig{}, "", fmt.Errorf("sso: %s is not enabled for this organization", strings.ToUpper(mode))
	}
	if strings.TrimSpace(cfg.ClientSecret) != "" || strings.TrimSpace(cfg.SPPrivateKeyPEM) != "" {
		return nil, OrganizationSSOConfig{}, "", fmt.Errorf("sso: plaintext SSO credentials are not supported; use encrypted secret references")
	}
	digestMaterial := org.SSOConfig
	if cfg.ClientSecretRef != "" {
		secret, secretDigest, secretErr := s.loadOrganizationSSOSecretWithDB(db, org.ID, cfg.ClientSecretRef, lock)
		if secretErr != nil {
			return nil, OrganizationSSOConfig{}, "", secretErr
		}
		cfg.ClientSecret = secret
		digestMaterial += "\x00" + secretDigest
	}
	if mode == "saml" {
		if strings.TrimSpace(cfg.SPPrivateKeyRef) == "" {
			return nil, OrganizationSSOConfig{}, "", fmt.Errorf("sso: encrypted SAML SP private-key reference is required")
		}
		secret, secretDigest, secretErr := s.loadOrganizationSSOSecretWithDB(db, org.ID, cfg.SPPrivateKeyRef, lock)
		if secretErr != nil {
			return nil, OrganizationSSOConfig{}, "", secretErr
		}
		cfg.SPPrivateKeyPEM = secret
		digestMaterial += "\x00" + secretDigest
	}
	digest := sha256.Sum256([]byte(digestMaterial))
	return &org, cfg, hex.EncodeToString(digest[:]), nil
}

// PutOrganizationSSOSecret seals one organization-scoped SSO credential. The
// plaintext is never stored in Organization.SSOConfig or returned by an API.
func (s *Service) PutOrganizationSSOSecret(orgID, name, plaintext string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		return s.putOrganizationSSOSecretWithDB(tx, orgID, name, plaintext)
	})
}

func (s *Service) putOrganizationSSOSecretWithDB(db *gorm.DB, orgID, name, plaintext string) error {
	orgID, name = strings.TrimSpace(orgID), strings.TrimSpace(name)
	if orgID == "" || name == "" || plaintext == "" {
		return fmt.Errorf("sso: organization, secret name, and value are required")
	}
	s.mu.RLock()
	provider := s.keyProvider
	s.mu.RUnlock()
	encoded, kekID, err := keymgmt.SealEncodedWithAAD(provider, plaintext, ssoSecretAAD(orgID, name))
	if err != nil {
		return fmt.Errorf("sso: seal organization secret: %w", err)
	}
	row := models.SSOSecret{OrganizationID: orgID, Name: name, Ciphertext: encoded, KEKID: kekID}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "organization_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"ciphertext", "kek_id", "updated_at"}),
	}).Create(&row).Error
}

func (s *Service) loadOrganizationSSOSecret(orgID, name string) (string, string, error) {
	return s.loadOrganizationSSOSecretWithDB(s.db, orgID, name, false)
}

func (s *Service) loadOrganizationSSOSecretWithDB(db *gorm.DB, orgID, name string, lock bool) (string, string, error) {
	s.mu.RLock()
	provider := s.keyProvider
	s.mu.RUnlock()
	if provider == nil {
		return "", "", fmt.Errorf("sso: SSO secret provider is not configured")
	}
	var row models.SSOSecret
	query := db
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Where("organization_id = ? AND name = ?", orgID, name).First(&row).Error; err != nil {
		return "", "", fmt.Errorf("sso: configured SSO secret is unavailable")
	}
	plaintext, err := keymgmt.OpenEncodedWithAAD(provider, row.Ciphertext, row.KEKID, ssoSecretAAD(orgID, name))
	if err != nil {
		return "", "", fmt.Errorf("sso: open organization secret: %w", err)
	}
	secretDigest := sha256.Sum256([]byte(row.KEKID + "\x00" + row.Ciphertext))
	return plaintext, hex.EncodeToString(secretDigest[:]), nil
}

func ssoSecretAAD(orgID, name string) []byte {
	return []byte("DARI-SSO-SECRET-v1\x00" + orgID + "\x00" + name)
}

func (s *Service) consumeSSOFlow(provider, state, browserBinding string) (*models.SSOAuthFlow, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(browserBinding) == "" {
		return nil, fmt.Errorf("sso: login state is missing or invalid")
	}
	bindingHash := hashSSOState(browserBinding)
	var consumed models.SSOAuthFlow
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("provider = ? AND state_hash = ? AND browser_binding = ? AND consumed_at IS NULL", provider, hashSSOState(state), bindingHash).
			First(&consumed).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if !consumed.ExpiresAt.After(now) {
			return gorm.ErrRecordNotFound
		}
		updated := tx.Model(&models.SSOAuthFlow{}).
			Where("id = ? AND browser_binding = ? AND consumed_at IS NULL AND expires_at > ?", consumed.ID, bindingHash, now).
			Update("consumed_at", now)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sso: login state is missing, expired, or already used")
	}
	return &consumed, nil
}

func (s *Service) CreateLoginHandoff(orgID, userID, provider, browserBindingHash, configDigest, sourceIssuer, sourceSubject string) (string, error) {
	source := identity.NormalizeExternalIdentity(sourceIssuer, sourceSubject)
	if orgID == "" || userID == "" || (provider != "oidc" && provider != "saml") || browserBindingHash == "" || configDigest == "" || source.Issuer == "" || source.Subject == "" {
		return "", fmt.Errorf("sso: invalid login handoff")
	}
	code, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	handoff := models.SSOLoginHandoff{
		OrganizationID: orgID,
		UserID:         userID,
		Provider:       provider,
		SourceIssuer:   source.Issuer,
		SourceSubject:  source.Subject,
		CodeHash:       hashSSOState(code),
		BrowserBinding: browserBindingHash,
		ConfigDigest:   configDigest,
		ExpiresAt:      time.Now().UTC().Add(ssoHandoffLifetime),
	}
	if err := s.db.Create(&handoff).Error; err != nil {
		return "", fmt.Errorf("sso: persist login handoff: %w", err)
	}
	return code, nil
}

func (s *Service) ConsumeLoginHandoff(code, provider, browserBinding string) (*models.SSOLoginHandoff, error) {
	if strings.TrimSpace(code) == "" || (provider != "oidc" && provider != "saml") || strings.TrimSpace(browserBinding) == "" {
		return nil, fmt.Errorf("sso: login handoff is missing or invalid")
	}
	var consumed models.SSOLoginHandoff
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("code_hash = ? AND provider = ? AND consumed_at IS NULL", hashSSOState(code), provider).
			First(&consumed).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if !consumed.ExpiresAt.After(now) || !matchesBrowserBinding(consumed.BrowserBinding, browserBinding) {
			return gorm.ErrRecordNotFound
		}
		updated := tx.Model(&models.SSOLoginHandoff{}).
			Where("id = ? AND consumed_at IS NULL AND expires_at > ?", consumed.ID, now).
			Update("consumed_at", now)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sso: login handoff is missing, expired, bound to another browser, or already used")
	}
	return &consumed, nil
}

// RedeemLoginHandoff performs the final SSO authorization decision in one
// transaction. The organization configuration, referenced secret, managed user,
// audit write, token decision, and one-time handoff consumption therefore share
// one commit boundary. A concurrent organization disable or SSO rotation either
// wins before these locks (and the login fails) or waits until no bearer can be
// delivered from the old configuration.
func (s *Service) RedeemLoginHandoff(
	code, provider, browserBinding string,
	complete func(tx *gorm.DB, handoff *models.SSOLoginHandoff, user *models.User) error,
) error {
	if strings.TrimSpace(code) == "" || (provider != "oidc" && provider != "saml") || strings.TrimSpace(browserBinding) == "" || complete == nil {
		return ErrInvalidLoginHandoff
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var handoff models.SSOLoginHandoff
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("code_hash = ? AND provider = ? AND consumed_at IS NULL", hashSSOState(code), provider).
			First(&handoff).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if !handoff.ExpiresAt.After(now) || !matchesBrowserBinding(handoff.BrowserBinding, browserBinding) {
			return gorm.ErrRecordNotFound
		}
		_, _, digest, err := s.loadOrganizationSSOConfigWithDB(tx, handoff.OrganizationID, provider, true)
		if err != nil {
			return err
		}
		if handoff.ConfigDigest == "" || subtle.ConstantTimeCompare([]byte(handoff.ConfigDigest), []byte(digest)) != 1 {
			return fmt.Errorf("sso: organization SSO configuration changed during login")
		}
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND organization_id = ? AND status = ?", handoff.UserID, handoff.OrganizationID, models.UserStatusActive).
			First(&user).Error; err != nil {
			return fmt.Errorf("sso: user is unavailable")
		}
		if err := complete(tx, &handoff, &user); err != nil {
			return err
		}
		updated := tx.Model(&models.SSOLoginHandoff{}).
			Where("id = ? AND consumed_at IS NULL AND expires_at > ?", handoff.ID, now).
			Update("consumed_at", now)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidLoginHandoff
		}
		return err
	}
	return nil
}

func (s *Service) LoginCompletionURL(orgID, provider, handoffCode string) (string, error) {
	_, cfg, _, err := s.loadOrganizationSSOConfig(orgID, provider)
	if err != nil {
		return "", err
	}
	callbackURL := cfg.RedirectURI
	if provider == "saml" {
		callbackURL = cfg.ACSURL
	}
	completion, err := url.Parse(callbackURL)
	if err != nil || !validSSOURL(completion, true) {
		return "", fmt.Errorf("sso: %s completion URL is invalid", strings.ToUpper(provider))
	}
	completion.Path = "/login"
	completion.RawQuery = url.Values{
		"sso_handoff": []string{handoffCode}, "sso_provider": []string{provider},
	}.Encode()
	completion.Fragment = ""
	return completion.String(), nil
}

func (s *Service) ValidateSSOCompletion(orgID, provider, expectedDigest string) error {
	_, _, digest, err := s.loadOrganizationSSOConfig(orgID, provider)
	if err != nil {
		return err
	}
	if expectedDigest == "" || subtle.ConstantTimeCompare([]byte(expectedDigest), []byte(digest)) != 1 {
		return fmt.Errorf("sso: organization SSO configuration changed during login")
	}
	return nil
}

func samlServiceProvider(cfg OrganizationSSOConfig) (*saml.ServiceProvider, error) {
	if strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.IDPMetadata) == "" ||
		strings.TrimSpace(cfg.SPEntityID) == "" || strings.TrimSpace(cfg.ACSURL) == "" {
		return nil, fmt.Errorf("sso: SAML issuer, metadata, SP entity ID, and ACS URL are required")
	}
	metadata, err := samlsp.ParseMetadata([]byte(cfg.IDPMetadata))
	if err != nil {
		return nil, fmt.Errorf("sso: parse SAML IdP metadata: %w", err)
	}
	if metadata.EntityID != cfg.Issuer {
		return nil, fmt.Errorf("sso: SAML metadata issuer does not match configured issuer")
	}
	acs, err := url.Parse(cfg.ACSURL)
	if err != nil || !validSSOURL(acs, true) {
		return nil, fmt.Errorf("sso: SAML ACS URL is invalid")
	}
	key, cert, signatureMethod, err := parseSAMLSigningCredential(cfg.SPPrivateKeyPEM, cfg.SPCertificatePEM)
	if err != nil {
		return nil, err
	}
	sp := &saml.ServiceProvider{
		EntityID: cfg.SPEntityID, AcsURL: *acs, IDPMetadata: metadata,
		Key: key, Certificate: cert, SignatureMethod: signatureMethod,
	}
	idpRedirect, err := url.Parse(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding))
	if err != nil || !validSSOURL(idpRedirect, true) {
		return nil, fmt.Errorf("sso: SAML HTTP-Redirect SSO endpoint is invalid")
	}
	return sp, nil
}

func validateOIDCConfig(cfg OrganizationSSOConfig) error {
	if strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.ClientID) == "" ||
		strings.TrimSpace(cfg.AuthorizationURL) == "" || strings.TrimSpace(cfg.TokenURL) == "" ||
		strings.TrimSpace(cfg.RedirectURI) == "" || len(cfg.JWKS) == 0 {
		return fmt.Errorf("sso: OIDC issuer, client, endpoints, redirect URI, and JWKS are required")
	}
	for name, raw := range map[string]string{
		"issuer": cfg.Issuer, "authorization endpoint": cfg.AuthorizationURL,
		"token endpoint": cfg.TokenURL, "redirect URI": cfg.RedirectURI,
	} {
		parsed, err := url.Parse(raw)
		if err != nil || !validSSOURL(parsed, true) {
			return fmt.Errorf("sso: OIDC %s is invalid", name)
		}
	}
	return nil
}

func validateSAMLAssertionBindings(assertion *saml.Assertion, cfg OrganizationSSOConfig, flow *models.SSOAuthFlow, now time.Time) error {
	if assertion == nil || assertion.Conditions == nil || len(assertion.Conditions.AudienceRestrictions) == 0 {
		return fmt.Errorf("sso: SAML assertion has no audience restriction")
	}
	for _, restriction := range assertion.Conditions.AudienceRestrictions {
		if strings.TrimSpace(restriction.Audience.Value) != cfg.SPEntityID {
			return fmt.Errorf("sso: SAML assertion audience mismatch")
		}
	}
	if assertion.Subject == nil {
		return fmt.Errorf("sso: SAML assertion has no subject confirmation")
	}
	const bearerMethod = "urn:oasis:names:tc:SAML:2.0:cm:bearer"
	for _, confirmation := range assertion.Subject.SubjectConfirmations {
		data := confirmation.SubjectConfirmationData
		if confirmation.Method != bearerMethod || data == nil {
			continue
		}
		if data.InResponseTo != flow.RequestID || data.Recipient != cfg.ACSURL || data.NotOnOrAfter.IsZero() || !now.Before(data.NotOnOrAfter) {
			continue
		}
		if !data.NotBefore.IsZero() && now.Add(2*time.Minute).Before(data.NotBefore) {
			continue
		}
		return nil
	}
	return fmt.Errorf("sso: SAML bearer subject confirmation is missing or invalid")
}

func parseSAMLSigningCredential(privateKeyPEM, certificatePEM string) (crypto.Signer, *x509.Certificate, string, error) {
	keyBlock, _ := pem.Decode([]byte(privateKeyPEM))
	certBlock, _ := pem.Decode([]byte(certificatePEM))
	if keyBlock == nil || certBlock == nil {
		return nil, nil, "", fmt.Errorf("sso: SAML SP signing key and certificate are required")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, "", fmt.Errorf("sso: parse SAML SP certificate: %w", err)
	}
	var signer crypto.Signer
	if key, parseErr := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); parseErr == nil {
		signer, _ = key.(crypto.Signer)
	} else if key, parseErr := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); parseErr == nil {
		signer = key
	} else if key, parseErr := x509.ParseECPrivateKey(keyBlock.Bytes); parseErr == nil {
		signer = key
	}
	if signer == nil {
		return nil, nil, "", fmt.Errorf("sso: SAML SP private key is invalid")
	}
	if !publicKeysEqual(signer.Public(), cert.PublicKey) {
		return nil, nil, "", fmt.Errorf("sso: SAML SP private key does not match certificate")
	}
	switch signer.(type) {
	case *rsa.PrivateKey:
		return signer, cert, dsig.RSASHA256SignatureMethod, nil
	case *ecdsa.PrivateKey:
		return signer, cert, dsig.ECDSASHA256SignatureMethod, nil
	default:
		return nil, nil, "", fmt.Errorf("sso: unsupported SAML SP signing key type")
	}
}

func publicKeysEqual(left, right interface{}) bool {
	leftDER, leftErr := x509.MarshalPKIXPublicKey(left)
	rightDER, rightErr := x509.MarshalPKIXPublicKey(right)
	return leftErr == nil && rightErr == nil && subtle.ConstantTimeCompare(leftDER, rightDER) == 1
}

func matchesBrowserBinding(expectedHash, presented string) bool {
	if expectedHash == "" || strings.TrimSpace(presented) == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(hashSSOState(presented))) == 1
}

func (s *Service) cleanupExpiredSSOTransactions() error {
	now := time.Now().UTC()
	// These are ephemeral bearer records, not domain objects that need soft-delete
	// retention. Physically remove them so public login traffic cannot grow the
	// underlying tables forever while ordinary GORM counts merely hide the rows.
	if err := s.db.Unscoped().Where("expires_at < ? OR (consumed_at IS NOT NULL AND consumed_at < ?)", now, now.Add(-time.Hour)).Delete(&models.SSOAuthFlow{}).Error; err != nil {
		return fmt.Errorf("sso: clean expired login transactions: %w", err)
	}
	if err := s.db.Unscoped().Where("expires_at < ? OR (consumed_at IS NOT NULL AND consumed_at < ?)", now, now.Add(-time.Hour)).Delete(&models.SSOLoginHandoff{}).Error; err != nil {
		return fmt.Errorf("sso: clean expired login handoffs: %w", err)
	}
	return nil
}

func validSSOURL(parsed *url.URL, allowLoopbackHTTP bool) bool {
	if parsed == nil || !parsed.IsAbs() || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return true
	}
	if !allowLoopbackHTTP || !strings.EqualFold(parsed.Scheme, "http") {
		return false
	}
	host := strings.TrimSpace(parsed.Hostname())
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func randomURLToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("sso: generate secure login state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashSSOState(state string) string {
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:])
}
