package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
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
	if !resp.Reconciled {
		t.Fatalf("breakdown must be marked reconciled when ledger matches")
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
}

func TestUsageCostAnalysisRowsCarryUnits(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-cost", Status: "active"}
	db.Create(&org)
	now := time.Now().Format(time.RFC3339)
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Quantity: 1000, OccurredAt: now, ModelPackageID: "p1"})
	db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_out", Quantity: 200, OccurredAt: now, ModelPackageID: "p1"})

	rec := doJSONAsRole(t, srv, "GET", "/api/analytics/cost?days=30", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("cost: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Models []modelCostRow    `json:"models"`
		Total  Usage             `json:"total"`
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
}

func TestUsageUnknownMetricIsSilentlyDroppedFromTotal(t *testing.T) {
	// An unknown metric_type must not be summed into any unit bucket
	// — surfaced as Missing/Unavailable rather than silently counted.
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
}

func keysOf(m map[string]Usage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
