package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/golang-jwt/jwt/v5"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	ssosvc "github.com/patrickrho-patty/pccp/internal/sso"
)

func TestOIDCAuthStartIgnoresCallerControlledProviderRedirectAndState(t *testing.T) {
	db := apiDB(t)
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwks, _ := json.Marshal(map[string]any{"keys": []map[string]any{{
		"kty": "EC", "crv": "P-256", "kid": "k1",
		"x": base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))),
		"y": base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32))),
	}}})
	var oidcNonce string
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		claims := jwt.MapClaims{
			"iss": idp.URL, "sub": "browser-subject", "email": "oidc-browser@example.com", "email_verified": true,
			"aud": "configured-client", "nonce": oidcNonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		token.Header["kid"] = "k1"
		signed, signErr := token.SignedString(key)
		if signErr != nil {
			t.Error(signErr)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": signed, "token_type": "Bearer"})
	}))
	defer idp.Close()
	config, _ := json.Marshal(map[string]any{
		"provider": "authentik", "mode": "oidc", "issuer": idp.URL,
		"client_id":         "configured-client",
		"authorization_url": idp.URL + "/authorize",
		"token_url":         idp.URL + "/token",
		"redirect_uri":      "https://console.patty.example/api/sso/oidc/callback",
		"jwks":              json.RawMessage(jwks),
	})
	org := models.Organization{Name: "SSO Org", Slug: "sso-org", Status: "active", SSOConfig: string(config)}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/sso/oidc/auth-url?organization=sso-org&issuer=https://evil.example&client_id=evil&redirect_uri=https://evil.example/callback&state=attacker-state", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(body["auth_url"])
	if err != nil {
		t.Fatal(err)
	}
	if authURL.Host != strings.TrimPrefix(idp.URL, "http://") || authURL.Query().Get("client_id") != "configured-client" ||
		authURL.Query().Get("redirect_uri") != "https://console.patty.example/api/sso/oidc/callback" {
		t.Fatalf("caller overrode organization SSO configuration: %s", authURL)
	}
	if authURL.Query().Get("state") == "attacker-state" || authURL.Query().Get("nonce") == "" || authURL.Query().Get("code_challenge") == "" {
		t.Fatalf("server did not generate OIDC transaction bindings: %s", authURL)
	}
	var transactionCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == ssoOIDCCookie {
			transactionCookie = cookie
			break
		}
	}
	if transactionCookie == nil || !transactionCookie.HttpOnly || !transactionCookie.Secure || transactionCookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("OIDC transaction cookie is not Secure/HttpOnly/SameSite=None: %+v", transactionCookie)
	}

	callbackPath := "/api/sso/oidc/callback?code=authorization-code&state=" + url.QueryEscape(authURL.Query().Get("state"))
	callback := httptest.NewRequest(http.MethodGet, callbackPath, nil)
	callbackRecorder := httptest.NewRecorder()
	srv.ServeHTTP(callbackRecorder, callback)
	if callbackRecorder.Code != http.StatusBadRequest {
		t.Fatalf("callback without initiating-browser cookie returned %d, want 400", callbackRecorder.Code)
	}
	var flow models.SSOAuthFlow
	if err := db.Where("organization_id = ? AND provider = ?", org.ID, "oidc").First(&flow).Error; err != nil {
		t.Fatal(err)
	}
	oidcNonce = flow.Nonce
	callback = httptest.NewRequest(http.MethodGet, callbackPath, nil)
	callback.AddCookie(transactionCookie)
	callbackRecorder = httptest.NewRecorder()
	srv.ServeHTTP(callbackRecorder, callback)
	if callbackRecorder.Code != http.StatusSeeOther {
		t.Fatalf("OIDC browser callback status=%d body=%s", callbackRecorder.Code, callbackRecorder.Body.String())
	}
	completion, err := url.Parse(callbackRecorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	handoffCode := completion.Query().Get("sso_handoff")
	if completion.Path != "/login" || completion.Query().Get("sso_provider") != "oidc" || handoffCode == "" {
		t.Fatalf("OIDC callback did not redirect through browser handoff: %s", completion)
	}
	exchangeBody, _ := json.Marshal(map[string]string{"code": handoffCode, "provider": "oidc"})
	exchange := httptest.NewRequest(http.MethodPost, "/api/sso/session", bytes.NewReader(exchangeBody))
	exchange.Header.Set("Content-Type", "application/json")
	exchange.AddCookie(transactionCookie)
	exchangeRecorder := httptest.NewRecorder()
	srv.ServeHTTP(exchangeRecorder, exchange)
	if exchangeRecorder.Code != http.StatusOK || !strings.Contains(exchangeRecorder.Body.String(), `"token"`) {
		t.Fatalf("OIDC browser handoff status=%d body=%s", exchangeRecorder.Code, exchangeRecorder.Body.String())
	}
}

