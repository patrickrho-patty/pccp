package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

func TestUsageLedgerMarksInvalidUnitRowsExcludedFromTotals(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage-quarantine", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "bytes", Quantity: 99, OccurredAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	rec := doJSONAsRole(t, srv, http.MethodGet, "/api/analytics/usage", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("usage response = %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		InputTokens string `json:"input_tokens"`
		Drilldown   []struct {
			MeterState       string `json:"meter_state"`
			ReasonCode       string `json:"reason_code"`
			IncludedInTotals bool   `json:"included_in_totals"`
		} `json:"drilldown"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.InputTokens != "0" || len(payload.Drilldown) != 1 {
		t.Fatalf("invalid row affected canonical totals: %+v", payload)
	}
	row := payload.Drilldown[0]
	if row.MeterState != string(MeterStateError) || row.ReasonCode != "invalid_meter_unit" || row.IncludedInTotals {
		t.Fatalf("invalid row was not visibly quarantined: %+v", row)
	}
}

func TestUsageCurrencySubtotalReportsIncompletePricing(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage-currency-state", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rows := []models.UsageRecord{
		{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1, CostMicros: 100, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: now},
		{OrganizationID: org.ID, MetricType: "tokens_out", Unit: "tokens", Quantity: 1, Currency: "KRW", PricingState: models.UsagePricingPending, OccurredAt: now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	rec := doJSONAsRole(t, srv, http.MethodGet, "/api/analytics/usage?summary_only=1", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("usage response = %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		CostByCurrency map[string]struct {
			State      MeterState `json:"state"`
			ReasonCode string     `json:"reason_code"`
		} `json:"cost_by_currency"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	krw := payload.CostByCurrency["KRW"]
	if krw.State != MeterStateUnavailable || krw.ReasonCode != "pricing_pending" {
		t.Fatalf("partial KRW subtotal looks complete: %+v", krw)
	}
}

func TestProjectUsageUsesImmutableUsageProjectSnapshot(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage-project-snapshot", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	projectA := models.Project{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "A", Slug: "a"}
	projectB := models.Project{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "B", Slug: "b"}
	if err := db.Create(&projectA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&projectB).Error; err != nil {
		t.Fatal(err)
	}
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, SessionID: "sess-project", HarnessID: "h", UserID: "u", ProjectID: projectA.ID, Status: "active"}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.Create(&models.UsageRecord{OrganizationID: org.ID, SessionID: session.SessionID, ProjectID: projectA.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 17, OccurredAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&session).Update("project_id", projectB.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		projectID string
		want      string
	}{{projectA.ID, "17"}, {projectB.ID, "0"}} {
		rec := doJSONAsRole(t, srv, http.MethodGet, "/api/projects/"+tc.projectID+"/usage?summary_only=1", "", org.ID, "admin")
		if rec.Code != http.StatusOK {
			t.Fatalf("project usage = %d %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			InputTokens string `json:"input_tokens"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.InputTokens != tc.want {
			t.Fatalf("project %s input tokens = %s, want %s", tc.projectID, payload.InputTokens, tc.want)
		}
	}
}
