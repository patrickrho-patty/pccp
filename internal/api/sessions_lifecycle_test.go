package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sessionlifecycle"
	"gorm.io/gorm"
)

func doSessionJSONWithPermissions(t *testing.T, srv *Server, method, path, body, orgID, role string, permissions ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{Email: "operator@example.test", OrganizationID: orgID, Role: role, Permissions: permissions}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func mkSession(t *testing.T, db *gorm.DB, orgID, status string) *models.Session {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	sess := models.Session{
		UserID: "u1", HarnessID: "hrn-1", SessionID: fmt.Sprintf("ws_%s_%d", status, time.Now().UnixNano()), Status: status,
		OpenedAt: now, LastActivityAt: now,
	}
	sess.OrganizationID = orgID
	if err := db.Create(&sess).Error; err != nil {
		t.Fatal(err)
	}
	return &sess
}

func TestSessionLifecycleTransitionsAndBroadcast(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgsl", Status: "active"}
	db.Create(&org)
	sess := mkSession(t, db, org.ID, "active")

	// active → paused: allowed.
	rec := doJSONAsRole(t, srv, "POST", "/api/sessions/"+sess.ID+"/pause", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("pause active → %d: %s", rec.Code, rec.Body.String())
	}
	var reloaded models.Session
	db.First(&reloaded, "id = ?", sess.ID)
	if reloaded.Status != "paused" || reloaded.LastActivityAt == "" {
		t.Fatalf("after pause: status=%s last_activity_at=%q", reloaded.Status, reloaded.LastActivityAt)
	}

	// paused → paused: invalid transition.
	rec = doJSONAsRole(t, srv, "POST", "/api/sessions/"+sess.ID+"/pause", "", org.ID, "admin")
	if rec.Code != http.StatusConflict {
		t.Fatalf("re-pause → %d, want 409", rec.Code)
	}

	// paused → active (resume): allowed; last_activity refreshed.
	rec = doJSONAsRole(t, srv, "POST", "/api/sessions/"+sess.ID+"/resume", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("resume paused → %d: %s", rec.Code, rec.Body.String())
	}

	// active → closed: allowed; closed is terminal.
	rec = doJSONAsRole(t, srv, "POST", "/api/sessions/"+sess.ID+"/close", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("close active → %d: %s", rec.Code, rec.Body.String())
	}
	for _, action := range []string{"pause", "resume", "close"} {
		rec = doJSONAsRole(t, srv, "POST", "/api/sessions/"+sess.ID+"/"+action, "", org.ID, "admin")
		if rec.Code != http.StatusConflict {
			t.Fatalf("terminal %s → %d, want 409", action, rec.Code)
		}
	}
}

func TestSessionLifecycleCrossOrgAndUnknown(t *testing.T) {
	srv, db := harnessTestServer(t)
	orgA := models.Organization{Name: "a", Slug: "sla", Status: "active"}
	orgB := models.Organization{Name: "b", Slug: "slb", Status: "active"}
	db.Create(&orgA)
	db.Create(&orgB)
	sess := mkSession(t, db, orgA.ID, "active")

	// Cross-org mutation must 404, never mutate.
	rec := doJSONAsRole(t, srv, "POST", "/api/sessions/"+sess.ID+"/pause", "", orgB.ID, "admin")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org pause → %d, want 404", rec.Code)
	}
	var reloaded models.Session
	db.First(&reloaded, "id = ?", sess.ID)
	if reloaded.Status != "active" {
		t.Fatalf("cross-org call mutated status: %s", reloaded.Status)
	}

	// Unknown session 404s.
	rec = doJSONAsRole(t, srv, "POST", "/api/sessions/nope/pause", "", orgA.ID, "admin")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session → %d, want 404", rec.Code)
	}
}

