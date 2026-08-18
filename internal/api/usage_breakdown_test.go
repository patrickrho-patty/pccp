package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type usageSQLLogger struct {
	logger.Interface
	statements []string
}

func (l *usageSQLLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, _ := fc()
	l.statements = append(l.statements, sql)
}

// PAT-1501 — usage breakdown redaction discipline tests.
//
// Cross-unit aggregation is structurally impossible at the response
// type level: every row carries an explicit Unit and the ByUnit map
// is keyed by unit. These tests assert the contract.

func TestUsageBreakdownReturnsByUnitMap(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage", Status: "active"}
	db.Create(&org)
	now := time.Now().Format(time.RFC3339)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1000, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_out", Unit: "tokens", Quantity: 500, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "gpu_seconds", Unit: "seconds", Quantity: 187, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "storage_bytes", Unit: "bytes", Quantity: 5242880, OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var resp UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp.ByUnit["tokens"]; !ok {
		t.Fatalf("by_unit must include tokens; got keys=%v", keysOf(resp.ByUnit))
	}
	if resp.ByUnit["tokens"].Quantity != 1500 {
		t.Fatalf("tokens total = %d, want 1500 (in+out)", resp.ByUnit["tokens"].Quantity)
	}
	if resp.ByUnit["seconds"].Quantity != 187 {
		t.Fatalf("seconds total = %d, want 187", resp.ByUnit["seconds"].Quantity)
	}
	if resp.ByUnit["bytes"].Quantity != 5242880 {
		t.Fatalf("bytes total = %d, want 5242880", resp.ByUnit["bytes"].Quantity)
	}
	// Cross-unit sums must NOT appear as a single number anywhere
	// in the response.
	body := rec.Body.String()
	if strings.Contains(body, `"total":5244567`) {
		t.Fatalf("response leaked a cross-unit sum: %s", body)
	}
	if resp.Reconciled || resp.DisplayTotal.State != MeterStateUnavailable {
		t.Fatalf("metered usage without asserted pricing must keep cost unavailable")
	}
}

func TestUsageBreakdownEveryUnitPresentEvenZero(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-zero", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var resp UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"tokens", "seconds", "bytes", "count", "usd_micro"} {
		if _, ok := resp.ByUnit[u]; !ok {
			t.Fatalf("by_unit must include %q even when zero, got keys=%v", u, keysOf(resp.ByUnit))
		}
	}
	if resp.DisplayTotal.State != MeterStateUnavailable {
		t.Fatalf("an empty ledger must be unavailable, not a trustworthy zero: %+v", resp.DisplayTotal)
	}
}

func TestUsageCostAnalysisRowsCarryUnits(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-cost", Status: "active"}
	db.Create(&org)
	now := time.Now().Format(time.RFC3339)
	db.Create(&models.ModelPackage{PackageID: "p1", Name: "priced", PriceInputPer1K: 100})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1000, OccurredAt: now, ModelPackageID: "p1"})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_out", Unit: "tokens", Quantity: 200, OccurredAt: now, ModelPackageID: "p1"})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/cost?days=30", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("cost: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Models      []modelCostRow `json:"models"`
		Total       Usage          `json:"total"`
		UsageReport UsageTotal     `json:"usage_report"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Models) == 0 {
		t.Fatalf("expected at least one model row, got 0")
	}
	for _, m := range resp.Models {
		if m.TokensUnit != "tokens" {
			t.Fatalf("model row tokens_unit = %q, want tokens", m.TokensUnit)
		}
		if m.CostUnit != "krw" {
			t.Fatalf("model row cost_unit = %q, want krw", m.CostUnit)
		}
	}
	if resp.Total.Unit != UnitCurrencyMicro {
		t.Fatalf("total unit = %q, want %s", resp.Total.Unit, UnitCurrencyMicro)
	}
	if resp.Total.State != MeterStateUnavailable || resp.Total.Quantity != 0 {
		t.Fatalf("catalog estimate became an authoritative ledger total: %+v", resp.Total)
	}
	if resp.Total.Reconciled || resp.Models[0].Reconciled {
		t.Fatalf("catalog estimate must not be labeled reconciled when the usage ledger has no matching cost")
	}
	if len(resp.UsageReport.Drilldown) != 0 || len(resp.UsageReport.ByUser) != 0 || len(resp.UsageReport.BySession) != 0 {
		t.Fatalf("cost analysis fetched report sections it never renders: %+v", resp.UsageReport)
	}
}

func TestUsageBreakdownHonorsRangeParam(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-range", Status: "active"}
	db.Create(&org)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 10, OccurredAt: time.Now().Add(-24 * time.Hour).Format(time.RFC3339)})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 90, OccurredAt: time.Now().Add(-10 * 24 * time.Hour).Format(time.RFC3339)})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var resp UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if got := resp.ByUnit["tokens"].Quantity; got != 10 {
		t.Fatalf("7d token total = %d, want 10", got)
	}
}

func TestUsageUnknownMetricIsReportedAndUnreconciled(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-unknown", Status: "active"}
	db.Create(&org)
	now := time.Now().Format(time.RFC3339)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "totally_unknown_meter", Quantity: 999, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 100, OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var resp UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ByUnit["tokens"].Quantity != 100 {
		t.Fatalf("tokens must reflect only the known meter (100), got %d", resp.ByUnit["tokens"].Quantity)
	}
	if resp.Reconciled {
		t.Fatalf("unknown unitless meter must make the report unreconciled")
	}
	if len(resp.Drilldown) != 2 {
		t.Fatalf("exact ledger must retain both rows, got %d", len(resp.Drilldown))
	}
	found := false
	for _, row := range resp.Drilldown {
		if row.Bucket == "totally_unknown_meter" {
			found = true
			if row.Unit != "unknown" {
				t.Fatalf("unknown meter unit = %q, want unknown", row.Unit)
			}
		}
	}
	if !found {
		t.Fatalf("unknown meter must remain visible in exact ledger")
	}
}

func TestUsageWrongUnitIsQuarantinedFromCanonicalTotals(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-wrong-unit", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, UserID: "user-a", ModelPackageID: "model-a", MetricType: "tokens_in", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, UserID: "user-a", ModelPackageID: "model-a", MetricType: "tokens_in", Unit: "bytes", Quantity: 99, CostMicros: 100, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d", "", org.ID, "admin")
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.InputTokens != 1 || report.TotalTokens != 1 || report.ByUnit["tokens"].Quantity != 1 || report.ByUser["user-a"].Quantity != 1 || report.ByModel["model-a"].Quantity != 1 {
		t.Fatalf("wrong-unit row contaminated canonical totals: %+v", report)
	}
	if report.Reconciled || report.DisplayTotal.State != MeterStateError {
		t.Fatalf("wrong-unit row must remain visible as a reconciliation error: %+v", report.DisplayTotal)
	}
}

func TestUsageNormalizedAliasesPreserveWorstPricingState(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-unit-alias-state", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "token", Quantity: 1, Currency: "KRW", PricingState: models.UsagePricingUnpriced, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 2, CostMicros: 20, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d", "", org.ID, "admin")
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	var input UsageMeter
	for _, meter := range report.Meters {
		if meter.MetricType == "tokens_in" && meter.Unit == "tokens" {
			input = meter
		}
	}
	if input.Quantity != 3 || input.CostState != MeterStateUnavailable || report.DisplayTotal.State != MeterStateUnavailable || report.Reconciled {
		t.Fatalf("normalized aliases erased unavailable pricing: meter=%+v display=%+v reconciled=%v", input, report.DisplayTotal, report.Reconciled)
	}
}

func TestUsageDimensionLimitRetainsOtherAndUnattributedTotals(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-dimension-limit", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	rows := make([]models.UsageRecord, 0, 256)
	for i := 0; i < 251; i++ {
		rows = append(rows, models.UsageRecord{OrganizationID: org.ID, UserID: fmt.Sprintf("user-%03d", i), MetricType: "tokens_in", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: now})
	}
	for i := 0; i < 5; i++ {
		rows = append(rows, models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: now})
	}
	if err := db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d&limit=1", "", org.ID, "admin")
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	meta := report.DimensionMeta["user"]
	if meta.Returned != 250 || !meta.HasOther || !meta.HasUnattributed {
		t.Fatalf("dimension truncation metadata missing: %+v", meta)
	}
	var summed int64
	for _, usage := range report.ByUser {
		summed += usage.Quantity
	}
	if summed != report.TotalTokens || report.ByUser["__other__"].Quantity != 1 || report.ByUser["__unattributed__"].Quantity != 5 {
		t.Fatalf("dimension buckets do not reconcile: sum=%d total=%d other=%+v unattributed=%+v", summed, report.TotalTokens, report.ByUser["__other__"], report.ByUser["__unattributed__"])
	}
}

func TestUsageBreakdownReturnsExactLedgerRows(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ledger", Status: "active"}
	db.Create(&org)
	first := models.UsageRecord{Base: models.Base{ID: "usage-first"}, OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 40, CostMicros: 400, Currency: "KRW", OccurredAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339), SessionID: "session-a"}
	second := models.UsageRecord{Base: models.Base{ID: "usage-second"}, OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 60, CostMicros: 600, Currency: "KRW", OccurredAt: time.Now().Add(-time.Hour).Format(time.RFC3339), SessionID: "session-b"}
	db.Create(&first)
	db.Create(&second)

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var resp UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Drilldown) != 2 {
		t.Fatalf("drilldown rows = %d, want 2 exact records", len(resp.Drilldown))
	}
	if resp.Drilldown[0].RefID == "" || resp.Drilldown[0].OccurredAt == "" {
		t.Fatalf("ledger row must carry source id and occurrence time: %+v", resp.Drilldown[0])
	}
}

func TestProjectUsageDoesNotRelabelStorageAsTokens(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-project-usage", Status: "active"}
	db.Create(&org)
	project := models.Project{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "p", Slug: "p"}
	db.Create(&project)
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, ProjectID: project.ID, SessionID: "session-project", HarnessID: "h", UserID: "u"}
	db.Create(&session)
	now := time.Now().Format(time.RFC3339)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, ProjectID: project.ID, SessionID: session.SessionID, MetricType: "tokens_in", Unit: "tokens", Quantity: 100, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, ProjectID: project.ID, SessionID: session.SessionID, MetricType: "storage_bytes", Unit: "bytes", Quantity: 5_242_880, OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/projects/"+project.ID+"/usage?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("project usage: %d %s", rec.Code, rec.Body.String())
	}
	var resp UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if got := resp.TotalTokens; got != 100 {
		t.Fatalf("project total_tokens = %d, want token meters only (100)", got)
	}
}

func TestUsageBreakdownDistinguishesMeterStates(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-meter-state", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC()
	db.Create(&models.UsageRecord{
		Base:           models.Base{ID: "zero-meter", CreatedAt: now},
		OrganizationID: org.ID,
		MetricType:     "tokens_in",
		Unit:           "tokens",
		Quantity:       0,
		Currency:       "KRW",
		PricingState:   models.UsagePricingPriced,
		OccurredAt:     now.Format(time.RFC3339),
	})
	db.Create(&models.UsageRecord{
		Base:           models.Base{ID: "delayed-meter", CreatedAt: now},
		OrganizationID: org.ID,
		MetricType:     "tool_call",
		Unit:           "count",
		Quantity:       2,
		OccurredAt:     now.Add(-2 * time.Hour).Format(time.RFC3339),
	})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Meters []struct {
			MetricType string `json:"metric_type"`
			State      string `json:"state"`
			Reason     string `json:"reason"`
		} `json:"meters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, meter := range resp.Meters {
		states[meter.MetricType] = meter.State
	}
	if states["tokens_in"] != "zero" {
		t.Fatalf("tokens_in state = %q, want zero", states["tokens_in"])
	}
	if states["tool_call"] != "delayed" {
		t.Fatalf("tool_call state = %q, want delayed", states["tool_call"])
	}
	if states["tokens_out"] != "unavailable" {
		t.Fatalf("tokens_out state = %q, want unavailable", states["tokens_out"])
	}
}

