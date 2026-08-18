package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
)

func doUsageWithPermissions(t *testing.T, srv *Server, path, orgID string, permissions ...string) *httptest.ResponseRecorder {
	return doUsageMethodWithPermissions(t, srv, http.MethodGet, path, orgID, permissions...)
}

func doUsageMethodWithPermissions(t *testing.T, srv *Server, method, path, orgID string, permissions ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{
		Email: "usage-viewer@pccp.test", OrganizationID: orgID, Role: "viewer", Permissions: permissions,
	}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestUsagePermissionsSeparateSummaryLedgerAndResourceScope(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage-permissions", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "scoped@example.test", Name: "Scoped", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	orgSummary := "usage.summary.read:organization:" + org.ID
	if rec := doUsageWithPermissions(t, srv, "/api/analytics/usage?summary_only=1", org.ID, orgSummary); rec.Code != http.StatusOK {
		t.Fatalf("scoped summary permission = %d %s", rec.Code, rec.Body.String())
	}
	if rec := doUsageWithPermissions(t, srv, "/api/analytics/usage", org.ID, orgSummary); rec.Code != http.StatusForbidden {
		t.Fatalf("summary permission exposed ledger: %d %s", rec.Code, rec.Body.String())
	}

	userSummary := "usage.summary.read:user:" + user.ID
	if rec := doUsageWithPermissions(t, srv, "/api/users/"+user.ID+"/usage?summary_only=1", org.ID, userSummary); rec.Code != http.StatusOK {
		t.Fatalf("matching user scope = %d %s", rec.Code, rec.Body.String())
	}
	if rec := doUsageWithPermissions(t, srv, "/api/users/"+user.ID+"/usage?summary_only=1", org.ID, "usage.summary.read:user:someone-else"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong user scope = %d %s", rec.Code, rec.Body.String())
	}
}

func TestUsageContinuationAlwaysRequiresLedgerPermission(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage-cursor-permission", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	for index := 0; index < 51; index++ {
		if err := db.Create(&models.UsageRecord{OrganizationID: org.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 1, PricingState: models.UsagePricingUnpriced, OccurredAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	first := doJSONAsRole(t, srv, http.MethodGet, "/api/analytics/usage?limit=50", "", org.ID, "admin")
	if first.Code != http.StatusOK {
		t.Fatalf("first ledger page = %d %s", first.Code, first.Body.String())
	}
	var report UsageTotal
	if err := json.Unmarshal(first.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.LedgerNextCursor == "" {
		t.Fatal("first ledger page did not return a continuation cursor")
	}
	path := "/api/analytics/usage?summary_only=1&cursor=" + report.LedgerNextCursor
	rec := doUsageWithPermissions(t, srv, path, org.ID, "usage.summary.read:organization:"+org.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("summary permission read a ledger continuation: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUsageExportPermissionIsIndependentAndResourceScoped(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage-export-permissions", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "Scoped", Slug: "scoped"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/analytics/usage-export-ticket?project_id=" + project.ID
	if rec := doUsageMethodWithPermissions(t, srv, http.MethodPost, path, org.ID, "usage.ledger.read:project:"+project.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("ledger read minted export ticket: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doUsageMethodWithPermissions(t, srv, http.MethodPost, path, org.ID, "usage.export:project:"+project.ID); rec.Code != http.StatusCreated {
		t.Fatalf("scoped export permission = %d %s", rec.Code, rec.Body.String())
	}
}

func TestUsageExportSessionRowIDResolvesToCanonicalSessionID(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-usage-export-session", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	session := models.Session{
		AuditBase: models.AuditBase{OrganizationID: org.ID},
		SessionID: "ws_usage_export_canonical",
		Status:    "active",
		OpenedAt:  time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	record := models.UsageRecord{
		OrganizationID: org.ID,
		SessionID:      session.SessionID,
		MetricType:     "tokens_in",
		Unit:           "tokens",
		Quantity:       17,
		OccurredAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}

	ticketRec := doUsageMethodWithPermissions(t, srv, http.MethodPost,
		"/api/analytics/usage-export-ticket?session_id="+session.ID, org.ID,
		"usage.export:session:"+session.ID)
	if ticketRec.Code != http.StatusCreated {
		t.Fatalf("ticket status = %d %s", ticketRec.Code, ticketRec.Body.String())
	}
	var ticket map[string]string
	if err := json.Unmarshal(ticketRec.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	downloadRec := httptest.NewRecorder()
	srv.ServeHTTP(downloadRec, httptest.NewRequest(http.MethodGet, ticket["download_url"], nil))
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download status = %d %s", downloadRec.Code, downloadRec.Body.String())
	}
	if !strings.Contains(downloadRec.Body.String(), record.ID) {
		t.Fatalf("session-scoped export omitted canonical session usage: %s", downloadRec.Body.String())
	}
}