func TestSessionsListExposesCanonicalFields(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgsf", Status: "active"}
	db.Create(&org)
	active := mkSession(t, db, org.ID, "active")
	mkSession(t, db, org.ID, "paused")
	mkSession(t, db, org.ID, "closed")

	rec := doJSON(t, srv, "GET", "/api/sessions?page=1&size=50", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("list → %d", rec.Code)
	}
	var page struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 3 {
		t.Fatalf("rows = %d, want 3", len(page.Data))
	}
	for _, row := range page.Data {
		for _, field := range []string{"id", "status", "opened_at"} {
			if _, ok := row[field]; !ok {
				t.Fatalf("row missing canonical field %q: %v", field, row)
			}
		}
	}
	_ = active
}

func TestBulkSessionsSkipInvalidTransitions(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgbs", Status: "active"}
	db.Create(&org)
	live := mkSession(t, db, org.ID, "active")
	done := mkSession(t, db, org.ID, "closed")

	// Bulk pause: the live row moves, the terminal row is skipped — bulk
	// can never resurrect a closed session (PAT-1496).
	rec := doJSONAsRole(t, srv, "POST", "/api/sessions/bulk",
		fmt.Sprintf(`{"ids":["%s","%s"],"action":"pause","reason":"test"}`, live.ID, done.ID), org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk → %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Affected int64    `json:"affected"`
		Skipped  []string `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Affected != 1 || len(resp.Skipped) != 1 {
		t.Fatalf("bulk result: affected=%d skipped=%v", resp.Affected, resp.Skipped)
	}
	var reloadedDone models.Session
	db.First(&reloadedDone, "id = ?", done.ID)
	if reloadedDone.Status != "closed" {
		t.Fatalf("terminal session mutated: %s", reloadedDone.Status)
	}
	var reloadedLive models.Session
	db.First(&reloadedLive, "id = ?", live.ID)
	if reloadedLive.Status != "paused" || reloadedLive.LastActivityAt == "" {
		t.Fatalf("live session after bulk: status=%s last_activity_at=%q", reloadedLive.Status, reloadedLive.LastActivityAt)
	}
}

func TestBulkTerminalTransitionDestroysBoundSandboxes(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(&models.SandboxRecord{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "org", Slug: "bulk-sandbox-org", Status: "active"}
	db.Create(&org)
	sess := mkSession(t, db, org.ID, "active")
	sandbox := models.SandboxRecord{OrganizationID: org.ID, SessionID: sess.SessionID, Mode: "remote_sandbox", Status: "running"}
	db.Create(&sandbox)

	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/sessions/bulk", fmt.Sprintf(`{"ids":["%s"],"action":"terminate","reason":"test"}`, sess.ID), org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk terminate = %d: %s", rec.Code, rec.Body.String())
	}
	var got models.SandboxRecord
	if err := db.First(&got, "id = ?", sandbox.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "destroyed" {
		t.Fatalf("bound sandbox status = %q, want destroyed", got.Status)
	}
}

func TestSessionReadRoutesAreOrganizationScoped(t *testing.T) {
	srv, db := harnessTestServer(t)
	orgA := models.Organization{Name: "a", Slug: "read-scope-a", Status: "active"}
	orgB := models.Organization{Name: "b", Slug: "read-scope-b", Status: "active"}
	db.Create(&orgA)
	db.Create(&orgB)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: orgA.ID}, Email: "a@example.com", Name: "A", Status: "active"}
	db.Create(&user)
	sess := mkSession(t, db, orgA.ID, "active")

	for _, path := range []string{
		"/api/users/" + user.ID,
		"/api/sessions/" + sess.ID,
		"/api/sessions/" + sess.ID + "/detail",
		"/api/sessions/" + sess.ID + "/decisions",
		"/api/sessions/" + sess.ID + "/replay",
		"/api/sessions/" + sess.ID + "/visibility",
		"/api/sessions/" + sess.ID + "/timeline",
		"/api/sessions/" + sess.ID + "/exchanges",
		"/api/sessions/" + sess.ID + "/provenance",
		"/api/sessions/" + sess.ID + "/provenance/receipts",
		"/api/sessions/" + sess.ID + "/provenance/export",
	} {
		rec := doJSON(t, srv, http.MethodGet, path, "", orgB.ID)
		if rec.Code != http.StatusNotFound {
			t.Errorf("cross-org GET %s = %d, want 404: %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestCanonicalSessionTransitionCannotResurrectTerminalState(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "cas-org", Status: "active"}
	db.Create(&org)
	sess := mkSession(t, db, org.ID, "active")

	closed := srv.sessionLifecycle.Transition(sessionlifecycle.Request{OrganizationID: org.ID, SessionRef: sess.ID, Target: "closed", Action: "test_close"})
	if closed.Result != sessionlifecycle.ResultUpdated {
		t.Fatalf("close outcome = %+v", closed)
	}
	paused := srv.sessionLifecycle.Transition(sessionlifecycle.Request{OrganizationID: org.ID, SessionRef: sess.ID, Target: "paused", Action: "test_pause"})
	if paused.Result != sessionlifecycle.ResultInvalidTransition {
		t.Fatalf("terminal pause outcome = %+v", paused)
	}

	var got models.Session
	db.First(&got, "id = ?", sess.ID)
	if got.Status != "closed" {
		t.Fatalf("final status = %q, want closed", got.Status)
	}
}

func TestSessionSweeperHonorsIdleAndSessionTTLs(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "sweep-org", Status: "active"}
	db.Create(&org)
	now := time.Now().UTC()
	makeTimed := func(status string, opened, activity time.Time, sessionTTL, idleTTL int) *models.Session {
		s := mkSession(t, db, org.ID, status)
		db.Model(s).Updates(map[string]interface{}{
			"opened_at": opened.Format(time.RFC3339), "last_activity_at": activity.Format(time.RFC3339),
			"session_ttl": sessionTTL, "idle_ttl": idleTTL,
		})
		return s
	}
	fresh := makeTimed("active", now.Add(-time.Hour), now.Add(-time.Minute), 8*60*60, 30*60)
	idle := makeTimed("active", now.Add(-time.Hour), now.Add(-31*time.Minute), 8*60*60, 30*60)
	expiredPaused := makeTimed("paused", now.Add(-9*time.Hour), now.Add(-time.Hour), 8*60*60, 30*60)
	terminal := makeTimed("terminated", now.Add(-24*time.Hour), now.Add(-24*time.Hour), 1, 1)

	srv.sweepSessionsAt(now)

	for _, tc := range []struct {
		id, want string
	}{{fresh.ID, "active"}, {idle.ID, "idle"}, {expiredPaused.ID, "closed"}, {terminal.ID, "terminated"}} {
		var got models.Session
		db.First(&got, "id = ?", tc.id)
		if got.Status != tc.want {
			t.Errorf("session %s status = %q, want %q", tc.id, got.Status, tc.want)
		}
	}
}

func TestSessionSweeperRotatesPastNonExpiredFirstPage(t *testing.T) {
	srv, db := harnessTestServer(t)
	now := time.Now().UTC()
	rows := make([]models.Session, 0, 501)
	for i := 0; i < 500; i++ {
		rows = append(rows, models.Session{
			AuditBase: models.AuditBase{Base: models.Base{ID: fmt.Sprintf("a-%04d", i)}, OrganizationID: "org-sweep"},
			HarnessID: "h", UserID: "u", SessionID: fmt.Sprintf("fresh-%04d", i), Status: "active",
			OpenedAt: now.Add(-time.Minute).Format(time.RFC3339), LastActivityAt: now.Format(time.RFC3339), SessionTTL: 3600,
		})
	}
	rows = append(rows, models.Session{
		AuditBase: models.AuditBase{Base: models.Base{ID: "z-expired"}, OrganizationID: "org-sweep"},
		HarnessID: "h", UserID: "u", SessionID: "expired-after-first-page", Status: "active",
		OpenedAt: now.Add(-time.Hour).Format(time.RFC3339), LastActivityAt: now.Add(-time.Hour).Format(time.RFC3339), SessionTTL: 1,
	})
	if err := db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	srv.sweepSessionsAt(now)
	srv.sweepSessionsAt(now)
	var expired models.Session
	if err := db.First(&expired, "id = ?", "z-expired").Error; err != nil {
		t.Fatal(err)
	}
	if expired.Status != "closed" {
		t.Fatalf("rotating sweep starved later expired session: %q", expired.Status)
	}
}

func TestSessionDetailTranscriptRequiresExplicitPermission(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "transcript-org", Status: "active"}
	db.Create(&org)
	sess := mkSession(t, db, org.ID, "active")
	db.Create(&models.PromptExchange{SessionID: sess.SessionID, ExchangeID: "exchange-1", PromptText: "private prompt", ResponseText: "private response", Status: "complete"})

	metadata := doSessionJSONWithPermissions(t, srv, http.MethodGet, "/api/sessions/"+sess.ID+"/detail", "", org.ID, "admin")
	if metadata.Code != http.StatusOK || strings.Contains(metadata.Body.String(), "private prompt") || strings.Contains(metadata.Body.String(), "private response") {
		t.Fatalf("metadata-only detail leaked transcript: %d %s", metadata.Code, metadata.Body.String())
	}
	full := doSessionJSONWithPermissions(t, srv, http.MethodGet, "/api/sessions/"+sess.ID+"/detail", "", org.ID, "viewer", permissionLiveTranscript)
	if full.Code != http.StatusOK || !strings.Contains(full.Body.String(), "private prompt") || !strings.Contains(full.Body.String(), "private response") {
		t.Fatalf("transcript grant did not reveal transcript: %d %s", full.Code, full.Body.String())
	}
}

func TestLiveSessionSnapshotIsBoundedCanonicalAndLinkSafe(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(&models.PromptExchange{}, &models.ModelPackage{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "org", Slug: "live-snapshot-org", Status: "active"}
	db.Create(&org)
	db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: "timezone", Value: "America/New_York"})
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "live@example.com", Name: "Live User", Status: "active"}
	db.Create(&user)
	harness := models.Harness{OrganizationID: org.ID, HarnessID: "hrn-live", Name: "Live Harness", Status: "enrolled"}
	db.Create(&harness)
	active := mkSession(t, db, org.ID, "active")
	active.UserID = user.ID
	active.HarnessID = harness.HarnessID
	active.ModelClass = "enterprise"
	db.Save(active)
	paused := mkSession(t, db, org.ID, "paused")
	paused.ModelClass = "vision"
	db.Save(paused)
	missing := mkSession(t, db, org.ID, "idle")
	missing.UserID = "deleted-user"
	missing.HarnessID = "deleted-harness"
	db.Save(missing)
	mkSession(t, db, org.ID, "closed")
	db.Create(&models.PromptExchange{SessionID: active.SessionID, ExchangeID: "ex-live", ModelPackageID: "pmp-live"})
	db.Create(&models.ModelPackage{PackageID: "pmp-live", ModelID: "model-live", Name: "Live Model", State: "published"})

	rec := doJSONAsRole(t, srv, http.MethodGet, "/api/sessions/live?limit=10", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("live snapshot = %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []struct {
			ID             string            `json:"id"`
			Status         string            `json:"status"`
			ModelPackageID string            `json:"model_package_id"`
			Links          map[string]string `json:"links"`
		} `json:"data"`
		ActiveCount     int64  `json:"active_count"`
		InProgressCount int64  `json:"in_progress_count"`
		Timezone        string `json:"timezone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ActiveCount != 1 || payload.InProgressCount != 3 {
		t.Fatalf("counts active=%d in_progress=%d, want 1/3", payload.ActiveCount, payload.InProgressCount)
	}
	if payload.Timezone != "America/New_York" {
		t.Fatalf("timezone = %q", payload.Timezone)
	}
	if len(payload.Data) != 3 {
		t.Fatalf("rows = %d, want 3", len(payload.Data))
	}
	byID := map[string]struct {
		model string
		links map[string]string
	}{}
	for _, row := range payload.Data {
		byID[row.ID] = struct {
			model string
			links map[string]string
		}{row.ModelPackageID, row.Links}
	}
	if got := byID[active.ID]; got.model != "pmp-live" || got.links["session"] != "/sessions/"+active.ID || got.links["user"] != "/users/"+user.ID || got.links["harness"] != "/harnesses/"+harness.ID || got.links["model"] != "/models/pmp-live" || got.links["fleet"] != "/fleet?harness_id=hrn-live" {
		t.Fatalf("active exact links/model = %#v", got)
	}
	if got := byID[paused.ID]; got.links["model"] != "/models?class=vision" {
		t.Fatalf("class-only model fallback link = %#v", got.links)
	}
	if got := byID[missing.ID]; got.links["user"] != "" || got.links["harness"] != "" || got.links["fleet"] != "" {
		t.Fatalf("deleted entities must not produce dead links: %#v", got.links)
	}
	_ = paused
}