func TestUsageBreakdownShowsMeterRateCurrencyAndAmount(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-meter-cost", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC()
	db.Create(&models.UsageRecord{
		Base:           models.Base{ID: "meter-cost", CreatedAt: now},
		OrganizationID: org.ID,
		MetricType:     "tokens_out",
		Unit:           "tokens",
		Quantity:       100,
		CostMicros:     250,
		Currency:       "KRW",
		OccurredAt:     now.Format(time.RFC3339),
	})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Meters []struct {
			MetricType   string `json:"metric_type"`
			Unit         string `json:"unit"`
			Quantity     int64  `json:"quantity,string"`
			RateMicros   string `json:"rate_micros_per_unit"`
			AmountMicros int64  `json:"amount_micros,string"`
			Currency     string `json:"currency"`
		} `json:"meters"`
		CostByCurrency map[string]struct {
			AmountMicros int64  `json:"amount_micros,string"`
			Currency     string `json:"currency"`
		} `json:"cost_by_currency"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, meter := range resp.Meters {
		if meter.MetricType == "tokens_out" {
			found = true
			if meter.Unit != "tokens" || meter.Quantity != 100 || meter.RateMicros != "2.5" || meter.AmountMicros != 250 || meter.Currency != "KRW" {
				t.Fatalf("typed meter mismatch: %+v", meter)
			}
		}
	}
	if !found {
		t.Fatalf("tokens_out meter missing")
	}
	if got := resp.CostByCurrency["KRW"].AmountMicros; got != 250 {
		t.Fatalf("KRW source amount = %d, want 250", got)
	}
}

func TestUsageBreakdownPreservesCustomMeterWithStoredUnit(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-custom-meter", Status: "active"}
	db.Create(&org)
	now := time.Now().Format(time.RFC3339)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 12_000, Currency: "KRW", OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "refund", Unit: "count", Quantity: 0, CostMicros: -2_000, Currency: "KRW", OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Reconciled bool `json:"reconciled"`
		Meters     []struct {
			MetricType   string `json:"metric_type"`
			Unit         string `json:"unit"`
			AmountMicros int64  `json:"amount_micros,string"`
		} `json:"meters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Reconciled {
		t.Fatalf("custom meters with explicit units must remain reconcilable")
	}
	amounts := map[string]int64{}
	for _, meter := range resp.Meters {
		amounts[meter.MetricType] = meter.AmountMicros
	}
	if amounts["flat_fee"] != 12_000 || amounts["refund"] != -2_000 {
		t.Fatalf("adjustment amounts not preserved: %+v", amounts)
	}
}

