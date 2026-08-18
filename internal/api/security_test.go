package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func securityTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/sec.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.Session{}, &models.Harness{}, &models.Project{},
		&models.SecurityFinding{}, &models.SecurityRule{}, &models.AlertEndpoint{},
		&models.PIILexicon{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.PromptExchange{}, &models.UsageRecord{}, &models.ModelPackage{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	// PAT-1502 PR 2: wire a local KeyProvider so the write paths
	// (create/rotate/test) can seal targets. A 32-byte deterministic
	// KEK derived from the test JWT secret keeps test runs hermetic.
	var kek [32]byte
	for i := range kek {
		kek[i] = byte('A' + (i % 26))
	}
	provider, err := keymgmt.NewLocalProvider(kek[:], "test-kek-v1")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetKeyProvider(provider)
	return srv, db
}

func TestSecurityRuleTogglePersists(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o", Status: "active"}
	db.Create(&org)
	// Seed rules then toggle one off; GET must reflect the change.
	srv.security.EnsureRulesSeeded(org.ID)
	rec := doJSON(t, srv, "PUT", "/api/security/policy", `{"rule_id":"secret-aws-key","enabled":false}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/security/rules", "", org.ID)
	var rules []models.SecurityRule
	json.Unmarshal(rec.Body.Bytes(), &rules)
	for _, r := range rules {
		if r.RuleID == "secret-aws-key" && r.Enabled {
			t.Fatal("secret-aws-key should be disabled after toggle")
		}
	}
}

func TestSuppressAndReopenEndpoints(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o2", Status: "active"}
	db.Create(&org)
	f := models.SecurityFinding{OrganizationID: org.ID, FindingType: "pii", Severity: "high", Title: "x", Status: "open", OccurredAt: "2026-01-01T00:00:00Z"}
	db.Create(&f)

	rec := doJSON(t, srv, "POST", "/api/security/findings/"+f.ID+"/suppress", `{"reason":"test data","days":30}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("suppress failed: %d %s", rec.Code, rec.Body.String())
	}
	var stored models.SecurityFinding
	db.First(&stored, "id = ?", f.ID)
	if !stored.Suppressed || stored.Status != "suppressed" {
		t.Fatalf("suppress not persisted: %+v", stored)
	}
	rec = doJSON(t, srv, "POST", "/api/security/findings/"+f.ID+"/reopen", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("reopen failed: %d", rec.Code)
	}
	db.First(&stored, "id = ?", f.ID)
	if stored.Suppressed || stored.Status != "open" {
		t.Fatalf("reopen not persisted: %+v", stored)
	}
}

func TestLockdownProjectScope(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o3", Status: "active"}
	db.Create(&org)
	proj1 := models.Project{Name: "p1", Slug: "p1", Status: "active"}
	proj1.OrganizationID = org.ID
	db.Create(&proj1)
	proj2 := models.Project{Name: "p2", Slug: "p2", Status: "active"}
	proj2.OrganizationID = org.ID
	db.Create(&proj2)
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, ProjectID: proj1.ID, SessionID: "s1", Status: "active", HarnessID: "h1", UserID: "u1"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, ProjectID: proj2.ID, SessionID: "s2", Status: "active", HarnessID: "h2", UserID: "u2"})

	rec := doJSON(t, srv, "POST", "/api/security/lockdown", `{"scope":"project","project_id":"`+proj1.ID+`","reason":"incident"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("lockdown failed: %d %s", rec.Code, rec.Body.String())
	}
	var s1, s2 models.Session
	db.First(&s1, "session_id = ?", "s1")
	db.First(&s2, "session_id = ?", "s2")
	if s1.Status != "terminated" {
		t.Fatalf("project-scoped session should terminate: %s", s1.Status)
	}
	if s2.Status != "active" {
		t.Fatalf("out-of-scope session should stay active: %s", s2.Status)
	}
}

func TestFindingsServerFilters(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o4", Status: "active"}
	db.Create(&org)
	db.Create(&models.SecurityFinding{OrganizationID: org.ID, FindingType: "pii", Severity: "critical", Title: "c1", Status: "open", OccurredAt: "2026-08-01T00:00:00Z"})
	db.Create(&models.SecurityFinding{OrganizationID: org.ID, FindingType: "secret", Severity: "low", Title: "l1", Status: "resolved", OccurredAt: "2026-08-02T00:00:00Z"})

	rec := doJSON(t, srv, "GET", "/api/security/findings?severity=critical&page=1&size=25", "", org.ID)
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	list := resp["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("critical filter should return 1, got %d", len(list))
	}
}

func TestLexiconEndpointRoundTrip(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o5", Status: "active"}
	db.Create(&org)
	rec := doJSON(t, srv, "PUT", "/api/security/lexicon", `{"version":"7","patterns":{"pii-kr-rrn":"\\bXXXXX\\d{2}-\\d{7}\\b"}}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("lexicon publish failed: %d %s", rec.Code, rec.Body.String())
	}
	var lexicon models.PIILexicon
	db.First(&lexicon, "organization_id = ?", org.ID)
	if lexicon.Version != "7" {
		t.Fatalf("lexicon version wrong: %s", lexicon.Version)
	}
}

