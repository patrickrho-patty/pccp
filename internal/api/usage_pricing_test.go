package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

func TestUpdateModelPriceDistinguishesAbsentFromConfiguredFreeAndStoresExactMicros(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-model-price", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	pkg := models.ModelPackage{
		PackageID: "pmp-price", ModelID: "price", Name: "Price", State: "published",
		PriceInputPer1K: 7.25, PriceOutputPer1K: 9.5,
	}
	if err := db.Create(&pkg).Error; err != nil {
		t.Fatal(err)
	}

	rec := doJSONAsRole(t, srv, http.MethodPut, "/api/models/"+pkg.ID, `{"name":"Renamed"}`, org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("absent price update = %d %s", rec.Code, rec.Body.String())
	}
	var stored models.ModelPackage
	if err := db.First(&stored, "id = ?", pkg.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PriceInputConfigured || stored.PriceOutputConfigured || stored.PriceInputPer1K != 7.25 || stored.PriceOutputPer1K != 9.5 {
		t.Fatalf("absent price fields changed pricing state: %+v", stored)
	}

	rec = doJSONAsRole(t, srv, http.MethodPut, "/api/models/"+pkg.ID,
		`{"price_input_per_1k":0,"price_output_per_1k":12.345678,"price_version":"contract-v2","price_source":"enterprise-contract"}`,
		org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("exact price update = %d %s", rec.Code, rec.Body.String())
	}
	if err := db.First(&stored, "id = ?", pkg.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.PriceInputConfigured || stored.PriceInputMicrosPer1K != 0 || !stored.PriceOutputConfigured || stored.PriceOutputMicrosPer1K != 12_345_678 {
		t.Fatalf("configured-free/exact prices not persisted: %+v", stored)
	}
	if stored.PriceVersion != "contract-v2" || stored.PriceSource != "enterprise-contract" {
		t.Fatalf("price provenance not persisted: %+v", stored)
	}
}

func TestCostAnalysisTreatsExplicitZeroAsConfiguredPrice(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-model-free-price", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	pkg := models.ModelPackage{
		PackageID: "pmp-free", ModelID: "free", Name: "Free", State: "published",
		PriceInputConfigured: true, PriceOutputConfigured: true,
		PriceVersion: "free-v1", PriceSource: "catalog",
	}
	if err := db.Create(&pkg).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.Create(&models.UsageRecord{OrganizationID: org.ID, ModelPackageID: pkg.PackageID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1_000, OccurredAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	rec := doJSONAsRole(t, srv, http.MethodGet, "/api/analytics/cost", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("cost analysis = %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Models []modelCostRow `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Models) != 1 || !payload.Models[0].Priced || payload.Models[0].ExpectedCostMicros != 0 {
		t.Fatalf("configured-free model was not priced exactly: %+v", payload.Models)
	}
}

func TestUsageLedgerTransportsConfiguredFreeAppliedRate(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ledger-free-price", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UsageRecord{
		OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1,
		PricingState: models.UsagePricingPriced, Currency: "KRW", CostMicros: 0,
		AppliedRateMicrosPer1K: 0, AppliedPriceVersion: "free-v1", AppliedPriceSource: "catalog",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	}).Error; err != nil {
		t.Fatal(err)
	}
	rec := doJSONAsRole(t, srv, http.MethodGet, "/api/analytics/usage-extended?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("usage ledger = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"applied_rate_micros_per_1k":"0"`) {
		t.Fatalf("configured-free applied rate was omitted from the wire: %s", rec.Body.String())
	}
}

func TestUpdateModelPriceRejectsUnrepresentableDecimalWithoutMutation(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-model-price-invalid", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	pkg := models.ModelPackage{PackageID: "pmp-price-invalid", ModelID: "price-invalid", Name: "Price", State: "published"}
	if err := db.Create(&pkg).Error; err != nil {
		t.Fatal(err)
	}
	rec := doJSONAsRole(t, srv, http.MethodPut, "/api/models/"+pkg.ID, `{"price_input_per_1k":0.0000001}`, org.ID, "admin")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inexact price update = %d %s, want 400", rec.Code, rec.Body.String())
	}
	var stored models.ModelPackage
	if err := db.First(&stored, "id = ?", pkg.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PriceInputConfigured || stored.PriceInputMicrosPer1K != 0 {
		t.Fatalf("rejected price mutated package: %+v", stored)
	}
}

func TestUpdateModelPriceRejectsExplicitlyEmptyProvenance(t *testing.T) {
	for name, body := range map[string]string{
		"version": `{"price_input_per_1k":1,"price_version":""}`,
		"source":  `{"price_output_per_1k":1,"price_source":"   "}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv, db := securityTestServer(t)
			org := models.Organization{Name: "o", Slug: "o-model-price-empty-" + name, Status: "active"}
			if err := db.Create(&org).Error; err != nil {
				t.Fatal(err)
			}
			pkg := models.ModelPackage{PackageID: "pmp-price-empty-" + name, ModelID: "price-empty-" + name, Name: "Price", State: "published"}
			if err := db.Create(&pkg).Error; err != nil {
				t.Fatal(err)
			}
			rec := doJSONAsRole(t, srv, http.MethodPut, "/api/models/"+pkg.ID, body, org.ID, "admin")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("empty %s = %d %s, want 400", name, rec.Code, rec.Body.String())
			}
			var stored models.ModelPackage
			if err := db.First(&stored, "id = ?", pkg.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.PriceInputConfigured || stored.PriceOutputConfigured {
				t.Fatalf("rejected provenance mutated package: %+v", stored)
			}
		})
	}
}