func TestUsageBreakdownUsesTenantDisplayCurrencyWithAuditableRate(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-display-currency", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.fx_rates", Value: `{"KRW->USD":{"rate":"0.00072","as_of":"2026-08-18","source":"organization-admin","version":"2026-08-18-v1"}}`})
	now := time.Now().UTC()
	db.Create(&models.UsageRecord{Base: models.Base{ID: "fx-row", CreatedAt: now}, OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 1_000_000, Currency: "KRW", OccurredAt: now.Format(time.RFC3339)})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		DisplayCurrency string `json:"display_currency"`
		DisplayTotal    struct {
			AmountMicros int64  `json:"amount_micros,string"`
			Currency     string `json:"currency"`
			RateSource   string `json:"rate_source"`
			RateAsOf     string `json:"rate_as_of"`
		} `json:"display_total"`
		Conversions []UsageConversion `json:"conversions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.DisplayCurrency != "USD" || resp.DisplayTotal.Currency != "USD" || resp.DisplayTotal.AmountMicros != 720 {
		t.Fatalf("display conversion mismatch: %+v", resp)
	}
	if resp.DisplayTotal.RateSource != "organization-admin" || resp.DisplayTotal.RateAsOf != "2026-08-18" {
		t.Fatalf("FX provenance missing: %+v", resp.DisplayTotal)
	}
	if len(resp.Conversions) != 1 || resp.Conversions[0].SourceCurrency != "KRW" || resp.Conversions[0].TargetCurrency != "USD" || resp.Conversions[0].ConvertedAmountMicros != 720 {
		t.Fatalf("auditable conversion line missing: %+v", resp.Conversions)
	}
}

func TestUsageBreakdownReconcilesMultipleSourceCurrenciesDeterministically(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-multi-currency", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.fx_rates", Value: `{"KRW->USD":{"rate":"0.00072","as_of":"2026-08-18","source":"organization-admin","version":"2026-08-18-v1"},"EUR->USD":{"rate":"1.1","as_of":"2026-08-18","source":"organization-admin","version":"2026-08-18-v1"}}`})
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 1_000_000, Currency: "KRW", OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "reservation", Unit: "count", Quantity: 1, CostMicros: 2_000_000, Currency: "EUR", OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Reconciled || report.DisplayTotal.AmountMicros != 2_200_720 {
		t.Fatalf("multi-currency total was not reconciled: %+v", report.DisplayTotal)
	}
	if len(report.Conversions) != 2 || report.Conversions[0].SourceCurrency != "EUR" || report.Conversions[1].SourceCurrency != "KRW" {
		t.Fatalf("conversion lines must be complete and deterministic: %+v", report.Conversions)
	}
}

func TestUsageReportIsCanonicalAcrossOrganizationUserSessionAndProject(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-canonical-usage", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "usage@example.com", Name: "Usage User", Status: "active"}
	db.Create(&user)
	project := models.Project{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "canonical", Slug: "canonical"}
	db.Create(&project)
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, ProjectID: project.ID, SessionID: "session-canonical", HarnessID: "h", UserID: user.ID}
	db.Create(&session)
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, ProjectID: project.ID, UserID: user.ID, SessionID: session.SessionID, ModelPackageID: "model-a", MetricType: "tokens_in", Unit: "tokens", Quantity: 40, CostMicros: 400, Currency: "KRW", OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, ProjectID: project.ID, UserID: user.ID, SessionID: session.SessionID, ModelPackageID: "model-a", MetricType: "tokens_out", Unit: "tokens", Quantity: 60, CostMicros: 600, Currency: "KRW", OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, ProjectID: project.ID, UserID: user.ID, SessionID: session.SessionID, MetricType: "storage_bytes", Unit: "bytes", Quantity: 5_242_880, CostMicros: 2_000, Currency: "KRW", OccurredAt: now})

	paths := []string{
		"/api/analytics/usage-extended?range=7d",
		"/api/users/" + user.ID + "/usage?range=7d",
		"/api/sessions/" + session.ID + "/usage?range=7d",
		"/api/projects/" + project.ID + "/usage?range=7d",
	}
	for _, path := range paths {
		rec := doJSONAsRole(t, srv, "GET", path, "", org.ID, "admin")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		var report UsageTotal
		if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if report.Range != "7d" || report.TotalTokens != 100 || report.DisplayTotal.AmountMicros != 3_000 || len(report.Drilldown) != 3 {
			t.Fatalf("%s returned a noncanonical report: range=%q tokens=%d amount=%d ledger=%d", path, report.Range, report.TotalTokens, report.DisplayTotal.AmountMicros, len(report.Drilldown))
		}
		if report.ByUnit[UnitBytes].Quantity != 5_242_880 || report.ByUnit[UnitTokens].Quantity != 100 {
			t.Fatalf("%s contaminated units: %+v", path, report.ByUnit)
		}
		if strings.Contains(path, "usage-extended") {
			if len(report.ByUser) == 0 || len(report.BySession) != 0 || len(report.ByModel) == 0 {
				t.Fatalf("analytics projection mismatch for %s: users=%d sessions=%d models=%d", path, len(report.ByUser), len(report.BySession), len(report.ByModel))
			}
		} else if len(report.ByUser) != 0 || len(report.BySession) != 0 || len(report.ByModel) != 0 || len(report.ModelTotals) != 0 {
			t.Fatalf("scoped usage fetched dimensions it never renders for %s", path)
		}
	}
}

func TestUsageReportDeterministicFixtureCoversMetersAndAdjustments(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-meter-fixture", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	fixture := []models.UsageRecord{
		{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 100, CostMicros: 1_000, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "tokens_out", Unit: "tokens", Quantity: 50, CostMicros: 1_000, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "cache_read", Unit: "tokens", Quantity: 25, CostMicros: 100, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "cache_write", Unit: "tokens", Quantity: 30, CostMicros: 200, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "media_tokens", Unit: "tokens", Quantity: 10, CostMicros: 300, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "tool_call", Unit: "count", Quantity: 2, CostMicros: 400, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "reservation", Unit: "count", Quantity: 1, CostMicros: 500, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 5_000, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "gpu_seconds", Unit: "seconds", Quantity: 12, CostMicros: 600, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "storage_bytes", Unit: "bytes", Quantity: 1024, CostMicros: 700, Currency: "KRW", OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "refund", Unit: "count", Quantity: 0, CostMicros: -800, Currency: "KRW", OccurredAt: now},
	}
	for i := range fixture {
		db.Create(&fixture[i])
	}

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("usage: %d %s", rec.Code, rec.Body.String())
	}
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Drilldown) != len(fixture) {
		t.Fatalf("ledger rows = %d, want %d", len(report.Drilldown), len(fixture))
	}
	if len(report.Meters) < len(fixture) || report.TotalTokens != 150 {
		t.Fatalf("meter coverage or token total wrong: meters=%d tokens=%d", len(report.Meters), report.TotalTokens)
	}
	if report.DisplayTotal.AmountMicros != 9_000 {
		t.Fatalf("adjusted total = %d, want 9000", report.DisplayTotal.AmountMicros)
	}
	var refund UsageMeter
	for _, meter := range report.Meters {
		if meter.MetricType == "refund" {
			refund = meter
		}
	}
	if refund.AmountMicros != -800 || !report.Reconciled {
		t.Fatalf("refund/reconciliation not preserved: refund=%+v reconciled=%v", refund, report.Reconciled)
	}
}

func TestUsageReportMarksLegacyTimestampFallbackUnreconciled(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-legacy-time", Status: "active"}
	db.Create(&org)
	db.Create(&models.UsageRecord{Base: models.Base{ID: "legacy-time", CreatedAt: time.Now().UTC().Add(-time.Hour)}, OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 8})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("usage: %d %s", rec.Code, rec.Body.String())
	}
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Reconciled || report.TotalTokens != 8 || len(report.Drilldown) != 1 || report.Drilldown[0].OccurredAt == "" {
		t.Fatalf("legacy timestamp fallback must remain visible and unreconciled: %+v", report)
	}
}

func TestUsageReportMissingFXRateIsUnavailableNotZero(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-missing-fx", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 10_000, Currency: "KRW", OccurredAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("usage: %d %s", rec.Code, rec.Body.String())
	}
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Reconciled || report.DisplayTotal.State != MeterStateUnavailable || report.DisplayTotal.Reason == "" {
		t.Fatalf("missing FX must be explicit, not a trustworthy zero: %+v", report.DisplayTotal)
	}
}

func TestUsageScopedEndpointsDoNotExposeAnotherOrganization(t *testing.T) {
	srv, db := securityTestServer(t)
	orgA := models.Organization{Name: "a", Slug: "usage-org-a", Status: "active"}
	orgB := models.Organization{Name: "b", Slug: "usage-org-b", Status: "active"}
	db.Create(&orgA)
	db.Create(&orgB)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: orgB.ID}, Email: "other@example.com", Status: "active"}
	db.Create(&user)
	project := models.Project{AuditBase: models.AuditBase{OrganizationID: orgB.ID}, Name: "other", Slug: "other"}
	db.Create(&project)
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: orgB.ID, ProjectID: project.ID}, SessionID: "other-session", HarnessID: "h", UserID: user.ID}
	db.Create(&session)

	for _, path := range []string{"/api/users/" + user.ID + "/usage", "/api/sessions/" + session.ID + "/usage", "/api/projects/" + project.ID + "/usage"} {
		rec := doJSONAsRole(t, srv, "GET", path, "", orgA.ID, "admin")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d for another organization", path, rec.Code)
		}
	}
}

func TestUsageLedgerCursorPagesWithoutOverlap(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ledger-pages", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Add(-time.Minute)
	for i := 0; i < 121; i++ {
		record := models.UsageRecord{
			Base: models.Base{ID: fmt.Sprintf("usage-page-%03d", i)}, OrganizationID: org.ID,
			MetricType: "tokens_in", Unit: "tokens", Quantity: 1, CostMicros: 1, Currency: "KRW",
			PricingState: models.UsagePricingPriced, OccurredAt: now.Add(-time.Duration(i) * time.Second).Format(time.RFC3339),
		}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}

	fetch := func(cursor string) UsageTotal {
		path := "/api/analytics/usage-extended?range=7d&limit=50"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		rec := doJSONAsRole(t, srv, "GET", path, "", org.ID, "admin")
		if rec.Code != http.StatusOK {
			t.Fatalf("page: %d %s", rec.Code, rec.Body.String())
		}
		var report UsageTotal
		if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		return report
	}

	first := fetch("")
	second := fetch(first.LedgerNextCursor)
	third := fetch(second.LedgerNextCursor)
	if first.RecordCount != 121 || len(first.Drilldown) != 50 || len(second.Drilldown) != 50 || len(third.Drilldown) != 21 {
		t.Fatalf("page sizes/count mismatch: total=%d pages=%d/%d/%d", first.RecordCount, len(first.Drilldown), len(second.Drilldown), len(third.Drilldown))
	}
	seen := map[string]bool{}
	for pageIndex, report := range []UsageTotal{first, second, third} {
		if pageIndex == 0 && report.TotalTokens != 121 {
			t.Fatalf("first-page summary = %d, want 121", report.TotalTokens)
		}
		if pageIndex > 0 && report.TotalTokens != 0 {
			t.Fatalf("continuation unexpectedly recomputed summary: %d", report.TotalTokens)
		}
		for _, row := range report.Drilldown {
			if seen[row.ID] {
				t.Fatalf("duplicate ledger row across pages: %s", row.ID)
			}
			seen[row.ID] = true
		}
	}
	if len(seen) != 121 || third.LedgerHasMore || third.LedgerNextCursor != "" {
		t.Fatalf("cursor traversal incomplete: rows=%d last_has_more=%v", len(seen), third.LedgerHasMore)
	}
}

func TestUsageCSVStreamsCompleteLedger(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ledger-csv", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Add(-time.Minute)
	for i := 0; i < 61; i++ {
		record := models.UsageRecord{OrganizationID: org.ID, MetricType: "tool_call", Unit: "count", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: now.Add(-time.Duration(i) * time.Second).Format(time.RFC3339)}
		db.Create(&record)
	}
	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d&format=csv", "", org.ID, "auditor")
	if rec.Code != http.StatusOK {
		t.Fatalf("csv: %d %s", rec.Code, rec.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 62 {
		t.Fatalf("CSV rows including header = %d, want 62", len(lines))
	}
	if !strings.Contains(lines[0], "pricing_state") {
		t.Fatalf("CSV must disclose cost availability state: %s", lines[0])
	}
}

func TestUsageCSVDisclosesQuarantinedRows(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ledger-csv-quarantine", Status: "active"}
	db.Create(&org)
	record := models.UsageRecord{
		OrganizationID: org.ID, MetricType: "tokens_in", Unit: "bytes", Quantity: 7,
		PricingState: models.UsagePricingUnpriced, OccurredAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d&format=csv", "", org.ID, "auditor")
	if rec.Code != http.StatusOK {
		t.Fatalf("csv: %d %s", rec.Code, rec.Body.String())
	}
	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("CSV rows = %d, want header + quarantined record", len(rows))
	}
	header := map[string]int{}
	for index, column := range rows[0] {
		header[column] = index
	}
	if rows[1][header["meter_state"]] != "error" || rows[1][header["reason_code"]] != "invalid_meter_unit" || rows[1][header["included_in_totals"]] != "false" {
		t.Fatalf("quarantine metadata missing from CSV: header=%v row=%v", rows[0], rows[1])
	}
}

func TestUsageCSVNeutralizesSpreadsheetFormulaCells(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ledger-csv-formula", Status: "active"}
	db.Create(&org)
	record := models.UsageRecord{
		Base:                models.Base{ID: "=RECORD"},
		OrganizationID:      org.ID,
		UserID:              "+USER",
		HarnessID:           "-HARNESS",
		SessionID:           "@SESSION",
		ProjectID:           "=PROJECT",
		ModelPackageID:      `=HYPERLINK("https://example.test")`,
		EndpointID:          "+ENDPOINT",
		MetricType:          "-METRIC",
		Unit:                "@UNIT",
		Quantity:            1,
		PricingState:        "=STATE",
		AppliedPriceVersion: "+VERSION",
		AppliedPriceSource:  "-SOURCE",
		OccurredAt:          time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	currencyRecord := models.UsageRecord{
		Base: models.Base{ID: "currency-record"}, OrganizationID: org.ID,
		MetricType: "tool_call", Unit: "count", Quantity: 1,
		PricingState: models.UsagePricingPriced, Currency: "@CURRENCY",
		OccurredAt: time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339Nano),
	}
	if err := db.Create(&currencyRecord).Error; err != nil {
		t.Fatal(err)
	}
	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d&format=csv", "", org.ID, "auditor")
	if rec.Code != http.StatusOK {
		t.Fatalf("csv: %d %s", rec.Code, rec.Body.String())
	}
	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("CSV rows = %d, want header + 2 records: %s", len(rows), rec.Body.String())
	}
	header := map[string]int{}
	for index, column := range rows[0] {
		header[column] = index
	}
	rowByID := map[string][]string{rows[1][header["record_id"]]: rows[1], rows[2][header["record_id"]]: rows[2]}
	formulaRow := rowByID["'=RECORD"]
	if formulaRow == nil {
		t.Fatalf("formula record missing: %v", rowByID)
	}
	for column, want := range map[string]string{
		"record_id": "'=RECORD", "metric_type": "'-METRIC", "unit": "'@unit", "pricing_state": "'=STATE",
		"user_id": "'+USER", "harness_id": "'-HARNESS", "session_id": "'@SESSION", "project_id": "'=PROJECT",
		"model_package_id": `'=HYPERLINK("https://example.test")`, "endpoint_id": "'+ENDPOINT",
		"applied_price_version": "'+VERSION", "applied_price_source": "'-SOURCE",
	} {
		if got := formulaRow[header[column]]; got != want {
			t.Errorf("%s = %q, want %q", column, got, want)
		}
	}
	currencyRow := rowByID["currency-record"]
	if currencyRow == nil || currencyRow[header["currency"]] != "'@CURRENCY" {
		t.Fatalf("currency formula was not neutralized: %v", currencyRow)
	}
}