// --- PAT-1433: catalog seeding, severity persistence, scoped overrides ---

func TestSecurityRulesEndpointSeedsCatalog(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "seed-org", Slug: "seed-org", Status: "active"}
	db.Create(&org)
	// NO explicit EnsureRulesSeeded: the endpoint itself must seed.
	rec := doJSON(t, srv, "GET", "/api/security/rules", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET rules: %d %s", rec.Code, rec.Body.String())
	}
	var rules []models.SecurityRule
	if err := json.Unmarshal(rec.Body.Bytes(), &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) < 40 {
		t.Fatalf("authoritative catalog expected (>=40 rules), got %d", len(rules))
	}
	sawPath := false
	for _, r := range rules {
		if r.RuleID == "path-etc-passwd" {
			sawPath = true
		}
	}
	if !sawPath {
		t.Fatal("catalog must include sensitive_path rules (the old UI presets never showed these)")
	}
}

func TestSecurityRuleSeverityPersists(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "sev-org", Slug: "sev-org", Status: "active"}
	db.Create(&org)
	rec := doJSON(t, srv, "PUT", "/api/security/policy", `{"rule_id":"pii-kr-phone","severity":"low"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("severity PUT failed: %d %s", rec.Code, rec.Body.String())
	}
	// Invalid vocabulary must be rejected.
	rec = doJSON(t, srv, "PUT", "/api/security/policy", `{"rule_id":"pii-kr-phone","severity":"ultra"}`, org.ID)
	if rec.Code == http.StatusOK {
		t.Fatal("invalid severity must be rejected")
	}
	rec = doJSON(t, srv, "GET", "/api/security/rules", "", org.ID)
	var rules []models.SecurityRule
	json.Unmarshal(rec.Body.Bytes(), &rules)
	for _, r := range rules {
		if r.RuleID == "pii-kr-phone" && r.Severity != "low" {
			t.Fatalf("pii-kr-phone severity = %q, want low", r.Severity)
		}
	}
}

func TestRuleOverrideEndpoints(t *testing.T) {
	srv, db := securityTestServer(t)
	if err := db.AutoMigrate(&models.SecurityRuleOverride{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "ovr-org", Slug: "ovr-org", Status: "active"}
	db.Create(&org)
	srv.security.EnsureRulesSeeded(org.ID)

	// PUT: user-scoped delta disabling kr-phone + lowering severity.
	body := `{"scope_level":"user","scope_id":"user-7","rule_id":"pii-kr-phone","enabled":false,"severity":"low"}`
	rec := doJSON(t, srv, "PUT", "/api/security/rules/overrides", body, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT override: %d %s", rec.Code, rec.Body.String())
	}
	// Pure no-op override must be rejected.
	rec = doJSON(t, srv, "PUT", "/api/security/rules/overrides", `{"scope_level":"user","scope_id":"user-7","rule_id":"pii-kr-rrn"}`, org.ID)
	if rec.Code == http.StatusOK {
		t.Fatal("inherit-only override must be rejected")
	}
	// GET: the delta is listed.
	rec = doJSON(t, srv, "GET", "/api/security/rules/overrides?scope_level=user&scope_id=user-7", "", org.ID)
	var overrides []models.SecurityRuleOverride
	if err := json.Unmarshal(rec.Body.Bytes(), &overrides); err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 1 || overrides[0].RuleID != "pii-kr-phone" {
		t.Fatalf("override list = %+v", overrides)
	}
	if overrides[0].Enabled == nil || *overrides[0].Enabled || overrides[0].Severity != "low" {
		t.Fatalf("override content wrong: %+v", overrides[0])
	}
	// DELETE: revert to inherit.
	rec = doJSON(t, srv, "DELETE", "/api/security/rules/overrides", `{"scope_level":"user","scope_id":"user-7","rule_id":"pii-kr-phone"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE override: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/security/rules/overrides?scope_level=user&scope_id=user-7", "", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &overrides)
	if len(overrides) != 0 {
		t.Fatalf("override must be reverted, got %+v", overrides)
	}
}
