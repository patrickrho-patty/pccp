package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
)

func TestEnterpriseViolationResolveRequiresDispositionAndReason(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "ovi", Status: "active"}
	db.Create(&org)
	v := models.EnterpriseFeatureViolation{OrganizationID: org.ID, FeatureKey: "network_egress", Severity: "high", Description: "raw egress"}
	db.Create(&v)

	// Missing disposition
	rec := doJSON(t, srv, "PUT", "/api/enterprise/violations/"+v.ID,
		`{"disposition_reason":"r"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing disposition, got %d", rec.Code)
	}
	// Missing reason
	rec = doJSON(t, srv, "PUT", "/api/enterprise/violations/"+v.ID,
		`{"disposition":"fixed"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing reason, got %d", rec.Code)
	}
	// Bogus disposition
	rec = doJSON(t, srv, "PUT", "/api/enterprise/violations/"+v.ID,
		`{"disposition":"banana","disposition_reason":"r"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bogus disposition, got %d", rec.Code)
	}
}

func TestEnterpriseViolationResolveHappyPaths(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    string
		expires string
	}{
		{"fixed", `{"disposition":"fixed","disposition_reason":"patched","evidence":[{"type":"pr","ref":"https://x"}],"owner_id":"u1"}`, "fixed", ""},
		{"false_positive", `{"disposition":"false_positive","disposition_reason":"misclassified","owner_id":"sec"}`, "false_positive", ""},
		{"duplicate", `{"disposition":"duplicate","disposition_reason":"already #2","owner_id":"sec"}`, "duplicate", ""},
		{"risk_accepted", `{"disposition":"risk_accepted","disposition_reason":"compensating control deployed","expires_at":"2026-12-31T00:00:00Z"}`, "risk_accepted", "2026-12-31T00:00:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, db := toolsSandboxTestServer(t)
			org := models.Organization{Name: "o", Slug: "ovi" + c.name, Status: "active"}
			db.Create(&org)
			v := models.EnterpriseFeatureViolation{OrganizationID: org.ID, FeatureKey: "network_egress", Severity: "high"}
			db.Create(&v)
			rec := doJSON(t, srv, "PUT", "/api/enterprise/violations/"+v.ID, c.body, org.ID)
			if rec.Code != http.StatusOK {
				t.Fatalf("resolve failed: %d %s", rec.Code, rec.Body.String())
			}
			var got models.EnterpriseFeatureViolation
			db.First(&got, "id = ?", v.ID)
			if !got.Resolved {
				t.Fatalf("expected resolved=true")
			}
			if got.Disposition != c.want {
				t.Fatalf("disposition: got %q want %q", got.Disposition, c.want)
			}
			if c.expires != "" && got.ExpiresAt != c.expires {
				t.Fatalf("expires_at: got %q want %q", got.ExpiresAt, c.expires)
			}
			// GORM stores empty timestamp as "0001-01-01T00:00:00Z" on
			// reload — treat that as unset.
			if c.expires == "" && got.ExpiresAt != "" && got.ExpiresAt != "0001-01-01T00:00:00Z" {
				t.Fatalf("did not expect expires_at, got %q", got.ExpiresAt)
			}
			if got.ResolvedAt == "" {
				t.Fatalf("resolved_at not stamped")
			}
		})
	}
}

func TestEnterpriseViolationRiskAcceptedRequiresExpiry(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "ovir", Status: "active"}
	db.Create(&org)
	v := models.EnterpriseFeatureViolation{OrganizationID: org.ID, FeatureKey: "f", Severity: "high"}
	db.Create(&v)
	rec := doJSON(t, srv, "PUT", "/api/enterprise/violations/"+v.ID,
		`{"disposition":"risk_accepted","disposition_reason":"compensating"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 risk_accepted without expiry, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "expires_at") {
		t.Fatalf("error should mention expires_at: %s", rec.Body.String())
	}
}

func TestEnterpriseViolationListCountsByFeature(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "ovlc", Status: "active"}
	db.Create(&org)
	db.Create(&models.EnterpriseFeatureViolation{OrganizationID: org.ID, FeatureKey: "f1", Severity: "high"})
	db.Create(&models.EnterpriseFeatureViolation{OrganizationID: org.ID, FeatureKey: "f1", Severity: "high"})
	db.Create(&models.EnterpriseFeatureViolation{OrganizationID: org.ID, FeatureKey: "f2", Severity: "low", Resolved: true})
	rec := doJSON(t, srv, "GET", "/api/enterprise/violations?counts=true", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("counts failed: %d", rec.Code)
	}
	var rows []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &rows)
	gotF1 := 0
	for _, r := range rows {
		if r["feature_key"] == "f1" { gotF1 = int(r["open"].(float64)) }
	}
	if gotF1 != 2 {
		t.Fatalf("expected 2 open f1, got %d (rows: %+v)", gotF1, rows)
	}
}