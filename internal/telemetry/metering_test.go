package telemetry

import (
	"context"
	"errors"
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
	if err := db.AutoMigrate(&models.Session{}, &models.UsageRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-1"}, SessionID: "session-1", Status: "active", HarnessID: "harness-1", UserID: "user-1", ProjectID: "project-1"}).Error; err != nil {
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

func TestRecordMeteringBatchContextHonorsCancellation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:metering-context?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Session{}, &models.UsageRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-1"}, SessionID: "session-1", Status: "active", HarnessID: "harness-1", UserID: "user-1", ProjectID: "project-1"}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = New(db).RecordMeteringBatchContext(ctx, []MeteringEvent{{
		OrganizationID: "org-1", SessionID: "session-1", ExchangeID: "exchange-1",
		MetricType: MetricTokensIn, Quantity: 1, Unit: "tokens", OccurredAt: time.Now().UTC(),
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled metering error = %v, want context.Canceled", err)
	}
	var count int64
	if err := db.Model(&models.UsageRecord{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("canceled metering persisted %d records", count)
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

func TestRecordMeteringValidatesOwnershipUnitAndAdjustment(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:metering-validation?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Session{}, &models.UsageRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-1"}, SessionID: "session-1", Status: "active", HarnessID: "harness-1", UserID: "user-1", ProjectID: "project-1"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	base := MeteringEvent{
		OrganizationID: "org-1", SessionID: "session-1", ExchangeID: "exchange-1",
		MetricType: MetricTokensIn, Quantity: 1, Unit: "tokens", OccurredAt: time.Now().UTC(),
		PricingState: models.UsagePricingPriced, Currency: "krw", CostMicros: 2,
		Adjustment: true, AppliedRateMicrosPer1K: 1_500, AppliedPriceVersion: "contract-v2", AppliedPriceSource: "model-catalog",
	}
	for name, mutate := range map[string]func(*MeteringEvent){
		"cross organization": func(event *MeteringEvent) { event.OrganizationID = "org-2" },
		"wrong user":         func(event *MeteringEvent) { event.UserID = "user-2" },
		"wrong harness":      func(event *MeteringEvent) { event.HarnessID = "harness-2" },
		"missing unit":       func(event *MeteringEvent) { event.Unit = "" },
		"wrong unit":         func(event *MeteringEvent) { event.Unit = "bytes" },
		"negative usage":     func(event *MeteringEvent) { event.Quantity = -1; event.Adjustment = false },
		"missing price version": func(event *MeteringEvent) {
			event.AppliedPriceVersion = ""
		},
		"missing price source": func(event *MeteringEvent) {
			event.AppliedPriceSource = ""
		},
		"inconsistent exact cost": func(event *MeteringEvent) {
			event.CostMicros = 3
		},
	} {
		t.Run(name, func(t *testing.T) {
			event := base
			mutate(&event)
			if err := svc.RecordMetering(event); err == nil {
				t.Fatal("invalid metering event was accepted")
			}
		})
	}
	if err := svc.RecordMetering(base); err != nil {
		t.Fatal(err)
	}
	var stored models.UsageRecord
	if err := db.First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.UserID != "user-1" || stored.HarnessID != "harness-1" || stored.SessionID != "session-1" || stored.ProjectID != "project-1" {
		t.Fatalf("authenticated session attribution missing: %+v", stored)
	}
	if !stored.Adjustment || stored.AppliedRateMicrosPer1K != 1_500 || stored.AppliedPriceVersion != "contract-v2" || stored.AppliedPriceSource != "model-catalog" {
		t.Fatalf("immutable pricing provenance missing: %+v", stored)
	}
}
