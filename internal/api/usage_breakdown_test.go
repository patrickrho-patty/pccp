package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

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
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Quantity: 1000, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_out", Quantity: 500, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "gpu_seconds", Quantity: 187, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "storage_bytes", Quantity: 5242880, OccurredAt: now})

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
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_out", Quantity: 200, OccurredAt: now, ModelPackageID: "p1"})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/cost?days=30", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("cost: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Models []modelCostRow `json:"models"`
		Total  Usage          `json:"total"`
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
	if resp.Total.Unit != "krw" {
		t.Fatalf("total unit = %q, want krw", resp.Total.Unit)
	}
	if resp.Total.Quantity != 100 {
		t.Fatalf("total quantity = %d, want 100 KRW", resp.Total.Quantity)
	}
	if resp.Total.Reconciled || resp.Models[0].Reconciled {
		t.Fatalf("catalog estimate must not be labeled reconciled when the usage ledger has no matching cost")
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
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Quantity: 100, OccurredAt: now})

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
	db.Create(&models.UsageRecord{OrganizationID: org.ID, SessionID: session.SessionID, MetricType: "tokens_in", Unit: "tokens", Quantity: 100, OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, SessionID: session.SessionID, MetricType: "storage_bytes", Unit: "bytes", Quantity: 5_242_880, OccurredAt: now})

	rec := doJSONAsRole(t, srv, "GET", "/api/projects/"+project.ID+"/usage?range=7d", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("project usage: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if got := int64(resp["total_tokens"].(float64)); got != 100 {
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
			Quantity     int64  `json:"quantity"`
			RateMicros   string `json:"rate_micros_per_unit"`
			AmountMicros int64  `json:"amount_micros"`
			Currency     string `json:"currency"`
		} `json:"meters"`
		CostByCurrency map[string]struct {
			AmountMicros int64  `json:"amount_micros"`
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
			AmountMicros int64  `json:"amount_micros"`
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
			AmountMicros int64  `json:"amount_micros"`
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
	db.Create(&models.UsageRecord{OrganizationID: org.ID, UserID: user.ID, SessionID: session.SessionID, ModelPackageID: "model-a", MetricType: "tokens_in", Unit: "tokens", Quantity: 40, CostMicros: 400, Currency: "KRW", OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, UserID: user.ID, SessionID: session.SessionID, ModelPackageID: "model-a", MetricType: "tokens_out", Unit: "tokens", Quantity: 60, CostMicros: 600, Currency: "KRW", OccurredAt: now})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, UserID: user.ID, SessionID: session.SessionID, MetricType: "storage_bytes", Unit: "bytes", Quantity: 5_242_880, CostMicros: 2_000, Currency: "KRW", OccurredAt: now})

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
	if len(report.ByMetric) < len(fixture) || report.TotalTokens != 150 {
		t.Fatalf("meter coverage or token total wrong: meters=%d tokens=%d", len(report.ByMetric), report.TotalTokens)
	}
	if report.DisplayTotal.AmountMicros != 9_000 {
		t.Fatalf("adjusted total = %d, want 9000", report.DisplayTotal.AmountMicros)
	}
	if report.ByMetric["refund"].AmountMicros != -800 || !report.Reconciled {
		t.Fatalf("refund/reconciliation not preserved: refund=%+v reconciled=%v", report.ByMetric["refund"], report.Reconciled)
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
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: orgB.ID}, ProjectID: project.ID, SessionID: "other-session", HarnessID: "h", UserID: user.ID}
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
	for _, report := range []UsageTotal{first, second, third} {
		if report.TotalTokens != 121 {
			t.Fatalf("summary changed across cursor pages: %d", report.TotalTokens)
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

func keysOf(m map[string]Usage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