func TestUsageCSVTicketIsScopedAndSigned(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ledger-csv-ticket", Status: "active"}
	db.Create(&org)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tool_call", Unit: "count", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)})

	denied := doJSONAsRole(t, srv, "POST", "/api/analytics/usage-export-ticket?range=7d", "", org.ID, "viewer")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer received export ticket: %d", denied.Code)
	}
	issued := doJSONAsRole(t, srv, "POST", "/api/analytics/usage-export-ticket?range=7d", "", org.ID, "auditor")
	if issued.Code != http.StatusCreated {
		t.Fatalf("ticket: %d %s", issued.Code, issued.Body.String())
	}
	var payload struct {
		DownloadURL string `json:"download_url"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	download := doJSONAsRole(t, srv, "GET", payload.DownloadURL, "", "", "")
	if download.Code != http.StatusOK || !strings.Contains(download.Body.String(), "metric_type") {
		t.Fatalf("signed download failed: %d %s", download.Code, download.Body.String())
	}
	tamperedURL := payload.DownloadURL + "x"
	tampered := doJSONAsRole(t, srv, "GET", tamperedURL, "", "", "")
	if tampered.Code != http.StatusUnauthorized {
		t.Fatalf("tampered download ticket returned %d", tampered.Code)
	}
}

func TestUsageFinancialRoutesRejectViewer(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage-rbac", Status: "active"}
	db.Create(&org)
	for _, path := range []string{"/api/analytics/usage", "/api/analytics/usage-breakdown", "/api/analytics/usage-extended", "/api/analytics/cost"} {
		rec := doJSONAsRole(t, srv, "GET", path, "", org.ID, "viewer")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer received %d from %s", rec.Code, path)
		}
	}
}

func TestUsageWindowIsHalfOpenAndBounded(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage-window", Status: "active"}
	db.Create(&org)
	since := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	for _, at := range []time.Time{since, until} {
		record := models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: at.Format(time.RFC3339)}
		db.Create(&record)
	}
	report, err := srv.buildUsageReport(org.ID, usageFilter{}, "1d", since.Format(time.RFC3339), until.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if report.RecordCount != 1 || report.TotalTokens != 1 {
		t.Fatalf("half-open window included wrong records: count=%d tokens=%d", report.RecordCount, report.TotalTokens)
	}
	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?days=5000", "", org.ID, "admin")
	var bounded UsageTotal
	_ = json.Unmarshal(rec.Body.Bytes(), &bounded)
	if bounded.Range != "30d" {
		t.Fatalf("unbounded days must fall back to 30d, got %q", bounded.Range)
	}
}

func TestUsageFXRequiresTargetAndVersionAndPropagatesFailure(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-fx-version", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.fx_rates", Value: `{"KRW->USD":{"rate":"0.00072","as_of":"2026-08-18","source":"admin"}}`})
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, ModelPackageID: "model-a", MetricType: "tokens_in", Unit: "tokens", Quantity: 10, CostMicros: 1000, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: now})
	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d", "", org.ID, "admin")
	var report UsageTotal
	_ = json.Unmarshal(rec.Body.Bytes(), &report)
	if report.Reconciled || report.DisplayTotal.State != MeterStateError || report.TotalCostMicros != nil || report.ByModel["model-a"].Reconciled {
		t.Fatalf("unversioned FX became authoritative: display=%+v total=%v model=%+v", report.DisplayTotal, report.TotalCostMicros, report.ByModel["model-a"])
	}
}

func TestUsageQueryNeverComparesTimestampToEmptyString(t *testing.T) {
	srv, _ := securityTestServer(t)
	now := time.Now().UTC()
	query := srv.usageRecordsQuery("org", usageFilter{}, now.Add(-time.Hour), now).Session(&gorm.Session{DryRun: true})
	var rows []models.UsageRecord
	statement := query.Find(&rows).Statement.SQL.String()
	if strings.Contains(statement, "occurred_at") || strings.Contains(statement, "= ''") {
		t.Fatalf("production usage predicate regressed to unsafe legacy timestamp SQL: %s", statement)
	}
}

func TestUsageQueryPostgresContractUsesTypedTimestamps(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=pccp dbname=pccp sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{db: db}
	now := time.Now().UTC()
	query := srv.usageRecordsQuery("org", usageFilter{SnapshotAt: now}, now.Add(-time.Hour), now).Session(&gorm.Session{DryRun: true})
	var rows []models.UsageRecord
	statement := query.Find(&rows).Statement.SQL.String()
	if strings.Contains(statement, "= ''") || strings.Contains(statement, "julianday") || !strings.Contains(statement, "metered_at") || !strings.Contains(statement, "created_at") {
		t.Fatalf("PostgreSQL usage predicate is not timestamp-safe: %s", statement)
	}
}

func TestUsageLedgerRejectsMalformedCursor(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-bad-cursor", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?cursor=not-a-cursor", "", org.ID, "admin")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed cursor returned %d: %s", rec.Code, rec.Body.String())
	}
	now := time.Now().UTC()
	valid := srv.encodeUsageCursor(usageCursor{OccurredAt: now.Add(-time.Minute), ID: "row", WindowStart: now.Add(-24 * time.Hour), WindowEnd: now, SnapshotAt: now, OrganizationID: org.ID})
	first := valid[0]
	replacement := byte('A')
	if first == replacement {
		replacement = 'B'
	}
	tampered := string(replacement) + valid[1:]
	rec = doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?cursor="+tampered, "", org.ID, "admin")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tampered cursor returned %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUsageLedgerCursorFreezesIngestionSnapshot(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ledger-snapshot", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Add(-time.Minute)
	for id, at := range map[string]time.Time{"newest": now, "oldest": now.Add(-2 * time.Minute)} {
		record := models.UsageRecord{Base: models.Base{ID: id}, OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: at.Format(time.RFC3339Nano)}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	firstRec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d&limit=1", "", org.ID, "admin")
	var first UsageTotal
	if err := json.Unmarshal(firstRec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Drilldown) != 1 || first.Drilldown[0].ID != "newest" || first.LedgerNextCursor == "" {
		t.Fatalf("unexpected first snapshot page: %+v", first)
	}

	cursor, err := srv.decodeUsageCursor(first.LedgerNextCursor)
	if err != nil {
		t.Fatal(err)
	}
	late := models.UsageRecord{Base: models.Base{ID: "late", CreatedAt: cursor.SnapshotAt.Add(50 * time.Millisecond)}, OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: now.Add(-time.Minute).Format(time.RFC3339Nano)}
	if err := db.Create(&late).Error; err != nil {
		t.Fatal(err)
	}
	secondRec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d&limit=10&cursor="+first.LedgerNextCursor, "", org.ID, "admin")
	var second UsageTotal
	if err := json.Unmarshal(secondRec.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Drilldown) != 1 || second.Drilldown[0].ID != "oldest" {
		t.Fatalf("late arrival leaked into frozen cursor snapshot: %+v", second.Drilldown)
	}
	time.Sleep(60 * time.Millisecond)
	freshRec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d&limit=10", "", org.ID, "admin")
	if !strings.Contains(freshRec.Body.String(), `"id":"late"`) {
		t.Fatalf("late arrival missing from a fresh snapshot: %s", freshRec.Body.String())
	}
}

func TestUsageJSONPreservesIntegersAboveJavaScriptSafeRange(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-large-integer", Status: "active"}
	db.Create(&org)
	const quantity int64 = 9_007_199_254_740_993
	record := models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: quantity, CostMicros: quantity, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)}
	db.Create(&record)
	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("usage: %d %s", rec.Code, rec.Body.String())
	}
	for _, exact := range []string{`"quantity":"9007199254740993"`, `"amount_micros":"9007199254740993"`, `"input_tokens":"9007199254740993"`} {
		if !strings.Contains(rec.Body.String(), exact) {
			t.Fatalf("exact integer %s missing from response: %s", exact, rec.Body.String())
		}
	}
}

func TestUsageLedgerResolvesOnlyAuthorizedExistingRelations(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ledger-relations", Status: "active"}
	foreignOrg := models.Organization{Name: "foreign", Slug: "o-ledger-relations-foreign", Status: "active"}
	db.Create(&org)
	db.Create(&foreignOrg)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "owner@example.com", Name: "Owner", NameKo: "담당자", Status: "active"}
	foreignUser := models.User{AuditBase: models.AuditBase{OrganizationID: foreignOrg.ID}, Email: "foreign@example.com", Name: "Foreign", Status: "active"}
	db.Create(&user)
	db.Create(&foreignUser)
	project := models.Project{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "검증 프로젝트", Slug: "usage-relations-project"}
	foreignProject := models.Project{AuditBase: models.AuditBase{OrganizationID: foreignOrg.ID}, Name: "Foreign Project", Slug: "usage-relations-foreign-project"}
	db.Create(&project)
	db.Create(&foreignProject)
	harness := models.Harness{OrganizationID: org.ID, HarnessID: "harness-external", Name: "업무 하네스", Status: "active", PublicKey: "test"}
	db.Create(&harness)
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, SessionID: "session-external", Title: "검증 세션", UserID: user.ID, HarnessID: harness.HarnessID, ProjectID: project.ID, Status: "active"}
	db.Create(&session)
	model := models.ModelPackage{PackageID: "model-external", Name: "검증 모델", State: "published"}
	db.Create(&model)
	endpoint := models.InferenceEndpoint{OrganizationID: org.ID, EndpointID: "endpoint-external", Name: "검증 엔드포인트", ModelPackageID: model.PackageID, Status: "active"}
	db.Create(&endpoint)
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, UserID: user.ID, HarnessID: harness.HarnessID, SessionID: session.SessionID, ProjectID: project.ID, ModelPackageID: model.PackageID, EndpointID: endpoint.EndpointID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, UserID: foreignUser.ID, ProjectID: foreignProject.ID, MetricType: "tokens_out", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-extended?range=7d", "", org.ID, "admin")
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Drilldown) != 2 {
		t.Fatalf("ledger rows = %d, want 2", len(report.Drilldown))
	}
	var valid, foreign UsageLedgerRow
	for _, row := range report.Drilldown {
		if row.UserID == user.ID {
			valid = row
		} else if row.UserID == foreignUser.ID {
			foreign = row
		}
	}
	if !valid.UserResolved || valid.UserLabel != "담당자" || !valid.HarnessResolved || !valid.SessionResolved || !valid.ModelResolved || !valid.EndpointResolved || !valid.ProjectResolved || valid.ProjectLabel != "검증 프로젝트" {
		t.Fatalf("existing relations did not resolve: %+v", valid)
	}
	if foreign.UserResolved || foreign.UserLabel != "" || foreign.ProjectResolved || foreign.ProjectLabel != "" {
		t.Fatalf("foreign relation was exposed as navigable: %+v", foreign)
	}
}

func TestUsageRelationLookupHonorsRequestCancellation(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-relation-cancel", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "cancel@example.com", Name: "Cancel", Status: "active"}
	db.Create(&user)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := srv.resolveUsageRelations(ctx, org.ID, []UsageLedgerRow{{UserID: user.ID}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled relation lookup error = %v, want context.Canceled", err)
	}
}

func TestUsageFXHistoryDoesNotRewritePriorWindows(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-fx-history", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	fx := models.OrgSetting{OrganizationID: org.ID, Key: "billing.fx_rates", Value: `{"KRW->USD":{"rate":"0.001","as_of":"2026-08-01","source":"treasury","version":"v1"}}`}
	db.Create(&fx)
	var snapshots int64
	if err := db.Model(&models.BillingFXRate{}).Where("organization_id = ?", org.ID).Count(&snapshots).Error; err != nil {
		t.Fatal(err)
	}
	if snapshots != 1 {
		t.Fatalf("FX setting write persisted %d immutable versions, want 1 before any report read", snapshots)
	}
	record := models.UsageRecord{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 1_000_000, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: "2026-08-05T00:00:00Z"}
	db.Create(&record)
	start, end := "2026-08-01T00:00:00Z", "2026-08-10T00:00:00Z"
	first, err := srv.buildUsageReport(org.ID, usageFilter{}, "fixed", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if first.DisplayTotal.AmountMicros != 1_000 || len(first.Conversions) != 1 || first.Conversions[0].RateVersion != "v1" {
		t.Fatalf("unexpected v1 conversion: %+v", first)
	}
	if err := db.Model(&models.BillingFXRate{}).Where("organization_id = ?", org.ID).Count(&snapshots).Error; err != nil {
		t.Fatal(err)
	}
	if snapshots != 1 {
		t.Fatalf("report read mutated FX history: versions=%d, want 1", snapshots)
	}
	if err := db.Model(&fx).Update("value", `{"KRW->USD":{"rate":"0.002","as_of":"2026-08-18","source":"treasury","version":"v2"}}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.BillingFXRate{}).Where("organization_id = ?", org.ID).Count(&snapshots).Error; err != nil {
		t.Fatal(err)
	}
	if snapshots != 2 {
		t.Fatalf("FX setting update persisted %d immutable versions, want 2", snapshots)
	}
	prior, err := srv.buildUsageReport(org.ID, usageFilter{}, "fixed", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if prior.DisplayTotal.AmountMicros != 1_000 || len(prior.Conversions) != 1 || prior.Conversions[0].RateVersion != "v1" {
		t.Fatalf("new FX setting rewrote prior window: %+v", prior)
	}
}

