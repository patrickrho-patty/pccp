package relay

import (
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/security"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// dlp_scope_pack_test.go pins PAT-1432's scoped DELTA pack builders
// and the team → user → harness push ordering.

func TestBuildDLPRulePackCarriesOrgScope(t *testing.T) {
	pack := BuildDLPRulePack("e", "org-1", []SecurityRuleView{
		{RuleID: "pii-kr-rrn", Pattern: "korean_pii", Severity: "critical"},
	}, time.Now())
	if pack.Scope.Level != wireScopeOrg || pack.Scope.ID != "org-1" {
		t.Fatalf("org pack scope = %+v", pack.Scope)
	}
}

func TestBuildScopedDLPOverridePack(t *testing.T) {
	on, off := true, false
	inherit := (*bool)(nil)

	pack := BuildScopedDLPOverridePack("e", "org-1", "user", "u1", []security.ScopedOverride{
		{RuleID: "pii-kr-rrn", Enabled: &off, Severity: "low", Action: "mask"},
		{RuleID: "pii-kr-phone", Enabled: &on},                                  // severity/action inherited
		{RuleID: "pii-kr-passport", Enabled: inherit, Severity: "", Action: ""}, // inherit-only
	}, time.Now())

	if pack == nil {
		t.Fatal("pack with real deltas must not be nil")
	}
	if pack.Scope.Level != wireScopeUser || pack.Scope.ID != "u1" {
		t.Fatalf("scope = %+v", pack.Scope)
	}
	if len(pack.RuleOverrides) != 2 {
		t.Fatalf("inherit-only rows must be dropped, got %d: %+v", len(pack.RuleOverrides), pack.RuleOverrides)
	}
	if pack.RuleOverrides[0].Enabled || pack.RuleOverrides[0].Severity != "low" {
		t.Fatalf("override[0] = %+v", pack.RuleOverrides[0])
	}
	if !pack.RuleOverrides[1].Enabled || pack.RuleOverrides[1].Severity != "" {
		t.Fatalf("override[1] = %+v", pack.RuleOverrides[1])
	}

	// All-inherit rows → no pack at all (nothing to push).
	if nil != BuildScopedDLPOverridePack("e", "org-1", "user", "u1", []security.ScopedOverride{
		{RuleID: "pii-kr-rrn", Enabled: inherit},
	}, time.Now()) {
		t.Fatal("inherit-only scope must produce NO pack")
	}
}

func TestDLPOverridePacksForOrdersBySpecificity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/s.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SecurityRule{}, &models.SecurityRuleOverride{}, &models.User{}, &models.BusinessUnit{}); err != nil {
		t.Fatal(err)
	}
	sec := security.New(db)
	svc := &Service{db: db, security: sec}

	if _, err := sec.EnsureRulesSeeded("org-1"); err != nil {
		t.Fatal(err)
	}
	u1 := models.User{BusinessUnitID: "bu-7"}
	u1.ID, u1.OrganizationID = "u1", "org-1"
	if err := db.Create(&u1).Error; err != nil {
		t.Fatal(err)
	}
	off, on := false, true
	for _, tc := range []struct {
		level, scopeID, ruleID string
		enabled                *bool
	}{
		{"team", "bu-7", "pii-kr-phone", &off},
		{"user", "u1", "pii-kr-rrn", &off},
		{"harness", "peer-9", "secret-aws-key", &on},
	} {
		if err := sec.SetRuleOverride("org-1", tc.level, tc.scopeID, tc.ruleID, tc.enabled, "", ""); err != nil {
			t.Fatalf("%s: %v", tc.level, err)
		}
	}

	packs := svc.dlpOverridePacksFor("org-1", "u1", "peer-9", "e")
	if len(packs) != 3 {
		t.Fatalf("expected 3 scoped packs, got %d", len(packs))
	}
	wantOrder := []string{wireScopeTeam, wireScopeUser, wireScopeHarness}
	for i, want := range wantOrder {
		if packs[i].Scope.Level != want {
			t.Fatalf("pack[%d].scope.level = %q, want %q (ascending specificity)", i, packs[i].Scope.Level, want)
		}
	}
	// Subject with no overrides → no packs at all.
	if got := svc.dlpOverridePacksFor("org-1", "nobody", "", "e"); len(got) != 0 {
		t.Fatalf("no overrides must mean no packs, got %d", len(got))
	}
}
