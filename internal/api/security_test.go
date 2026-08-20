package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func securityTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/sec.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Session{}, &models.Harness{}, &models.Project{},
		&models.SecurityFinding{}, &models.SecurityRule{}, &models.AlertEndpoint{},
		&models.AlertDeliveryJob{}, &identity.AdminCredentials{},
		&models.PIILexicon{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.PromptExchange{}, &models.UsageRecord{}, &models.ModelPackage{}, &models.InferenceEndpoint{},
		&models.OrgSetting{}, &models.BillingFXRate{}, &models.SandboxRecord{},
		&models.SecurityLockdown{}, &models.Approval{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	// PAT-1502 PR 2: wire a local KeyProvider so the write paths
	// (create/rotate/test) can seal targets. A 32-byte deterministic
	// KEK derived from the test JWT secret keeps test runs hermetic.
	var kek [32]byte
	for i := range kek {
		kek[i] = byte('A' + (i % 26))
	}
	provider, err := keymgmt.NewLocalProvider(kek[:], "test-kek-v1")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetKeyProvider(provider)
	return srv, db
}

func TestSecurityRuleTogglePersists(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o", Status: "active"}
	db.Create(&org)
	// Seed rules then toggle one off; GET must reflect the change.
	srv.security.EnsureRulesSeeded(org.ID)
	rec := doJSON(t, srv, "PUT", "/api/security/policy", `{"rule_id":"secret-aws-key","enabled":false}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/security/rules", "", org.ID)
	var rules []models.SecurityRule
	json.Unmarshal(rec.Body.Bytes(), &rules)
	for _, r := range rules {
		if r.RuleID == "secret-aws-key" && r.Enabled {
			t.Fatal("secret-aws-key should be disabled after toggle")
		}
	}
}

func TestSuppressAndReopenEndpoints(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o2", Status: "active"}
	db.Create(&org)
	f := models.SecurityFinding{OrganizationID: org.ID, FindingType: "pii", Severity: "high", Title: "x", Status: "open", OccurredAt: "2026-01-01T00:00:00Z"}
	db.Create(&f)

	rec := doJSON(t, srv, "POST", "/api/security/findings/"+f.ID+"/suppress", `{"reason":"test data","days":30}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("suppress failed: %d %s", rec.Code, rec.Body.String())
	}
	var stored models.SecurityFinding
	db.First(&stored, "id = ?", f.ID)
	if !stored.Suppressed || stored.Status != "suppressed" {
		t.Fatalf("suppress not persisted: %+v", stored)
	}
	rec = doJSON(t, srv, "POST", "/api/security/findings/"+f.ID+"/reopen", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("reopen failed: %d", rec.Code)
	}
	db.First(&stored, "id = ?", f.ID)
	if stored.Suppressed || stored.Status != "open" {
		t.Fatalf("reopen not persisted: %+v", stored)
	}
}

func TestLockdownProjectScope(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o3", Status: "active"}
	db.Create(&org)
	proj1 := models.Project{Name: "p1", Slug: "p1", Status: "active"}
	proj1.OrganizationID = org.ID
	db.Create(&proj1)
	proj2 := models.Project{Name: "p2", Slug: "p2", Status: "active"}
	proj2.OrganizationID = org.ID
	db.Create(&proj2)
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, ProjectID: proj1.ID, SessionID: "s1", Status: "active", HarnessID: "h1", UserID: "u1"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, ProjectID: proj2.ID, SessionID: "s2", Status: "active", HarnessID: "h2", UserID: "u2"})

	rec := doSessionJSONWithPermissions(t, srv, "POST", "/api/security/lockdown", `{"scope":"project","project_id":"`+proj1.ID+`","reason":"incident"}`, org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("lockdown failed: %d %s", rec.Code, rec.Body.String())
	}
	var s1, s2 models.Session
	db.First(&s1, "session_id = ?", "s1")
	db.First(&s2, "session_id = ?", "s2")
	if s1.Status != "terminated" {
		t.Fatalf("project-scoped session should terminate: %s", s1.Status)
	}
	if s2.Status != "active" {
		t.Fatalf("out-of-scope session should stay active: %s", s2.Status)
	}
}

func TestLockdownRejectsUnknownScopeWithoutMutation(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "lockdown-invalid", Status: "active"}
	db.Create(&org)
	sess := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, SessionID: "invalid-scope", Status: "active", HarnessID: "h1", UserID: "u1"}
	db.Create(&sess)

	rec := doSessionJSONWithPermissions(t, srv, "POST", "/api/security/lockdown", `{"scope":"typo","reason":"incident"}`, org.ID, "admin")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown scope status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	db.First(&sess, "id = ?", sess.ID)
	if sess.Status != "active" {
		t.Fatalf("unknown scope triggered org lockdown: %s", sess.Status)
	}
}