func TestMonthlyUsageReportUsesCanonicalWindowAndPermission(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-monthly-canonical", Status: "active"}
	db.Create(&org)
	for _, record := range []models.UsageRecord{
		{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 7, PricingState: models.UsagePricingUnpriced, OccurredAt: "2026-08-10T00:00:00Z"},
		{OrganizationID: org.ID, MetricType: "tool_call", Unit: "count", Quantity: 2, PricingState: models.UsagePricingUnpriced, OccurredAt: "2026-08-11T00:00:00Z"},
		{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 99, PricingState: models.UsagePricingUnpriced, OccurredAt: "2026-07-31T23:59:59Z"},
	} {
		db.Create(&record)
	}
	body := `{"type":"monthly_usage","period":"2026-08","generated_by":"admin"}`
	denied := doJSONAsRole(t, srv, http.MethodPost, "/api/reports/generate", body, org.ID, "viewer")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer monthly usage report = %d, want 403: %s", denied.Code, denied.Body.String())
	}
	allowed := doJSONAsRole(t, srv, http.MethodPost, "/api/reports/generate", body, org.ID, "admin")
	if allowed.Code != http.StatusOK {
		t.Fatalf("admin monthly usage report = %d: %s", allowed.Code, allowed.Body.String())
	}
	var payload struct {
		Period string     `json:"period"`
		Data   UsageTotal `json:"data"`
	}
	if err := json.Unmarshal(allowed.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Period != "2026-08" || payload.Data.WindowStart != "2026-08-01T00:00:00Z" || payload.Data.WindowEnd != "2026-09-01T00:00:00Z" {
		t.Fatalf("monthly report window = period=%q %s..%s", payload.Period, payload.Data.WindowStart, payload.Data.WindowEnd)
	}
	if payload.Data.ByUnit[UnitTokens].Quantity != 7 || payload.Data.ByUnit[UnitCount].Quantity != 2 || payload.Data.RecordCount != 2 {
		t.Fatalf("monthly report did not use canonical unit-safe ledger: %+v", payload.Data)
	}
}

