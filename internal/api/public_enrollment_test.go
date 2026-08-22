package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sso"
)

type fixedPublicTokenVerifier struct {
	claims *sso.FirstPartyAccessClaims
	err    error
}

func TestPublicEnrollmentGrantRateLimitsBeforeTokenVerification(t *testing.T) {
	srv, _ := harnessTestServer(t)
	for i := 0; i < 30; i++ {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(`{}`)))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was limited early", i+1)
		}
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(`{}`)))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 31=%d want 429: %s", rec.Code, rec.Body.String())
	}
}

func (v fixedPublicTokenVerifier) VerifyAccessToken(context.Context, string) (*sso.FirstPartyAccessClaims, error) {
	return v.claims, v.err
}

func TestPublicEnrollmentGrantBindsPaidOIDCAccountSeatAndHarnessKey(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	account := models.Account{
		Email: "subscriber@example.com", DisplayName: "Subscriber", Profile: "public",
		SubscriptionStatus: "active", SubscriptionPlan: "pro", MaxHarnesses: 1, MaxActiveHarnesses: 1,
		AuthorityID: "worker-account-paid",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Subscription{
		AccountID: account.ID, Plan: "pro", Status: "active", StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339), MaxHarnesses: 1, MaxActiveHarnesses: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AccountExternalIdentity{AccountID: account.ID, Issuer: "https://login.patty.io/realms/patty", Subject: "keycloak-subject"}).Error; err != nil {
		t.Fatal(err)
	}
	srv.SetPublicTokenVerifier(fixedPublicTokenVerifier{claims: &sso.FirstPartyAccessClaims{
		Issuer: "https://login.patty.io/realms/patty", Subject: "keycloak-subject", Email: account.Email,
		EmailVerified: true, Scopes: []string{"openid", "harness-enroll"},
	}})

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	grantBody, _ := json.Marshal(map[string]string{
		"harness_id": "hrn-public-paid", "public_key_hex": hex.EncodeToString(publicKey),
	})
	grantReq := httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(string(grantBody)))
	grantReq.Header.Set("Authorization", "Bearer oidc-access-token")
	grantRec := httptest.NewRecorder()
	srv.ServeHTTP(grantRec, grantReq)
	if grantRec.Code != http.StatusCreated {
		t.Fatalf("grant = %d, want 201: %s", grantRec.Code, grantRec.Body.String())
	}
	var grant struct {
		EnrollmentCode string `json:"enrollment_code"`
		OrganizationID string `json:"organization_id"`
		UserID         string `json:"user_id"`
		Plan           string `json:"plan"`
	}
	if err := json.Unmarshal(grantRec.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}
	if grant.EnrollmentCode == "" || grant.OrganizationID != account.ID || grant.UserID == "" || grant.Plan != "pro" {
		t.Fatalf("grant response = %+v", grant)
	}

	tamperedKey, _, _ := ed25519.GenerateKey(rand.Reader)
	tamperedBody, _ := json.Marshal(map[string]string{
		"organization_id": grant.OrganizationID, "user_id": grant.UserID, "harness_id": "hrn-public-paid",
		"public_key_hex": hex.EncodeToString(tamperedKey), "binary_version": "1.0.0", "enrollment_code": grant.EnrollmentCode,
	})
	tamperedRec := httptest.NewRecorder()
	srv.ServeHTTP(tamperedRec, httptest.NewRequest(http.MethodPost, "/api/public/harnesses/enroll", strings.NewReader(string(tamperedBody))))
	if tamperedRec.Code != http.StatusForbidden {
		t.Fatalf("tampered public key = %d, want 403: %s", tamperedRec.Code, tamperedRec.Body.String())
	}

	enrollBody, _ := json.Marshal(map[string]string{
		"organization_id": grant.OrganizationID, "user_id": grant.UserID, "harness_id": "hrn-public-paid",
		"public_key_hex": hex.EncodeToString(publicKey), "binary_version": "1.0.0", "enrollment_code": grant.EnrollmentCode,
	})
	enrollRec := httptest.NewRecorder()
	srv.ServeHTTP(enrollRec, httptest.NewRequest(http.MethodPost, "/api/public/harnesses/enroll", strings.NewReader(string(enrollBody))))
	if enrollRec.Code != http.StatusCreated {
		t.Fatalf("public enrollment = %d, want 201: %s", enrollRec.Code, enrollRec.Body.String())
	}
	var enrollmentResponse publicHarnessEnrollmentResponseV1
	if err := json.Unmarshal(enrollRec.Body.Bytes(), &enrollmentResponse); err != nil {
		t.Fatal(err)
	}
	if len(enrollmentResponse.Credential.SignedCredential) == 0 || strings.Contains(enrollRec.Body.String(), `"harness"`) {
		t.Fatalf("public enrollment response does not match v1 projection: %s", enrollRec.Body.String())
	}
	var harness models.Harness
	if err := db.Where("organization_id = ? AND harness_id = ?", account.ID, "hrn-public-paid").First(&harness).Error; err != nil {
		t.Fatal(err)
	}
	if harness.CredentialJSON == "" {
		t.Fatal("public enrollment did not persist a PPC")
	}
	t.Setenv("PCCP_ACCOUNTS_SERVICE_TOKEN", "accounts-service-secret")
	listReq := httptest.NewRequest(http.MethodGet, "/api/internal/public-harnesses?account_id=worker-account-paid&issuer=https%3A%2F%2Flogin.patty.io%2Frealms%2Fpatty&subject=keycloak-subject", nil)
	listReq.Header.Set("Authorization", "Bearer accounts-service-secret")
	listRec := httptest.NewRecorder()
	srv.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), harness.HarnessID) || !strings.Contains(listRec.Body.String(), `"name":"pro"`) {
		t.Fatalf("account Harness list = %d: %s", listRec.Code, listRec.Body.String())
	}
	oldCredential, _ := hex.DecodeString(harness.CredentialJSON)
	oldDigest := sha256.Sum256(oldCredential)
	signedAt := time.Now().UTC().Format(time.RFC3339Nano)
	signature := ed25519.Sign(privateKey, identity.HarnessRenewalSigningBytes(harness.HarnessID, hex.EncodeToString(oldDigest[:]), signedAt))
	renewBody, _ := json.Marshal(map[string]string{
		"harness_id": harness.HarnessID, "credential_sha256": hex.EncodeToString(oldDigest[:]),
		"signed_at": signedAt, "signature_hex": hex.EncodeToString(signature),
	})
	if err := db.Model(&account).Update("account_integrity_state", "restricted").Error; err != nil {
		t.Fatal(err)
	}
	blockedRec := httptest.NewRecorder()
	srv.ServeHTTP(blockedRec, httptest.NewRequest(http.MethodPost, "/api/public/harnesses/renew", strings.NewReader(string(renewBody))))
	if blockedRec.Code != http.StatusForbidden {
		t.Fatalf("restricted account renewal = %d, want 403: %s", blockedRec.Code, blockedRec.Body.String())
	}
	if err := db.Model(&account).Update("account_integrity_state", "normal").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Subscription{}).Where("account_id = ?", account.ID).Update("expires_at", time.Now().Add(-8*24*time.Hour).Format(time.RFC3339)).Error; err != nil {
		t.Fatal(err)
	}
	expiredRec := httptest.NewRecorder()
	srv.ServeHTTP(expiredRec, httptest.NewRequest(http.MethodPost, "/api/public/harnesses/renew", strings.NewReader(string(renewBody))))
	if expiredRec.Code != http.StatusPaymentRequired {
		t.Fatalf("expired plan renewal = %d, want 402: %s", expiredRec.Code, expiredRec.Body.String())
	}
	if err := db.Model(&models.Subscription{}).Where("account_id = ?", account.ID).Update("expires_at", time.Now().Add(time.Hour).Format(time.RFC3339)).Error; err != nil {
		t.Fatal(err)
	}
	renewRec := httptest.NewRecorder()
	srv.ServeHTTP(renewRec, httptest.NewRequest(http.MethodPost, "/api/public/harnesses/renew", strings.NewReader(string(renewBody))))
	if renewRec.Code != http.StatusOK {
		t.Fatalf("public renewal = %d, want 200: %s", renewRec.Code, renewRec.Body.String())
	}
	if err := db.First(&harness, "id = ?", harness.ID).Error; err != nil {
		t.Fatal(err)
	}
	if harness.CredentialJSON == hex.EncodeToString(oldCredential) {
		t.Fatal("renewal did not rotate the Harness PPC")
	}
	replayRec := httptest.NewRecorder()
	srv.ServeHTTP(replayRec, httptest.NewRequest(http.MethodPost, "/api/public/harnesses/renew", strings.NewReader(string(renewBody))))
	if replayRec.Code != http.StatusConflict {
		t.Fatalf("renewal replay = %d, want 409: %s", replayRec.Code, replayRec.Body.String())
	}

	secondKey, _, _ := ed25519.GenerateKey(rand.Reader)
	secondGrantBody, _ := json.Marshal(map[string]string{
		"harness_id": "hrn-public-over-seat", "public_key_hex": hex.EncodeToString(secondKey),
	})
	secondReq := httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(string(secondGrantBody)))
	secondReq.Header.Set("Authorization", "Bearer oidc-access-token")
	secondRec := httptest.NewRecorder()
	srv.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusPaymentRequired {
		t.Fatalf("full seat grant = %d, want 402: %s", secondRec.Code, secondRec.Body.String())
	}
}

