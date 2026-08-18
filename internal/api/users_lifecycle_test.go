package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func usersTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/u.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.BusinessUnit{}, &models.Harness{},
		&models.Device{}, &models.Session{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.PolicyEpoch{}, &models.PolicyPack{}, &models.UsageRecord{}, &models.Role{},
		&models.UserRole{}, &models.EnrollmentCode{}, &models.Project{}, &models.Repository{},
		&models.ChangeRequest{}, &models.ChangeSet{}, &models.CapabilityLease{}, &models.OrgSetting{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return srv, db
}

func mkUser(t *testing.T, db *gorm.DB, orgID, email string) *models.User {
	t.Helper()
	u := models.User{Email: email, Name: email, Status: "active"}
	u.OrganizationID = orgID
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return &u
}

func TestUserListServerFiltersAndSort(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgu", Status: "active"}
	db.Create(&org)
	bu := models.BusinessUnit{Name: "ENG", NameKo: "엔지니어링", Type: "department", Level: 1}
	bu.OrganizationID = org.ID
	db.Create(&bu)
	a := mkUser(t, db, org.ID, "a@corp.kr")
	b := mkUser(t, db, org.ID, "b@corp.kr")
	db.Model(b).Update("business_unit_id", bu.ID)

	rec := doJSON(t, srv, "GET", "/api/users?page=1&size=25&business_unit="+bu.ID, "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("list failed: %d", rec.Code)
	}
	var page map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &page)
	rows := page["data"].([]interface{})
	if len(rows) != 1 {
		t.Fatalf("business_unit filter returned %d rows", len(rows))
	}
	_ = a

	rec = doJSON(t, srv, "GET", "/api/users?page=1&size=25&search=b@corp", "", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &page)
	rows = page["data"].([]interface{})
	if len(rows) != 1 {
		t.Fatalf("search filter returned %d rows", len(rows))
	}
}

func TestUserHarnessBindingGrantRevoke(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgh", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "dev@corp.kr")
	h := models.Harness{Name: "h1", HarnessID: "peer-1", Status: "enrolled"}
	h.OrganizationID = org.ID
	db.Create(&h)

	rec := doJSON(t, srv, "POST", "/api/users/"+u.ID+"/harnesses",
		fmt.Sprintf(`{"harness_id":"%s"}`, h.ID), org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("grant failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/users/"+u.ID+"/harnesses", "", org.ID)
	var list []models.Harness
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].ID != h.ID {
		t.Fatalf("binding list wrong: %v", list)
	}
	rec = doJSON(t, srv, "DELETE", "/api/users/"+u.ID+"/harnesses/"+h.ID, "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke failed: %d", rec.Code)
	}
	rec = doJSON(t, srv, "GET", "/api/users/"+u.ID+"/harnesses", "", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("binding remains after revoke: %v", list)
	}
}

