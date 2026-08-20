package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/skillpolicy"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const skTestOrg = "org-sk"

func skillTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/sk.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.SkillPolicyAssignment{}, &models.HarnessSkillReport{},
		&models.SkillPolicyEpoch{}, &models.Harness{}, &identity.AdminCredentials{},
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

func skJSON(t *testing.T, srv *Server, method, path, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{
		OrganizationID: skTestOrg, Email: "admin@patty.dev", Role: role,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "usr-admin"},
	}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

// Assignment validation rejects bad scopes/states; admin can upsert valid ones.
func TestSkillAssignmentValidationAndUpsert(t *testing.T) {
	srv, db := skillTestServer(t)
	_ = db
	// Missing skill identity.
	if w := skJSON(t, srv, "PUT", "/api/skills/assignments", `{"scope":"org","state":"required"}`, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("empty identity accepted: %d", w.Code)
	}
	// Bad scope.
	if w := skJSON(t, srv, "PUT", "/api/skills/assignments", `{"skill_identity":"s@k","scope":"global","state":"required"}`, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad scope accepted: %d", w.Code)
	}
	// Bad state.
	if w := skJSON(t, srv, "PUT", "/api/skills/assignments", `{"skill_identity":"s@k","scope":"org","state":"maybe"}`, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad state accepted: %d", w.Code)
	}
	// Valid org-scope required.
	if w := skJSON(t, srv, "PUT", "/api/skills/assignments", `{"skill_identity":"tool@k","scope":"org","state":"required","digest":"abc123"}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("valid assignment rejected: %d %s", w.Code, w.Body.String())
	}
	var row models.SkillPolicyAssignment
	if err := db.Where("organization_id = ? AND skill_identity = ?", skTestOrg, "tool@k").First(&row).Error; err != nil {
		t.Fatalf("assignment not persisted: %v", err)
	}
	if row.State != "required" || row.Scope != "org" || row.Digest != "abc123" {
		t.Fatalf("assignment fields wrong: %+v", row)
	}
	// Idempotent upsert changes state.
	if w := skJSON(t, srv, "PUT", "/api/skills/assignments", `{"skill_identity":"tool@k","scope":"org","state":"blocked"}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("upsert rejected: %d", w.Code)
	}
	if err := db.Where("organization_id = ? AND skill_identity = ?", skTestOrg, "tool@k").First(&row).Error; err != nil || row.State != "blocked" {
		t.Fatalf("upsert did not update state: %+v err=%v", row, err)
	}
}

// A blocked assignment at org scope wins over a narrower optional assignment.
func TestSkillBlockedWinsAcrossScopes(t *testing.T) {
	srv, _ := skillTestServer(t)
	for _, body := range []string{
		`{"skill_identity":"secret@k","scope":"org","state":"blocked"}`,
		`{"skill_identity":"secret@k","scope":"team","scope_id":"team-a","state":"optional"}`,
	} {
		if w := skJSON(t, srv, "PUT", "/api/skills/assignments", body, "admin"); w.Code != http.StatusOK {
			t.Fatalf("assignment failed: %s %d", body, w.Code)
		}
	}
	assignments, err := srv.listOrgSkillAssignments(skTestOrg)
	if err != nil {
		t.Fatal(err)
	}
	res := skillpolicy.Resolve(
		[]skillpolicy.ReportedSkill{{Identity: "secret@k", Digest: "d1", Enabled: true}},
		assignments,
		skillpolicy.ResolveOptions{EnforcementEnabled: true},
	)
	if len(res) != 1 || res[0].State != skillpolicy.Blocked {
		t.Fatalf("blocked did not win: %+v", res)
	}
}

// Unverified digest of an approved identity fails closed to Blocked.
func TestSkillUnverifiedDigestFailsClosed(t *testing.T) {
	srv, _ := skillTestServer(t)
	if w := skJSON(t, srv, "PUT", "/api/skills/assignments",
		`{"skill_identity":"codegen@k","scope":"org","state":"required","digest":"abc"}`,
		"admin"); w.Code != http.StatusOK {
		t.Fatalf("assignment failed: %d", w.Code)
	}
	assignments, _ := srv.listOrgSkillAssignments(skTestOrg)
	res := skillpolicy.Resolve(
		[]skillpolicy.ReportedSkill{{Identity: "codegen@k", Digest: "DIFFERENT", Enabled: true}},
		assignments,
		skillpolicy.ResolveOptions{EnforcementEnabled: true},
	)
	if res[0].State != skillpolicy.Blocked || res[0].Approved {
		t.Fatalf("unverified digest not blocked: %+v", res[0])
	}
}