func TestFleetDeepLinkUsesExactHarnessID(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "fleet-deep-link-org", Status: "active"}
	db.Create(&org)
	db.Create(&models.Harness{OrganizationID: org.ID, HarnessID: "hrn-1", Name: "Target", Status: "enrolled"})
	db.Create(&models.Harness{OrganizationID: org.ID, HarnessID: "hrn-10", Name: "Similar", Status: "enrolled"})

	rec := doJSON(t, srv, http.MethodGet, "/api/fleet/inventory?page=1&size=25&harness_id=hrn-1", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("fleet exact filter = %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []struct {
			Harness models.Harness `json:"harness"`
		} `json:"data"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 1 || len(payload.Data) != 1 || payload.Data[0].Harness.HarnessID != "hrn-1" {
		t.Fatalf("exact fleet result = %#v", payload)
	}
}

func TestSessionListDeepLinkUsesExactHarnessID(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "session-harness-filter", Status: "active"}
	db.Create(&org)
	target := mkSession(t, db, org.ID, "active")
	db.Model(target).Update("harness_id", "hrn-1")
	other := mkSession(t, db, org.ID, "active")
	db.Model(other).Update("harness_id", "hrn-10")

	rec := doJSON(t, srv, http.MethodGet, "/api/sessions?page=1&size=25&harness_id=hrn-1", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("session exact filter = %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data  []models.Session `json:"data"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 1 || len(payload.Data) != 1 || payload.Data[0].ID != target.ID {
		t.Fatalf("exact session filter = %#v", payload)
	}
}

