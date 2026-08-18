package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
		&models.RepoBaseline{}, &models.ChangeRequest{}, &models.ChangeSet{}, &models.CapabilityLease{}, &models.OrgSetting{},
		&models.BillingFXRate{}, &models.ProjectMember{}, &identity.AdminCredentials{},
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

	rec := doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/harnesses",
		fmt.Sprintf(`{"harness_id":"%s"}`, h.ID), org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("grant failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/users/"+u.ID+"/harnesses", "", org.ID)
	var list []models.Harness
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].ID != h.ID {
		t.Fatalf("binding list wrong: %v", list)
	}
	rec = doUserJSONAs(t, srv, "DELETE", "/api/users/"+u.ID+"/harnesses/"+h.ID, "", org.ID, "admin", "operator@corp.kr")
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
	device := models.Device{OrganizationID: org.ID, UserID: u.ID, Hostname: "leaver-device", Status: "active"}
	db.Create(&device)
	h := models.Harness{Name: "h1", HarnessID: "peer-9", Status: "enrolled",
		DeviceID: device.ID, AllowedUsers: fmt.Sprintf(`["%s"]`, u.ID)}
	h.OrganizationID = org.ID
	db.Create(&h)
	role := models.Role{OrganizationID: org.ID, Name: "leaver-role", NameKo: "퇴사자 역할", Permissions: `[]`}
	db.Create(&role)
	db.Create(&models.UserRole{OrganizationID: org.ID, UserID: u.ID, RoleID: role.ID, Scope: "org"})
	db.Create(&models.EnrollmentCode{OrganizationID: org.ID, Code: "pending-code", UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)})
	lease := models.CapabilityLease{OrganizationID: org.ID, LeaseID: "lease-offboard", SubjectPeerID: h.HarnessID, UserID: u.ID, SessionID: sess.SessionID, PolicyEpochID: "epoch", NotBefore: time.Now().Add(-time.Hour).Format(time.RFC3339), NotAfter: time.Now().Add(time.Hour).Format(time.RFC3339), Status: "active"}
	db.Create(&lease)

	rec := doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/offboard",
		`{"reason":"contract end"}`, org.ID, "admin", "operator@corp.kr")
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
	db.First(&device, "id = ?", device.ID)
	db.First(&lease, "id = ?", lease.ID)
	if device.Status != "revoked" || lease.Status != "revoked" {
		t.Fatalf("offboard left device/lease access: device=%s lease=%s", device.Status, lease.Status)
	}
	var roleCount int64
	db.Model(&models.UserRole{}).Where("organization_id = ? AND user_id = ?", org.ID, u.ID).Count(&roleCount)
	var code models.EnrollmentCode
	db.Where("organization_id = ? AND user_id = ?", org.ID, u.ID).First(&code)
	if roleCount != 0 || !code.Used {
		t.Fatalf("offboard left entitlement/enrollment access: roles=%d code_used=%v", roleCount, code.Used)
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
	rec = doUserJSONAs(t, srv, "PUT", "/api/users/"+u.ID+"/entitlements",
		fmt.Sprintf(`{"roles":[{"role_id":"%s","scope":"org","scope_id":"%s"}]}`, paid.ID, org.ID), org.ID, "admin", "operator@corp.kr")
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
		Meters          []map[string]interface{} `json:"meters"`
		TotalCostMicros int64                    `json:"total_cost_micros,string"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var tokensOut float64
	for _, meter := range resp.Meters {
		if meter["metric_type"] == "tokens_out" {
			parsed, err := strconv.ParseFloat(meter["quantity"].(string), 64)
			if err != nil {
				t.Fatal(err)
			}
			tokensOut = parsed
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

	rec := doUserJSONAs(t, srv, "PUT", "/api/users/"+u.ID+"/contractor",
		`{"sponsor_user_id":"s1","company":"Vendor","contract_start":"2026-02-01","contract_end":"2026-01-01"}`, org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid contract window accepted: %d", rec.Code)
	}
	rec = doUserJSONAs(t, srv, "PUT", "/api/users/"+u.ID+"/contractor",
		`{"sponsor_user_id":"s1","company":"Vendor","contract_start":"2026-01-01","contract_end":"2026-12-31","allowed_repo_ids":["r1"],"allowed_model_classes":["text"],"network_zone":"dmz"}`, org.ID, "admin", "operator@corp.kr")
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

func doUserJSONWithClaims(t *testing.T, srv *Server, method, path, body string, claims *identity.Claims) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req = req.WithContext(contextWithClaims(req.Context(), claims))
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
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, UserID: u.ID, HarnessID: "peer-sm", SessionID: "session-sm", Status: "active"}
	db.Create(&session)
	lease := models.CapabilityLease{OrganizationID: org.ID, UserID: u.ID, SessionID: session.SessionID, LeaseID: "lease-sm", Status: "active"}
	db.Create(&lease)

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
	var suspendResponse struct {
		ClosedSessions int64 `json:"closed_sessions"`
		RevokedLeases  int64 `json:"revoked_leases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &suspendResponse); err != nil {
		t.Fatal(err)
	}
	if suspendResponse.ClosedSessions != 1 || suspendResponse.RevokedLeases != 1 {
		t.Fatalf("suspend evidence wrong: %+v", suspendResponse)
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

func TestUserLifecycleSelfGuardUsesImmutableSubject(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-self-id", Status: "active"}
	db.Create(&org)
	self := mkUser(t, db, org.ID, "old-email@corp.kr")
	db.Model(self).Update("email", "new-email@corp.kr")

	rec := doUserJSONWithClaims(t, srv, "POST", "/api/users/"+self.ID+"/suspend", `{"reason":"must fail"}`, &identity.Claims{
		Email: "old-email@corp.kr", OrganizationID: org.ID, Role: "admin", RegisteredClaims: jwt.RegisteredClaims{Subject: self.ID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("immutable-subject self action got %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestExistingConsoleTokenRejectedAfterLifecycleChange(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-token-life", Status: "active"}
	db.Create(&org)
	user := mkUser(t, db, org.ID, "token-user@corp.kr")
	token, err := srv.auth.IssueToken(user.Email, org.ID, "member")
	if err != nil {
		t.Fatal(err)
	}

	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/users/"+user.ID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(); rec.Code != http.StatusOK {
		t.Fatalf("active token rejected: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := identity.TransitionUserLifecycle(db, identity.UserLifecycleMutation{
		OrganizationID: org.ID, UserID: user.ID, To: models.UserStatusSuspended,
		Reason: "security hold", ActorID: "system", ActorType: "system",
	}); err != nil {
		t.Fatal(err)
	}
	if rec := request(); rec.Code != http.StatusUnauthorized {
		t.Fatalf("pre-existing token after suspension got %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	if _, err := identity.TransitionUserLifecycle(db, identity.UserLifecycleMutation{
		OrganizationID: org.ID, UserID: user.ID, To: models.UserStatusActive,
		Reason: "security hold cleared", ActorID: "system", ActorType: "system",
	}); err != nil {
		t.Fatal(err)
	}
	if rec := request(); rec.Code != http.StatusUnauthorized {
		t.Fatalf("pre-suspension token became valid after resume: %d (%s)", rec.Code, rec.Body.String())
	}
	freshToken, err := srv.auth.IssueToken(user.Email, org.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/users/"+user.ID, nil)
	req.Header.Set("Authorization", "Bearer "+freshToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post-resume token rejected: %d %s", rec.Code, rec.Body.String())
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

func TestGenericUserEditCannotRelinkExternalIdentityProvider(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-auth-method", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "oidc-edit@corp.kr")
	_ = mkUser(t, db, org.ID, "operator@corp.kr")
	db.Model(u).Updates(map[string]interface{}{
		"auth_method": "oidc", "external_id": "subject-1", "external_issuer": "https://idp.example",
	})

	rec := doUserJSONAs(t, srv, http.MethodPut, "/api/users/"+u.ID, `{"auth_method":"local"}`, org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusConflict {
		t.Fatalf("generic auth-method edit returned %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var reloaded models.User
	if err := db.First(&reloaded, "id = ?", u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.AuthMethod != "oidc" || reloaded.ExternalID != "subject-1" || reloaded.ExternalIssuer != "https://idp.example" {
		t.Fatalf("external identity namespace changed: %+v", reloaded)
	}
}

func TestUserLifecycleRequiresAdministratorRole(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-rbac", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "managed@corp.kr")

	for _, role := range []string{"", "member", "viewer", "auditor", "security_admin"} {
		rec := doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/suspend", `{"reason":"unauthorized"}`, org.ID, role, "operator@corp.kr")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("role %q suspended user: got %d, want 403 (%s)", role, rec.Code, rec.Body.String())
		}
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", u.ID)
	if reloaded.Status != "active" {
		t.Fatalf("unauthorized lifecycle request changed status to %s", reloaded.Status)
	}
}

func TestUserAdministrationAndIdentityEntryPointsFailClosed(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-entry-a", Status: "active"}
	other := models.Organization{Name: "other", Slug: "org-entry-b", Status: "active"}
	db.Create(&org)
	db.Create(&other)

	rec := doUserJSONAs(t, srv, "POST", "/api/users", `{"email":"member-create@corp.kr","name":"Member"}`, org.ID, "member", "member@corp.kr")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member created user: got %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	rec = doUserJSONAs(t, srv, "POST", "/api/users", `{"organization_id":"`+other.ID+`","email":"cross-org@corp.kr","name":"Cross"}`, org.ID, "admin", "admin@corp.kr")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin overrode tenant on create: got %d, want 403 (%s)", rec.Code, rec.Body.String())
	}

	suspended := mkUser(t, db, org.ID, "suspended-entry@corp.kr")
	db.Model(suspended).Update("status", models.UserStatusSuspended)
	rec = doUserJSONAs(t, srv, "POST", "/api/sessions", `{"user_id":"`+suspended.ID+`"}`, org.ID, "admin", "admin@corp.kr")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("suspended user opened session: got %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	var sessionCount int64
	db.Model(&models.Session{}).Where("organization_id = ? AND user_id = ?", org.ID, suspended.ID).Count(&sessionCount)
	if sessionCount != 0 {
		t.Fatalf("suspended session denial left %d session rows", sessionCount)
	}

	rec = doUserJSONAs(t, srv, "POST", "/api/harnesses/enroll", `{"organization_id":"`+other.ID+`","harness_id":"cross-org"}`, org.ID, "admin", "admin@corp.kr")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("harness enrollment overrode tenant: got %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
}

func TestUserLifecycleNormalizesSelfIdentityAndRecordsActor(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-actor", Status: "active"}
	db.Create(&org)
	self := mkUser(t, db, org.ID, "Operator@Corp.KR")

	rec := doUserJSONAs(t, srv, "POST", "/api/users/"+self.ID+"/suspend", `{"reason":"case bypass"}`, org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("case-variant self action got %d, want 400 (%s)", rec.Code, rec.Body.String())
	}

	target := mkUser(t, db, org.ID, "target@corp.kr")
	rec = doUserJSONAs(t, srv, "POST", "/api/users/"+target.ID+"/suspend", `{"reason":"security review"}`, org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin suspend failed: %d %s", rec.Code, rec.Body.String())
	}
	var event models.AuditEvent
	if err := db.Where("resource_id = ? AND event_type = ?", target.ID, "cp.user.suspended").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.ActorID != "operator@corp.kr" {
		t.Fatalf("audit actor = %q, want operator email", event.ActorID)
	}
	var details map[string]interface{}
	if err := json.Unmarshal([]byte(event.Details), &details); err != nil {
		t.Fatal(err)
	}
	if details["from"] != "active" || details["to"] != "suspended" || details["reason"] != "security review" {
		t.Fatalf("audit transition details incomplete: %#v", details)
	}
}

func TestUserDetailIsTenantScopedAndProjectsAuthorizedActions(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-detail-a", Status: "active"}
	other := models.Organization{Name: "other", Slug: "org-detail-b", Status: "active"}
	db.Create(&org)
	db.Create(&other)
	u := mkUser(t, db, org.ID, "detail@corp.kr")

	rec := doUserJSONAs(t, srv, "GET", "/api/users/"+u.ID, "", other.ID, "admin", "admin@other.kr")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant detail got %d, want 404", rec.Code)
	}

	rec = doUserJSONAs(t, srv, "GET", "/api/users/"+u.ID, "", org.ID, "admin", "admin@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("detail failed: %d %s", rec.Code, rec.Body.String())
	}
	var detail map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	actions, ok := detail["allowed_actions"].([]interface{})
	if !ok || len(actions) != 2 || actions[0] != "suspend" || actions[1] != "offboard" || detail["can_manage"] != true {
		t.Fatalf("admin allowed-action projection wrong: %#v", detail)
	}

	rec = doUserJSONAs(t, srv, "GET", "/api/users/"+u.ID, "", org.ID, "member", "member@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("member detail failed: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if actions, ok := detail["allowed_actions"].([]interface{}); !ok || len(actions) != 0 || detail["can_manage"] != false {
		t.Fatalf("member received lifecycle actions: %#v", detail)
	}
	if detail["lifecycle_denial_reason"] != "insufficient_role" {
		t.Fatalf("member denial reason = %#v", detail["lifecycle_denial_reason"])
	}
}

func TestUserDetailExplainsLastAdministratorDenial(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-last-admin-view", Status: "active"}
	db.Create(&org)
	admin := mkUser(t, db, org.ID, "only-admin-view@corp.kr")
	operator := mkUser(t, db, org.ID, "other-admin@corp.kr")
	db.Create(&identity.AdminCredentials{Email: admin.Email, Password: "unused", OrganizationID: org.ID, Role: "admin"})

	claims := &identity.Claims{Email: operator.Email, OrganizationID: org.ID, Role: "admin"}
	claims.Subject = operator.ID
	rec := doUserJSONWithClaims(t, srv, "GET", "/api/users/"+admin.ID, "", claims)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", rec.Code, rec.Body.String())
	}
	var row map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["lifecycle_denial_reason"] != "last_administrator" {
		t.Fatalf("denial reason = %v", row["lifecycle_denial_reason"])
	}
	if actions, ok := row["allowed_actions"].([]interface{}); !ok || len(actions) != 0 {
		t.Fatalf("last administrator actions = %#v", row["allowed_actions"])
	}
}

func TestLeaseEndpointCannotMintForAnotherTenantOrInactiveUser(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-lease", Status: "active"}
	other := models.Organization{Name: "other", Slug: "other-lease", Status: "active"}
	db.Create(&org)
	db.Create(&other)
	user := mkUser(t, db, org.ID, "lease-user@corp.kr")
	harness := models.Harness{OrganizationID: org.ID, HarnessID: "lease-peer", Status: "enrolled", AllowedUsers: `["` + user.ID + `"]`}
	db.Create(&harness)
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, HarnessID: harness.HarnessID, UserID: user.ID, SessionID: "lease-session", Status: "active"}
	db.Create(&session)
	epoch := models.PolicyEpoch{OrganizationID: org.ID, EpochID: "lease-epoch", EpochNumber: 1, Status: "active", EffectiveAt: time.Now().Format(time.RFC3339)}
	db.Create(&epoch)
	body := fmt.Sprintf(`{"organization_id":"%s","subject_peer_id":"%s","user_id":"%s","session_id":"%s","policy_epoch_id":"%s","allowed_models":["code"]}`,
		other.ID, harness.HarnessID, user.ID, session.SessionID, epoch.EpochID)
	rec := doUserJSONAs(t, srv, "POST", "/api/policy/leases", body, org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant lease got %d, want 403 (%s)", rec.Code, rec.Body.String())
	}

	db.Model(user).Update("status", models.UserStatusSuspended)
	body = fmt.Sprintf(`{"organization_id":"%s","subject_peer_id":"%s","user_id":"%s","session_id":"%s","policy_epoch_id":"%s","allowed_models":["code"]}`,
		org.ID, harness.HarnessID, user.ID, session.SessionID, epoch.EpochID)
	rec = doUserJSONAs(t, srv, "POST", "/api/policy/leases", body, org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("inactive-user lease got %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	var count int64
	db.Model(&models.CapabilityLease{}).Count(&count)
	if count != 0 {
		t.Fatalf("rejected lease requests persisted %d leases", count)
	}
}

func TestOpenSessionRequiresBoundUserAndRollsBackWithoutGovernance(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-session-bind", Status: "active"}
	db.Create(&org)
	caller := mkUser(t, db, org.ID, "caller@corp.kr")
	other := mkUser(t, db, org.ID, "other@corp.kr")

	rec := doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{"user_id":"`+other.ID+`"}`, &identity.Claims{
		Email: caller.Email, OrganizationID: org.ID, Role: "member", RegisteredClaims: jwt.RegisteredClaims{Subject: caller.ID},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member spoofed another user: %d %s", rec.Code, rec.Body.String())
	}
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{}`, &identity.Claims{
		Email: caller.Email, OrganizationID: org.ID, Role: "member", RegisteredClaims: jwt.RegisteredClaims{Subject: caller.ID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing user binding got %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{"user_id":"`+caller.ID+`"}`, &identity.Claims{
		Email: caller.Email, OrganizationID: org.ID, Role: "member", RegisteredClaims: jwt.RegisteredClaims{Subject: caller.ID},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("session without epoch got %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	var sessions int64
	db.Model(&models.Session{}).Where("organization_id = ?", org.ID).Count(&sessions)
	if sessions != 0 {
		t.Fatalf("failed governed open left %d session rows", sessions)
	}

	epoch := models.PolicyEpoch{OrganizationID: org.ID, EpochID: "session-bind-epoch", EpochNumber: 1, Status: "active"}
	db.Create(&epoch)
	harness := models.Harness{OrganizationID: org.ID, HarnessID: "other-user-harness", PublicKey: "key", AllowedUsers: `["` + other.ID + `"]`, Status: "enrolled"}
	db.Create(&harness)
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{"user_id":"`+caller.ID+`","harness_id":"`+harness.HarnessID+`"}`, &identity.Claims{
		Email: caller.Email, OrganizationID: org.ID, Role: "member", RegisteredClaims: jwt.RegisteredClaims{Subject: caller.ID},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user opened through another user's harness: %d %s", rec.Code, rec.Body.String())
	}
	db.Model(&models.Session{}).Where("organization_id = ?", org.ID).Count(&sessions)
	if sessions != 0 {
		t.Fatalf("rejected harness binding left %d session rows", sessions)
	}

	project := models.Project{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "Bound Project", Status: "active"}
	db.Create(&project)
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{"user_id":"`+caller.ID+`","project_id":"`+project.ID+`"}`, &identity.Claims{
		Email: caller.Email, OrganizationID: org.ID, Role: "member", RegisteredClaims: jwt.RegisteredClaims{Subject: caller.ID},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member opened project session: %d %s", rec.Code, rec.Body.String())
	}
	db.Create(&models.ProjectMember{OrganizationID: org.ID, ProjectID: project.ID, UserID: caller.ID, Role: "member"})
	otherProject := models.Project{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "Other Project", Status: "active"}
	db.Create(&otherProject)
	repository := models.Repository{AuditBase: models.AuditBase{OrganizationID: org.ID}, ProjectID: otherProject.ID, Name: "other-repo", Status: "active"}
	if err := db.Create(&repository).Error; err != nil {
		t.Fatal(err)
	}
	baseline := models.RepoBaseline{RepositoryID: repository.ID, Branch: "main", CommitSHA: "abc123", TreeDigest: "tree123", OrgID: org.ID}
	if err := db.Create(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	var persistedBaseline models.RepoBaseline
	if err := db.Where("id = ? AND repository_id = ? AND org_id = ?", baseline.ID, repository.ID, org.ID).First(&persistedBaseline).Error; err != nil {
		t.Fatalf("baseline fixture did not persist: %v", err)
	}
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{"user_id":"`+caller.ID+`","project_id":"`+project.ID+`","repository_id":"`+repository.ID+`","baseline_id":"`+baseline.ID+`"}`, &identity.Claims{
		Email: caller.Email, OrganizationID: org.ID, Role: "member", RegisteredClaims: jwt.RegisteredClaims{Subject: caller.ID},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-project repository session got %d: %s", rec.Code, rec.Body.String())
	}
	db.Model(&models.Session{}).Where("organization_id = ?", org.ID).Count(&sessions)
	if sessions != 0 {
		t.Fatalf("project authorization failures left %d session rows", sessions)
	}
}

func TestUserOffboardRollsBackEverySideEffectWhenAuditFails(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-rollback", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "rollback@corp.kr")
	sess := models.Session{UserID: u.ID, HarnessID: "peer-rb", SessionID: "sess-rb", Status: "active"}
	sess.OrganizationID = org.ID
	db.Create(&sess)

	callbackName := "pat1489_fail_offboard_audit"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*models.AuditEvent); ok && event.EventType == "cp.user.offboarded" {
			tx.AddError(errors.New("forced audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	rec := doUserJSONAs(t, srv, "POST", "/api/users/"+u.ID+"/offboard", `{"reason":"rollback test"}`, org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("offboard with failed audit got %d, want 500 (%s)", rec.Code, rec.Body.String())
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", u.ID)
	if reloaded.Status != "active" {
		t.Fatalf("failed offboard left user %s, want active", reloaded.Status)
	}
	var reloadedSession models.Session
	db.First(&reloadedSession, "id = ?", sess.ID)
	if reloadedSession.Status != "active" {
		t.Fatalf("failed offboard left session %s, want active", reloadedSession.Status)
	}
}

func TestOffboardedUserIsReadOnlyAndCannotReceiveEnrollment(t *testing.T) {
	srv, db := usersTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-terminal", Status: "active"}
	db.Create(&org)
	u := mkUser(t, db, org.ID, "terminal@corp.kr")
	db.Model(u).Update("status", "offboarded")
	h := models.Harness{Name: "terminal-h", HarnessID: "peer-terminal", Status: "enrolled", AllowedUsers: fmt.Sprintf(`["%s"]`, u.ID)}
	h.OrganizationID = org.ID
	db.Create(&h)
	role := models.Role{OrganizationID: org.ID, Name: "terminal-role", NameKo: "종료 계정 역할", Permissions: `[]`}
	db.Create(&role)

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{"PUT", "/api/users/" + u.ID, `{"title":"must not change"}`},
		{"POST", "/api/users/" + u.ID + "/enrollment-code", ""},
		{"POST", "/api/users/" + u.ID + "/harnesses", fmt.Sprintf(`{"harness_id":"%s"}`, h.ID)},
		{"DELETE", "/api/users/" + u.ID + "/harnesses/" + h.ID, ""},
		{"PUT", "/api/users/" + u.ID + "/entitlements", fmt.Sprintf(`{"roles":[{"role_id":"%s","scope":"org"}]}`, role.ID)},
		{"PUT", "/api/users/" + u.ID + "/contractor", `{"company":"Must Not Change"}`},
	}
	for _, tc := range tests {
		rec := doUserJSONAs(t, srv, tc.method, tc.path, tc.body, org.ID, "admin", "operator@corp.kr")
		if rec.Code != http.StatusConflict {
			t.Errorf("%s %s got %d, want 409 (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	var codes int64
	db.Model(&models.EnrollmentCode{}).Where("organization_id = ? AND user_id = ?", org.ID, u.ID).Count(&codes)
	if codes != 0 {
		t.Fatalf("terminal user received %d enrollment codes", codes)
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
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: orgID, Role: "admin", Email: "operator@corp.kr"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}
