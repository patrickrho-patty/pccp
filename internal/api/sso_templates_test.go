package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
)

func TestAdminAppliesReviewedSSOTemplateWithEncryptedSecretAndAudit(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(&models.SSOSecret{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Base: models.Base{ID: "org-sso-template"}, Name: "Template Org", Slug: "template-org", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	provider, err := keymgmt.NewLocalProvider([]byte("0123456789abcdef0123456789abcdef"), "test-kek")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetKeyProvider(provider)
	body, _ := json.Marshal(map[string]interface{}{
		"template_id": "okta-oidc",
		"config": map[string]interface{}{
			"issuer": "https://example.okta.com/oauth2/default", "client_id": "client-id", "client_secret_ref": "okta-client-secret",
			"redirect_uri": "https://console.example/api/sso/oidc/callback", "jwks": map[string]interface{}{"keys": []interface{}{}},
		},
		"client_secret": "plaintext-must-be-sealed",
	})
	res := doUserJSONAs(t, srv, http.MethodPut, "/api/organizations/"+org.ID+"/sso-template", string(body), org.ID, "admin", "admin@example.com")
	if res.Code != http.StatusOK {
		t.Fatalf("apply template: HTTP %d: %s", res.Code, res.Body.String())
	}
	if err := db.First(&org, "id = ?", org.ID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(org.SSOConfig, "plaintext-must-be-sealed") || !strings.Contains(org.SSOConfig, `"provider":"okta-oidc"`) {
		t.Fatalf("organization has unsafe or missing SSO config: %s", org.SSOConfig)
	}
	var secret models.SSOSecret
	if err := db.Where("organization_id = ? AND name = ?", org.ID, "okta-client-secret").First(&secret).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(secret.Ciphertext, "plaintext-must-be-sealed") {
		t.Fatal("SSO secret was stored in plaintext")
	}
	var audit models.AuditEvent
	if err := db.Where("organization_id = ? AND event_type = ?", org.ID, "cp.auth.sso_template_applied").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
}

func TestSSOTemplateApplyRejectsMemberAndCrossTenantAdmin(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(&models.SSOSecret{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Base: models.Base{ID: "org-sso-target"}, Name: "Target", Slug: "sso-target", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	body := `{"template_id":"generic-oidc","config":{}}`
	member := doUserJSONAs(t, srv, http.MethodPut, "/api/organizations/"+org.ID+"/sso-template", body, org.ID, "member", "member@example.com")
	if member.Code != http.StatusForbidden {
		t.Fatalf("member apply returned HTTP %d", member.Code)
	}
	crossTenant := doUserJSONAs(t, srv, http.MethodPut, "/api/organizations/"+org.ID+"/sso-template", body, "other-org", "admin", "admin@example.com")
	if crossTenant.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant apply returned HTTP %d", crossTenant.Code)
	}
}
