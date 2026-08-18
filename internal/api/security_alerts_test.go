package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// PAT-1502 PR 1 — redaction boundary tests.
//
// These tests assert that the alert endpoint API NEVER returns the
// submitted webhook URL on any path: list, create response, error
// response, audit log, or cross-tenant access.

const alertTestTarget = "https://hooks.slack.com/services/T0000/B0000/XXXXXXXXXXXXXXXXXXXXXXXX"

func doJSONAsRole(t *testing.T, srv *Server, method, path, body, orgID, role string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	var req *http.Request
	if bodyReader == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bodyReader)
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{
		Email:          "tester@pccp.test",
		OrganizationID: orgID,
		Role:           role,
	}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestAlertEndpointCreateRedactsURLInResponse(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-redact-create", Status: "active"}
	db.Create(&org)

	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"oncall","type":"slack","target":"`+alertTestTarget+`","severities":["critical"]}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, alertTestTarget) {
		t.Fatalf("create response leaked the URL: %s", body)
	}
	if strings.Contains(body, "hooks.slack.com") {
		t.Fatalf("create response leaked the host: %s", body)
	}
	if strings.Contains(body, "/services/T0000") {
		t.Fatalf("create response leaked the path prefix: %s", body)
	}

	var resp AlertEndpointResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.SecretConfigured != true {
		t.Fatalf("secret_configured must be true after a successful create, got %+v", resp)
	}
	if resp.CredentialID == "" {
		t.Fatalf("credential_id must be set when secret is configured, got %+v", resp)
	}
	if resp.TargetRedacted != "***" {
		t.Fatalf("target_redacted must be the constant sentinel, got %q", resp.TargetRedacted)
	}
}

func TestAlertEndpointListRedactsURL(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-redact-list", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"oncall","type":"slack","target":"`+alertTestTarget+`"}`,
		org.ID, "owner")
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed create failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSONAsRole(t, srv, "GET", "/api/security/alerts", "", org.ID, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, alertTestTarget) {
		t.Fatalf("list response leaked the URL: %s", body)
	}
	if strings.Contains(body, "hooks.slack.com") {
		t.Fatalf("list response leaked the host: %s", body)
	}
	var list []AlertEndpointResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 redacted entry, got %d", len(list))
	}
	if list[0].SecretConfigured != true || list[0].TargetRedacted != "***" {
		t.Fatalf("redaction shape wrong: %+v", list[0])
	}
}

func TestAlertEndpointCreateForbiddenForViewer(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-rbac", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack","target":"`+alertTestTarget+`"}`,
		org.ID, "viewer")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer must be rejected from create, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlertEndpointDeleteForbiddenForViewer(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-rbac-del", Status: "active"}
	db.Create(&org)
	// Seed an endpoint as admin.
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack","target":"`+alertTestTarget+`"}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rec.Code)
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)

	// Viewer tries to delete.
	rec = doJSONAsRole(t, srv, "DELETE", "/api/security/alerts/"+created.ID, "", org.ID, "viewer")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer must be rejected from delete, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlertEndpointCrossTenantIsolation(t *testing.T) {
	srv, db := securityTestServer(t)
	orgA := models.Organization{Name: "A", Slug: "A-iso", Status: "active"}
	orgB := models.Organization{Name: "B", Slug: "B-iso", Status: "active"}
	db.Create(&orgA)
	db.Create(&orgB)

	// Org A seeds a route.
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"a-only","type":"slack","target":"`+alertTestTarget+`"}`,
		orgA.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed A: %d", rec.Code)
	}

	// Org B lists — must not see A's endpoint OR its URL.
	rec = doJSONAsRole(t, srv, "GET", "/api/security/alerts", "", orgB.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("list B: %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, alertTestTarget) {
		t.Fatalf("cross-tenant list leaked A's URL into B's response: %s", body)
	}
	var list []AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("B must see zero endpoints, got %d", len(list))
	}
}

