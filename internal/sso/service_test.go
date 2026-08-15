package sso

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDB(t *testing.T) *gorm.DB {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(append(models.AllModels(), &identity.AdminCredentials{})...)
	return db
}

func TestSAMLRedirect(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	svc.ConfigureSAML("http://idp.example.com", "http://idp.example.com/sso", "http://pccp.example.com/saml/callback")

	redirectURL, err := svc.GenerateSAMLRedirect("relay-state-123")
	if err != nil {
		t.Fatal(err)
	}
	if redirectURL == "" {
		t.Fatal("expected redirect URL")
	}
	if !contains(redirectURL, "SAMLRequest") {
		t.Fatal("expected SAMLRequest in URL")
	}
}

func TestSAMLCallback(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")

	// A REAL SAML response shape with an authenticated subject and
	// attributes (base64 of the XML).
	real := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
		<saml:Assertion>
			<saml:Subject><saml:NameID>dev-kim@partner.example</saml:NameID></saml:Subject>
			<saml:AttributeStatement>
				<saml:Attribute Name="email"><saml:AttributeValue>dev-kim@partner.example</saml:AttributeValue></saml:Attribute>
				<saml:Attribute Name="nameKo"><saml:AttributeValue>김개발</saml:AttributeValue></saml:Attribute>
			</saml:AttributeStatement>
		</saml:Assertion>
	</samlp:Response>`
	resp, err := svc.HandleSAMLCallback(base64.StdEncoding.EncodeToString([]byte(real)), "relay")
	if err != nil {
		t.Fatal(err)
	}
	if resp.UserID != "dev-kim@partner.example" || resp.NameKo != "김개발" {
		t.Fatalf("parsed = %+v", resp)
	}

	// An EMPTY assertion response fails closed (no mock fallback).
	empty := base64.StdEncoding.EncodeToString([]byte(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"></samlp:Response>`))
	if _, err := svc.HandleSAMLCallback(empty, "relay"); err == nil {
		t.Fatal("subject-less SAML response must fail closed")
	}
	// Garbage base64/XML fails closed.
	if _, err := svc.HandleSAMLCallback("!!!not-base64!!!", "relay"); err == nil {
		t.Fatal("garbage SAML response must fail closed")
	}
}

func TestOIDCAuthURL(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	svc.ConfigureOIDC("http://oidc.example.com", "client-id", "secret")

	authURL, err := svc.OIDCAuthURL("http://pccp.example.com/callback", "state-123")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(authURL, "client_id=client-id") {
		t.Fatal("expected client_id in auth URL")
	}
}

func TestProvisionUserFromSSO(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")

	// Create org first
	org := models.Organization{Name: "Test", Slug: "sso-test", Status: "active"}
	db.Create(&org)

	// Provision new user
	user, err := svc.ProvisionUserFromSSO(org.ID, &SAMLResponse{
		UserID: "sso-user-001",
		Email:  "kim@patty.dev",
		Name:   "Kim Gaebal",
		NameKo: "김개발",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.NameKo != "김개발" {
		t.Fatal("expected Korean name")
	}
	if user.AuthMethod != "saml" {
		t.Fatal("expected saml auth method")
	}
	if user.Locale != "ko-KR" {
		t.Fatal("expected Korean locale")
	}

	// Provisioning same user again should update, not create
	user2, err := svc.ProvisionUserFromSSO(org.ID, &SAMLResponse{
		UserID: "sso-user-001",
		Email:  "kim@patty.dev",
		Name:   "Kim Updated",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user2.ID != user.ID {
		t.Fatal("should return same user")
	}
}

func TestSCIMProvisioning(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")

	org := models.Organization{Name: "SCIM Test", Slug: "scim-test", Status: "active"}
	db.Create(&org)

	// Fail closed: unconfigured SCIM refuses every request.
	unconfigured := httptest.NewRequest("POST", "/scim?org="+org.ID, nil)
	rec := httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, unconfigured)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured SCIM must 503, got %d", rec.Code)
	}

	svc.ConfigureSCIMToken("scim-admin-token")

	// No/incorrect bearer token refused.
	noAuth := httptest.NewRequest("POST", "/scim?org="+org.ID, strings.NewReader(`{"userName":"scim-1"}`))
	rec = httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, noAuth)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token-less SCIM must 401, got %d", rec.Code)
	}

	// Missing org scope refused.
	withAuth := httptest.NewRequest("POST", "/scim", strings.NewReader(`{"userName":"scim-1"}`))
	withAuth.Header.Set("Authorization", "Bearer scim-admin-token")
	rec = httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, withAuth)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unscoped SCIM must 400, got %d", rec.Code)
	}

	// Authorized + scoped creation succeeds.
	create := httptest.NewRequest("POST", "/scim?org="+org.ID, strings.NewReader(`{"userName":"scim-1","email":"s@e.com","displayName":"SCIM One"}`))
	create.Header.Set("Authorization", "Bearer scim-admin-token")
	rec = httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("authorized SCIM create must 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Cross-org delete refused.
	var created models.User
	db.Where("organization_id = ?", org.ID).First(&created)
	del := httptest.NewRequest("DELETE", "/scim/users?userID="+created.ID+"&org=other-org", nil)
	del.Header.Set("Authorization", "Bearer scim-admin-token")
	rec = httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, del)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org SCIM delete must 404, got %d", rec.Code)
	}
}

func TestGenerateSessionToken(t *testing.T) {
	svc := New(setupDB(t), "test-secret")
	token, err := svc.GenerateSessionToken("user-1", "org-1", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected token")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > 0 && containsStr(s, substr)))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