func TestUsageFXAppliesEffectiveRateToEachLedgerInterval(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-fx-interval", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	db.Create(&models.BillingFXRate{OrganizationID: org.ID, SourceCurrency: "KRW", TargetCurrency: "USD", Version: "v1", Rate: "0.001", Source: "treasury", EffectiveAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)})
	db.Create(&models.BillingFXRate{OrganizationID: org.ID, SourceCurrency: "KRW", TargetCurrency: "USD", Version: "v2", Rate: "0.002", Source: "treasury", EffectiveAt: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)})
	for _, at := range []string{"2026-08-02T00:00:00Z", "2026-08-06T00:00:00Z"} {
		db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 1_000_000, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: at})
	}
	report, err := srv.buildUsageReport(org.ID, usageFilter{}, "fixed", "2026-08-01T00:00:00Z", "2026-08-10T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if report.DisplayTotal.AmountMicros != 3_000 || len(report.Conversions) != 2 {
		t.Fatalf("effective-dated conversion = total %d conversions %+v, want 3000 across v1/v2", report.DisplayTotal.AmountMicros, report.Conversions)
	}
}

func TestUsageFXLookupIsSkippedOrGroupedIntoOneQuery(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-fx-query-count", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	for _, rate := range []models.BillingFXRate{
		{OrganizationID: org.ID, SourceCurrency: "KRW", TargetCurrency: "USD", Version: "krw-v1", Rate: "0.001", Source: "treasury", EffectiveAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{OrganizationID: org.ID, SourceCurrency: "KRW", TargetCurrency: "USD", Version: "krw-v2", Rate: "0.002", Source: "treasury", EffectiveAt: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)},
		{OrganizationID: org.ID, SourceCurrency: "EUR", TargetCurrency: "USD", Version: "eur-v1", Rate: "1.1", Source: "treasury", EffectiveAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
	} {
		db.Create(&rate)
	}
	for _, record := range []models.UsageRecord{
		{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 1_000_000, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: "2026-08-02T00:00:00Z"},
		{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 1_000_000, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: "2026-08-06T00:00:00Z"},
		{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 1_000_000, Currency: "EUR", PricingState: models.UsagePricingPriced, OccurredAt: "2026-08-06T00:00:00Z"},
	} {
		db.Create(&record)
	}
	capture := &usageSQLLogger{Interface: logger.Default.LogMode(logger.Info)}
	srv.db = db.Session(&gorm.Session{Logger: capture})
	report, err := srv.buildUsageReport(org.ID, usageFilter{}, "fixed", "2026-08-01T00:00:00Z", "2026-08-10T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	fxQueries := 0
	for _, statement := range capture.statements {
		if strings.Contains(statement, "billing_fx_rates") {
			fxQueries++
		}
	}
	if fxQueries != 1 {
		t.Fatalf("FX conversion database queries = %d, want one grouped effective-rate query", fxQueries)
	}
	if report.DisplayTotal.AmountMicros != 1_103_000 || len(report.Conversions) != 3 {
		t.Fatalf("grouped FX conversion changed effective-rate results: %+v", report.Conversions)
	}

	capture.statements = nil
	db.Model(&models.UsageRecord{}).Where("organization_id = ?", org.ID).Update("currency", "USD")
	if _, err := srv.buildUsageReport(org.ID, usageFilter{}, "fixed", "2026-08-01T00:00:00Z", "2026-08-10T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	fxQueries = 0
	for _, statement := range capture.statements {
		if strings.Contains(statement, "billing_fx_rates") {
			fxQueries++
		}
	}
	if fxQueries != 0 {
		t.Fatalf("same-currency report issued %d unnecessary FX history queries", fxQueries)
	}
}

func TestUsageFXRejectsBackdatedVersionAndPastReportIgnoresMalformedCurrentConfig(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-fx-backdated", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	db.Create(&models.BillingFXRate{OrganizationID: org.ID, SourceCurrency: "KRW", TargetCurrency: "USD", Version: "v1", Rate: "0.001", Source: "treasury", EffectiveAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)})
	bad := models.OrgSetting{OrganizationID: org.ID, Key: "billing.fx_rates", Value: `{"KRW->USD":{"rate":"0.002","as_of":"2026-07-01","source":"treasury","version":"v2"}}`}
	if err := db.Create(&bad).Error; err == nil {
		t.Fatal("backdated FX version was accepted")
	}
	var count int64
	db.Model(&models.BillingFXRate{}).Where("organization_id = ?", org.ID).Count(&count)
	if count != 1 {
		t.Fatalf("backdated FX changed immutable history: count=%d", count)
	}
	bad = models.OrgSetting{OrganizationID: org.ID, Key: "billing.fx_rates", Value: `{not-json`}
	if err := db.Create(&bad).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 1_000_000, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: "2026-08-02T00:00:00Z"})
	report, err := srv.buildUsageReport(org.ID, usageFilter{}, "fixed", "2026-08-01T00:00:00Z", "2026-08-10T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if report.DisplayTotal.State == MeterStateError || report.DisplayTotal.AmountMicros != 1_000 {
		t.Fatalf("mutable malformed setting rewrote historical report: %+v", report.DisplayTotal)
	}
}

