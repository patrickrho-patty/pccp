package ssomigrate

import (
	"errors"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
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
	for _, statement := range []string{
		"CREATE UNIQUE INDEX idx_sso_link_legacy ON sso_identity_links (organization_id, legacy_issuer, legacy_subject)",
		"CREATE UNIQUE INDEX idx_sso_manifest_org_manifest ON sso_migration_manifests (organization_id, manifest_id)",
		"CREATE UNIQUE INDEX idx_sso_manifest_org_import ON sso_migration_manifests (organization_id, import_id)",
		"CREATE INDEX idx_sso_link_org_subject ON sso_identity_links (organization_id, legacy_subject)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	org := models.Organization{Base: models.Base{ID: "org-m"}, Name: "Migration", Slug: "migration-tests", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"u1", "u2", "u9", "other-user", "patty-user"} {
		user := models.User{AuditBase: models.AuditBase{Base: models.Base{ID: id}, OrganizationID: org.ID},
			Email: id + "@example.com", Name: id, Status: models.UserStatusActive}
		user.Email = strings.ReplaceAll(user.Email, "_", "-")
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("seed user %d: %v", index, err)
		}
	}
	return db
}

func bridgeWithVerifiedUser(
	t *testing.T,
	db *gorm.DB,
	svc *Service,
	orgID, userID string,
	event BridgeEvent,
	complete func(*gorm.DB, *models.User) error,
) (*models.SSOMigrationBridgeEvent, error) {
	t.Helper()
	var row *models.SSOMigrationBridgeEvent
	err := db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Where("organization_id = ? AND id = ?", orgID, userID).First(&user).Error; err != nil {
			return err
		}
		var bridgeErr error
		row, bridgeErr = svc.BridgeLegacyWithDB(tx, orgID, event,
			identity.NormalizeExternalIdentity(event.LegacyIssuer, event.LegacySubject), &user, complete)
		return bridgeErr
	})
	return row, err
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
	db := migDB(t)
	svc := New(db)
	// Unlinked → fail closed (422-style decision, no session).
	out, err := bridgeWithVerifiedUser(t, db, svc, "org-m", "u9", BridgeEvent{LegacyIssuer: "https://kc/realms/x", LegacySubject: "nobody"}, nil)
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
	completed := false
	out, err = bridgeWithVerifiedUser(t, db, svc, "org-m", "u9", BridgeEvent{LegacyIssuer: "https://kc/realms/x", LegacySubject: "sub-9"}, func(tx *gorm.DB, user *models.User) error {
		completed = true
		if user.ID != "u9" || user.OrganizationID != "org-m" || user.Status != models.UserStatusActive {
			t.Fatalf("completion received untrusted user: %+v", user)
		}
		return tx.Model(user).Update("last_login_at", "2026-08-22T12:00:00Z").Error
	})
	if err != nil {
		t.Fatal(err)
	}
	if !completed || out.Decision != "linked_issued_session" || !out.NewSessionIssued || out.PattyUserID != "u9" {
		t.Fatalf("linked bridge wrong: %+v", out)
	}
}

