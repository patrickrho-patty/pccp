package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func bgTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/bg.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.CapabilityLease{}, &models.BrowserPolicy{}, &models.BrowserTask{},
		&models.BrowserApproval{}, &models.BrowserActionEvent{},
		&identity.AdminCredentials{},
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

func bgJSON(t *testing.T, srv *Server, method, path, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-bg", Email: "br@patty.dev", Role: role}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

// Takeover-class actions can never be weakened by policy, never
// approved, and never executed.
func TestBGTakeoverBoundary(t *testing.T) {
	srv, db := bgTestServer(t)
	_ = db
	// Policy attempt to allow password_entry → rejected.
	pol := `{"destinations":[{"scheme":"https","host":"shop.example.com"}],"actions":{"password_entry":"allowed"}}`
	if w := bgJSON(t, srv, "PUT", "/api/browsergov/policy", pol, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("takeover weakened via policy: %d %s", w.Code, w.Body.String())
	}
	// Approval request for mfa → 422.
	if w := bgJSON(t, srv, "POST", "/api/browsergov/approvals",
		`{"task_id":"t","effect_type":"mfa","details":{"k":"v"}}`, "admin"); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("takeover approval accepted: %d", w.Code)
	}
	// Effect gate for captcha → 403.
	if w := bgJSON(t, srv, "POST", "/api/browsergov/effects/gate",
		`{"task_id":"t","effect_type":"captcha","details":{},"approval_id":1}`, "admin"); w.Code != http.StatusForbidden {
		t.Fatalf("takeover execution allowed: %d", w.Code)
	}
}

// Policy versioning: signed, superseding, non-https destinations
// rejected, unknown actions rejected; viewer cannot publish.
func TestBGPolicyVersioningAndValidation(t *testing.T) {
	srv, db := bgTestServer(t)
	if w := bgJSON(t, srv, "PUT", "/api/browsergov/policy", `{"destinations":[],"actions":{}}`, "viewer"); w.Code != http.StatusForbidden {
		t.Fatalf("viewer published policy: %d", w.Code)
	}
	bad := `{"destinations":[{"scheme":"http","host":"x.com"}],"actions":{}}`
	if w := bgJSON(t, srv, "PUT", "/api/browsergov/policy", bad, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("http destination accepted: %d", w.Code)
	}
	unknown := `{"destinations":[],"actions":{"format_disk":"allowed"}}`
	if w := bgJSON(t, srv, "PUT", "/api/browsergov/policy", unknown, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown action accepted: %d", w.Code)
	}
	good := `{"destinations":[{"scheme":"https","host":"shop.example.com"}],"actions":{"place_order":"approval"}}`
	wrapped := fmt.Sprintf(`{"policy_json":%q}`, good)
	if w := bgJSON(t, srv, "PUT", "/api/browsergov/policy", wrapped, "admin"); w.Code != http.StatusCreated {
		t.Fatalf("valid policy rejected: %d %s", w.Code, w.Body.String())
	}
	var p1 models.BrowserPolicy
	db.First(&p1)
	if p1.Version != 1 || !p1.Active || p1.Signature == "" {
		t.Fatalf("policy v1 not signed/active: %+v", p1)
	}
	// Second version supersedes the first.
	bgJSON(t, srv, "PUT", "/api/browsergov/policy", wrapped, "admin")
	var p2 models.BrowserPolicy
	db.Order("version DESC").First(&p2)
	if p2.Version != 2 {
		t.Fatalf("version did not advance: %d", p2.Version)
	}
	db.First(&p1, "version = ?", 1)
	if p1.Active {
		t.Fatal("old version still active")
	}
	// Effective policy GET returns signature + foundations.
	w := bgJSON(t, srv, "GET", "/api/browsergov/policy", "", "viewer")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["version"] != float64(2) || resp["signature"] == nil {
		t.Fatalf("effective policy wrong: %v", resp["version"])
	}
	if _, ok := resp["foundations_ko"].([]interface{}); !ok {
		t.Fatal("foundations summary missing")
	}
}