func TestUsageFXPreservesHighestFailureSeverity(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-fx-severity", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	db.Create(&models.BillingFXRate{OrganizationID: org.ID, SourceCurrency: "EUR", TargetCurrency: "USD", Version: "bad", Rate: "invalid", Source: "treasury", EffectiveAt: time.Now().UTC().AddDate(-1, 0, 0)})
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 10, Currency: "EUR", PricingState: models.UsagePricingPriced, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 10, Currency: "GBP", PricingState: models.UsagePricingPriced, OccurredAt: now})
	report, err := srv.buildUsageReport(org.ID, usageFilter{}, "7d", time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if report.DisplayTotal.State != MeterStateError || report.DisplayTotal.ReasonCode != "fx_rate_invalid" {
		t.Fatalf("FX error was downgraded by a missing rate: %+v", report.DisplayTotal)
	}
}

func TestUsageFXOverflowIsExplicitInsteadOfWrapping(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-fx-overflow", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "billing.display_currency", Value: "USD"})
	db.Create(&models.BillingFXRate{OrganizationID: org.ID, SourceCurrency: "KRW", TargetCurrency: "USD", Version: "huge-v1", Rate: "9223372036854775808", Source: "test", EffectiveAt: time.Now().UTC().AddDate(-1, 0, 0)})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "flat_fee", Unit: "count", Quantity: 1, CostMicros: 1, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)})
	report, err := srv.buildUsageReport(org.ID, usageFilter{}, "7d", time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if report.DisplayTotal.State != MeterStateError || report.DisplayTotal.ReasonCode != "fx_conversion_overflow" || report.TotalCostMicros != nil {
		t.Fatalf("overflowed conversion was not quarantined: %+v", report.DisplayTotal)
	}
}