func TestSAMLFormPostUsesBrowserBoundOneTimeSPAHandoff(t *testing.T) {
	db := apiDB(t)
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := keymgmt.NewLocalProvider(bytes.Repeat([]byte{0x51}, 32), "api-sso-kek")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetKeyProvider(provider)

	idpKey, idpCert := testSAMLCertificate(t, "api-test-idp", 11)
	metadataURL, _ := url.Parse("https://idp.example/metadata")
	ssoURL, _ := url.Parse("https://idp.example/sso")
	idp := &saml.IdentityProvider{Key: idpKey, Certificate: idpCert, MetadataURL: *metadataURL, SSOURL: *ssoURL}
	idpMetadata, err := xml.Marshal(idp.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	spKey, spCert := testSAMLCertificate(t, "api-test-sp", 12)
	spKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(spKey)}))
	spCertPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: spCert.Raw}))
	cfg := ssosvc.OrganizationSSOConfig{
		Provider: "authentik", Mode: "saml", Issuer: metadataURL.String(),
		IDPMetadata: string(idpMetadata), SPEntityID: "https://pccp.example/saml/metadata",
		ACSURL: "https://pccp.example/api/sso/saml/callback", SPPrivateKeyRef: "sp-signing-key",
		SPCertificatePEM: spCertPEM,
	}
	rawConfig, _ := json.Marshal(cfg)
	org := models.Organization{Name: "SAML Browser Org", Slug: "saml-browser", Status: "active", SSOConfig: string(rawConfig)}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	if err := srv.ext().SSO.PutOrganizationSSOSecret(org.ID, cfg.SPPrivateKeyRef, spKeyPEM); err != nil {
		t.Fatal(err)
	}

	start := httptest.NewRequest(http.MethodPost, "/api/sso/saml/redirect", strings.NewReader(`{"organization":"saml-browser"}`))
	start.Header.Set("Content-Type", "application/json")
	startRecorder := httptest.NewRecorder()
	srv.ServeHTTP(startRecorder, start)
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("SAML start status=%d body=%s", startRecorder.Code, startRecorder.Body.String())
	}
	var startBody map[string]string
	if err := json.Unmarshal(startRecorder.Body.Bytes(), &startBody); err != nil {
		t.Fatal(err)
	}
	redirectURL, err := url.Parse(startBody["redirect_url"])
	if err != nil {
		t.Fatal(err)
	}
	state := redirectURL.Query().Get("RelayState")
	if state == "" || redirectURL.Query().Get("Signature") == "" {
		t.Fatalf("SAML redirect lacks transaction/signature bindings: %s", redirectURL)
	}
	var transactionCookie *http.Cookie
	for _, cookie := range startRecorder.Result().Cookies() {
		if cookie.Name == ssoSAMLCookie {
			transactionCookie = cookie
		}
	}
	if transactionCookie == nil || !transactionCookie.HttpOnly || !transactionCookie.Secure {
		t.Fatalf("missing secure SAML transaction cookie: %+v", transactionCookie)
	}
	var flow models.SSOAuthFlow
	if err := db.Where("organization_id = ? AND provider = ? AND consumed_at IS NULL", org.ID, "saml").First(&flow).Error; err != nil {
		t.Fatal(err)
	}

	acsURL, _ := url.Parse(cfg.ACSURL)
	sp := &saml.ServiceProvider{EntityID: cfg.SPEntityID, Key: spKey, Certificate: spCert, AcsURL: *acsURL, IDPMetadata: idp.Metadata()}
	metadata := sp.Metadata()
	descriptor := &metadata.SPSSODescriptors[0]
	endpoint := &descriptor.AssertionConsumerServices[0]
	now := time.Now().UTC()
	idpRequest := &saml.IdpAuthnRequest{
		IDP: idp, HTTPRequest: httptest.NewRequest(http.MethodGet, idp.SSOURL.String(), nil),
		Request:                 saml.AuthnRequest{ID: flow.RequestID, IssueInstant: now, Version: "2.0", Issuer: &saml.Issuer{Value: cfg.SPEntityID}},
		ServiceProviderMetadata: metadata, SPSSODescriptor: descriptor, ACSEndpoint: endpoint, Now: now,
	}
	if err := (saml.DefaultAssertionMaker{}).MakeAssertion(idpRequest, &saml.Session{
		ID: "session", Index: "session-index", NameID: "api-saml-subject",
		CreateTime: now, ExpireTime: now.Add(time.Hour), UserEmail: "api-saml@example.com",
		CustomAttributes: []saml.Attribute{{Name: "email", Values: []saml.AttributeValue{{Type: "xs:string", Value: "api-saml@example.com"}}}},
	}); err != nil {
		t.Fatal(err)
	}
	idpRequest.Assertion.Subject.NameID.Format = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	idpRequest.Assertion.Subject.NameID.NameQualifier = cfg.Issuer
	idpRequest.Assertion.Subject.NameID.SPNameQualifier = cfg.SPEntityID
	form, err := idpRequest.PostBinding()
	if err != nil {
		t.Fatal(err)
	}
	callbackForm := url.Values{"SAMLResponse": {form.SAMLResponse}, "RelayState": {state}}
	callback := httptest.NewRequest(http.MethodPost, "/api/sso/saml/callback", strings.NewReader(callbackForm.Encode()))
	callback.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callback.AddCookie(transactionCookie)
	callbackRecorder := httptest.NewRecorder()
	srv.ServeHTTP(callbackRecorder, callback)
	if callbackRecorder.Code != http.StatusSeeOther {
		t.Fatalf("SAML ACS status=%d body=%s", callbackRecorder.Code, callbackRecorder.Body.String())
	}
	completion, err := url.Parse(callbackRecorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	handoffCode := completion.Query().Get("sso_handoff")
	if completion.Path != "/login" || completion.Query().Get("sso_provider") != "saml" || handoffCode == "" {
		t.Fatalf("SAML ACS did not redirect through a one-time SPA handoff: %s", completion)
	}

	exchangeBody, _ := json.Marshal(map[string]string{"code": handoffCode, "provider": "saml"})
	exchange := httptest.NewRequest(http.MethodPost, "/api/sso/session", bytes.NewReader(exchangeBody))
	exchange.Header.Set("Content-Type", "application/json")
	exchange.AddCookie(transactionCookie)
	exchangeRecorder := httptest.NewRecorder()
	srv.ServeHTTP(exchangeRecorder, exchange)
	if exchangeRecorder.Code != http.StatusOK || !strings.Contains(exchangeRecorder.Body.String(), `"token"`) {
		t.Fatalf("SAML handoff exchange status=%d body=%s", exchangeRecorder.Code, exchangeRecorder.Body.String())
	}

	replay := httptest.NewRequest(http.MethodPost, "/api/sso/session", bytes.NewReader(exchangeBody))
	replay.Header.Set("Content-Type", "application/json")
	replay.AddCookie(transactionCookie)
	replayRecorder := httptest.NewRecorder()
	srv.ServeHTTP(replayRecorder, replay)
	if replayRecorder.Code != http.StatusBadRequest {
		t.Fatalf("replayed SAML handoff returned %d, want 400", replayRecorder.Code)
	}
}

