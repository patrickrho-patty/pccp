package sso

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type IdPTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Mode        string `json:"mode"`
	Description string `json:"description"`
}

type ApplyIdPTemplateRequest struct {
	TemplateID   string
	Input        IdPTemplateInput
	ClientSecret string
	SPPrivateKey string
}

// ApplyOrganizationIdPTemplate renders a reviewed template, seals the only
// plaintext credentials in the organization-scoped secret store, and switches
// the active SSO configuration in one transaction. Organization.SSOConfig
// therefore never contains a client secret or private key.
func (s *Service) ApplyOrganizationIdPTemplate(orgID, actorID string, req ApplyIdPTemplateRequest) (OrganizationSSOConfig, error) {
	orgID, actorID = strings.TrimSpace(orgID), strings.TrimSpace(actorID)
	if orgID == "" || actorID == "" {
		return OrganizationSSOConfig{}, errors.New("sso: organization and actor are required")
	}
	cfg, err := RenderIdPTemplate(req.TemplateID, req.Input)
	if err != nil {
		return OrganizationSSOConfig{}, err
	}
	if cfg.Mode == "oidc" && strings.TrimSpace(req.ClientSecret) == "" {
		return OrganizationSSOConfig{}, errors.New("sso: OIDC client secret is required")
	}
	if cfg.Mode == "saml" && strings.TrimSpace(req.SPPrivateKey) == "" {
		return OrganizationSSOConfig{}, errors.New("sso: SAML SP private key is required")
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return OrganizationSSOConfig{}, fmt.Errorf("sso: encode organization configuration: %w", err)
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var org models.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", orgID, "active").First(&org).Error; err != nil {
			return fmt.Errorf("sso: active organization not found: %w", err)
		}
		if cfg.Mode == "oidc" {
			if err := s.putOrganizationSSOSecretWithDB(tx, orgID, cfg.ClientSecretRef, req.ClientSecret); err != nil {
				return err
			}
		} else if err := s.putOrganizationSSOSecretWithDB(tx, orgID, cfg.SPPrivateKeyRef, req.SPPrivateKey); err != nil {
			return err
		}
		if err := tx.Model(&org).Update("sso_config", string(encoded)).Error; err != nil {
			return fmt.Errorf("sso: activate organization configuration: %w", err)
		}
		details, _ := json.Marshal(map[string]string{"template_id": strings.TrimSpace(req.TemplateID), "mode": cfg.Mode})
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.auth.sso_template_applied", ActorType: "admin", ActorID: actorID,
			Action: "apply_sso_template", ResourceType: "organization", ResourceID: orgID,
			Details: string(details), Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		return OrganizationSSOConfig{}, err
	}
	return cfg, nil
}

type IdPTemplateInput struct {
	Issuer           string          `json:"issuer,omitempty"`
	Tenant           string          `json:"tenant,omitempty"`
	ClientID         string          `json:"client_id,omitempty"`
	ClientSecretRef  string          `json:"client_secret_ref,omitempty"`
	AuthorizationURL string          `json:"authorization_url,omitempty"`
	TokenURL         string          `json:"token_url,omitempty"`
	RedirectURI      string          `json:"redirect_uri,omitempty"`
	JWKS             json.RawMessage `json:"jwks,omitempty"`
	IDPMetadata      string          `json:"saml_idp_metadata,omitempty"`
	SPEntityID       string          `json:"saml_sp_entity_id,omitempty"`
	ACSURL           string          `json:"saml_acs_url,omitempty"`
	SPPrivateKeyRef  string          `json:"saml_sp_private_key_ref,omitempty"`
	SPCertificatePEM string          `json:"saml_sp_certificate,omitempty"`
}

func IdPTemplates() []IdPTemplate {
	return []IdPTemplate{
		{ID: "okta-oidc", Name: "Okta OIDC", Mode: "oidc", Description: "Okta authorization server with immutable subject matching"},
		{ID: "entra-oidc", Name: "Microsoft Entra ID", Mode: "oidc", Description: "Tenant-specific Microsoft identity platform v2 endpoints"},
		{ID: "google-workspace-oidc", Name: "Google Workspace", Mode: "oidc", Description: "Google OpenID Connect for managed Workspace tenants"},
		{ID: "generic-oidc", Name: "Generic OIDC", Mode: "oidc", Description: "Explicit issuer, authorization, token, and JWKS configuration"},
		{ID: "generic-saml", Name: "Generic SAML 2.0", Mode: "saml", Description: "Signed metadata and encrypted SP private-key reference"},
	}
}