// Task lifecycle: tabs required, lease narrowed to read-only+reversible,
// close revokes the lease.
func TestBGTaskLifecycle(t *testing.T) {
	srv, db := bgTestServer(t)
	// No tabs → 400 (privacy boundary: explicit attachment only).
	if w := bgJSON(t, srv, "POST", "/api/browsergov/tasks",
		`{"harness_id":"h1","user_id":"u1","goal_ko":"양말 구매","tabs":[]}`, "viewer"); w.Code != http.StatusBadRequest {
		t.Fatalf("tab-less task accepted: %d", w.Code)
	}
	w := bgJSON(t, srv, "POST", "/api/browsergov/tasks",
		`{"harness_id":"h1","user_id":"u1","session_id":"s1","tabs":["tab-1"],"goal_ko":"양말 구매"}`, "viewer")
	if w.Code != http.StatusCreated {
		t.Fatalf("task create: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	taskID := resp["task_id"].(string)
	var lease models.CapabilityLease
	db.Where("lease_id = ?", resp["lease_id"]).First(&lease)
	if lease.ToolClasses != `["browser.read_only","browser.reversible"]` {
		t.Fatalf("lease too broad: %s", lease.ToolClasses)
	}
	// Close revokes.
	if w := bgJSON(t, srv, "POST", fmt.Sprintf("/api/browsergov/tasks/%s/close", taskID),
		`{"outcome":"completed"}`, "viewer"); w.Code != http.StatusOK {
		t.Fatalf("close: %d", w.Code)
	}
	db.First(&lease, "lease_id = ?", resp["lease_id"])
	if lease.Status != "revoked" {
		t.Fatalf("lease not revoked on close: %s", lease.Status)
	}
}

// Approval boundary: exact-digest binding, material change invalidates,
// single-use, expiry.
func TestBGApprovalExactEffectBinding(t *testing.T) {
	srv, db := bgTestServer(t)
	_ = db
	taskID := bgCreateTask(t, srv)
	// Request approval for a purchase with exact details.
	details := `{"product":"양말 5켤레","seller":"shop.example.com","quantity":1,"unit_price":15900,"shipping":2500,"payment":"**** 1234","total":18400}`
	w := bgJSON(t, srv, "POST", "/api/browsergov/approvals",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"place_order","details":%s}`, taskID, details), "viewer")
	if w.Code != http.StatusCreated {
		t.Fatalf("approval request: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	approvalID := uint(resp["approval_id"].(float64))
	// Read-only action cannot request an approval.
	if w := bgJSON(t, srv, "POST", "/api/browsergov/approvals",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"navigate","details":{}}`, taskID), "viewer"); w.Code != http.StatusBadRequest {
		t.Fatalf("read-only approval accepted: %d", w.Code)
	}
	// Approve via the terminal path.
	if w := bgJSON(t, srv, "POST", fmt.Sprintf("/api/browsergov/approvals/%d/decide", approvalID),
		`{"approve":true,"reason":"주문 확인"}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("decide: %d", w.Code)
	}
	// Gate with the SAME details → authorized once with a deterministic id.
	w = bgJSON(t, srv, "POST", "/api/browsergov/effects/gate",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"place_order","details":%s,"approval_id":%d}`, taskID, details, approvalID), "viewer")
	if w.Code != http.StatusOK {
		t.Fatalf("gate: %d %s", w.Code, w.Body.String())
	}
	var gate map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &gate)
	opID := gate["effect_op_id"].(string)
	// Second use of the same approval → 409 (single-use; duplicate
	// effects impossible).
	w = bgJSON(t, srv, "POST", "/api/browsergov/effects/gate",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"place_order","details":%s,"approval_id":%d}`, taskID, details, approvalID), "viewer")
	if w.Code != http.StatusConflict {
		t.Fatalf("approval reused: %d", w.Code)
	}
	// A NEW approval, but the price changed → material-change invalidation.
	details2 := `{"product":"양말 5켤레","seller":"shop.example.com","quantity":1,"unit_price":18900,"shipping":2500,"payment":"**** 1234","total":21400}`
	w = bgJSON(t, srv, "POST", "/api/browsergov/approvals",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"place_order","details":%s}`, taskID, details2), "viewer")
	json.Unmarshal(w.Body.Bytes(), &resp)
	approvalID2 := uint(resp["approval_id"].(float64))
	bgJSON(t, srv, "POST", fmt.Sprintf("/api/browsergov/approvals/%d/decide", approvalID2), `{"approve":true}`, "admin")
	w = bgJSON(t, srv, "POST", "/api/browsergov/effects/gate",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"place_order","details":%s,"approval_id":%d}`, taskID, details, approvalID2), "viewer")
	if w.Code != http.StatusConflict {
		t.Fatalf("changed details accepted: %d", w.Code)
	}
	_ = opID
}

// Approval expiry: an expired pending approval cannot be decided.
func TestBGApprovalExpiry(t *testing.T) {
	srv, db := bgTestServer(t)
	_ = db
	taskID := bgCreateTask(t, srv)
	w := bgJSON(t, srv, "POST", "/api/browsergov/approvals",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"submit_form","details":{"form":"회원가입"}}`, taskID), "viewer")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	id := uint(resp["approval_id"].(float64))
	var ap models.BrowserApproval
	db.First(&ap, "id = ?", id)
	db.Model(&ap).Update("expires_at", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339))
	if w := bgJSON(t, srv, "POST", fmt.Sprintf("/api/browsergov/approvals/%d/decide", id), `{"approve":true}`, "admin"); w.Code != http.StatusConflict {
		t.Fatalf("expired approval decided: %d", w.Code)
	}
}

