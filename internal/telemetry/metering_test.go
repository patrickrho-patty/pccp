package telemetry

import (
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecordMeteringIsIdempotentAndUnpriced(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:metering-idempotent?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UsageRecord{}); err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	event := MeteringEvent{
		OrganizationID: "org-1", SessionID: "session-1", ExchangeID: "exchange-1",
		MetricType: MetricType("tokens_in"), Quantity: 42, Unit: "tokens",
		OccurredAt: time.Now().UTC(),
	}
	if err := svc.RecordMetering(event); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordMetering(event); err != nil {
		t.Fatal(err)
	}
	var rows []models.UsageRecord
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("duplicate delivery created %d ledger rows", len(rows))
	}
	if rows[0].PricingState != models.UsagePricingUnpriced || rows[0].Currency != "" || rows[0].EventKey == nil || rows[0].MeteredAt == nil {
		t.Fatalf("metering provenance/pricing state missing: %+v", rows[0])
	}
}

func TestRecordMeteringRequiresExchangeAndOccurrence(t *testing.T) {
	svc := New(nil)
	event := MeteringEvent{OrganizationID: "org-1", SessionID: "session-1", MetricType: MetricTokens}
	if err := svc.RecordMetering(event); err == nil {
		t.Fatal("missing exchange must be rejected before persistence")
	}
	event.ExchangeID = "exchange-1"
	if err := svc.RecordMetering(event); err == nil {
		t.Fatal("missing occurrence time must be rejected before persistence")
	}
}
