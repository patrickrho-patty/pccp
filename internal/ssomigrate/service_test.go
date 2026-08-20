package ssomigrate

import (
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func migDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/mig.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.SSOIdentityLink{}, &models.SSOMigrationItem{}, &models.SSOMigrationManifest{},
		&models.SSOMigrationWave{}, &models.SSOMigrationBridgeEvent{}, &models.User{}, &models.Organization{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// A legacy issuer+subject maps to exactly one Patty user. A conflicting user
// for the same legacy identity is flagged ambiguous — never silently overridden
// (email is never used to auto-link).
func TestLinkIdentityConflictIsAmbiguous(t *testing.T) {
	svc := New(migDB(t))
	if _, err := svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://kc.patty.io/realms/work", LegacySubject: "sub-1", PattyUserID: "u1",
	}, "admin"); err != nil {
		t.Fatalf("first link: %v", err)
	}
	// Same legacy identity → same Patty user → idempotent update (target added).
	if _, err := svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://kc.patty.io/realms/work", LegacySubject: "sub-1", PattyUserID: "u1",
		TargetIssuer: "https://auth.patty.io/application/o/work/", TargetSubject: "sub-1-new",
	}, "admin"); err != nil {
		t.Fatalf("idempotent relink: %v", err)
	}
	// Conflicting Patty user → ambiguous error.
	_, err := svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://kc.patty.io/realms/work", LegacySubject: "sub-1", PattyUserID: "u2",
	}, "admin")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("conflict must be ambiguous, got: %v", err)
	}
}

// The bridge resolves a legacy identity to its Patty user and issues a NEW
// session decision — it never copies a Keycloak token — and fails closed on
// unlinked/ambiguous identities instead of guessing.
func TestBridgeIssuesNewSessionNotCopy(t *testing.T) {
	svc := New(migDB(t))
	// Unlinked → fail closed (422-style decision, no session).
	out, err := svc.BridgeLegacy("org-m", BridgeEvent{LegacyIssuer: "https://kc/realms/x", LegacySubject: "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "unlinked" || out.NewSessionIssued {
		t.Fatalf("unlinked must fail closed: %+v", out)
	}
	// Linked → new session issued for the resolved Patty user only.
	_, _ = svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://kc/realms/x", LegacySubject: "sub-9", PattyUserID: "u9",
	}, "admin")
	out, err = svc.BridgeLegacy("org-m", BridgeEvent{LegacyIssuer: "https://kc/realms/x", LegacySubject: "sub-9"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "linked_issued_session" || !out.NewSessionIssued || out.PattyUserID != "u9" {
		t.Fatalf("linked bridge wrong: %+v", out)
	}
}

// Manifests are idempotent: re-import with the same import_id replaces items,
// never duplicates, and reconciliation reports deterministic counts.
func TestManifestIdempotentAndReconcile(t *testing.T) {
	db := migDB(t)
	svc := New(db)
	items := []ManifestItem{
		{Kind: "realm", LegacyKey: "realm:work", Criticality: "high"},
		{Kind: "client", LegacyKey: "client:pccp", Protocol: "oidc", Criticality: "critical"},
		{Kind: "user", LegacyKey: "user:123", Criticality: "medium"},
	}
	m1, err := svc.ImportManifest("org-m", "estate-1", "keycloak-discovery", 1, "import-1", "admin", items)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := svc.ImportManifest("org-m", "estate-1", "keycloak-discovery", 1, "import-1", "admin", items)
	if err != nil {
		t.Fatal(err)
	}
	if m1.ManifestID != m2.ManifestID {
		t.Fatalf("idempotent replay created a new manifest")
	}
	var count int64
	db.Model(&models.SSOMigrationItem{}).Where("manifest_id = ?", m1.ManifestID).Count(&count)
	if count != 3 {
		t.Fatalf("expected 3 items after idempotent replay, got %d", count)
	}
	// Link two identities so reconciliation reflects them.
	_, _ = svc.LinkIdentity("org-m", IdentityLinkRequest{LegacyIssuer: "https://kc/realms/x", LegacySubject: "user:123", PattyUserID: "u1"}, "admin")
	recon, err := svc.Reconcile("org-m", m1.ManifestID)
	if err != nil {
		t.Fatal(err)
	}
	if recon.Status != "reconciled" || recon.SourceCount != 3 || recon.LinkedCount != 1 {
		t.Fatalf("reconcile wrong: %+v", recon)
	}
}
