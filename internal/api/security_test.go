package api

import (
	"encoding/json"
	"net/http"
	"testing"

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
		&models.PromptExchange{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return srv, db
}

func TestSecurityRuleTogglePersists(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o", Status: "active"}
	db.Create(&org)
	// Seed rules then toggle one off; GET must reflect the change.
	srv.security.EnsureRulesSeeded(org.ID)
	rec := doJSON(t, srv, "PUT", "/api/security/policy", `{"rule_id":"secret-aws","enabled":false}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/security/rules", "", org.ID)
	var rules []models.SecurityRule
	json.Unmarshal(rec.Body.Bytes(), &rules)
	for _, r := range rules {
		if r.RuleID == "secret-aws" && r.Enabled {
			t.Fatal("secret-aws should be disabled after toggle")
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
