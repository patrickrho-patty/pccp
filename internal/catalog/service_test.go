package catalog

import (
	"path/filepath"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDB(t *testing.T) *gorm.DB {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(models.AllModels()...)
	return db
}

func TestSeedAndQueryCatalog(t *testing.T) {
	db := setupDB(t)
	svc, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	// Seed defaults
	if err := svc.SeedDefaultCatalog(); err != nil {
		t.Fatal(err)
	}

	// Get effective catalog (public/global)
	descs, err := svc.GetEffectiveCatalog("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(descs) < 2 {
		t.Fatalf("expected 2+ models, got %d", len(descs))
	}

	// Check Korean names
	found := false
	for _, d := range descs {
		if d.DisplayNameKo == "패티 코드 스탠다드" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Korean display name")
	}
}

func TestCatalogEpochGeneration(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)
	svc.SeedDefaultCatalog()

	epoch, err := svc.GenerateCatalogEpoch("acct-1", "", "rev-1")
	if err != nil {
		t.Fatal(err)
	}
	if epoch.EpochID == "" {
		t.Fatal("expected epoch ID")
	}
	if epoch.ModelsJSON == "" {
		t.Fatal("expected models JSON")
	}
}

func TestCatalogValidation(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)
	svc.SeedDefaultCatalog()

	epoch, _ := svc.GenerateCatalogEpoch("acct-1", "", "rev-1")

	// Valid model in valid epoch
	_, cm, err := svc.ValidateCatalogEpoch(epoch.EpochID, "patty-code-standard")
	if err != nil {
		t.Fatalf("expected valid: %v", err)
	}
	if cm.CatalogModelID != "patty-code-standard" {
		t.Fatal("wrong model returned")
	}

	// Invalid model
	_, _, err = svc.ValidateCatalogEpoch(epoch.EpochID, "fake-model")
	if err == nil {
		t.Fatal("expected error for fake model")
	}
}

func TestModelWithdraw(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)
	svc.SeedDefaultCatalog()

	// Withdraw a model
	svc.WithdrawModel("patty-code-standard", "security issue")

	// Should fail validation
	epoch, _ := svc.GenerateCatalogEpoch("acct-1", "", "rev-1")
	_, _, err := svc.ValidateCatalogEpoch(epoch.EpochID, "patty-code-standard")
	if err == nil {
		t.Fatal("withdrawn model should fail validation")
	}
}

func TestResolveToPackage(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)
	svc.SeedDefaultCatalog()

	pkgID, err := svc.ResolveToPackage("patty-code-standard")
	if err != nil {
		t.Fatal(err)
	}
	if pkgID != "pmp_kocoder_v1" {
		t.Fatalf("expected pmp_kocoder_v1, got %s", pkgID)
	}
}