func TestSessionDetailDoesNotBypassUsagePermission(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(
		&models.UsageRecord{},
		&models.ActionEnvelope{},
		&models.ChangeSet{},
		&models.PromptExchange{},
		&models.ProvenanceSpan{},
	); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "org", Slug: "session-detail-usage-rbac", Status: "active"}
	db.Create(&org)
	sess := mkSession(t, db, org.ID, "active")
	db.Create(&models.ActionEnvelope{
		OrganizationID: org.ID, SessionID: sess.SessionID, ActionID: "action-secret",
		ActionType: "tool_use", ActionPayload: `{"token":"do-not-return"}`, OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	db.Create(&models.UsageRecord{
		OrganizationID: org.ID, SessionID: sess.SessionID, MetricType: "tokens_in",
		Unit: "tokens", Quantity: 42, CostMicros: 100, Currency: "KRW",
		PricingState: models.UsagePricingPriced, OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})

	rec := doJSONAsRole(t, srv, http.MethodGet, "/api/sessions/"+sess.ID+"/detail", "", org.ID, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer session detail = %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, exposed := payload["usage"]; exposed {
		t.Fatalf("session detail exposed financial usage to viewer: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "do-not-return") || strings.Contains(rec.Body.String(), "action_payload") {
		t.Fatalf("metadata-only detail exposed action payload: %s", rec.Body.String())
	}
	for _, path := range []string{"/api/sessions/" + sess.ID + "/replay", "/api/fleet/sessions/" + sess.ID + "/inspect"} {
		rec = doJSONAsRole(t, srv, http.MethodGet, path, "", org.ID, "viewer")
		if rec.Code != http.StatusOK {
			t.Fatalf("metadata-only %s = %d: %s", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "do-not-return") || strings.Contains(rec.Body.String(), "action_payload") {
			t.Fatalf("metadata-only %s exposed action payload: %s", path, rec.Body.String())
		}
	}
}

func TestLiveTicketAndSnapshotRequireExplicitReadPermission(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "live-ticket-rbac", Status: "active"}
	db.Create(&org)

	denied := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/realtime/ticket", "", org.ID, "viewer")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer ticket = %d, want 403: %s", denied.Code, denied.Body.String())
	}
	denied = doSessionJSONWithPermissions(t, srv, http.MethodGet, "/api/sessions/live", "", org.ID, "viewer")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer snapshot = %d, want 403", denied.Code)
	}
	allowed := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/realtime/ticket", "", org.ID, "viewer", "live:read")
	if allowed.Code != http.StatusCreated {
		t.Fatalf("delegated ticket = %d: %s", allowed.Code, allowed.Body.String())
	}
	var payload struct {
		StreamURL         string `json:"stream_url"`
		TranscriptVisible bool   `json:"transcript_visible"`
	}
	if err := json.Unmarshal(allowed.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.StreamURL, "ticket=") || strings.Contains(payload.StreamURL, "operator@example.test") || payload.TranscriptVisible {
		t.Fatalf("least-privilege ticket response = %+v", payload)
	}
	transcript := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/realtime/ticket", "", org.ID, "viewer", "live:read", "live:transcript")
	if transcript.Code != http.StatusCreated || !strings.Contains(transcript.Body.String(), `"transcript_visible":true`) {
		t.Fatalf("transcript grant = %d %s", transcript.Code, transcript.Body.String())
	}
}

