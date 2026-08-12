package publiccloud

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

func TestCreateAccountAndSubscription(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)

	acct, err := svc.CreateAccount("test@patty.dev", "Test User", "테스트 사용자", "developer")
	if err != nil {
		t.Fatal(err)
	}
	if acct.SubscriptionStatus != "active" {
		t.Fatalf("expected active subscription, got %s", acct.SubscriptionStatus)
	}
	if acct.MaxHarnesses != 2 {
		t.Fatalf("expected 2 max harnesses for developer plan, got %d", acct.MaxHarnesses)
	}
}

func TestSubscriptionLookup(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)

	acct, _ := svc.CreateAccount("test2@patty.dev", "Test", "테스트", "pro")
	sub, err := svc.GetSubscription(acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Plan != "pro" {
		t.Fatal("expected pro plan")
	}
}

func TestHarnessLimit(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)

	acct, _ := svc.CreateAccount("test3@patty.dev", "Test", "테스트", "developer")

	// Should allow (0 harnesses registered)
	allowed, _ := svc.CheckHarnessLimit(acct.ID)
	if !allowed {
		t.Fatal("should allow first harness")
	}
}

func TestCapacityLease(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)

	acct, _ := svc.CreateAccount("test4@patty.dev", "Test", "테스트", "pro")
	lease, err := svc.IssueCapacityLease(acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lease.ActiveAgentSlots != 5 {
		t.Fatalf("expected 5 slots for pro, got %d", lease.ActiveAgentSlots)
	}
	if lease.CPSignature == "" {
		t.Fatal("expected signed lease")
	}

	// Validate
	valid, _ := svc.ValidateCapacityLease(lease.ID, acct.ID)
	if valid == nil {
		t.Fatal("expected valid lease")
	}
}

func TestWorkSlotAcquire(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)

	acct, _ := svc.CreateAccount("test5@patty.dev", "Test", "테스트", "developer")

	// Acquire interactive slot
	ok, _ := svc.AcquireWorkSlot(acct.ID, SlotInteractive)
	if !ok {
		t.Fatal("should acquire interactive slot")
	}

	// Release
	svc.ReleaseWorkSlot(acct.ID, SlotInteractive)
}

func TestRiskStateSeparation(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)

	acct, _ := svc.CreateAccount("test6@patty.dev", "Test", "테스트", "pro")

	// Set capacity state without affecting integrity
	svc.SetCapacityState(acct.ID, "high_usage")

	// Check states are separate
	got, _ := svc.GetAccount(acct.ID)
	if got.CapacityState != "high_usage" {
		t.Fatal("expected high_usage capacity state")
	}
	if got.AccountIntegrityState != "normal" {
		t.Fatal("integrity should remain normal")
	}
	if got.TrustSafetyState != "normal" {
		t.Fatal("T&S should remain normal")
	}
}

func TestPlanConfigs(t *testing.T) {
	// Free plan has 1 harness
	cfg := getPlanConfig("free")
	if cfg.MaxHarnesses != 1 {
		t.Fatal("free should have 1 harness")
	}

	// Enterprise has 10
	cfg = getPlanConfig("enterprise")
	if cfg.MaxHarnesses != 10 {
		t.Fatal("enterprise should have 10 harnesses")
	}
	if cfg.NormalSlots != 10 {
		t.Fatal("enterprise should have 10 work slots")
	}
}