func TestOffboardWorkflowCascades(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgo", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "leaver@corp.kr")
	sess := models.Session{UserID: u.ID, HarnessID: "peer-9", SessionID: "s-1", Status: "active"}
	sess.OrganizationID = org.ID
	db.Create(&sess)
	h := models.Harness{Name: "h1", HarnessID: "peer-9", Status: "enrolled",
		AllowedUsers: fmt.Sprintf(`["%s"]`, u.ID)}
	h.OrganizationID = org.ID
	db.Create(&h)

	rec := doJSON(t, srv, "POST", "/api/users/"+u.ID+"/offboard",
		`{"reason":"contract end"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("offboard failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status           string `json:"status"`
		ClosedSessions   int64  `json:"closed_sessions"`
		RevokedHarnesses int64  `json:"revoked_harnesses"`
		RemainingActive  int64  `json:"remaining_active"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "offboarded" || resp.ClosedSessions != 1 || resp.RevokedHarnesses != 1 || resp.RemainingActive != 0 {
		t.Fatalf("offboard evidence wrong: %+v", resp)
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", u.ID)
	if reloaded.Status != "offboarded" {
		t.Fatalf("user status: %s", reloaded.Status)
	}
}

func TestEntitlementAssignAndEvaluate(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orge", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "dev@corp.kr")

	rec := doJSON(t, srv, "GET", "/api/roles", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("roles failed: %d", rec.Code)
	}
	var roles []models.Role
	json.Unmarshal(rec.Body.Bytes(), &roles)
	if len(roles) < 4 {
		t.Fatalf("expected seeded roles, got %d", len(roles))
	}
	var paid models.Role
	for _, r := range roles {
		if r.Name == "global-developer" {
			paid = r
		}
	}
	rec = doJSON(t, srv, "PUT", "/api/users/"+u.ID+"/entitlements",
		fmt.Sprintf(`{"roles":[{"role_id":"%s","scope":"org","scope_id":"%s"}]}`, paid.ID, org.ID), org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign failed: %d %s", rec.Code, rec.Body.String())
	}
	ok, err := srv.identity.EvaluateEntitlement(org.ID, u.ID, "class:interactive-paid")
	if err != nil || !ok {
		t.Fatalf("entitlement evaluate: ok=%v err=%v", ok, err)
	}
}

func TestUsageRollup(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgw", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "dev@corp.kr")
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 3; i++ {
		r := models.UsageRecord{MetricType: "tokens_out", Unit: "tokens", Quantity: 10, CostMicros: 2, Currency: "KRW", PricingState: models.UsagePricingPriced, OccurredAt: now}
		r.OrganizationID = org.ID
		r.UserID = u.ID
		db.Create(&r)
	}
	rec := doJSONAsRole(t, srv, "GET", "/api/users/"+u.ID+"/usage", "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("usage failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Metrics         []map[string]interface{} `json:"metrics"`
		TotalCostMicros int64                    `json:"total_cost_micros"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	var tokensOut float64
	for _, meter := range resp.Metrics {
		if meter["metric_type"] == "tokens_out" {
			tokensOut = meter["quantity"].(float64)
		}
	}
	if tokensOut != 30 || resp.TotalCostMicros != 6 {
		t.Fatalf("usage rollup wrong: %+v", resp)
	}
}

func TestContractorProfileValidation(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgc", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "ctr@corp.kr")

	rec := doJSON(t, srv, "PUT", "/api/users/"+u.ID+"/contractor",
		`{"sponsor_user_id":"s1","company":"Vendor","contract_start":"2026-02-01","contract_end":"2026-01-01"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid contract window accepted: %d", rec.Code)
	}
	rec = doJSON(t, srv, "PUT", "/api/users/"+u.ID+"/contractor",
		`{"sponsor_user_id":"s1","company":"Vendor","contract_start":"2026-01-01","contract_end":"2026-12-31","allowed_repo_ids":["r1"],"allowed_model_classes":["text"],"network_zone":"dmz"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("contractor set failed: %d %s", rec.Code, rec.Body.String())
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", u.ID)
	if !strings.Contains(reloaded.ContractorInfo, `"company":"Vendor"`) {
		t.Fatalf("contractor json missing: %s", reloaded.ContractorInfo)
	}
}

func TestCSVImportDryRunAndApply(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgi", Status: "active"}
	db.Create(&org)

	body := "email,name\na@corp.kr,Alice\nb@corp.kr,Bob\n"
	req := doMultipart(t, srv, "POST", "/api/users/import", "file", "users.csv", body, org.ID)
	if req.Code != http.StatusOK {
		t.Fatalf("import failed: %d %s", req.Code, req.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(req.Body.Bytes(), &resp)
	if resp["dry_run"] != true || resp["imported"].(float64) != 2 {
		t.Fatalf("dry run wrong: %+v", resp)
	}
	var count int64
	db.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&count)
	if count != 0 {
		t.Fatalf("dry-run imported rows: %d", count)
	}

	req = doMultipart(t, srv, "POST", "/api/users/import?apply=true", "file", "users.csv", body, org.ID)
	json.Unmarshal(req.Body.Bytes(), &resp)
	if resp["dry_run"] != false || resp["imported"].(float64) != 2 {
		t.Fatalf("apply wrong: %+v", resp)
	}
	db.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&count)
	if count != 2 {
		t.Fatalf("apply imported %d rows", count)
	}
}

// --- PAT-1489: canonical user lifecycle state machine ---

func doUserJSONAs(t *testing.T, srv *Server, method, path, body, orgID, role, email string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{Email: email, OrganizationID: orgID, Role: role}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func countAuditEvents(db *gorm.DB, resourceID, eventType string) int64 {
	var n int64
	db.Model(&models.AuditEvent{}).Where("resource_id = ? AND event_type = ?", resourceID, eventType).Count(&n)
	return n
}

func TestUserLifecycleStateMachineTransitions(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgsm", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "sm@corp.kr")

	// active → suspended: allowed, distinct audit event.
	rec := doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/suspend", `{"reason":"휴직 조사"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("suspend active rejected: %d %s", rec.Code, rec.Body.String())
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", u.ID)
	if reloaded.Status != "suspended" {
		t.Fatalf("status after suspend: %s", reloaded.Status)
	}
	if n := countAuditEvents(db, u.ID, "cp.user.suspended"); n != 1 {
		t.Fatalf("cp.user.suspended events: %d", n)
	}

	// suspended → suspended: conflict (stale/duplicate action).
	rec = doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/suspend", `{"reason":"again"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusConflict {
		t.Fatalf("re-suspend not 409: %d", rec.Code)
	}

	// suspended → active (resume): allowed, distinct audit event.
	rec = doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/resume", `{"reason":"복직"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("resume suspended rejected: %d %s", rec.Code, rec.Body.String())
	}
	db.First(&reloaded, "id = ?", u.ID)
	if reloaded.Status != "active" {
		t.Fatalf("status after resume: %s", reloaded.Status)
	}
	if n := countAuditEvents(db, u.ID, "cp.user.resumed"); n != 1 {
		t.Fatalf("cp.user.resumed events: %d", n)
	}

	// active → active: conflict.
	rec = doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/resume", `{"reason":"again"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusConflict {
		t.Fatalf("resume active not 409: %d", rec.Code)
	}

	// suspended → offboarded allowed; offboarded is terminal.
	rec = doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/suspend", `{"reason":"재정지"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("re-suspend before offboard failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/offboard", `{"reason":"퇴사"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("offboard suspended rejected: %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/suspend", "/resume", "/offboard"} {
		rec = doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+path, `{"reason":"late"}`, org.ID, "admin", "op@corp.kr")
		if rec.Code != http.StatusConflict {
			t.Fatalf("terminal state %s not 409: %d", path, rec.Code)
		}
	}
}

