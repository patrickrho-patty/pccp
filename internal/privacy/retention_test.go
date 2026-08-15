package privacy

import (
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func retentionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/p.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(&models.AuditEvent{})
	return db
}

// TestRetentionRespectsLegalHold (Task 16 / §40.4): expired rows are
// deleted per policy; rows under an active legal hold are NEVER
// deleted; the spoliation guard refuses audit deletes while a hold is
// active.
func TestRetentionRespectsLegalHold(t *testing.T) {
	db := retentionDB(t)
	svc := New(db)

	old := time.Now().AddDate(-2, 0, 0).Format(time.RFC3339)
	db.Create(&models.AuditEvent{OrganizationID: "o", EventType: "t", Action: "a", OccurredAt: old, Result: "success"})
	db.Create(&models.AuditEvent{OrganizationID: "o", EventType: "t", Action: "held", OccurredAt: old, LegalHold: true, Result: "success"})
	db.Create(&models.AuditEvent{OrganizationID: "o", EventType: "t", Action: "recent", OccurredAt: time.Now().Format(time.RFC3339), Result: "success"})

	results, err := svc.EnforceAuditRetention(DefaultRetentionPolicies(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var totalDeleted int64
	for _, r := range results {
		totalDeleted += r.Deleted
	}
	if totalDeleted != 1 {
		t.Fatalf("deleted = %d (only the expired, unheld row)", totalDeleted)
	}
	var remaining []models.AuditEvent
	db.Where("action IN ?", []string{"held", "recent"}).Find(&remaining)
	if len(remaining) != 2 {
		t.Fatalf("held + recent rows must survive: %d", len(remaining))
	}

	// Spoliation guard: with an ACTIVE hold, the org's deletes refuse;
	// an UNRELATED org is unaffected (org-scoped).
	if err := svc.SetLegalHold("o", "litigation", "counsel"); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDeleteRespectsLegalHold("o", "audit", "any"); err == nil ||
		!strings.Contains(err.Error(), "legal hold") {
		t.Fatalf("expected spoliation guard, got %v", err)
	}
	if err := svc.EnsureDeleteRespectsLegalHold("other-org", "audit", "any"); err != nil {
		t.Fatalf("unrelated org blocked by another org's hold: %v", err)
	}
	if err := svc.EnsureDeleteRespectsLegalHold("", "audit", "any"); err == nil {
		t.Fatal("unscoped deletion gate must refuse")
	}
	// After release, deletes proceed.
	if err := svc.ReleaseLegalHold("o", "settled", "counsel"); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDeleteRespectsLegalHold("o", "audit", "any"); err != nil {
		t.Fatalf("delete still refused after release: %v", err)
	}
	// Hold markers themselves are LegalHold-flagged (survive retention).
	var marker models.AuditEvent
	if err := db.Where("event_type = ?", "cp.legal.hold_activated").First(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if !marker.LegalHold {
		t.Fatal("hold marker must carry LegalHold=true (spoliation guard)")
	}
}
