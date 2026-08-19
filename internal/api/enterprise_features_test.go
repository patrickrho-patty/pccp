package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

	rec := putEnterpriseFeature(srv, orgA.ID, "admin", featB.ID, `{"enabled":false,"enforced":false,"config":"","reason":"freeze for audit"}`)
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
		`{"enabled":false,"enforced":false,"config":"","reason":"freeze for audit"}`)
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
	rec := putEnterpriseFeature(srv, org.ID, "admin", mand.ID, `{"enabled":false,"enforced":false,"config":"","reason":"weaken"}`)
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
	rec = putEnterpriseFeature(srv, org.ID, "owner", mand.ID, `{"enabled":true,"enforced":false,"config":"","reason":"weaken"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("owner un-enforcing mandatory = %d, want 403", rec.Code)
	}

	// Privileged role may weaken.
	rec = putEnterpriseFeature(srv, org.ID, "super_admin", mand.ID, `{"enabled":true,"enforced":false,"config":"","reason":"approved exception"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("super_admin weakening mandatory = %d, want 200", rec.Code)
	}

	// Non-mandatory features are tenant-configurable.
	nonMand := addEnterpriseFeature(t, db, org.ID, "change_freeze", true, false, "")
	rec = putEnterpriseFeature(srv, org.ID, "admin", nonMand.ID, `{"enabled":false,"enforced":false,"config":"","reason":"freeze for audit"}`)
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
		`{"enabled":false,"enforced":false,"config":"{}","reason":"freeze","expected_epoch":2}`)
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
		`{"enabled":false,"enforced":false,"config":"{}","reason":"freeze","expected_epoch":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("matching epoch = %d, want 200", rec.Code)
	}

	// Omitted expected_epoch stays accepted (legacy clients).
	rec = putEnterpriseFeature(srv, org.ID, "admin", feat.ID,
		fmt.Sprintf(`{"enabled":true,"enforced":false,"config":%q,"reason":"freeze"}`, `{"rollouts":[{"epoch":4}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("omitted epoch = %d, want 200", rec.Code)
	}
}

// Non-admin roles may not mutate ANY feature (mandatory or not), and a
// rejected attempt writes no audit row.
func TestEnterpriseFeatureUpdateRequiresAdminRole(t *testing.T) {
	srv, db := enterpriseFeatureFixture(t)
	org := models.Organization{Name: "A", Slug: "orga-role", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	feat := addEnterpriseFeature(t, db, org.ID, "change_freeze", true, false, "")

	for _, role := range []string{"viewer", "member"} {
		rec := putEnterpriseFeature(srv, org.ID, role, feat.ID,
			`{"enabled":false,"enforced":false,"config":"","reason":"curious"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s mutating non-mandatory feature = %d, want 403", role, rec.Code)
		}
	}
	var after models.EnterpriseHarnessFeature
	if err := db.Where("id = ?", feat.ID).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !after.Enabled {
		t.Fatal("forbidden role mutated the feature")
	}
	var audits int64
	db.Model(&models.AuditEvent{}).Where("organization_id = ?", org.ID).Count(&audits)
	if audits != 0 {
		t.Fatalf("rejected attempts wrote %d audit rows", audits)
	}
}

// The UI promises "변경 사유 (필수, 감사 로그에 기록)": the server requires a
// reason and records an audit event with reason/epoch/actor on success.
func TestEnterpriseFeatureUpdateRequiresReasonAndAudits(t *testing.T) {
	srv, db := enterpriseFeatureFixture(t)
	org := models.Organization{Name: "A", Slug: "orga-audit", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	feat := addEnterpriseFeature(t, db, org.ID, "change_freeze", true, false, `{"rollouts":[{"epoch":1}]}`)

	// Missing/blank reason → 400, row untouched.
	rec := putEnterpriseFeature(srv, org.ID, "admin", feat.ID,
		`{"enabled":false,"enforced":false,"config":"{}","reason":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank reason = %d, want 400", rec.Code)
	}

	rec = putEnterpriseFeature(srv, org.ID, "admin", feat.ID,
		`{"enabled":false,"enforced":false,"config":"{\"rollouts\":[{\"epoch\":2}]}","reason":"분기 감사 동결","expected_epoch":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update with reason = %d, want 200", rec.Code)
	}
	var ev models.AuditEvent
	if err := db.Where("organization_id = ? AND resource_id = ?", org.ID, feat.ID).First(&ev).Error; err != nil {
		t.Fatalf("no audit event recorded: %v", err)
	}
	if ev.EventType != "enterprise.feature_updated" || ev.ResourceType != "enterprise_feature" || ev.Result != "success" {
		t.Fatalf("unexpected audit event: %+v", ev)
	}
	if ev.ActorID != "ops@test" {
		t.Fatalf("audit actor = %q, want ops@test", ev.ActorID)
	}
	var details map[string]interface{}
	if err := json.Unmarshal([]byte(ev.Details), &details); err != nil {
		t.Fatalf("audit details not JSON: %v", err)
	}
	if details["reason"] != "분기 감사 동결" {
		t.Fatalf("audit reason = %v", details["reason"])
	}
	if details["head_epoch"] != float64(2) {
		t.Fatalf("audit head_epoch = %v, want 2", details["head_epoch"])
	}
}

// Two concurrent PUTs based on the same head epoch: exactly one wins; the
// loser's CAS predicate matches no row and it gets 409 — no double-apply.
func TestEnterpriseFeatureConcurrentEpochCAS(t *testing.T) {
	// File-backed SQLite with a busy timeout so the loser of the write-lock
	// race waits instead of erroring.
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/ef-cas.db?_busy_timeout=5000"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.SecurityRule{}, &models.SecurityFinding{},
		&models.AuditEvent{}, &models.ServiceSigningKey{}, &models.ComplianceEvidence{},
		&models.ComplianceRemediation{}, &models.ComplianceAssessmentRecord{},
		&models.Session{}, &models.Harness{}, &models.PolicyEpoch{}, &models.CapabilityLease{},
		&models.EvidenceReceipt{}, &models.Conversation{}, &models.Message{},
		&models.EnterpriseHarnessFeature{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "A", Slug: "orga-cas", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	feat := addEnterpriseFeature(t, db, org.ID, "change_freeze", true, false, `{"rollouts":[{"epoch":1}]}`)

	body := `{"enabled":false,"enforced":false,"config":"{\"rollouts\":[{\"epoch\":2}]}","reason":"race","expected_epoch":1}`
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- putEnterpriseFeature(srv, org.ID, "admin", feat.ID, body).Code
		}()
	}
	wg.Wait()
	close(codes)
	ok, conflict := 0, 0
	for c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected status %d", c)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrent updates: ok=%d conflict=%d, want exactly one each", ok, conflict)
	}
	var after models.EnterpriseHarnessFeature
	if err := db.Where("id = ?", feat.ID).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if head := enterpriseConfigHeadEpoch(after.Config); head != 2 {
		t.Fatalf("final head epoch = %d, want 2 (single apply)", head)
	}
}