func TestUpdateModelPriceAdvancesVersionWhenRateChanges(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-model-price-version", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	pkg := models.ModelPackage{
		PackageID: "pmp-price-version", ModelID: "price-version", Name: "Price", State: "published",
		PriceInputConfigured: true, PriceInputMicrosPer1K: 1_000_000,
		PriceVersion: "contract-v1", PriceSource: "enterprise-contract",
	}
	if err := db.Create(&pkg).Error; err != nil {
		t.Fatal(err)
	}

	rec := doJSONAsRole(t, srv, http.MethodPut, "/api/models/"+pkg.ID, `{"price_input_per_1k":2}`, org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("price update = %d %s", rec.Code, rec.Body.String())
	}
	var stored models.ModelPackage
	if err := db.First(&stored, "id = ?", pkg.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PriceVersion == "contract-v1" || stored.PriceVersion == "" {
		t.Fatalf("changed rate reused stale price version: %+v", stored)
	}
	if stored.PriceSource != "enterprise-contract" {
		t.Fatalf("rate change unexpectedly replaced price source: %+v", stored)
	}
}

func TestUpdateModelPriceRejectsReusedVersionForChangedRate(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-model-price-version-reuse", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	pkg := models.ModelPackage{
		PackageID: "pmp-price-version-reuse", ModelID: "price-version-reuse", Name: "Price", State: "published",
		PriceInputConfigured: true, PriceInputMicrosPer1K: 1_000_000,
		PriceVersion: "contract-v1", PriceSource: "enterprise-contract",
	}
	if err := db.Create(&pkg).Error; err != nil {
		t.Fatal(err)
	}

	rec := doJSONAsRole(t, srv, http.MethodPut, "/api/models/"+pkg.ID, `{"price_input_per_1k":2,"price_version":"contract-v1"}`, org.ID, "admin")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reused version update = %d %s, want 400", rec.Code, rec.Body.String())
	}
	var stored models.ModelPackage
	if err := db.First(&stored, "id = ?", pkg.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PriceInputMicrosPer1K != 1_000_000 || stored.PriceVersion != "contract-v1" {
		t.Fatalf("rejected version reuse mutated price: %+v", stored)
	}
}