func TestBridgeRecordsNoIssuedSessionWhenCompletionFails(t *testing.T) {
	db := migDB(t)
	svc := New(db)
	if _, err := svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://kc/realms/x", LegacySubject: "sub-rollback", PattyUserID: "u9",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("token signer unavailable")
	row, err := bridgeWithVerifiedUser(t, db, svc, "org-m", "u9", BridgeEvent{
		LegacyIssuer: "https://kc/realms/x", LegacySubject: "sub-rollback",
	}, func(tx *gorm.DB, user *models.User) error {
		if updateErr := tx.Model(user).Update("last_login_at", "2026-08-22T13:00:00Z").Error; updateErr != nil {
			return updateErr
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || row != nil {
		t.Fatalf("failed completion returned row=%+v err=%v", row, err)
	}
	var events int64
	if countErr := db.Model(&models.SSOMigrationBridgeEvent{}).
		Where("organization_id = ? AND legacy_subject = ?", "org-m", "sub-rollback").Count(&events).Error; countErr != nil {
		t.Fatal(countErr)
	}
	if events != 0 {
		t.Fatalf("failed completion recorded %d issued-session events", events)
	}
	var user models.User
	if queryErr := db.Where("organization_id = ? AND id = ?", "org-m", "u9").First(&user).Error; queryErr != nil {
		t.Fatal(queryErr)
	}
	if user.LastLoginAt != nil {
		t.Fatalf("failed completion escaped transaction rollback: %q", *user.LastLoginAt)
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
	m1, err := svc.ImportManifest("org-m", "estate-1", "keycloak-discovery", "https://kc/realms/x", 1, "import-1", "admin", items)
	if err != nil {
		t.Fatal(err)
	}
	var beforeItems []models.SSOMigrationItem
	if err := db.Where("organization_id = ? AND manifest_id = ?", "org-m", m1.ManifestID).Order("id").Find(&beforeItems).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(m1).Update("status", models.SSOManifestStatusWaveReady).Error; err != nil {
		t.Fatal(err)
	}
	m2, err := svc.ImportManifest("org-m", "estate-1", "keycloak-discovery", "https://kc/realms/x", 1, "import-1", "admin", items)
	if err != nil {
		t.Fatal(err)
	}
	if m1.ManifestID != m2.ManifestID {
		t.Fatalf("idempotent replay created a new manifest")
	}
	if m2.Status != models.SSOManifestStatusWaveReady {
		t.Fatalf("exact replay reset reconciliation status: %s", m2.Status)
	}
	var afterItems []models.SSOMigrationItem
	if err := db.Where("organization_id = ? AND manifest_id = ?", "org-m", m1.ManifestID).Order("id").Find(&afterItems).Error; err != nil {
		t.Fatal(err)
	}
	for i := range beforeItems {
		if beforeItems[i].ID != afterItems[i].ID {
			t.Fatalf("exact replay replaced item %q with %q", beforeItems[i].ID, afterItems[i].ID)
		}
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
	if recon.Manifest.Status != models.SSOManifestStatusReconciled || recon.Manifest.SourceCount != 3 || recon.Manifest.LinkedCount != 1 {
		t.Fatalf("reconcile wrong: %+v", recon)
	}
}

func TestReconcileIsManifestScopedAndRequiresEveryItemDisposition(t *testing.T) {
	db := migDB(t)
	svc := New(db)
	manifest, err := svc.ImportManifest("org-m", "sso-to-patty", "sso", "https://login.patty.io/realms/sso", 1, "realm-import-1", "admin", []ManifestItem{
		{Kind: "client", LegacyKey: "patty-web", Disposition: "keep"},
		{Kind: "user", LegacyKey: "subject-in-manifest"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// An organization-wide link outside this manifest must not inflate parity.
	_, _ = svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://login.patty.io/realms/sso", LegacySubject: "other-subject", PattyUserID: "other-user",
	}, "admin")
	_, _ = svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://attacker.example/realm", LegacySubject: "subject-in-manifest", PattyUserID: "u2",
	}, "admin")

	recon, err := svc.Reconcile("org-m", manifest.ManifestID)
	if err != nil {
		t.Fatal(err)
	}
	if recon.Manifest.LinkedCount != 0 || recon.Manifest.TargetCount != 1 || recon.Manifest.ConflictCount != 1 || recon.Manifest.Status != models.SSOManifestStatusReconciled {
		t.Fatalf("unresolved manifest must remain blocked from wave readiness: %+v", recon)
	}

	_, _ = svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://login.patty.io/realms/sso", LegacySubject: "subject-in-manifest", PattyUserID: "patty-user",
		TargetIssuer: "https://login.patty.io/realms/patty", TargetSubject: "subject-in-manifest",
	}, "admin")
	recon, err = svc.Reconcile("org-m", manifest.ManifestID)
	if err != nil {
		t.Fatal(err)
	}
	if recon.Manifest.LinkedCount != 1 || recon.Manifest.TargetCount != 2 || recon.Manifest.ConflictCount != 0 || recon.Manifest.Status != models.SSOManifestStatusWaveReady {
		t.Fatalf("fully dispositioned manifest must become wave ready: %+v", recon)
	}
}

func TestBridgeRejectsCallerIdentityDifferentFromVerifiedHandoffSource(t *testing.T) {
	db := migDB(t)
	svc := New(db)
	if _, err := svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://kc/realms/x", LegacySubject: "verified-subject", PattyUserID: "u9",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	var verified models.User
	if err := db.Where("organization_id = ? AND id = ?", "org-m", "u9").First(&verified).Error; err != nil {
		t.Fatal(err)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, bridgeErr := svc.BridgeLegacyWithDB(tx, "org-m", BridgeEvent{
			LegacyIssuer: "https://kc/realms/x", LegacySubject: "caller-subject",
		}, identity.NormalizeExternalIdentity("https://kc/realms/x", "verified-subject"), &verified,
			func(*gorm.DB, *models.User) error { return nil })
		return bridgeErr
	})
	if err == nil || !strings.Contains(err.Error(), "does not match verified login source") {
		t.Fatalf("bridge caller identity mismatch was accepted: %v", err)
	}
	var events int64
	if err := db.Model(&models.SSOMigrationBridgeEvent{}).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("mismatched bridge persisted %d audit events", events)
	}
}