func TestGenericFleetActionsCannotBypassScopedLockdownWorkflow(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "fleet-lockdown-boundary", Status: "active"}
	db.Create(&org)
	harness := models.Harness{HarnessID: "hrn-lockdown", Status: "enrolled"}
	harness.OrganizationID = org.ID
	db.Create(&harness)
	sess := mkSession(t, db, org.ID, "active")
	db.Model(sess).Update("harness_id", harness.HarnessID)

	requests := []struct {
		path string
		body string
	}{
		{"/api/fleet/actions", `{"harness_id":"hrn-lockdown","action":"emergency_lockdown","reason":"test"}`},
		{"/api/fleet/actions/bulk", `{"harness_ids":["hrn-lockdown","hrn-lockdown"],"action":"emergency_lockdown","reason":"test"}`},
		{"/api/fleet/actions/bulk", `{"harness_ids":["hrn-lockdown"],"action":"invented_action","reason":"test"}`},
		{"/api/fleet/actions/bulk", `{"harness_ids":["hrn-lockdown"],"action":"terminate_session","reason":"test"}`},
	}
	for _, request := range requests {
		rec := doSessionJSONWithPermissions(t, srv, http.MethodPost, request.path, request.body, org.ID, "admin", "sessions:manage")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("generic containment %s = %d, want 400: %s", request.path, rec.Code, rec.Body.String())
		}
	}
	var stored models.Session
	if err := db.First(&stored, "id = ?", sess.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "active" {
		t.Fatalf("generic fleet route mutated session: %q", stored.Status)
	}
}