// Evidence: risk class derived server-side, origin redacted to
// scheme://host, timeline returns the attributable chain.
func TestBGEvidenceRedactionAndTimeline(t *testing.T) {
	srv, db := bgTestServer(t)
	taskID := bgCreateTask(t, srv)
	w := bgJSON(t, srv, "POST", "/api/browsergov/events",
		fmt.Sprintf(`{"task_id":%q,"action":"navigate","target_summary":"상품 목록","origin":"https://shop.example.com/products?session_token=SECRET123&cart=1","result":"ok"}`, taskID), "viewer")
	if w.Code != http.StatusCreated {
		t.Fatalf("event: %d %s", w.Code, w.Body.String())
	}
	var ev models.BrowserActionEvent
	db.First(&ev, "task_id = ?", taskID)
	if ev.Origin != "https://shop.example.com" {
		t.Fatalf("origin not redacted: %q", ev.Origin)
	}
	if ev.RiskClass != brRiskReadOnly {
		t.Fatalf("risk class not derived: %s", ev.RiskClass)
	}
	// Untrusted client risk-class claims cannot inject unknown actions.
	if w := bgJSON(t, srv, "POST", "/api/browsergov/events",
		fmt.Sprintf(`{"task_id":%q,"action":"format_disk","result":"ok"}`, taskID), "viewer"); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown action recorded: %d", w.Code)
	}
	// Timeline.
	w = bgJSON(t, srv, "GET", fmt.Sprintf("/api/browsergov/tasks/%s/timeline", taskID), "", "viewer")
	var tl struct {
		Events []models.BrowserActionEvent `json:"events"`
	}
	json.Unmarshal(w.Body.Bytes(), &tl)
	if len(tl.Events) != 1 || tl.Events[0].IntegrityDigest == "" {
		t.Fatalf("timeline wrong: %+v", tl.Events)
	}
}