func TestPublicEnrollmentGrantRejectsUnpaidOrUnscopedIdentity(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	account := models.Account{Email: "free@example.com", Profile: "public", SubscriptionStatus: "none"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AccountExternalIdentity{AccountID: account.ID, Issuer: "https://login.patty.io/realms/patty", Subject: "free-sub"}).Error; err != nil {
		t.Fatal(err)
	}
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	body, _ := json.Marshal(map[string]string{"harness_id": "hrn-free", "public_key_hex": hex.EncodeToString(publicKey)})

	srv.SetPublicTokenVerifier(fixedPublicTokenVerifier{claims: &sso.FirstPartyAccessClaims{
		Issuer: "https://login.patty.io/realms/patty", Subject: "free-sub", Email: account.Email,
		EmailVerified: true, Scopes: []string{"openid", "harness-enroll"},
	}})
	req := httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer unpaid")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("unpaid grant = %d, want 402: %s", rec.Code, rec.Body.String())
	}

	srv.SetPublicTokenVerifier(fixedPublicTokenVerifier{claims: &sso.FirstPartyAccessClaims{
		Issuer: "https://login.patty.io/realms/patty", Subject: "free-sub", Email: account.Email,
		EmailVerified: true, Scopes: []string{"openid"},
	}})
	req = httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer unscoped")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unscoped grant = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestEnterpriseEnrollmentGrantScopesImmutableIdentityToRequestedOrganization(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "Acme", Slug: "acme", Profile: "enterprise", Type: "enterprise", Status: "active", PlanTier: "enterprise", MaxHarnessSeats: 2}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{
		AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "developer@acme.example", Status: models.UserStatusActive,
		AuthMethod: "scim", ExternalIssuer: "scim", ExternalID: "customer-subject", ExternalIssuerVerified: true,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SSOIdentityLink{
		OrganizationID: org.ID, LegacyIssuer: "https://customer-idp.example", LegacySubject: "customer-subject",
		TargetIssuer: "https://login.patty.io/realms/patty", TargetSubject: "acme-subject",
		PattyUserID: user.ID, Status: models.SSOLinkStatusLinked,
	}).Error; err != nil {
		t.Fatal(err)
	}
	srv.SetPublicTokenVerifier(fixedPublicTokenVerifier{claims: &sso.FirstPartyAccessClaims{
		Issuer: "https://login.patty.io/realms/patty", Subject: "acme-subject", Email: user.Email, EmailVerified: true,
		Scopes: []string{"openid", "harness-enroll"},
	}})
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	body, _ := json.Marshal(map[string]string{
		"organization": org.Slug, "harness_id": "hrn-acme", "public_key_hex": hex.EncodeToString(publicKey),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer enterprise-oidc")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), org.ID) || !strings.Contains(rec.Body.String(), `"plan":"enterprise"`) {
		t.Fatalf("enterprise grant = %d: %s", rec.Code, rec.Body.String())
	}

	srv.SetPublicTokenVerifier(fixedPublicTokenVerifier{claims: &sso.FirstPartyAccessClaims{
		Issuer: "https://login.patty.io/realms/patty", Subject: "different-subject", Email: user.Email, EmailVerified: true,
		Scopes: []string{"openid", "harness-enroll"},
	}})
	req = httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer enterprise-oidc")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong immutable subject = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestPublicEnrollmentNeverBindsUnknownIdentityByEmail(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	account := models.Account{Email: "shared@example.com", Profile: "public", SubscriptionStatus: "active"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Subscription{AccountID: account.ID, Plan: "pro", Status: "active", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339), MaxHarnesses: 3}).Error; err != nil {
		t.Fatal(err)
	}
	srv.SetPublicTokenVerifier(fixedPublicTokenVerifier{claims: &sso.FirstPartyAccessClaims{
		Issuer: "https://other-issuer.example", Subject: "attacker-subject", Email: account.Email,
		EmailVerified: true, Scopes: []string{"harness-enroll"},
	}})
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	body, _ := json.Marshal(map[string]string{"harness_id": "hrn-email-jit", "public_key_hex": hex.EncodeToString(publicKey)})
	req := httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer unknown-identity")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown identity with matching email = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	var identities int64
	if err := db.Model(&models.AccountExternalIdentity{}).Where("account_id = ?", account.ID).Count(&identities).Error; err != nil || identities != 0 {
		t.Fatalf("email hint created an immutable binding: count=%d err=%v", identities, err)
	}
}

