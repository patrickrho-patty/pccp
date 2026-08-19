package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// enterpriseFeatureFixture boots a test server with the enterprise feature
// table migrated and one feature row per call to add.
func enterpriseFeatureFixture(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	srv, db := complianceTestServer(t)
	if err := db.AutoMigrate(&models.EnterpriseHarnessFeature{}); err != nil {
		t.Fatal(err)
	}
	return srv, db
}

func addEnterpriseFeature(t *testing.T, db *gorm.DB, orgID, key string, enabled, enforced bool, config string) models.EnterpriseHarnessFeature {
	t.Helper()
	f := models.EnterpriseHarnessFeature{
		OrganizationID: orgID,
		FeatureKey:     key,
		FeatureName:    key,
		Category:       "security",
		Enabled:        enabled,
		Enforced:       enforced,
		Status:         "active",
		Config:         config,
	}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func putEnterpriseFeature(srv *Server, orgID, role, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/enterprise/features/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: orgID, Role: role, Email: "ops@test"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// Cross-tenant update: org A's operator must not modify org B's feature,
// even with the exact row id (fail-closed 404, row untouched).
func TestEnterpriseFeatureUpdateOrgScoped(t *testing.T) {
	srv, db := enterpriseFeatureFixture(t)
	orgA := models.Organization{Name: "A", Slug: "orga-ef", Status: "active"}
	orgB := models.Organization{Name: "B", Slug: "orgb-ef", Status: "active"}
	if err := db.Create(&orgA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&orgB).Error; err != nil {
		t.Fatal(err)
	}
	featB := addEnterpriseFeature(t, db, orgB.ID, "change_freeze", true, false, "")

	rec := putEnterpriseFeature(srv, orgA.ID, "admin", featB.ID, `{"enabled":false,"enforced":false,"config":""}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org update = %d, want 404", rec.Code)
	}
	var after models.EnterpriseHarnessFeature
	if err := db.Where("id = ?", featB.ID).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !after.Enabled {
		t.Fatal("cross-org update mutated org B's feature")
	}

	// Same-org update still works.
	rec = putEnterpriseFeature(srv, orgA.ID, "admin", addEnterpriseFeature(t, db, orgA.ID, "change_freeze", true, false, "").ID,
		`{"enabled":false,"enforced":false,"config":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-org update = %d, want 200", rec.Code)
	}
}

// Patty-mandatory (패티 필수) features: tenant admins may not weaken them;
// privileged roles (super_admin, security_admin) may.
func TestEnterpriseFeatureMandatoryWeakeningForbidden(t *testing.T) {
	srv, db := enterpriseFeatureFixture(t)
	org := models.Organization{Name: "A", Slug: "orga-mand", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}

	// Tenant admin disabling a mandatory feature → 403, row untouched.
	mand := addEnterpriseFeature(t, db, org.ID, "network_egress", true, true, "")
	rec := putEnterpriseFeature(srv, org.ID, "admin", mand.ID, `{"enabled":false,"enforced":false,"config":""}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin weakening mandatory = %d, want 403", rec.Code)
	}
	var after models.EnterpriseHarnessFeature
	if err := db.Where("id = ?", mand.ID).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !after.Enabled || !after.Enforced {
		t.Fatal("forbidden weakening mutated the feature")
	}

	// Un-enforcing (not disabling) is also weakening → 403.
	rec = putEnterpriseFeature(srv, org.ID, "owner", mand.ID, `{"enabled":true,"enforced":false,"config":""}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("owner un-enforcing mandatory = %d, want 403", rec.Code)
	}

	// Privileged role may weaken.
	rec = putEnterpriseFeature(srv, org.ID, "super_admin", mand.ID, `{"enabled":true,"enforced":false,"config":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("super_admin weakening mandatory = %d, want 200", rec.Code)
	}

	// Non-mandatory features are tenant-configurable.
	nonMand := addEnterpriseFeature(t, db, org.ID, "change_freeze", true, false, "")
	rec = putEnterpriseFeature(srv, org.ID, "admin", nonMand.ID, `{"enabled":false,"enforced":false,"config":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin weakening non-mandatory = %d, want 200", rec.Code)
	}
}

// Optimistic concurrency: expected_epoch must match the config head epoch.
func TestEnterpriseFeatureEpochConflict(t *testing.T) {
	srv, db := enterpriseFeatureFixture(t)
	org := models.Organization{Name: "A", Slug: "orga-epoch", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	feat := addEnterpriseFeature(t, db, org.ID, "change_freeze", true, false, `{"rollouts":[{"epoch":1},{"epoch":3}]}`)

	// Stale epoch → 409, row untouched.
	rec := putEnterpriseFeature(srv, org.ID, "admin", feat.ID,
		`{"enabled":false,"enforced":false,"config":"{}","expected_epoch":2}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale epoch = %d, want 409", rec.Code)
	}
	var after models.EnterpriseHarnessFeature
	if err := db.Where("id = ?", feat.ID).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !after.Enabled {
		t.Fatal("conflicted update mutated the feature")
	}

	// Matching head epoch → 200.
	rec = putEnterpriseFeature(srv, org.ID, "admin", feat.ID,
		`{"enabled":false,"enforced":false,"config":"{}","expected_epoch":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("matching epoch = %d, want 200", rec.Code)
	}

	// Omitted expected_epoch stays accepted (legacy clients).
	rec = putEnterpriseFeature(srv, org.ID, "admin", feat.ID,
		fmt.Sprintf(`{"enabled":true,"enforced":false,"config":%q}`, `{"rollouts":[{"epoch":4}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("omitted epoch = %d, want 200", rec.Code)
	}
}