func TestOrganizationReadsAreTenantScopedAndSSOConfigIsRedacted(t *testing.T) {
	db := apiDB(t)
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	orgA := models.Organization{Name: "Tenant A", Slug: "tenant-a", Status: "active", SSOConfig: `{"client_secret":"must-not-leak"}`}
	orgB := models.Organization{Name: "Tenant B", Slug: "tenant-b", Status: "active", SSOConfig: `{"client_secret":"other-secret"}`}
	if err := db.Create(&orgA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&orgB).Error; err != nil {
		t.Fatal(err)
	}
	if err := srv.auth.BootstrapAdmin("tenant-a-admin@example.com", "password", orgA.ID); err != nil {
		t.Fatal(err)
	}
	token, err := srv.auth.Login("tenant-a-admin@example.com", "password")
	if err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/organizations/", nil)
	list.Header.Set("Authorization", "Bearer "+token)
	listRecorder := httptest.NewRecorder()
	srv.ServeHTTP(listRecorder, list)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("organization list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var organizations []map[string]any
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &organizations); err != nil {
		t.Fatal(err)
	}
	if len(organizations) != 1 || organizations[0]["id"] != orgA.ID {
		t.Fatalf("tenant A saw an unexpected organization set: %+v", organizations)
	}
	if _, leaked := organizations[0]["sso_config"]; leaked || strings.Contains(listRecorder.Body.String(), "must-not-leak") {
		t.Fatal("organization response exposed server-only SSO configuration")
	}

	getOther := httptest.NewRequest(http.MethodGet, "/api/organizations/"+orgB.ID, nil)
	getOther.Header.Set("Authorization", "Bearer "+token)
	getOtherRecorder := httptest.NewRecorder()
	srv.ServeHTTP(getOtherRecorder, getOther)
	if getOtherRecorder.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant organization read returned %d, want 404", getOtherRecorder.Code)
	}
}

