package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func backfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/backfill.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AlertEndpoint{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestBackfillMigratesVerifiesAndQuarantinesExposedPlaintext(t *testing.T) {
	db := backfillTestDB(t)
	provider, _ := keymgmt.NewLocalProvider([]byte("0123456789abcdef0123456789abcdef"), "prod-v1")
	target := "https://hooks.slack.com/services/A/B/exposed"
	ep := models.AlertEndpoint{OrganizationID: "org-1", Name: "legacy", Type: "slack", Target: target, Enabled: true}
	db.Create(&ep)

	report, err := BackfillAlertEndpoints(db, provider, 100)
	if err != nil {
		t.Fatal(err)
	}
	if report.Migrated != 1 || report.Verified != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	var stored models.AlertEndpoint
	db.First(&stored, "id = ?", ep.ID)
	if stored.Target != "" || stored.TargetEnc == "" || stored.CredentialID == "" {
		t.Fatalf("legacy secret not fully migrated: %+v", stored)
	}
	if stored.Enabled || !stored.RotationRequired {
		t.Fatalf("previously exposed credential must be disabled pending provider rotation: %+v", stored)
	}
	if _, err := BackfillAlertEndpoints(db, provider, 100); err != nil {
		t.Fatalf("backfill must be idempotent: %v", err)
	}
}

type failingProvider struct{}

func (f failingProvider) KEKID() string                    { return "broken" }
func (f failingProvider) GetKEK() ([]byte, error)          { return nil, errors.New("broken") }
func (f failingProvider) WrapKey([]byte) ([]byte, error)   { return nil, errors.New("broken") }
func (f failingProvider) UnwrapKey([]byte) ([]byte, error) { return nil, errors.New("broken") }

func TestBackfillReturnsFailureAndRollsBackBatch(t *testing.T) {
	db := backfillTestDB(t)
	for i := 0; i < 2; i++ {
		db.Create(&models.AlertEndpoint{OrganizationID: "org-1", Name: "legacy", Type: "webhook", Target: "https://example.com/hook", Enabled: true})
	}
	if _, err := BackfillAlertEndpoints(db, failingProvider{}, 100); err == nil {
		t.Fatal("provider failure must make the command fail")
	}
	var remaining int64
	db.Model(&models.AlertEndpoint{}).Where("target <> ''").Count(&remaining)
	if remaining != 2 {
		t.Fatalf("failed batch partially committed: remaining=%d", remaining)
	}
}

func TestBackfillRebindsRetainedKEKRowsUnderPrimary(t *testing.T) {
	db := backfillTestDB(t)
	oldProvider, _ := keymgmt.NewLocalProvider([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), "old")
	newProvider, _ := keymgmt.NewLocalProvider([]byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), "new")
	ring, err := keymgmt.NewProviderRing(newProvider, oldProvider)
	if err != nil {
		t.Fatal(err)
	}
	encoded, oldID, err := keymgmt.SealEncoded(oldProvider, "https://example.com/legacy")
	if err != nil {
		t.Fatal(err)
	}
	ep := models.AlertEndpoint{OrganizationID: "org-1", Name: "legacy-envelope", Type: "webhook", TargetEnc: encoded, TargetKEKID: oldID, Enabled: true}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}

	report, err := BackfillAlertEndpoints(db, ring, 1)
	if err != nil {
		t.Fatal(err)
	}
	if report.Fingerprint != 1 || report.Verified != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	var stored models.AlertEndpoint
	db.First(&stored, "id = ?", ep.ID)
	if stored.TargetKEKID != "new" || stored.TargetBindingVersion != keymgmt.AlertBindingVersion || stored.CredentialID == "" {
		t.Fatalf("retained-key row was not rebound under primary: %+v", stored)
	}
	if !stored.Enabled || stored.RotationRequired {
		t.Fatalf("never-plaintext encrypted row must not be quarantined: %+v", stored)
	}
}

func TestBackfillCompareAndSwapRejectsConcurrentRotation(t *testing.T) {
	db := backfillTestDB(t)
	ep := models.AlertEndpoint{OrganizationID: "org-1", Name: "legacy", Type: "webhook", Target: "https://example.com/old", Enabled: true}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	original := ep
	if err := db.Model(&models.AlertEndpoint{}).Where("id = ?", ep.ID).Updates(map[string]interface{}{
		"target": "", "target_enc": "concurrently-rotated", "target_kek_id": "new", "target_binding_version": 1, "credential_id": "hm:new",
	}).Error; err != nil {
		t.Fatal(err)
	}
	err := applyPreparedUpdates(db, []preparedUpdate{{original: original, values: map[string]interface{}{"target": "", "target_enc": "stale"}}})
	if err == nil {
		t.Fatal("stale migration update must not overwrite a concurrent rotation")
	}
	var stored models.AlertEndpoint
	if err := db.First(&stored, "id = ?", ep.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TargetEnc != "concurrently-rotated" || stored.CredentialID != "hm:new" {
		t.Fatalf("concurrent rotation was overwritten: %+v", stored)
	}
}

func TestBackfillMigratesLegacyNullMetadata(t *testing.T) {
	db := backfillTestDB(t)
	provider, _ := keymgmt.NewLocalProvider([]byte("0123456789abcdef0123456789abcdef"), "prod-v1")
	if err := db.Exec(`INSERT INTO alert_endpoints (id, organization_id, name, type, target, target_enc, target_kek_id, target_binding_version, credential_id, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		"legacy-null", "org-1", "legacy", "webhook", "https://example.com/legacy", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := BackfillAlertEndpoints(db, provider, 10); err != nil {
		t.Fatalf("nullable legacy metadata should not be mistaken for a concurrent rotation: %v", err)
	}
	var stored models.AlertEndpoint
	if err := db.First(&stored, "id = ?", "legacy-null").Error; err != nil {
		t.Fatal(err)
	}
	if stored.Target != "" || stored.TargetEnc == "" || stored.CredentialID == "" {
		t.Fatalf("legacy nullable row was not migrated: %+v", stored)
	}
}

func TestBackfillMigratesBoundEnvelopeWithLegacyFingerprint(t *testing.T) {
	db := backfillTestDB(t)
	provider, _ := keymgmt.NewLocalProvider([]byte("0123456789abcdef0123456789abcdef"), "prod-v1")
	target := "https://example.com/previous-format"
	ctx := keymgmt.AlertSecretContext{OrganizationID: "org-1", EndpointID: "legacy-h-bound", ProviderType: "webhook"}
	encoded, kekID, _, bindingVersion, err := keymgmt.SealAlertSecret(provider, target, ctx)
	if err != nil {
		t.Fatal(err)
	}
	ep := models.AlertEndpoint{Base: models.Base{ID: ctx.EndpointID}, OrganizationID: ctx.OrganizationID, Name: "previous", Type: ctx.ProviderType,
		TargetEnc: encoded, TargetKEKID: kekID, TargetBindingVersion: bindingVersion,
		CredentialID: keymgmt.DomainFingerprint("DARI-ALERT-CREDENTIAL-v1", target), Enabled: true}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	report, err := BackfillAlertEndpoints(db, provider, 10)
	if err != nil {
		t.Fatalf("bound envelope with legacy fingerprint must migrate: %v", err)
	}
	if report.Fingerprint != 1 || report.Verified != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	var stored models.AlertEndpoint
	if err := db.First(&stored, "id = ?", ep.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored.CredentialID, "hm:") {
		t.Fatalf("legacy fingerprint was not replaced: %q", stored.CredentialID)
	}
}