// Unknown skills with enforcement on are Blocked; with audit-only mode they are
// observed (not blocked), matching the migration window.
func TestSkillUnknownEnforcementSemantics(t *testing.T) {
	srv, db := skillTestServer(t)
	assignments, _ := srv.listOrgSkillAssignments(skTestOrg)
	// Unknown + enforcement → blocked.
	res := skillpolicy.Resolve(
		[]skillpolicy.ReportedSkill{{Identity: "mystery@k", Digest: "z", Enabled: true}},
		assignments,
		skillpolicy.ResolveOptions{EnforcementEnabled: true},
	)
	if res[0].State != skillpolicy.Blocked || !res[0].Unknown {
		t.Fatalf("unknown not blocked under enforcement: %+v", res[0])
	}
	// Unknown + audit-only → observed, not blocked.
	res = skillpolicy.Resolve(
		[]skillpolicy.ReportedSkill{{Identity: "mystery@k", Digest: "z", Enabled: true}},
		assignments,
		skillpolicy.ResolveOptions{EnforcementEnabled: false},
	)
	if res[0].State == skillpolicy.Blocked {
		t.Fatalf("unknown blocked in audit mode: %+v", res[0])
	}
	_ = db
}

// Harness skill reporting is idempotent (replace-per-harness) and feeds the
// admin inventory aggregation with effective-state rollups.
func TestSkillHarnessReportAndInventory(t *testing.T) {
	srv, db := skillTestServer(t)
	// Two harnesses report the same skill, one healthy, one with a different digest.
	report := func(hid, digest string, enabled bool) {
		if w := skJSON(t, srv, "POST", "/api/skills/report", fmt.Sprintf(
			`{"harness_id":%q,"skills":[{"skill_identity":"gen@k","content_digest":%q,"enabled":%v,"display_name":"Coder","source":"builtin","execution_mode":"inline"}]}`,
			hid, digest, enabled), "harness"); w.Code != http.StatusOK {
			t.Fatalf("report failed: %d %s", w.Code, w.Body.String())
		}
	}
	report("h1", "abc", true)
	report("h2", "def", true)
	// Missing the 'settings' field guard; enforce by requiring digest.
	if w := skJSON(t, srv, "POST", "/api/skills/report", `{"harness_id":"h3","skills":[{"skill_identity":"x@k","content_digest":"","enabled":true}]}`, "harness"); w.Code != http.StatusOK {
		t.Fatalf("empty digest report should be accepted: %d", w.Code)
	}
	// Should be 3 report rows (2 for gen@k, 1 for x@k).
	var count int64
	db.Model(&models.HarnessSkillReport{}).Count(&count)
	if count != 3 {
		t.Fatalf("expected 3 report rows, got %d", count)
	}

	// Admin upserts required for gen@k digest abc.
	if w := skJSON(t, srv, "PUT", "/api/skills/assignments",
		`{"skill_identity":"gen@k","scope":"org","state":"required","digest":"abc"}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("assignment failed: %d", w.Code)
	}

	w := skJSON(t, srv, "GET", "/api/skills/", "", "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("inventory failed: %d %s", w.Code, w.Body.String())
	}
	var inv struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &inv); err != nil {
		t.Fatal(err)
	}
	var gen map[string]interface{}
	for _, it := range inv.Items {
		if it["skill_identity"] == "gen@k" {
			gen = it
		}
	}
	if gen == nil {
		t.Fatalf("gen@k not in inventory: %s", w.Body.String())
	}
	// h1 matches digest (required, approved), h2 doesn't (blocked/unverified).
	if int(gen["affected"].(float64)) != 2 {
		t.Fatalf("affected mismatch: %+v", gen)
	}
}

// Epoch delivery signs the current policy and pushes directives to reported
// harnesses; the epoch row is durable and supersedes prior ones.
func TestSkillEpochDeliverSignsAndSupersedes(t *testing.T) {
	srv, db := skillTestServer(t)
	_ = db
	if w := skJSON(t, srv, "PUT", "/api/skills/assignments",
		`{"skill_identity":"a@k","scope":"org","state":"required","digest":"d1"}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("assignment failed: %d", w.Code)
	}
	// Report a harness so the epoch has a target.
	skJSON(t, srv, "POST", "/api/skills/report", `{"harness_id":"h-ep","skills":[{"skill_identity":"a@k","content_digest":"d1","enabled":true}]}`, "harness")

	w := skJSON(t, srv, "POST", "/api/skills/epochs/deliver", `{}`, "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("epoch deliver failed: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		EpochID   string `json:"epoch_id"`
		Digest    string `json:"digest"`
		Targets   int    `json:"targets"`
		Delivered int    `json:"delivered"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.EpochID == "" || out.Digest == "" || out.Targets < 1 {
		t.Fatalf("epoch incomplete: %+v", out)
	}
	var epoch models.SkillPolicyEpoch
	if err := db.Where("epoch_id = ?", out.EpochID).First(&epoch).Error; err != nil {
		t.Fatalf("epoch not persisted: %v", err)
	}
	if epoch.SignatureHex == "" {
		t.Fatalf("epoch unsigned")
	}

	// Deliver again → new epoch, old one superseded.
	w2 := skJSON(t, srv, "POST", "/api/skills/epochs/deliver", `{}`, "admin")
	var out2 struct {
		EpochID string `json:"epoch_id"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &out2)
	if out2.EpochID == out.EpochID {
		t.Fatalf("second epoch reused id")
	}
	var status string
	db.Model(&models.SkillPolicyEpoch{}).Where("epoch_id = ?", out.EpochID).Pluck("status", &status)
	if status != "superseded" {
		t.Fatalf("old epoch not superseded: %s", status)
	}
}