func TestFleetBulkActionIsBoundedAndIdempotent(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "fleet-bulk-idempotency", Status: "active"}
	db.Create(&org)
	harness := models.Harness{HarnessID: "hrn-bulk", Status: "active"}
	harness.OrganizationID = org.ID
	db.Create(&harness)

	call := func(key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/fleet/actions/bulk", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{Email: "operator@example.test", OrganizationID: org.ID, Role: "admin", Permissions: []string{"sessions:manage"}}))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	body := `{"harness_ids":["hrn-bulk"],"action":"isolate_sandbox","reason":"test \"quoted\" reason"}`
	if missing := call("", body); missing.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key = %d, want 400", missing.Code)
	}
	first := call("bulk-key-1", body)
	if first.Code != http.StatusOK {
		t.Fatalf("first bulk = %d: %s", first.Code, first.Body.String())
	}
	second := call("bulk-key-1", body)
	if second.Code != http.StatusOK || second.Header().Get("Idempotent-Replay") != "true" || second.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d replay=%q body=%s", second.Code, second.Header().Get("Idempotent-Replay"), second.Body.String())
	}
	conflict := call("bulk-key-1", `{"harness_ids":["hrn-bulk"],"action":"isolate_sandbox","reason":"different"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("key reuse for different request = %d, want 409", conflict.Code)
	}
	var actions int64
	db.Model(&models.AuditEvent{}).Where("organization_id = ? AND event_type = ?", org.ID, "cp.fleet.isolate_sandbox").Count(&actions)
	if actions != 1 {
		t.Fatalf("fleet action audits = %d, want exactly one", actions)
	}
}