func RenderIdPTemplate(templateID string, input IdPTemplateInput) (OrganizationSSOConfig, error) {
	templateID = strings.ToLower(strings.TrimSpace(templateID))
	cfg := OrganizationSSOConfig{
		Provider: templateID, ClientID: strings.TrimSpace(input.ClientID), ClientSecretRef: strings.TrimSpace(input.ClientSecretRef),
		RedirectURI: strings.TrimSpace(input.RedirectURI), JWKS: input.JWKS, IDPMetadata: strings.TrimSpace(input.IDPMetadata),
		SPEntityID: strings.TrimSpace(input.SPEntityID), ACSURL: strings.TrimSpace(input.ACSURL),
		SPPrivateKeyRef: strings.TrimSpace(input.SPPrivateKeyRef), SPCertificatePEM: strings.TrimSpace(input.SPCertificatePEM),
	}
	switch templateID {
	case "okta-oidc":
		cfg.Mode = "oidc"
		cfg.Issuer = strings.TrimRight(strings.TrimSpace(input.Issuer), "/")
		cfg.AuthorizationURL = cfg.Issuer + "/v1/authorize"
		cfg.TokenURL = cfg.Issuer + "/v1/token"
	case "entra-oidc":
		cfg.Mode = "oidc"
		tenant := strings.Trim(strings.TrimSpace(input.Tenant), "/")
		if tenant == "" || strings.ContainsAny(tenant, "?#") {
			return OrganizationSSOConfig{}, errors.New("sso: Entra tenant is required")
		}
		cfg.Issuer = "https://login.microsoftonline.com/" + tenant + "/v2.0"
		cfg.AuthorizationURL = "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/authorize"
		cfg.TokenURL = "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/token"
	case "google-workspace-oidc":
		cfg.Mode, cfg.Issuer = "oidc", "https://accounts.google.com"
		cfg.AuthorizationURL = "https://accounts.google.com/o/oauth2/v2/auth"
		cfg.TokenURL = "https://oauth2.googleapis.com/token"
	case "generic-oidc":
		cfg.Mode = "oidc"
		cfg.Issuer, cfg.AuthorizationURL, cfg.TokenURL = strings.TrimSpace(input.Issuer), strings.TrimSpace(input.AuthorizationURL), strings.TrimSpace(input.TokenURL)
	case "generic-saml":
		cfg.Mode, cfg.Issuer = "saml", strings.TrimSpace(input.Issuer)
	default:
		return OrganizationSSOConfig{}, fmt.Errorf("sso: unknown IdP template %q", templateID)
	}
	if err := validateRenderedTemplate(cfg); err != nil {
		return OrganizationSSOConfig{}, err
	}
	return cfg, nil
}

func validateRenderedTemplate(cfg OrganizationSSOConfig) error {
	if cfg.Mode == "oidc" {
		for label, raw := range map[string]string{"issuer": cfg.Issuer, "authorization_url": cfg.AuthorizationURL, "token_url": cfg.TokenURL, "redirect_uri": cfg.RedirectURI} {
			parsed, err := url.Parse(raw)
			if err != nil || !validSSOURL(parsed, false) {
				return fmt.Errorf("sso: OIDC %s must be an absolute HTTPS URL", label)
			}
		}
		if cfg.ClientID == "" || cfg.ClientSecretRef == "" || len(cfg.JWKS) == 0 || !json.Valid(cfg.JWKS) {
			return errors.New("sso: OIDC client_id, client_secret_ref, and valid JWKS are required")
		}
		return nil
	}
	if cfg.Issuer == "" || cfg.IDPMetadata == "" || cfg.SPEntityID == "" || cfg.ACSURL == "" || cfg.SPPrivateKeyRef == "" || cfg.SPCertificatePEM == "" {
		return errors.New("sso: SAML issuer, metadata, SP entity/ACS, private-key reference, and certificate are required")
	}
	for label, raw := range map[string]string{"saml_sp_entity_id": cfg.SPEntityID, "saml_acs_url": cfg.ACSURL} {
		parsed, err := url.Parse(raw)
		if err != nil || !validSSOURL(parsed, false) {
			return fmt.Errorf("sso: %s must be an absolute HTTPS URL", label)
		}
	}
	return nil
}
