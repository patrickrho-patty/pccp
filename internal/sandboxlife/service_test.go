package sandboxlife

import (
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func sbDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/sbl.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.SandboxEnvironmentTemplate{}, &models.SandboxLifecyclePolicy{},
		&models.SandboxRunner{}, &models.SandboxEnvironment{}, &models.User{}, &models.Organization{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// A narrower scope may strengthen but never weaken a parent's decision.
func TestStrengthenOnlyInheritance(t *testing.T) {
	svc := New(sbDB(t))
	// org persistent → project may strengthen to ephemeral.
	if _, err := svc.SetPolicy("org-s", LifecyclePolicyRequest{Scope: "org", Mode: "persistent"}, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetPolicy("org-s", LifecyclePolicyRequest{Scope: "project", ScopeID: "proj-1", Mode: "ephemeral"}, "a"); err != nil {
		t.Fatalf("strengthening to ephemeral should be allowed: %v", err)
	}
	// org ephemeral (strictest) → project may NOT weaken to persistent.
	svc2 := New(sbDB(t))
	if _, err := svc2.SetPolicy("org-x", LifecyclePolicyRequest{Scope: "org", Mode: "ephemeral"}, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.SetPolicy("org-x", LifecyclePolicyRequest{Scope: "project", ScopeID: "proj-1", Mode: "persistent"}, "a"); err == nil {
		t.Fatalf("persistent must NOT weaken org ephemeral (strengthen-only)")
	}
	// Without any parent policy, an org persistent is a legitimate first choice.
	svc3 := New(sbDB(t))
	if _, err := svc3.SetPolicy("org-y", LifecyclePolicyRequest{Scope: "org", Mode: "persistent"}, "a"); err != nil {
		t.Fatalf("first persistent policy at org must be allowed: %v", err)
	}
	// Invalid mode rejected.
	if _, err := svc3.SetPolicy("org-y", LifecyclePolicyRequest{Scope: "org", Mode: "ehemeral"}, "a"); err == nil {
		t.Fatalf("typo mode must be rejected")
	}
}

// ResolveLifecycle picks the narrowest applicable non-weakening decision and
// exposes its origin scope.
func TestResolveLifecycleInheritance(t *testing.T) {
	svc := New(sbDB(t))
	// org policy persistent; repo overrides to ephemeral (stricter).
	_, _ = svc.SetPolicy("org-s", LifecyclePolicyRequest{Scope: "org", Mode: "persistent"}, "a")
	_, _ = svc.SetPolicy("org-s", LifecyclePolicyRequest{Scope: "repository", ScopeID: "repo-7", Mode: "ephemeral"}, "a")
	eff := svc.ResolveLifecycle("org-s", "repo-7", "repository")
	if eff.Mode != "ephemeral" {
		t.Fatalf("repo ephemeral should win over org persistent: %+v", eff)
	}
	// Different repo inherits org persistent.
	eff2 := svc.ResolveLifecycle("org-s", "repo-8", "repository")
	if eff2.Mode != "persistent" {
		t.Fatalf("repo-8 should inherit persistent: %+v", eff2)
	}
}

// Persistent prepare reattaches to the same workspace identity WITHOUT
// resetting state; ephemeral prepares a fresh environment.
func TestPersistentReattachPreservesState(t *testing.T) {
	svc := New(sbDB(t))
	_, _ = svc.SetPolicy("org-s", LifecyclePolicyRequest{Scope: "org", Mode: "persistent"}, "a")
	first, err := svc.Prepare("org-s", "u1", "repo-9", "", "s1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Prepare("org-s", "u1", "repo-9", "", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if first.EnvironmentID != second.EnvironmentID {
		t.Fatalf("persistent must reattach same env: %s vs %s", first.EnvironmentID, second.EnvironmentID)
	}
	// Ephemeral → distinct environments.
	svc2 := New(sbDB(t))
	setupEphemeral(t, svc2)
	e1, _ := svc2.Prepare("org-e", "u1", "repo-9", "", "s1")
	e2, _ := svc2.Prepare("org-e", "u1", "repo-9", "", "s2")
	if e1.EnvironmentID == e2.EnvironmentID {
		t.Fatalf("ephemeral must create fresh environments per session")
	}
}

func setupEphemeral(t *testing.T, svc *Service) {
	t.Helper()
	if _, err := svc.SetPolicy("org-e", LifecyclePolicyRequest{Scope: "org", Mode: "ephemeral"}, "a"); err != nil {
		t.Fatal(err)
	}
	_, _ = svc.RegisterRunner("org-e", models.SandboxRunner{RunnerID: "r1", RuntimeType: "docker", MaxConcurrency: 8}, "a")
}

// Pinned: without a compliant pinned workstation the environment is
// unavailable — no automated fallback, no state copy.
func TestPinnedNoFallback(t *testing.T) {
	svc := New(sbDB(t))
	if _, err := svc.SetPolicy("org-s", LifecyclePolicyRequest{Scope: "org", Mode: "pinned"}, "a"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Prepare("org-s", "u1", "repo-1", "", "s1")
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("pinned without workstation must not fall back: %v", err)
	}
	// Register compliant pinned workstation for user.
	_, _ = svc.RegisterRunner("org-s", models.SandboxRunner{RunnerID: "ws-9", RuntimeType: "workstation", PinnedUserID: "u1", Status: "ok", MaxConcurrency: 1}, "a")
	res, err := svc.Prepare("org-s", "u1", "repo-1", "ws-9", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if res.RunnerID != "ws-9" {
		t.Fatalf("pinned must route to the workstation: %+v", res)
	}
}

// Single-writable: a second live session on the same env is denied.
func TestConcurrencyDenied(t *testing.T) {
	svc := New(sbDB(t))
	_, _ = svc.SetPolicy("org-s", LifecyclePolicyRequest{Scope: "org", Mode: "persistent"}, "a")
	res, _ := svc.Prepare("org-s", "u1", "repo-2", "", "s1")
	// Same user attaching again is the same env (a resume) — but a DIFFERENT
	// live session on the same writable workspace is denied.
	ok, reason := svc.IsSingleWritable("org-s", res.EnvironmentID, "s2")
	if ok {
		t.Fatalf("concurrent writable attach must be denied: %s", reason)
	}
}

// Actions transition environments deterministically.
func TestActions(t *testing.T) {
	svc := New(sbDB(t))
	_, _ = svc.SetPolicy("org-s", LifecyclePolicyRequest{Scope: "org", Mode: "ephemeral"}, "a")
	res, _ := svc.Prepare("org-s", "u1", "repo-3", "", "s1")
	if err := svc.Action("org-s", res.EnvironmentID, "quarantine", "admin"); err != nil {
		t.Fatal(err)
	}
	var env models.SandboxEnvironment
	if err := svc.db.Where("environment_id = ?", res.EnvironmentID).First(&env).Error; err != nil || env.Status != "quarantined" {
		t.Fatalf("quarantine failed: %+v %v", env, err)
	}
	if err := svc.Action("org-s", res.EnvironmentID, "destroy", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Where("environment_id = ?", res.EnvironmentID).First(&env).Error; err != nil || env.Status != "destroyed" {
		t.Fatalf("destroy failed: %+v %v", env, err)
	}
}
