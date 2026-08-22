package sso

import (
	"encoding/json"
	"testing"
)

func TestReviewedOIDCTemplatesRenderHTTPSSecretReferenceConfigs(t *testing.T) {
	jwks := json.RawMessage(`{"keys":[{"kty":"RSA"}]}`)
	for _, test := range []struct {
		id    string
		input IdPTemplateInput
	}{
		{"okta-oidc", IdPTemplateInput{Issuer: "https://acme.okta.com/oauth2/default", ClientID: "client", ClientSecretRef: "okta-secret", RedirectURI: "https://console.example/api/sso/oidc/callback", JWKS: jwks}},
		{"entra-oidc", IdPTemplateInput{Tenant: "tenant-id", ClientID: "client", ClientSecretRef: "entra-secret", RedirectURI: "https://console.example/api/sso/oidc/callback", JWKS: jwks}},
		{"google-workspace-oidc", IdPTemplateInput{ClientID: "client", ClientSecretRef: "google-secret", RedirectURI: "https://console.example/api/sso/oidc/callback", JWKS: jwks}},
	} {
		cfg, err := RenderIdPTemplate(test.id, test.input)
		if err != nil {
			t.Fatalf("%s: %v", test.id, err)
		}
		if cfg.ClientSecret != "" || cfg.ClientSecretRef == "" || cfg.Mode != "oidc" {
			t.Fatalf("%s rendered unsafe config: %+v", test.id, cfg)
		}
	}
}

func TestTemplateRejectsPlainHTTPAndMissingSecretReference(t *testing.T) {
	_, err := RenderIdPTemplate("generic-oidc", IdPTemplateInput{
		Issuer: "http://idp.example", AuthorizationURL: "http://idp.example/auth", TokenURL: "http://idp.example/token",
		RedirectURI: "https://console.example/callback", ClientID: "client", JWKS: json.RawMessage(`{"keys":[]}`),
	})
	if err == nil {
		t.Fatal("expected unsafe generic OIDC template to fail")
	}
	_, err = RenderIdPTemplate("generic-oidc", IdPTemplateInput{
		Issuer: "http://localhost:8080", AuthorizationURL: "http://localhost:8080/auth", TokenURL: "http://localhost:8080/token",
		RedirectURI: "https://console.example/callback", ClientID: "client", ClientSecretRef: "secret", JWKS: json.RawMessage(`{"keys":[]}`),
	})
	if err == nil {
		t.Fatal("reviewed production template accepted loopback HTTP")
	}
}
