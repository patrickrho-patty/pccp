package keycloakconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type realmImport struct {
	Realm                  string             `json:"realm"`
	Enabled                bool               `json:"enabled"`
	RegistrationAllowed    bool               `json:"registrationAllowed"`
	VerifyEmail            bool               `json:"verifyEmail"`
	DuplicateEmailsAllowed bool               `json:"duplicateEmailsAllowed"`
	BruteForceProtected    bool               `json:"bruteForceProtected"`
	Internationalization   bool               `json:"internationalizationEnabled"`
	SupportedLocales       []string           `json:"supportedLocales"`
	DefaultLocale          string             `json:"defaultLocale"`
	Clients                []realmClient      `json:"clients"`
	ClientScopes           []realmClientScope `json:"clientScopes"`
	IdentityProviders      []identityProvider `json:"identityProviders"`
}

func TestKeycloakImageAndProviderArtifactsArePinned(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(source), "..", "..", "deployments", "keycloak", "Dockerfile")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Keycloak Dockerfile: %v", err)
	}
	dockerfile := string(b)
	for _, required := range []string{
		"quay.io/keycloak/keycloak:26.7.1@sha256:f1f1f01e472c8a78df40d8f2a49a925274eda4d3d80d5f6edbb5c880ee3c01c6",
		"apple-identity-provider-1.17.0.jar",
		"4091dee2a1ec9e0771bef4bd46005197d86b0a2b1f25198c41738476b1d102bb",
		"naver-identity-provider-1.0.0.jar",
		"kc.sh build",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Fatalf("Keycloak image contract is missing %q", required)
		}
	}
}

type realmClient struct {
	ClientID            string            `json:"clientId"`
	PublicClient        bool              `json:"publicClient"`
	StandardFlowEnabled bool              `json:"standardFlowEnabled"`
	ImplicitFlowEnabled bool              `json:"implicitFlowEnabled"`
	DirectGrantsEnabled bool              `json:"directAccessGrantsEnabled"`
	RedirectURIs        []string          `json:"redirectUris"`
	WebOrigins          []string          `json:"webOrigins"`
	Attributes          map[string]string `json:"attributes"`
	DefaultClientScopes []string          `json:"defaultClientScopes"`
	ProtocolMappers     []protocolMapper  `json:"protocolMappers"`
}

type realmClientScope struct {
	Name       string            `json:"name"`
	Protocol   string            `json:"protocol"`
	Attributes map[string]string `json:"attributes"`
}

type protocolMapper struct {
	ProtocolMapper string            `json:"protocolMapper"`
	Config         map[string]string `json:"config"`
}

type identityProvider struct {
	Alias   string            `json:"alias"`
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config"`
}

func TestPublicRealmImportContract(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(source), "..", "..", "deployments", "keycloak", "realm-patty.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read realm import: %v", err)
	}
	var realm realmImport
	if err := json.Unmarshal(b, &realm); err != nil {
		t.Fatalf("decode realm import: %v", err)
	}
	if realm.Realm != "patty" || !realm.Enabled {
		t.Fatalf("public realm must be enabled and named patty: %+v", realm)
	}
	if !realm.RegistrationAllowed || !realm.VerifyEmail || realm.DuplicateEmailsAllowed || !realm.BruteForceProtected {
		t.Fatalf("public registration security contract violated: %+v", realm)
	}
	if !realm.Internationalization || realm.DefaultLocale != "ko" || !sameStrings(realm.SupportedLocales, []string{"en", "ko"}) {
		t.Fatalf("KO/EN locale contract violated: %+v", realm)
	}

	account := clientByID(realm.Clients, "patty-account")
	if account == nil || !account.PublicClient || !account.StandardFlowEnabled || account.ImplicitFlowEnabled || account.DirectGrantsEnabled {
		t.Fatalf("account client must be public authorization-code-only: %+v", account)
	}
	if !sameStrings(account.RedirectURIs, []string{"https://id.patty.io/auth/callback"}) || !sameStrings(account.WebOrigins, []string{"https://id.patty.io"}) {
		t.Fatalf("account redirect/origin must be exact: %+v", account)
	}

	device := clientByID(realm.Clients, "patcode-device")
	if device == nil || !device.PublicClient || device.Attributes["oauth2.device.authorization.grant.enabled"] != "true" {
		t.Fatalf("patcode device grant must be enabled: %+v", device)
	}
	if device.StandardFlowEnabled || device.ImplicitFlowEnabled || device.DirectGrantsEnabled || len(device.RedirectURIs) != 0 || device.Attributes["pkce.code.challenge.method"] != "" {
		t.Fatalf("device client must be isolated from browser/password grants: %+v", device)
	}
	assertHarnessEnrollmentContract(t, realm, device, "patcode-device")

	pkce := clientByID(realm.Clients, "patcode-pkce")
	if pkce == nil || !pkce.PublicClient || !pkce.StandardFlowEnabled || pkce.Attributes["pkce.code.challenge.method"] != "S256" {
		t.Fatalf("native system-browser client must require authorization code + S256 PKCE: %+v", pkce)
	}
	if pkce.ImplicitFlowEnabled || pkce.DirectGrantsEnabled || pkce.Attributes["oauth2.device.authorization.grant.enabled"] == "true" {
		t.Fatalf("native PKCE client must be isolated from device/implicit/password grants: %+v", pkce)
	}
	assertHarnessEnrollmentContract(t, realm, pkce, "patcode-pkce")

	for _, alias := range []string{"google", "apple", "kakao", "naver"} {
		provider := providerByAlias(realm.IdentityProviders, alias)
		if provider == nil {
			t.Fatalf("missing declared social provider %q", alias)
		}
		if provider.Enabled {
			t.Fatalf("provider %q must remain disabled until its secret and claim evidence are installed", alias)
		}
	}
	if got := providerByAlias(realm.IdentityProviders, "kakao").Config["jwksUrl"]; got != "https://kauth.kakao.com/.well-known/jwks.json" {
		t.Fatalf("Kakao signature verification needs its explicit JWKS URL, got %q", got)
	}
}

func assertHarnessEnrollmentContract(t *testing.T, realm realmImport, client *realmClient, audience string) {
	t.Helper()
	foundScope := false
	for _, scope := range realm.ClientScopes {
		if scope.Name == "harness-enroll" && scope.Protocol == "openid-connect" && scope.Attributes["include.in.token.scope"] == "true" {
			foundScope = true
		}
	}
	if !foundScope || !containsString(client.DefaultClientScopes, "harness-enroll") {
		t.Fatalf("%s must receive the explicit harness-enroll scope", audience)
	}
	for _, mapper := range client.ProtocolMappers {
		if mapper.ProtocolMapper == "oidc-audience-mapper" && mapper.Config["included.client.audience"] == audience && mapper.Config["access.token.claim"] == "true" {
			return
		}
	}
	t.Fatalf("%s must add its client audience to access tokens", audience)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func clientByID(clients []realmClient, id string) *realmClient {
	for i := range clients {
		if clients[i].ClientID == id {
			return &clients[i]
		}
	}
	return nil
}

func providerByAlias(providers []identityProvider, alias string) *identityProvider {
	for i := range providers {
		if providers[i].Alias == alias {
			return &providers[i]
		}
	}
	return nil
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