func TestBridgeWithVerifiedLoginRejectsAnotherLinkedUser(t *testing.T) {
	db := migDB(t)
	svc := New(db)
	if _, err := svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://kc/realms/x", LegacySubject: "sub-u9", PattyUserID: "u9",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	var verified models.User
	if err := db.Where("organization_id = ? AND id = ?", "org-m", "u1").First(&verified).Error; err != nil {
		t.Fatal(err)
	}
	called := false
	err := db.Transaction(func(tx *gorm.DB) error {
		event := BridgeEvent{
			LegacyIssuer: "https://kc/realms/x", LegacySubject: "sub-u9",
		}
		_, bridgeErr := svc.BridgeLegacyWithDB(tx, "org-m", event,
			identity.NormalizeExternalIdentity(event.LegacyIssuer, event.LegacySubject), &verified, func(*gorm.DB, *models.User) error {
				called = true
				return nil
			})
		return bridgeErr
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched verified user was accepted: %v", err)
	}
	if called {
		t.Fatal("session issuance callback ran for mismatched verified user")
	}
	var events int64
	if err := db.Model(&models.SSOMigrationBridgeEvent{}).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("mismatched verified login persisted %d bridge events", events)
	}
}

func TestSignOffWaveRequiresReconciledParityAndRollbackWindow(t *testing.T) {
	db := migDB(t)
	svc := New(db)
	manifest, err := svc.ImportManifest("org-m", "sso-to-patty", "sso", "https://login.patty.io/realms/sso", 1, "realm-import-2", "admin", []ManifestItem{
		{Kind: "user", LegacyKey: "subject-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wave := models.SSOMigrationWave{OrganizationID: "org-m", ManifestID: manifest.ManifestID, Wave: 1, Status: "planned"}
	if err := db.Create(&wave).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SignOffWave("org-m", wave.ID, "admin", "48h"); err == nil {
		t.Fatal("unreconciled wave must not be signed off")
	}
	_, _ = svc.LinkIdentity("org-m", IdentityLinkRequest{
		LegacyIssuer: "https://login.patty.io/realms/sso", LegacySubject: "subject-1", PattyUserID: "u1",
		TargetIssuer: "https://login.patty.io/realms/patty", TargetSubject: "subject-1",
	}, "admin")
	if _, err := svc.Reconcile("org-m", manifest.ManifestID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SignOffWave("org-m", wave.ID, "admin", ""); err == nil {
		t.Fatal("wave sign-off must require a rollback window")
	}
	if err := svc.SignOffWave("org-m", wave.ID, "admin", "48h"); err != nil {
		t.Fatalf("wave-ready sign-off failed: %v", err)
	}
}
