package sso

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/golang-jwt/jwt/v5"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

func TestOIDCLoginBindsStateNoncePKCEAndOrganizationConfig(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	provider, err := keymgmt.NewLocalProvider(bytes.Repeat([]byte{0x41}, 32), "sso-test-kek")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetKeyProvider(provider)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwks, err := json.Marshal(map[string]any{"keys": []map[string]any{{
		"kty": "EC", "crv": "P-256", "kid": "oidc-key",
		"x": base64.RawURLEncoding.EncodeToString(privateKey.X.FillBytes(make([]byte, 32))),
		"y": base64.RawURLEncoding.EncodeToString(privateKey.Y.FillBytes(make([]byte, 32))),
	}}})
	if err != nil {
		t.Fatal(err)
	}

	var nonce, verifier, configuredRedirect string
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		verifier = r.Form.Get("code_verifier")
		if r.Form.Get("redirect_uri") != configuredRedirect {
			t.Errorf("token redirect_uri = %q, want configured %q", r.Form.Get("redirect_uri"), configuredRedirect)
		}
		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"iss": idp.URL, "aud": "pccp-client", "sub": "subject-1",
			"email": "subject-1@example.com", "name": "Subject One",
			"email_verified": true, "nonce": nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = "oidc-key"
		signed, signErr := token.SignedString(privateKey)
		if signErr != nil {
			t.Error(signErr)
		}
		_ = json.NewEncoder(w).Encode(OIDCTokenResponse{IDToken: signed, TokenType: "Bearer"})
	}))
	defer idp.Close()

	configuredRedirect = "https://console.patty.example/api/sso/oidc/callback"
	cfg := OrganizationSSOConfig{
		Provider: "authentik", Mode: "oidc", Issuer: idp.URL,
		ClientID: "pccp-client", ClientSecretRef: "oidc-client-secret",
		AuthorizationURL: idp.URL + "/authorize", TokenURL: idp.URL + "/token",
		RedirectURI: configuredRedirect, JWKS: jwks,
	}
	rawConfig, _ := json.Marshal(cfg)
	org := models.Organization{Name: "OIDC Org", Slug: "oidc-org", Status: "active", SSOConfig: string(rawConfig)}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.PutOrganizationSSOSecret(org.ID, cfg.ClientSecretRef, "server-secret"); err != nil {
		t.Fatal(err)
	}
	var storedSecret models.SSOSecret
	if err := db.Where("organization_id = ? AND name = ?", org.ID, cfg.ClientSecretRef).First(&storedSecret).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(storedSecret.Ciphertext, "server-secret") || strings.Contains(org.SSOConfig, "server-secret") {
		t.Fatal("OIDC client secret was persisted in plaintext")
	}
	if err := ValidateProviderReadiness(db, nil); err == nil {
		t.Fatal("startup readiness accepted encrypted SSO configuration without a key provider")
	}
	if err := ValidateProviderReadiness(db, provider); err != nil {
		t.Fatalf("startup readiness rejected decryptable SSO configuration: %v", err)
	}

	const browserBinding = "browser-transaction-binding"
	authURL, err := svc.BeginOIDCLogin(org.Slug, browserBinding)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	state := query.Get("state")
	nonce = query.Get("nonce")
	challenge := query.Get("code_challenge")
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != cfg.AuthorizationURL {
		t.Fatalf("authorization endpoint = %q, want configured %q", parsed.Scheme+"://"+parsed.Host+parsed.Path, cfg.AuthorizationURL)
	}
	if state == "" || nonce == "" || challenge == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("missing generated OIDC binding: %s", parsed.RawQuery)
	}
	if query.Get("client_id") != cfg.ClientID || query.Get("redirect_uri") != cfg.RedirectURI {
		t.Fatalf("OIDC request was not config-bound: %s", parsed.RawQuery)
	}
	if _, err := svc.CompleteOIDCLogin(context.Background(), "code", state, "different-browser"); err == nil {
		t.Fatal("OIDC transaction was accepted from a different browser binding")
	}
	if _, err := svc.CompleteOIDCLogin(context.Background(), "code", "attacker-state", browserBinding); err == nil {
		t.Fatal("attacker-controlled OIDC state was accepted")
	}
	nonce = "substituted-nonce"
	if _, err := svc.CompleteOIDCLogin(context.Background(), "code", state, browserBinding); err == nil {
		t.Fatal("ID token with a substituted nonce was accepted")
	}

	authURL, err = svc.BeginOIDCLogin(org.Slug, browserBinding)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	query = parsed.Query()
	state = query.Get("state")
	nonce = query.Get("nonce")
	challenge = query.Get("code_challenge")
	result, err := svc.CompleteOIDCLogin(context.Background(), "code", state, browserBinding)
	if err != nil {
		t.Fatal(err)
	}
	if result.OrganizationID != org.ID || result.Issuer != idp.URL || result.User.Sub != "subject-1" {
		t.Fatalf("OIDC result = %+v", result)
	}
	verifierDigest := sha256.Sum256([]byte(verifier))
	if verifier == "" || base64.RawURLEncoding.EncodeToString(verifierDigest[:]) != challenge {
		t.Fatal("token exchange was not bound to the authorization request PKCE challenge")
	}
	if _, err := svc.CompleteOIDCLogin(context.Background(), "code", state, browserBinding); err == nil {
		t.Fatal("OIDC state replay was accepted")
	}
}

