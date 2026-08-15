package detection

import (
	"path/filepath"
	"testing"
	"time"

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

func TestGeoImplausible(t *testing.T) {
	svc := New(setupDB(t))
	findings := svc.CheckGeoImplausible("acct-1", []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}, []string{"Seoul", "NYC", "London"})
	if len(findings) == 0 {
		t.Fatal("expected geo implausible finding")
	}
	if findings[0].Severity != "high" {
		t.Fatalf("expected high severity, got %s", findings[0].Severity)
	}
}

func TestCredentialReplay(t *testing.T) {
	svc := New(setupDB(t))
	findings := svc.CheckCredentialReplay("acct-1", "hash123", []string{"conn-1", "conn-2"})
	if len(findings) == 0 {
		t.Fatal("expected credential replay finding")
	}
	if findings[0].Severity != "critical" {
		t.Fatalf("expected critical, got %s", findings[0].Severity)
	}
}

func TestMultiShift(t *testing.T) {
	svc := New(setupDB(t))
	hours := make([]int, 22)
	for i := 0; i < 22; i++ {
		hours[i] = i
	}
	findings := svc.CheckMultiShiftPattern("acct-1", hours)
	if len(findings) == 0 {
		t.Fatal("expected multi-shift finding")
	}
}

func TestRiskScore(t *testing.T) {
	svc := New(setupDB(t))
	// Add some signals
	svc.CheckGeoImplausible("acct-1", nil, []string{"A", "B", "C"}) // high = 20
	svc.CheckCredentialReplay("acct-1", "h", []string{"1", "2"})    // critical = 30

	score := svc.GetRiskScore("acct-1")
	if score != 50 {
		t.Fatalf("expected score 50, got %d", score)
	}
}

func TestGraduatedResponse(t *testing.T) {
	svc := New(setupDB(t))

	// Low risk
	if action := svc.GetRecommendedAction("acct-1"); action != "observe" {
		t.Fatalf("expected observe, got %s", action)
	}

	// Add critical signal
	svc.CheckCredentialReplay("acct-1", "h", []string{"1", "2"})
	action := svc.GetRecommendedAction("acct-1")
	if action != "step_up_auth" && action != "revoke_harness" {
		t.Fatalf("expected elevated action, got %s", action)
	}
}

func TestClearSignals(t *testing.T) {
	svc := New(setupDB(t))
	svc.RecordConcurrentHarness("acct-1", "hrn-1", "1.1.1.1")
	if len(svc.GetSignals("acct-1")) == 0 {
		t.Fatal("expected signals")
	}
	svc.ClearSignals("acct-1")
	if len(svc.GetSignals("acct-1")) != 0 {
		t.Fatal("expected cleared")
	}
}

func TestNoBanFromSingleSignal(t *testing.T) {
	svc := New(setupDB(t))
	// Per §10C.9: single signal should not cause permanent ban
	svc.RecordConcurrentHarness("acct-1", "hrn-1", "1.1.1.1")
	action := svc.GetRecommendedAction("acct-1")
	if action == "suspend" || action == "restrict" {
		t.Fatalf("single info signal should not trigger %s", action)
	}
}

// Ensure time import is used
var _ = time.Now