func TestUsageAggregateOverflowIsExplicitInsteadOfWrapping(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-aggregate-overflow", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Format(time.RFC3339)
	// Raw units remain separate SQL groups but normalize to the same canonical
	// token unit in Go. This exercises the cross-group fold rather than the
	// database SUM.
	db.Create(&models.UsageRecord{OrganizationID: org.ID, ModelPackageID: "model-overflow", MetricType: "tokens_in", Unit: "token", Quantity: int64(^uint64(0) >> 1), Currency: "KRW", PricingState: models.UsagePricingUnpriced, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, ModelPackageID: "model-overflow", MetricType: "tokens_in", Unit: "tokens", Quantity: 1, CostMicros: 1, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.InputTokens != 0 || report.InputTokensState != MeterStateError || report.TotalTokensState != MeterStateError {
		t.Fatalf("overflowed token aggregate was not quarantined: input=%d input_state=%s total=%d total_state=%s", report.InputTokens, report.InputTokensState, report.TotalTokens, report.TotalTokensState)
	}
	if got := report.ByUnit[UnitTokens]; got.Quantity != 0 || got.State != MeterStateError || got.ReasonCode != "quantity_total_overflow" {
		t.Fatalf("overflowed unit aggregate was not explicit: %+v", got)
	}
	var meterOverflow bool
	for _, meter := range report.Meters {
		if meter.MetricType == "tokens_in" && meter.Unit == UnitTokens && meter.Currency == "KRW" {
			meterOverflow = meter.Quantity == 0 && meter.State == MeterStateError && meter.ReasonCode == "quantity_total_overflow"
		}
	}
	if !meterOverflow {
		t.Fatalf("overflowed normalized meter aggregate was not quarantined: %+v", report.Meters)
	}
	if got := report.ModelTotals["model-overflow"]; got.InputTokens != 0 || got.PricingState != MeterStateError {
		t.Fatalf("overflowed model aggregate was not quarantined: %+v", got)
	}
	if got := report.ByModel["model-overflow"]; got.Quantity != 0 || got.State != MeterStateError || got.ReasonCode != "quantity_total_overflow" {
		t.Fatalf("overflowed model dimension was not explicit: %+v", got)
	}
	if report.Reconciled {
		t.Fatalf("overflowed aggregate cannot be reconciled")
	}
	var found bool
	for _, issue := range report.ReconciliationIssues {
		if issue.Code == "quantity_total_overflow" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("aggregate overflow reconciliation issue missing: %+v", report.ReconciliationIssues)
	}
}

func TestUsageCostAggregateOverflowIsExplicitInsteadOfWrapping(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-cost-aggregate-overflow", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Format(time.RFC3339)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "token", Quantity: 1, CostMicros: int64(^uint64(0) >> 1), Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1, CostMicros: 1, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/usage-breakdown?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("breakdown: %d %s", rec.Code, rec.Body.String())
	}
	var report UsageTotal
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if amount := report.CostByCurrency["KRW"]; amount.AmountMicros != 0 || amount.State != MeterStateError || amount.ReasonCode != "cost_total_overflow" {
		t.Fatalf("overflowed currency aggregate was not quarantined: %+v", amount)
	}
	if report.DisplayTotal.AmountMicros != 0 || report.DisplayTotal.State != MeterStateError || report.DisplayTotal.ReasonCode != "cost_total_overflow" || report.TotalCostMicros != nil {
		t.Fatalf("overflowed display aggregate was not quarantined: %+v", report.DisplayTotal)
	}
	var meterOverflow bool
	for _, meter := range report.Meters {
		if meter.MetricType == "tokens_in" && meter.Unit == UnitTokens && meter.Currency == "KRW" {
			meterOverflow = meter.AmountMicros == 0 && meter.CostState == MeterStateError && meter.CostReasonCode == "cost_total_overflow"
		}
	}
	if !meterOverflow {
		t.Fatalf("overflowed meter cost was not quarantined: %+v", report.Meters)
	}
}

func TestUsageMeterReasonAndCodeStayAtomicAcrossUnitAliases(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-alias-reason", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "token", Quantity: 1, PricingState: models.UsagePricingPending, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: now})
	report, err := srv.buildUsageReport(org.ID, usageFilter{}, "7d", time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	for _, meter := range report.Meters {
		if meter.MetricType == "tokens_in" && meter.Unit == UnitTokens {
			if (meter.CostReason == "pricing is pending") != (meter.CostReasonCode == "pricing_pending") {
				t.Fatalf("pricing reason/code diverged: %+v", meter)
			}
			return
		}
	}
	t.Fatal("tokens_in meter missing")
}

func TestUsageCSVTicketPinsWindowAndSnapshot(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-export-snapshot", Status: "active"}
	db.Create(&org)
	windowStart := time.Now().UTC().Add(-24 * time.Hour)
	windowEnd := time.Now().UTC()
	snapshot := windowEnd.Add(-time.Minute)
	stable := models.UsageRecord{Base: models.Base{ID: "stable", CreatedAt: snapshot.Add(-time.Minute)}, OrganizationID: org.ID, MetricType: "tool_call", Unit: "count", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: windowEnd.Add(-time.Hour).Format(time.RFC3339Nano)}
	db.Create(&stable)
	path := fmt.Sprintf("/api/analytics/usage-export-ticket?range=7d&window_start=%s&window_end=%s&snapshot_at=%s", windowStart.Format(time.RFC3339Nano), windowEnd.Format(time.RFC3339Nano), snapshot.Format(time.RFC3339Nano))
	issued := doJSONAsRole(t, srv, http.MethodPost, path, "", org.ID, "auditor")
	if issued.Code != http.StatusCreated {
		t.Fatalf("ticket = %d: %s", issued.Code, issued.Body.String())
	}
	var payload struct {
		DownloadURL string `json:"download_url"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	late := models.UsageRecord{Base: models.Base{ID: "late", CreatedAt: snapshot.Add(time.Minute)}, OrganizationID: org.ID, MetricType: "tool_call", Unit: "count", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: windowEnd.Add(-time.Hour).Format(time.RFC3339Nano)}
	db.Create(&late)
	download := doJSONAsRole(t, srv, http.MethodGet, payload.DownloadURL, "", "", "")
	if download.Code != http.StatusOK || !strings.Contains(download.Body.String(), "stable") || strings.Contains(download.Body.String(), "late") {
		t.Fatalf("export did not preserve issued snapshot: %d %s", download.Code, download.Body.String())
	}
}

func TestUsageCSVTicketRejectsFutureSnapshot(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-export-future-snapshot", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC()
	path := fmt.Sprintf("/api/analytics/usage-export-ticket?range=7d&window_start=%s&window_end=%s&snapshot_at=%s",
		now.Add(-24*time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano))
	issued := doJSONAsRole(t, srv, http.MethodPost, path, "", org.ID, "auditor")
	if issued.Code != http.StatusBadRequest {
		t.Fatalf("future snapshot ticket = %d %s, want 400", issued.Code, issued.Body.String())
	}
}

func keysOf(m map[string]Usage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