func TestRedeemLoginHandoffLocksCurrentConfigAndConsumesOnlyAfterCommit(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	config := `{"mode":"oidc","provider":"authentik"}`
	org := models.Organization{Name: "Atomic SSO", Slug: "atomic-sso", Status: "active", SSOConfig: config}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{
		AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "atomic@example.com",
		Name: "Before", Status: models.UserStatusActive, AuthMethod: "oidc",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	_, _, digest, err := svc.loadOrganizationSSOConfig(org.ID, "oidc")
	if err != nil {
		t.Fatal(err)
	}
	const browserBinding = "atomic-browser-binding"
	code, err := svc.CreateLoginHandoff(org.ID, user.ID, "oidc", hashSSOState(browserBinding), digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Organization{}).Where("id = ?", org.ID).
		Update("sso_config", `{"mode":"oidc","provider":"rotated"}`).Error; err != nil {
		t.Fatal(err)
	}
	called := false
	if err := svc.RedeemLoginHandoff(code, "oidc", browserBinding, func(*gorm.DB, *models.SSOLoginHandoff, *models.User) error {
		called = true
		return nil
	}); err == nil {
		t.Fatal("handoff created under a replaced SSO configuration was redeemed")
	}
	if called {
		t.Fatal("completion callback ran after configuration revocation")
	}
	var stale models.SSOLoginHandoff
	if err := db.Where("code_hash = ?", hashSSOState(code)).First(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if stale.ConsumedAt != nil {
		t.Fatal("rejected handoff was consumed")
	}

	_, _, currentDigest, err := svc.loadOrganizationSSOConfig(org.ID, "oidc")
	if err != nil {
		t.Fatal(err)
	}
	rollbackCode, err := svc.CreateLoginHandoff(org.ID, user.ID, "oidc", hashSSOState(browserBinding), currentDigest)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("audit write failed")
	if err := svc.RedeemLoginHandoff(rollbackCode, "oidc", browserBinding, func(tx *gorm.DB, _ *models.SSOLoginHandoff, locked *models.User) error {
		if err := tx.Model(locked).Update("name", "Must Roll Back").Error; err != nil {
			return err
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("completion error = %v, want %v", err, sentinel)
	}
	var rolledBackUser models.User
	if err := db.First(&rolledBackUser, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if rolledBackUser.Name != "Before" {
		t.Fatalf("failed completion persisted profile side effect: %q", rolledBackUser.Name)
	}
	var retryable models.SSOLoginHandoff
	if err := db.Where("code_hash = ?", hashSSOState(rollbackCode)).First(&retryable).Error; err != nil {
		t.Fatal(err)
	}
	if retryable.ConsumedAt != nil {
		t.Fatal("failed completion burned the one-time handoff")
	}
	if err := svc.RedeemLoginHandoff(rollbackCode, "oidc", browserBinding, func(*gorm.DB, *models.SSOLoginHandoff, *models.User) error {
		return nil
	}); err != nil {
		t.Fatalf("retry after rolled-back completion failed: %v", err)
	}
	if err := db.Where("code_hash = ?", hashSSOState(rollbackCode)).First(&retryable).Error; err != nil {
		t.Fatal(err)
	}
	if retryable.ConsumedAt == nil {
		t.Fatal("successful completion did not consume the handoff")
	}
}

func TestSAMLLoginRequiresSignedConfigBoundResponseAndOneTimeRequest(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	provider, err := keymgmt.NewLocalProvider(bytes.Repeat([]byte{0x42}, 32), "saml-test-kek")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetKeyProvider(provider)
	idp, cfg := newTestSAMLIdentity(t)
	privateKeyPEM := cfg.SPPrivateKeyPEM
	assertionCfg := cfg
	cfg.SPPrivateKeyPEM = ""
	cfg.SPPrivateKeyRef = "saml-sp-key"
	rawConfig, _ := json.Marshal(cfg)
	org := models.Organization{Name: "SAML Org", Slug: "saml-org", Status: "active", SSOConfig: string(rawConfig)}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.PutOrganizationSSOSecret(org.ID, cfg.SPPrivateKeyRef, privateKeyPEM); err != nil {
		t.Fatal(err)
	}

	const browserBinding = "saml-browser-transaction-binding"
	redirect, err := svc.BeginSAMLLogin(org.ID, browserBinding)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(redirect)
	state := parsed.Query().Get("RelayState")
	if state == "" || parsed.Query().Get("SAMLRequest") == "" || parsed.Query().Get("SigAlg") == "" || parsed.Query().Get("Signature") == "" {
		t.Fatalf("SAML request missing transaction binding: %s", redirect)
	}
	var flow models.SSOAuthFlow
	if err := db.Where("organization_id = ? AND provider = ?", org.ID, "saml").First(&flow).Error; err != nil {
		t.Fatal(err)
	}

	unsigned := base64.StdEncoding.EncodeToString([]byte(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"><saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"><saml:Subject><saml:NameID>attacker</saml:NameID></saml:Subject></saml:Assertion></samlp:Response>`))
	if _, err := svc.CompleteSAMLLogin(unsigned, state, browserBinding); err == nil {
		t.Fatal("unsigned SAML response was accepted")
	}

	assertRejected := func(name string, mutate func(*saml.Assertion)) {
		t.Helper()
		redirectURL, beginErr := svc.BeginSAMLLogin(org.ID, browserBinding)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		parsedURL, _ := url.Parse(redirectURL)
		var pending models.SSOAuthFlow
		if err := db.Where("organization_id = ? AND provider = ? AND consumed_at IS NULL", org.ID, "saml").Order("created_at DESC").First(&pending).Error; err != nil {
			t.Fatal(err)
		}
		response := signedSAMLResponseWithMutation(t, idp, assertionCfg, pending.RequestID, mutate)
		if _, err := svc.CompleteSAMLLogin(response, parsedURL.Query().Get("RelayState"), browserBinding); err == nil {
			t.Fatalf("%s SAML assertion was accepted", name)
		}
	}
	assertRejected("missing-audience", func(assertion *saml.Assertion) {
		assertion.Conditions.AudienceRestrictions = nil
	})
	assertRejected("wrong-audience", func(assertion *saml.Assertion) {
		assertion.Conditions.AudienceRestrictions[0].Audience.Value = "https://other-sp.example"
	})
	assertRejected("missing-subject-confirmation", func(assertion *saml.Assertion) {
		assertion.Subject.SubjectConfirmations = nil
	})
	assertRejected("wrong-subject-confirmation-method", func(assertion *saml.Assertion) {
		assertion.Subject.SubjectConfirmations[0].Method = "urn:example:not-bearer"
	})
	assertRejected("wrong-subject-confirmation-recipient", func(assertion *saml.Assertion) {
		assertion.Subject.SubjectConfirmations[0].SubjectConfirmationData.Recipient = "https://attacker.example/acs"
	})
	assertRejected("transient-name-id", func(assertion *saml.Assertion) {
		assertion.Subject.NameID.Format = "urn:oasis:names:tc:SAML:2.0:nameid-format:transient"
	})
	assertRejected("wrong-name-qualifier", func(assertion *saml.Assertion) {
		assertion.Subject.NameID.NameQualifier = "https://other-idp.example"
	})

	redirect, err = svc.BeginSAMLLogin(org.ID, browserBinding)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ = url.Parse(redirect)
	state = parsed.Query().Get("RelayState")
	flow = models.SSOAuthFlow{}
	if err := db.Where("organization_id = ? AND provider = ? AND consumed_at IS NULL", org.ID, "saml").Order("created_at DESC").First(&flow).Error; err != nil {
		t.Fatal(err)
	}
	signed := signedSAMLResponse(t, idp, assertionCfg, flow.RequestID)
	result, err := svc.CompleteSAMLLogin(signed, state, browserBinding)
	if err != nil {
		t.Fatal(err)
	}
	if result.OrganizationID != org.ID || result.Issuer != cfg.Issuer || result.User.UserID != "saml-subject" || result.User.Email != "saml@example.com" {
		t.Fatalf("verified SAML result = %+v", result)
	}
	if _, err := svc.CompleteSAMLLogin(signed, state, browserBinding); err == nil {
		t.Fatal("SAML RelayState replay was accepted")
	}
}

func TestExternalIdentityIsScopedByMethodAndVerifiedIssuer(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	org := models.Organization{Name: "Identity Namespaces", Slug: "identity-namespaces", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	one, err := svc.ProvisionOIDCUser(org.ID, "https://issuer-a.example", &OIDCUserInfo{Sub: "shared-sub", Email: "one@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := svc.ProvisionOIDCUser(org.ID, "https://issuer-b.example", &OIDCUserInfo{Sub: "shared-sub", Email: "two@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	three, err := svc.ProvisionUserFromSSO(org.ID, "https://issuer-a.example", &SAMLResponse{UserID: "shared-sub", Email: "three@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if one.ID == two.ID || one.ID == three.ID || two.ID == three.ID {
		t.Fatalf("provider namespaces collapsed: %s %s %s", one.ID, two.ID, three.ID)
	}
	if one.ExternalIssuer != "https://issuer-a.example" || two.ExternalIssuer != "https://issuer-b.example" || three.AuthMethod != "saml" {
		t.Fatalf("provider namespace was not persisted: %+v %+v %+v", one, two, three)
	}
}

func TestSSOHandoffAndPublicLimitsAreBrowserBoundAndBounded(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	code, err := svc.CreateLoginHandoff("org-1", "user-1", "saml", hashSSOState("browser-one"), "config-digest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConsumeLoginHandoff(code, "saml", "browser-two"); err == nil {
		t.Fatal("SSO handoff was accepted from a different browser")
	}
	if _, err := svc.ConsumeLoginHandoff(code, "saml", "browser-one"); err != nil {
		t.Fatalf("valid SSO handoff was rejected: %v", err)
	}
	if _, err := svc.ConsumeLoginHandoff(code, "saml", "browser-one"); err == nil {
		t.Fatal("SSO handoff replay was accepted")
	}
	var releases []func()
	for i := 0; i < 16; i++ {
		release, allowed := svc.BeginPublicRequest("concurrent-request")
		if !allowed {
			t.Fatalf("public SSO concurrency slot %d was unexpectedly denied", i+1)
		}
		releases = append(releases, release)
	}
	if release, allowed := svc.BeginPublicRequest("overflow-request"); allowed {
		release()
		t.Fatal("public SSO concurrency ceiling was not enforced")
	}
	for _, release := range releases {
		release()
	}

	for i := 0; i < 30; i++ {
		release, allowed := svc.BeginPublicRequest("oidc-start|org|127.0.0.1")
		if !allowed {
			t.Fatalf("public SSO request %d was throttled before the configured ceiling", i+1)
		}
		release()
	}
	if release, allowed := svc.BeginPublicRequest("oidc-start|org|127.0.0.1"); allowed {
		release()
		t.Fatal("public SSO request budget was not enforced")
	}

	expiredAt := time.Now().UTC().Add(-time.Hour)
	expiredFlow := models.SSOAuthFlow{
		OrganizationID: "org-1", Provider: "oidc", StateHash: hashSSOState("expired-state"),
		ConfigDigest: "digest", BrowserBinding: hashSSOState("expired-browser"), ExpiresAt: expiredAt,
	}
	expiredHandoff := models.SSOLoginHandoff{
		OrganizationID: "org-1", UserID: "user-1", Provider: "saml", CodeHash: hashSSOState("expired-code"),
		BrowserBinding: hashSSOState("expired-browser"), ExpiresAt: expiredAt,
	}
	if err := db.Create(&expiredFlow).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&expiredHandoff).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.cleanupExpiredSSOTransactions(); err != nil {
		t.Fatal(err)
	}
	var retained int64
	if err := db.Unscoped().Model(&models.SSOAuthFlow{}).Where("id = ?", expiredFlow.ID).Count(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if retained != 0 {
		t.Fatal("expired SSO flow was only soft-deleted")
	}
	if err := db.Unscoped().Model(&models.SSOLoginHandoff{}).Where("id = ?", expiredHandoff.ID).Count(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if retained != 0 {
		t.Fatal("expired SSO handoff was only soft-deleted")
	}
}

// Self-contained SAML test identity: production verification is exercised
// against a real signed crewjam/saml response without depending on a network IdP.
func newTestSAMLIdentity(t *testing.T) (*saml.IdentityProvider, OrganizationSSOConfig) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-idp"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	metadataURL, _ := url.Parse("https://idp.example/metadata")
	ssoURL, _ := url.Parse("https://idp.example/sso")
	idp := &saml.IdentityProvider{Key: key, Certificate: cert, MetadataURL: *metadataURL, SSOURL: *ssoURL}
	metadata, err := xml.Marshal(idp.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	spKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	spTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "test-sp"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true,
	}
	spDER, err := x509.CreateCertificate(rand.Reader, spTemplate, spTemplate, &spKey.PublicKey, spKey)
	if err != nil {
		t.Fatal(err)
	}
	return idp, OrganizationSSOConfig{
		Provider: "authentik", Mode: "saml", Issuer: metadataURL.String(),
		IDPMetadata: string(metadata), SPEntityID: "https://pccp.example/saml/metadata",
		ACSURL:           "https://pccp.example/api/sso/saml/callback",
		SPPrivateKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(spKey)})),
		SPCertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: spDER})),
	}
}

// Self-contained signed response fixture; using the same library on the IdP
// side keeps the test focused on the SP's cryptographic validation contract.
func signedSAMLResponse(t *testing.T, idp *saml.IdentityProvider, cfg OrganizationSSOConfig, requestID string) string {
	return signedSAMLResponseWithMutation(t, idp, cfg, requestID, nil)
}

func signedSAMLResponseWithMutation(t *testing.T, idp *saml.IdentityProvider, cfg OrganizationSSOConfig, requestID string, mutate func(*saml.Assertion)) string {
	t.Helper()
	sp, err := samlServiceProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata := sp.Metadata()
	descriptor := &metadata.SPSSODescriptors[0]
	endpoint := &descriptor.AssertionConsumerServices[0]
	now := time.Now().UTC()
	req := &saml.IdpAuthnRequest{
		IDP: idp, HTTPRequest: httptest.NewRequest(http.MethodGet, idp.SSOURL.String(), nil),
		Request:                 saml.AuthnRequest{ID: requestID, IssueInstant: now, Version: "2.0", Issuer: &saml.Issuer{Value: cfg.SPEntityID}},
		ServiceProviderMetadata: metadata, SPSSODescriptor: descriptor, ACSEndpoint: endpoint, Now: now,
	}
	if err := (saml.DefaultAssertionMaker{}).MakeAssertion(req, &saml.Session{
		ID: "session", Index: "session-index", NameID: "saml-subject",
		CreateTime: now, ExpireTime: now.Add(time.Hour), UserEmail: "saml@example.com",
		CustomAttributes: []saml.Attribute{{Name: "email", Values: []saml.AttributeValue{{Type: "xs:string", Value: "saml@example.com"}}}},
	}); err != nil {
		t.Fatal(err)
	}
	req.Assertion.Subject.NameID.Format = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	req.Assertion.Subject.NameID.NameQualifier = cfg.Issuer
	req.Assertion.Subject.NameID.SPNameQualifier = cfg.SPEntityID
	if mutate != nil {
		mutate(req.Assertion)
	}
	form, err := req.PostBinding()
	if err != nil {
		t.Fatal(err)
	}
	return form.SAMLResponse
}