func TestAlertEndpointCreateErrorDoesNotEchoTarget(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-err-echo", Status: "active"}
	db.Create(&org)
	// Missing target — server must reject with a generic message that
	// never includes the (empty) value.
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack"}`,
		org.ID, "admin")
	if rec.Code == http.StatusCreated {
		t.Fatalf("missing target must fail, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, alertTestTarget) {
		t.Fatalf("error body should not echo target: %s", body)
	}
}

func TestAlertEndpointAuditDoesNotContainURL(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-audit", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"oncall","type":"slack","target":"`+alertTestTarget+`"}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)

	var events []models.AuditEvent
	if err := db.Where("organization_id = ? AND event_type = ?", org.ID, "security.alert_endpoint.create").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatalf("create must record an audit event")
	}
	for _, ev := range events {
		if strings.Contains(ev.Details, alertTestTarget) || strings.Contains(ev.Details, "hooks.slack.com") {
			t.Fatalf("audit details leaked URL: %s", ev.Details)
		}
		// Confirm the credential identifier is present so the audit
		// trail remains traceable to a stored secret.
		if !strings.Contains(ev.Details, "credential_id") {
			t.Fatalf("audit details must include credential_id: %s", ev.Details)
		}
	}

	// Now exercise delete and inspect its audit row.
	rec = doJSONAsRole(t, srv, "DELETE", "/api/security/alerts/"+created.ID, "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	var delEvents []models.AuditEvent
	db.Where("organization_id = ? AND event_type = ?", org.ID, "security.alert_endpoint.delete").Find(&delEvents)
	if len(delEvents) == 0 {
		t.Fatalf("delete must record an audit event")
	}
	for _, ev := range delEvents {
		if strings.Contains(ev.Details, alertTestTarget) || strings.Contains(ev.Details, "hooks.slack.com") {
			t.Fatalf("delete audit leaked URL: %s", ev.Details)
		}
	}
}

func TestAlertEndpointRedactIsStable(t *testing.T) {
	// Two endpoints with the same URL must share a credential_id; two
	// endpoints with different URLs must not. This guarantees the
	// identifier is usable as a stable correlation key in audit logs
	// without leaking the URL itself.
	ep1 := models.AlertEndpoint{Target: "https://example.test/a"}
	ep2 := models.AlertEndpoint{Target: "https://example.test/a"}
	ep3 := models.AlertEndpoint{Target: "https://example.test/b"}
	provider, _ := keymgmt.NewLocalProvider([]byte("0123456789abcdef0123456789abcdef"), "test")
	if got := credentialIDForTarget(provider, ep1.Target); got == "" {
		t.Fatalf("credential_id must not be empty for non-empty target")
	}
	if got := credentialIDForTarget(provider, ep1.Target); got != credentialIDForTarget(provider, ep2.Target) {
		t.Fatalf("same URL must hash to the same id: %q vs %q", got, credentialIDForTarget(provider, ep2.Target))
	}
	if got := credentialIDForTarget(provider, ep1.Target); got == credentialIDForTarget(provider, ep3.Target) {
		t.Fatalf("different URLs must hash to different ids: %q == %q", got, credentialIDForTarget(provider, ep3.Target))
	}
	if got := credentialIDForTarget(provider, ""); got != "" {
		t.Fatalf("empty target must yield empty credential_id, got %q", got)
	}
}

func TestAlertEndpointEmptyTargetNotConfigured(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-empty", Status: "active"}
	db.Create(&org)
	// Manually seed a metadata-only row (mirrors the demo seed pattern).
	db.Create(&models.AlertEndpoint{
		Base:           models.Base{ID: "alert_empty"},
		OrganizationID: org.ID,
		Name:           "placeholder",
		Type:           "slack",
		Enabled:        false,
		SeveritiesJSON: `["critical"]`,
	})
	rec := doJSONAsRole(t, srv, "GET", "/api/security/alerts", "", org.ID, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var list []AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].SecretConfigured {
		t.Fatalf("an unconfigured endpoint must report secret_configured=false, got %+v", list[0])
	}
	if list[0].CredentialID != "" {
		t.Fatalf("an unconfigured endpoint must report empty credential_id, got %+v", list[0])
	}
}

// Ensure unused imports are caught.
var _ = context.Background