func TestLockdownImpactMatchesProjectLifecycleScope(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "lockdown-impact", Status: "active"}
	db.Create(&org)
	project := models.Project{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "p", Slug: "impact", Status: "active"}
	db.Create(&project)
	for i := 0; i < 2; i++ {
		db.Create(&models.Harness{OrganizationID: org.ID, HarnessID: fmt.Sprintf("h%d", i), Name: "harness", PublicKey: fmt.Sprintf("key-%d", i), Status: "active"})
	}
	for i, status := range []string{"pending", "active", "idle", "paused"} {
		db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, ProjectID: project.ID, SessionID: "impact-" + status, Status: status, HarnessID: fmt.Sprintf("h%d", i%2), UserID: "u1"})
	}
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, ProjectID: project.ID, SessionID: "impact-closed", Status: "closed", HarnessID: "h3", UserID: "u1"})

	rec := doSessionJSONWithPermissions(t, srv, "GET", "/api/security/lockdown-impact?scope=project&project_id="+project.ID, "", org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("impact status = %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["in_progress_sessions"] != float64(4) || body["affected_harnesses"] != float64(2) {
		t.Fatalf("impact = %#v", body)
	}
}

func TestFindingsServerFilters(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o4", Status: "active"}
	db.Create(&org)
	db.Create(&models.SecurityFinding{OrganizationID: org.ID, FindingType: "pii", Severity: "critical", Title: "c1", Status: "open", OccurredAt: "2026-08-01T00:00:00Z"})
	db.Create(&models.SecurityFinding{OrganizationID: org.ID, FindingType: "secret", Severity: "low", Title: "l1", Status: "resolved", OccurredAt: "2026-08-02T00:00:00Z"})

	rec := doJSON(t, srv, "GET", "/api/security/findings?severity=critical&page=1&size=25", "", org.ID)
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	list := resp["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("critical filter should return 1, got %d", len(list))
	}
}

