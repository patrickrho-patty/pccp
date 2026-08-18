package relay

import (
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRevocationControlEventIsAppliedByEveryRelayReplica(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/relay-control.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Organization{Base: models.Base{ID: "org"}, Name: "org", Slug: "org", Status: "active"})
	db.Create(&models.Harness{OrganizationID: "org", HarnessID: "harness", Status: "active"})

	first, err := New(db, "", "relay-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(db, "", "relay-b")
	if err != nil {
		t.Fatal(err)
	}
	event, err := first.QueueRevocationEvent("org", "harness", "security incident")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	first.processControlEvents(now)
	second.processControlEvents(now)
	var ackCount int64
	if err := db.Model(&models.RelayControlAck{}).Where("event_id = ?", event.ID).Count(&ackCount).Error; err != nil {
		t.Fatal(err)
	}
	if ackCount != 2 {
		t.Fatalf("replica acknowledgements = %d, want 2", ackCount)
	}
	var harness models.Harness
	if err := db.Where("organization_id = ? AND harness_id = ?", "org", "harness").First(&harness).Error; err != nil || harness.Status != "revoked" {
		t.Fatalf("durable harness status = %q err=%v", harness.Status, err)
	}
}