func TestPublicEnrollmentLinkedAccountIdentitiesShareOneCanonicalUserSeat(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	account := models.Account{Email: "owner@example.com", DisplayName: "Owner", Profile: "public", SubscriptionStatus: "active", SubscriptionPlan: "pro", MaxHarnesses: 3, AuthorityID: "patty-account:99"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Subscription{AccountID: account.ID, Plan: "pro", Status: "active", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339), MaxHarnesses: 3}).Error; err != nil {
		t.Fatal(err)
	}
	issuer := "https://login.patty.io/realms/patty"
	if err := db.Create(&[]models.AccountExternalIdentity{{AccountID: account.ID, Issuer: issuer, Subject: "sub-a"}, {AccountID: account.ID, Issuer: issuer, Subject: "sub-b"}}).Error; err != nil {
		t.Fatal(err)
	}
	var userIDs []string
	for i, subject := range []string{"sub-a", "sub-b"} {
		srv.SetPublicTokenVerifier(fixedPublicTokenVerifier{claims: &sso.FirstPartyAccessClaims{Issuer: issuer, Subject: subject, Email: account.Email, EmailVerified: true, Scopes: []string{"harness-enroll"}}})
		key, _, _ := ed25519.GenerateKey(rand.Reader)
		body, _ := json.Marshal(map[string]string{"harness_id": fmt.Sprintf("hrn-linked-%d", i), "public_key_hex": hex.EncodeToString(key)})
		req := httptest.NewRequest(http.MethodPost, "/api/me/harness-enroll-grant", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("identity %d grant=%d body=%s", i, rec.Code, rec.Body.String())
		}
		var response publicHarnessEnrollmentGrantResponseV1
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		userIDs = append(userIDs, response.UserID)
	}
	if userIDs[0] != userIDs[1] {
		t.Fatalf("linked identities created distinct users: %v", userIDs)
	}
	var users int64
	if err := db.Model(&models.User{}).Where("organization_id = ?", account.ID).Count(&users).Error; err != nil || users != 1 {
		t.Fatalf("users=%d err=%v", users, err)
	}
}