func TestProtectedAPINeverDisablesAuthenticationForSSOOnlyTenant(t *testing.T) {
	db := apiDB(t)
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "SSO Only", Slug: "sso-only", Status: "active", SSOConfig: `{"mode":"oidc"}`}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.User{
		AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "sso@example.com", Name: "SSO User",
		Status: models.UserStatusActive, AuthMethod: "oidc", ExternalIssuer: "https://idp.example", ExternalID: "subject-1",
	}).Error; err != nil {
		t.Fatal(err)
	}
	var credentials int64
	if err := db.Model(&identity.AdminCredentials{}).Count(&credentials).Error; err != nil {
		t.Fatal(err)
	}
	if credentials != 0 {
		t.Fatalf("fixture unexpectedly has %d local credentials", credentials)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	recorder := httptest.NewRecorder()
	srv.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("SSO-only tenant exposed protected API without a token: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPublicSSORateKeyCannotBeRotatedWithCallerInput(t *testing.T) {
	first := httptest.NewRequest(http.MethodGet, "/api/sso/oidc/auth-url?organization=tenant-a", nil)
	first.RemoteAddr = "192.0.2.10:41000"
	second := httptest.NewRequest(http.MethodGet, "/api/sso/oidc/auth-url?organization=attacker-controlled", nil)
	second.RemoteAddr = "192.0.2.10:42000"
	if got, want := publicSSORateKey(first, "oidc-start"), publicSSORateKey(second, "oidc-start"); got != want {
		t.Fatalf("caller-controlled organization or source port changed the rate key: %q != %q", got, want)
	}
}

func testSAMLCertificate(t *testing.T, commonName string, serial int64) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: commonName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, certificate
}