func TestLexiconEndpointRoundTrip(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o5", Status: "active"}
	db.Create(&org)
	rec := doJSON(t, srv, "PUT", "/api/security/lexicon", `{"version":"7","patterns":{"pii-kr-rrn":"\\bXXXXX\\d{2}-\\d{7}\\b"}}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("lexicon publish failed: %d %s", rec.Code, rec.Body.String())
	}
	var lexicon models.PIILexicon
	db.First(&lexicon, "organization_id = ?", org.ID)
	if lexicon.Version != "7" {
		t.Fatalf("lexicon version wrong: %s", lexicon.Version)
	}
}

// --- PAT-1433: catalog seeding, severity persistence, scoped overrides ---

func TestSecurityRulesEndpointSeedsCatalog(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "seed-org", Slug: "seed-org", Status: "active"}
	db.Create(&org)
	// NO explicit EnsureRulesSeeded: the endpoint itself must seed.
	rec := doJSON(t, srv, "GET", "/api/security/rules", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET rules: %d %s", rec.Code, rec.Body.String())
	}
	var rules []models.SecurityRule
	if err := json.Unmarshal(rec.Body.Bytes(), &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) < 40 {
		t.Fatalf("authoritative catalog expected (>=40 rules), got %d", len(rules))
	}
	sawPath := false
	for _, r := range rules {
		if r.RuleID == "path-etc-passwd" {
			sawPath = true
		}
	}
	if !sawPath {
		t.Fatal("catalog must include sensitive_path rules (the old UI presets never showed these)")
	}
}

func TestSecurityRuleSeverityPersists(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "sev-org", Slug: "sev-org", Status: "active"}
	db.Create(&org)
	rec := doJSON(t, srv, "PUT", "/api/security/policy", `{"rule_id":"pii-kr-phone","severity":"low"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("severity PUT failed: %d %s", rec.Code, rec.Body.String())
	}
	// Invalid vocabulary must be rejected.
	rec = doJSON(t, srv, "PUT", "/api/security/policy", `{"rule_id":"pii-kr-phone","severity":"ultra"}`, org.ID)
	if rec.Code == http.StatusOK {
		t.Fatal("invalid severity must be rejected")
	}
	rec = doJSON(t, srv, "GET", "/api/security/rules", "", org.ID)
	var rules []models.SecurityRule
	json.Unmarshal(rec.Body.Bytes(), &rules)
	for _, r := range rules {
		if r.RuleID == "pii-kr-phone" && r.Severity != "low" {
			t.Fatalf("pii-kr-phone severity = %q, want low", r.Severity)
		}
	}
}

func TestRuleOverrideEndpoints(t *testing.T) {
	srv, db := securityTestServer(t)
	if err := db.AutoMigrate(&models.SecurityRuleOverride{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "ovr-org", Slug: "ovr-org", Status: "active"}
	db.Create(&org)
	srv.security.EnsureRulesSeeded(org.ID)

	// PUT: user-scoped delta disabling kr-phone + lowering severity.
	body := `{"scope_level":"user","scope_id":"user-7","rule_id":"pii-kr-phone","enabled":false,"severity":"low"}`
	rec := doJSON(t, srv, "PUT", "/api/security/rules/overrides", body, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT override: %d %s", rec.Code, rec.Body.String())
	}
	// Pure no-op override must be rejected.
	rec = doJSON(t, srv, "PUT", "/api/security/rules/overrides", `{"scope_level":"user","scope_id":"user-7","rule_id":"pii-kr-rrn"}`, org.ID)
	if rec.Code == http.StatusOK {
		t.Fatal("inherit-only override must be rejected")
	}
	// GET: the delta is listed.
	rec = doJSON(t, srv, "GET", "/api/security/rules/overrides?scope_level=user&scope_id=user-7", "", org.ID)
	var overrides []models.SecurityRuleOverride
	if err := json.Unmarshal(rec.Body.Bytes(), &overrides); err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 1 || overrides[0].RuleID != "pii-kr-phone" {
		t.Fatalf("override list = %+v", overrides)
	}
	if overrides[0].Enabled == nil || *overrides[0].Enabled || overrides[0].Severity != "low" {
		t.Fatalf("override content wrong: %+v", overrides[0])
	}
	// DELETE: revert to inherit.
	rec = doJSON(t, srv, "DELETE", "/api/security/rules/overrides", `{"scope_level":"user","scope_id":"user-7","rule_id":"pii-kr-phone"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE override: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/security/rules/overrides?scope_level=user&scope_id=user-7", "", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &overrides)
	if len(overrides) != 0 {
		t.Fatalf("override must be reverted, got %+v", overrides)
	}
}

// PAT-1484 contract: the dashboard "미해결 심각·높음 보안 발견" KPI must open a
// findings list scoped to severity in (critical,high) AND status != resolved,
// and the two must reconcile (card count == destination list count) through the
// shared canonical scope builder.
func TestDashboardFindingKPIScopeReconcilesWithList(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-kpi", Status: "active"}
	db.Create(&org)

	seed := []models.SecurityFinding{
		{OrganizationID: org.ID, FindingType: "secret", Severity: "critical", Title: "c1", Status: "open", OccurredAt: "2026-01-01T00:00:00Z"},
		{OrganizationID: org.ID, FindingType: "secret", Severity: "high", Title: "h1", Status: "investigating", OccurredAt: "2026-01-01T00:00:01Z"},
		{OrganizationID: org.ID, FindingType: "secret", Severity: "critical", Title: "c2", Status: "resolved", OccurredAt: "2026-01-01T00:00:02Z"}, // excluded: resolved
		{OrganizationID: org.ID, FindingType: "secret", Severity: "medium", Title: "m1", Status: "open", OccurredAt: "2026-01-01T00:00:03Z"},       // excluded: not critical/high
		{OrganizationID: org.ID, FindingType: "secret", Severity: "high", Title: "h2", Status: "suppressed", OccurredAt: "2026-01-01T00:00:04Z"},   // included: != resolved
	}
	for _, f := range seed {
		db.Create(&f)
	}

	// Destination list: /api/security/findings?severity=critical,high&status=unresolved
	rec := doJSON(t, srv, "GET", "/api/security/findings?severity=critical,high&status=unresolved", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped list failed: %d %s", rec.Code, rec.Body.String())
	}
	var list []models.SecurityFinding
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("scoped list count = %d, want 3 (c1, h1, h2)", len(list))
	}
	seen := map[string]bool{}
	for _, f := range list {
		seen[f.Title] = true
		if f.Severity != "critical" && f.Severity != "high" {
			t.Fatalf("list leaked severity %q for %s", f.Severity, f.Title)
		}
		if f.Status == "resolved" {
			t.Fatalf("list leaked resolved finding %s", f.Title)
		}
	}
	for _, want := range []string{"c1", "h1", "h2"} {
		if !seen[want] {
			t.Fatalf("scoped list missing %q", want)
		}
	}

	// Dashboard KPI: open_critical_findings must equal the scoped list count.
	rec = doJSON(t, srv, "GET", "/api/dashboard", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard failed: %d", rec.Code)
	}
	var dash map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &dash); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	kpi, _ := dash["open_critical_findings"].(float64)
	if int(kpi) != 3 {
		t.Fatalf("dashboard open_critical_findings = %v, want 3 (reconcile with list)", kpi)
	}

	// Clear (no status filter) returns all findings regardless.
	rec = doJSON(t, srv, "GET", "/api/security/findings?severity=critical", "", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 2 {
		t.Fatalf("severity-only filter count = %d, want 2 (c1,c2)", len(list))
	}
}

// PAT-1490 contract: repository "보안 발견" count drills to a findings list
// scoped by the repository (via sessions) — the list filters by the same
// parent scope so the destination reconciles with the source count.
func TestRepositoryScopedFindingsAndSessions(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-repo", Status: "active"}
	db.Create(&org)
	repo := models.Repository{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "pay", FullName: "o/pay", Status: "active", DefaultBranch: "main"}
	db.Create(&repo)
	// sessions on this repo (one per status) + one on another repo
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, SessionID: "ses_r1", RepositoryID: repo.ID, UserID: "u1", HarnessID: "h1", Status: "active", Title: "refund"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, SessionID: "ses_r2", RepositoryID: repo.ID, UserID: "u1", HarnessID: "h1", Status: "closed", Title: "old"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, SessionID: "ses_other", RepositoryID: "repo-other", UserID: "u1", HarnessID: "h1", Status: "active", Title: "x"})
	db.Create(&models.SecurityFinding{OrganizationID: org.ID, FindingType: "secret", Severity: "high", Title: "repo finding", Status: "open", SessionID: "ses_r1", OccurredAt: "2026-01-01T00:00:00Z"})
	db.Create(&models.SecurityFinding{OrganizationID: org.ID, FindingType: "secret", Severity: "high", Title: "old finding", Status: "open", SessionID: "ses_r2", OccurredAt: "2026-01-01T00:00:00Z"})
	db.Create(&models.SecurityFinding{OrganizationID: org.ID, FindingType: "secret", Severity: "high", Title: "other repo", Status: "open", SessionID: "ses_other", OccurredAt: "2026-01-01T00:00:00Z"})

	// Scoped findings: only the two session of THIS repo, not the other repo.
	rec := doJSON(t, srv, "GET", "/api/security/findings?repository="+repo.ID, "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped findings failed: %d %s", rec.Code, rec.Body.String())
	}
	var list []models.SecurityFinding
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 2 {
		t.Fatalf("repo-scoped findings = %d, want 2 (repo finding + old finding)", len(list))
	}
	for _, f := range list {
		if f.SessionID == "ses_other" {
			t.Fatalf("repo scope leaked other-repo finding %q", f.Title)
		}
	}

	// Scoped sessions: active ones on THIS repo only (status=active&repository).
	rec = doJSON(t, srv, "GET", "/api/sessions?status=active&repository="+repo.ID+"&page=1&size=25", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped sessions failed: %d", rec.Code)
	}
	var paged struct {
		Data  []models.Session `json:"data"`
		Total int64            `json:"total"`
	}
	json.Unmarshal(rec.Body.Bytes(), &paged)
	if len(paged.Data) != 1 || paged.Data[0].SessionID != "ses_r1" {
		t.Fatalf("repo+active sessions = %d (want 1, ses_r1)", len(paged.Data))
	}
}

// PAT-1487 contract: the dashboard metric dictionary resolves every repeated
// security-finding count through the SAME canonical scope contracts as the
// destination lists, and intentionally-different scopes carry distinct
// canonical keys. A fixture must produce reconcilable card ↔ list counts.
func TestDashboardMetricDictionaryReconcilesPAT1487(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-met", Status: "active"}
	db.Create(&org)
	seed := []models.SecurityFinding{
		{OrganizationID: org.ID, FindingType: "secret", Severity: "critical", Title: "c1", Status: "open", OccurredAt: "2026-01-01T00:00:00Z"},
		{OrganizationID: org.ID, FindingType: "secret", Severity: "high", Title: "h1", Status: "investigating", OccurredAt: "2026-01-01T00:00:01Z"},
		{OrganizationID: org.ID, FindingType: "secret", Severity: "critical", Title: "c2", Status: "resolved", OccurredAt: "2026-01-01T00:00:02Z"},
		{OrganizationID: org.ID, FindingType: "secret", Severity: "medium", Title: "m1", Status: "open", OccurredAt: "2026-01-01T00:00:03Z"},
		{OrganizationID: org.ID, FindingType: "secret", Severity: "high", Title: "h2", Status: "suppressed", OccurredAt: "2026-01-01T00:00:04Z"},
	}
	for _, f := range seed {
		db.Create(&f)
	}

	rec := doJSON(t, srv, "GET", "/api/dashboard", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard failed: %d", rec.Code)
	}
	var dash map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &dash)

	// open_critical_findings = severity IN (critical,high) AND status != resolved → c1, h1, h2
	if got := int(dash["open_critical_findings"].(float64)); got != 3 {
		t.Fatalf("open_critical_findings = %d, want 3", got)
	}
	// unresolved_findings = any severity AND status != resolved → c1, h1, m1, h2
	if got := int(dash["unresolved_findings"].(float64)); got != 4 {
		t.Fatalf("unresolved_findings = %d, want 4", got)
	}
	// total_findings = all → 5
	if got := int(dash["total_findings"].(float64)); got != 5 {
		t.Fatalf("total_findings = %d, want 5", got)
	}
	if _, ok := dash["dashboard_last_updated"]; !ok {
		t.Fatalf("dashboard_last_updated missing")
	}

	// Count/equivalence: the scoped list for the unresolved metric matches the
	// dashboard unresolved_findings count exactly.
	rec = doJSON(t, srv, "GET", "/api/security/findings?status=unresolved", "", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &seed)
	// reuse slice; list is a raw array (non-paged) — recompute length
	var list []models.SecurityFinding
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 4 {
		t.Fatalf("unresolved list count = %d, want 4 (reconcile with card)", len(list))
	}
}

// PAT-1488 contract: the admin action center backs every group with a real
// server-side count using the same scope contract as its destination queue —
// quarantined harnesses and pending approvals reconcile with their lists, and
// no group is ever manufactured from a status the model cannot provide.
func TestDashboardActionCenterMetricsReconcilePAT1488(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ac", Status: "active"}
	db.Create(&org)
	// quarantined harness (counted) vs active (not counted)
	db.Create(&models.Harness{OrganizationID: org.ID, HarnessID: "hrn_q1", Status: "quarantined"})
	db.Create(&models.Harness{OrganizationID: org.ID, HarnessID: "hrn_a1", Status: "active"})
	// pending approvals (counted) vs decided (not counted)
	db.Create(&models.Approval{OrganizationID: org.ID, ApprovalType: "tool_use_bash", Decision: "pending"})
	db.Create(&models.Approval{OrganizationID: org.ID, ApprovalType: "tool_use_bash", Decision: "approved"})

	rec := doJSON(t, srv, "GET", "/api/dashboard", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard failed: %d", rec.Code)
	}
	var dash map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &dash)
	if got := int(dash["quarantined_harnesses"].(float64)); got != 1 {
		t.Fatalf("quarantined_harnesses = %d, want 1", got)
	}
	if got := int(dash["pending_approvals"].(float64)); got != 1 {
		t.Fatalf("pending_approvals = %d, want 1", got)
	}
}

// PAT-1508 contract: unsafe/invalid lexicon patterns cannot publish — the
// server rejects them (mirroring the UI validator) so they never reach the
// detector.
func TestLexiconRejectsUnsafeAndInvalidPatternsPAT1508(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-lx", Status: "active"}
	db.Create(&org)

	// Invalid regex syntax → rejected
	rec := doJSON(t, srv, "PUT", "/api/security/lexicon", `{"version":"1","patterns":{"kr-x":"(unclosed"}}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid regex publish = %d, want 400", rec.Code)
	}

	// Catastrophic nested quantifier → rejected
	rec = doJSON(t, srv, "PUT", "/api/security/lexicon", `{"version":"1","patterns":{"kr-x":"(a+)+$"}}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsafe regex publish = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	// Empty lexicon → rejected
	rec = doJSON(t, srv, "PUT", "/api/security/lexicon", `{"version":"1","patterns":{}}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty lexicon publish = %d, want 400", rec.Code)
	}

	// Valid pattern still publishes
	rec = doJSON(t, srv, "PUT", "/api/security/lexicon", `{"version":"2","patterns":{"pii-kr-rrn":"\\b[0-9]{6}-[1-4][0-9]{6}\\b"}}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid publish = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}
