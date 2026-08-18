package security

import (
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// scope_override_test.go pins PAT-1432's scoped delta storage and the
// session-subject resolution the relay uses to build scoped packs.

func newOverrideTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/s.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SecurityRule{}, &models.SecurityRuleOverride{}, &models.User{}, &models.BusinessUnit{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedCatalog(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := New(db)
	if _, err := svc.EnsureRulesSeeded("org-1"); err != nil {
		t.Fatal(err)
	}
}

func TestSetRuleOverrideValidation(t *testing.T) {
	db := newOverrideTestDB(t)
	seedCatalog(t, db)
	svc := New(db)
	enabled := true

	if err := svc.SetRuleOverride("org-1", "galactic", "x", "pii-kr-rrn", &enabled, "", ""); err == nil {
		t.Fatal("invalid level must be rejected")
	}
	if err := svc.SetRuleOverride("org-1", "user", "", "pii-kr-rrn", &enabled, "", ""); err == nil {
		t.Fatal("empty scope id must be rejected")
	}
	if err := svc.SetRuleOverride("org-1", "user", "u1", "pii-kr-rrn", nil, "", ""); err == nil {
		t.Fatal("pure no-op override must be rejected")
	}
	if err := svc.SetRuleOverride("org-1", "user", "u1", "pii-kr-rrn", nil, "ultra", ""); err == nil {
		t.Fatal("invalid severity must be rejected")
	}
	if err := svc.SetRuleOverride("org-1", "user", "u1", "no-such-rule", &enabled, "", ""); err == nil {
		t.Fatal("override for unknown rule must be rejected")
	}
	if err := svc.SetRuleOverride("org-1", "user", "u1", "pii-kr-rrn", &enabled, "", ""); err != nil {
		t.Fatalf("valid override rejected: %v", err)
	}
	// Replace path: same (level, scope, rule) updates in place.
	disabled := false
	if err := svc.SetRuleOverride("org-1", "user", "u1", "pii-kr-rrn", &disabled, "low", ""); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListRuleOverrides("org-1", "user", "u1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("replace must keep one row, got %d (%v)", len(rows), err)
	}
	if rows[0].Enabled == nil || *rows[0].Enabled || rows[0].Severity != "low" {
		t.Fatalf("row not updated: %+v", rows[0])
	}
}

func TestDeleteRuleOverride(t *testing.T) {
	db := newOverrideTestDB(t)
	seedCatalog(t, db)
	svc := New(db)
	enabled := true
	if err := svc.SetRuleOverride("org-1", "user", "u1", "pii-kr-rrn", &enabled, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteRuleOverride("org-1", "user", "u1", "pii-kr-rrn"); err != nil {
		t.Fatal(err)
	}
	rows, _ := svc.ListRuleOverrides("org-1", "user", "u1")
	if len(rows) != 0 {
		t.Fatalf("override must be deleted, got %d rows", len(rows))
	}
}

func TestOverridesForResolvesTeamUserHarness(t *testing.T) {
	db := newOverrideTestDB(t)
	seedCatalog(t, db)
	svc := New(db)

	// User u1 belongs to business unit bu-7.
	u1 := models.User{BusinessUnitID: "bu-7"}
	u1.ID = "u1"
	u1.OrganizationID = "org-1"
	if err := db.Create(&u1).Error; err != nil {
		t.Fatal(err)
	}
	on := true
	off := false
	for _, tc := range []struct {
		level, scopeID, ruleID string
		enabled                *bool
		sev                    string
	}{
		{"team", "bu-7", "pii-kr-phone", &off, ""},
		{"user", "u1", "pii-kr-rrn", &on, "low"},
		{"harness", "peer-9", "secret-aws-key", &off, ""},
		// A different user's override must NOT resolve for u1.
		{"user", "u2", "pii-kr-passport", &off, ""},
	} {
		if err := svc.SetRuleOverride("org-1", tc.level, tc.scopeID, tc.ruleID, tc.enabled, tc.sev, ""); err != nil {
			t.Fatalf("%s/%s: %v", tc.level, tc.scopeID, err)
		}
	}

	resolved := svc.OverridesFor("org-1", "u1", "peer-9")
	if len(resolved) != 3 {
		t.Fatalf("expected team+user+harness scopes, got %d: %+v", len(resolved), resolved)
	}
	wantOrder := []struct{ level, key string }{
		{"team", "bu-7"}, {"user", "u1"}, {"harness", "peer-9"},
	}
	for i, want := range wantOrder {
		if resolved[i].Level != want.level || resolved[i].ScopeID != want.key {
			t.Fatalf("resolved[%d] = %s:%s, want %s:%s", i, resolved[i].Level, resolved[i].ScopeID, want.level, want.key)
		}
	}
	if ov := resolved[0].Overrides; len(ov) != 1 || *ov[0].Enabled {
		t.Fatalf("team override wrong: %+v", ov)
	}
	if ov := resolved[1].Overrides; len(ov) != 1 || ov[0].Severity != "low" {
		t.Fatalf("user override wrong: %+v", ov)
	}
	if ov := resolved[2].Overrides; len(ov) != 1 || *ov[0].Enabled {
		t.Fatalf("harness override wrong: %+v", ov)
	}
}