// Policy explain: taxonomy defaults + managed overrides.
func TestBGPolicyExplain(t *testing.T) {
	srv, _ := bgTestServer(t)
	w := bgJSON(t, srv, "GET", "/api/browsergov/policy/explain?action=place_order", "", "viewer")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["verdict"] != "approval" {
		t.Fatalf("place_order default: %v", resp["verdict"])
	}
	w = bgJSON(t, srv, "GET", "/api/browsergov/policy/explain?action=captcha", "", "viewer")
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["verdict"] != "takeover" {
		t.Fatalf("captcha default: %v", resp["verdict"])
	}
}

func bgCreateTask(t *testing.T, srv *Server) string {
	t.Helper()
	w := bgJSON(t, srv, "POST", "/api/browsergov/tasks",
		`{"harness_id":"h1","user_id":"u1","tabs":["tab-1"],"goal_ko":"테스트"}`, "viewer")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["task_id"].(string)
}

// Effect-gate expiry: an approved-but-aging approval cannot execute
// past its TTL even though decide-time succeeded.
func TestBGEffectGateExpiry(t *testing.T) {
	srv, db := bgTestServer(t)
	taskID := bgCreateTask(t, srv)
	w := bgJSON(t, srv, "POST", "/api/browsergov/approvals",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"submit_form","details":{"form":"가입"}}`, taskID), "viewer")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	id := uint(resp["approval_id"].(float64))
	bgJSON(t, srv, "POST", fmt.Sprintf("/api/browsergov/approvals/%d/decide", id), `{"approve":true}`, "admin")
	// Age the approval past its TTL, then attempt the gate.
	var ap models.BrowserApproval
	db.First(&ap, "id = ?", id)
	db.Model(&ap).Update("expires_at", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339))
	if w := bgJSON(t, srv, "POST", "/api/browsergov/effects/gate",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"submit_form","details":{"form":"가입"},"approval_id":%d}`, taskID, id), "viewer"); w.Code != http.StatusConflict {
		t.Fatalf("expired approval executed: %d", w.Code)
	}
}

// Approval decide authorization: the delegating user or an admin —
// not any org member.
func TestBGApprovalDecideAuthorization(t *testing.T) {
	srv, db := bgTestServer(t)
	_ = db
	taskID := bgCreateTask(t, srv) // created by br@patty.dev (claims email)
	w := bgJSON(t, srv, "POST", "/api/browsergov/approvals",
		fmt.Sprintf(`{"task_id":%q,"effect_type":"submit_form","details":{"form":"x"}}`, taskID), "viewer")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	id := uint(resp["approval_id"].(float64))
	// Another org member's viewer tries to decide → 403.
	other := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, bytes.NewReader([]byte(body)))
		req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-bg", Email: "someoneelse@patty.dev", Role: "viewer"}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)
		return w
	}
	if w := other(fmt.Sprintf("/api/browsergov/approvals/%d/decide", id), `{"approve":true}`); w.Code != http.StatusForbidden {
		t.Fatalf("non-delegating user decided: %d", w.Code)
	}
	// Admin still can.
	if w := bgJSON(t, srv, "POST", fmt.Sprintf("/api/browsergov/approvals/%d/decide", id), `{"approve":true}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("admin decide rejected: %d", w.Code)
	}
}

// Origin redaction drops userinfo credentials via net/url.
func TestBGOriginUserInfoRedaction(t *testing.T) {
	srv, db := bgTestServer(t)
	taskID := bgCreateTask(t, srv)
	bgJSON(t, srv, "POST", "/api/browsergov/events",
		fmt.Sprintf(`{"task_id":%q,"action":"navigate","origin":"https://user:secretpass@shop.example.com/p?x=1","result":"ok"}`, taskID), "viewer")
	var ev models.BrowserActionEvent
	db.First(&ev, "task_id = ?", taskID)
	if ev.Origin != "https://shop.example.com" {
		t.Fatalf("userinfo leaked into evidence: %q", ev.Origin)
	}
}