func TestChangeSubmissionPayloadRequiresTranscriptPermission(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "submission-redaction", Status: "active"}
	db.Create(&org)
	db.Create(&models.ActionEnvelope{
		OrganizationID: org.ID, ActionID: "action-1", ActionType: "changeboard.submit",
		ActionPayload: `{"submission_id":"sub-1","secret":"private patch"}`,
		HarnessID:     "harness", SessionID: "session", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})

	metadata := doSessionJSONWithPermissions(t, srv, http.MethodGet, "/api/enterprise/submissions", "", org.ID, "admin")
	if metadata.Code != http.StatusOK || strings.Contains(metadata.Body.String(), "private patch") || strings.Contains(metadata.Body.String(), `"payload"`) {
		t.Fatalf("metadata submission leaked payload: %d %s", metadata.Code, metadata.Body.String())
	}
	transcript := doSessionJSONWithPermissions(t, srv, http.MethodGet, "/api/enterprise/submissions", "", org.ID, "viewer", permissionLiveTranscript)
	if transcript.Code != http.StatusOK || !strings.Contains(transcript.Body.String(), "private patch") {
		t.Fatalf("explicit transcript grant did not expose payload: %d %s", transcript.Code, transcript.Body.String())
	}
}

func TestBulkSessionsRequiresManagePermissionAndReason(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "bulk-session-rbac", Status: "active"}
	db.Create(&org)
	sess := mkSession(t, db, org.ID, "active")
	body := fmt.Sprintf(`{"ids":["%s"],"action":"pause","reason":"incident containment"}`, sess.ID)
	denied := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/sessions/bulk", body, org.ID, "viewer")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer bulk = %d, want 403", denied.Code)
	}
	missingReason := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/sessions/bulk", fmt.Sprintf(`{"ids":["%s"],"action":"pause"}`, sess.ID), org.ID, "admin")
	if missingReason.Code != http.StatusBadRequest {
		t.Fatalf("bulk without reason = %d, want 400", missingReason.Code)
	}
	allowed := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/sessions/bulk", body, org.ID, "viewer", "sessions:manage")
	if allowed.Code != http.StatusOK || !strings.Contains(allowed.Body.String(), `"result":"updated"`) {
		t.Fatalf("delegated bulk = %d %s", allowed.Code, allowed.Body.String())
	}
}