func TestUserLifecycleMutationGuards(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orggr", Status: "active"}
	db.Create(&org)
	other := models.Organization{Name: "other", Slug: "orggo", Status: "active"}
	db.Create(&other)
	u := mkUser(t, db, org.ID, "guard@corp.kr")

	// Viewer role: forbidden on every lifecycle mutation.
	for _, path := range []string{"/suspend", "/resume", "/offboard"} {
		rec := doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+path, `{"reason":"x"}`, org.ID, "viewer", "viewer@corp.kr")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer %s → %d, want 403", path, rec.Code)
		}
	}
	// Self-action: an operator must not change their own lifecycle state.
	rec := doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/suspend", `{"reason":"x"}`, org.ID, "admin", "guard@corp.kr")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("self-suspend → %d, want 400", rec.Code)
	}
	// Reason is required for every lifecycle mutation.
	rec = doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/suspend", `{}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing reason → %d, want 400", rec.Code)
	}
	// Cross-org target: not found, never a hint that it exists.
	rec = doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/suspend", `{"reason":"x"}`, other.ID, "admin", "op@other.kr")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org suspend → %d, want 404", rec.Code)
	}
	// No state change leaked through the rejected calls.
	db.First(&u, "id = ?", u.ID)
	if u.Status != "active" {
		t.Fatalf("rejected mutations changed status: %s", u.Status)
	}
}

func TestUserUpdateRejectsStatusBypass(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgbp", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "bypass@corp.kr")

	// Generic PUT can no longer flip lifecycle status.
	rec := doUserJSONAs(t, srv, "PUT", "/api/users/"+u.ID, `{"status":"suspended","reason":"우회"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusConflict {
		t.Fatalf("PUT status bypass → %d, want 409", rec.Code)
	}
	// Same-value status is a no-op and must not block profile edits.
	rec = doUserJSONAs(t, srv, "PUT", "/api/users/"+u.ID, `{"status":"active","title":"Staff Engineer"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT same-status edit → %d: %s", rec.Code, rec.Body.String())
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", u.ID)
	if reloaded.Status != "active" || reloaded.Title != "Staff Engineer" {
		t.Fatalf("unexpected state after PUT: %+v", reloaded)
	}
}

func TestUserLifecycleConcurrentSuspendExactlyOnce(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgcc", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "race@corp.kr")

	// Two racing suspends: the conditional update must let exactly one
	// win with 200; the loser gets 409 and no second audit event.
	const racers = 2
	codes := make([]int, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/suspend", `{"reason":"경합"}`, org.ID, "admin", "op@corp.kr")
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()
	sort.Ints(codes)
	if codes[0] != http.StatusOK || codes[1] != http.StatusConflict {
		t.Fatalf("race outcomes = %v, want [200 409]", codes)
	}
	if n := countAuditEvents(db, u.ID, "cp.user.suspended"); n != 1 {
		t.Fatalf("suspended audit events after race: %d, want 1", n)
	}
}

func TestUserLifecycleExternallyManagedSameTable(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgx", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "oidc@corp.kr")
	db.Model(u).Updates(map[string]interface{}{"auth_method": "oidc", "external_id": "sub-1"})

	// SCIM/IdP-managed accounts follow the same transition table — no
	// special casing that would let their state drift outside the machine.
	rec := doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/suspend", `{"reason":"IdP 계정 정지"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("oidc suspend → %d %s", rec.Code, rec.Body.String())
	}
	rec = doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/resume", `{"reason":"IdP 계정 복원"}`, org.ID, "admin", "op@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("oidc resume → %d %s", rec.Code, rec.Body.String())
	}
}

func doMultipart(t *testing.T, srv *Server, method, path, field, filename, content, orgID string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte(content))
	writer.Close()
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: orgID}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}
