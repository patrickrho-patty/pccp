package api

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PAT-1497 contract: the pending-approval presenter (enrichApprovals) produces
// ONE typed governed-decision contract shared by Fleet and Tools — requester,
// session/harness context, requested tool, risk, waiting age, expiry, and an
// exact detail route. No surface has to infer meaning from raw tool_use strings.
func TestApprovalEnrichmentContractPAT1497(t *testing.T) {
	srv := withApprovalModels(t)
	db := srv.db

	org := models.Organization{Name: "o", Slug: "oa", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "req@patty.dev", Name: "Req User"}
	db.Create(&user)
	tool := models.Tool{AuditBase: models.AuditBase{OrganizationID: org.ID}, Name: "bash", DangerLevel: "critical", Category: "execute"}
	db.Create(&tool)
	sess := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, SessionID: "ses_req1", HarnessID: "hrn_1", UserID: user.ID, Title: "환불 로직 구현", Status: "active"}
	db.Create(&sess)

	arr := models.Approval{
		OrganizationID: org.ID, SessionID: sess.SessionID, ActionID: tool.ID,
		ApprovalType: "tool_use_bash", RequestedBy: user.ID, Decision: "pending",
		Base:     models.Base{CreatedAt: time.Now().Add(-5 * time.Minute)},
		ExpiresAt: time.Now().Add(2 * time.Hour).Format(time.RFC3339),
	}
	db.Create(&arr)

	// Fleet endpoint (pending only)
	rec := doJSON(t, srv, "GET", "/api/fleet/approvals", "", org.ID)
	if rec.Code != 200 {
		t.Fatalf("fleet approvals failed: %d %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 {
		t.Fatalf("approvals = %d, want 1", len(rows))
	}
	r := rows[0]
	if r["requested_by_name"] != "Req User" {
		t.Fatalf("requested_by_name = %v", r["requested_by_name"])
	}
	if r["harness_id"] != "hrn_1" || r["session_title"] != "환불 로직 구현" {
		t.Fatalf("session/harness join wrong: %v %v", r["harness_id"], r["session_title"])
	}
	if r["tool_name"] != "bash" {
		t.Fatalf("tool_name = %v", r["tool_name"])
	}
	if r["risk"] != "critical" {
		t.Fatalf("risk = %v", r["risk"])
	}
	if r["approval_type_ko"] != "도구 실행 승인" {
		t.Fatalf("approval_type_ko = %v", r["approval_type_ko"])
	}
	age, ok := r["waiting_age_seconds"].(float64)
	if !ok || age < 240 || age > 420 {
		t.Fatalf("waiting_age_seconds = %v", r["waiting_age_seconds"])
	}
	if r["expired"] != false {
		t.Fatalf("expired = %v", r["expired"])
	}
	if r["detail_route"] != "/sessions/ses_req1" {
		t.Fatalf("detail_route = %v", r["detail_route"])
	}
}

// withApprovalModels returns a test server whose DB is migrated for the
// approval-presentation contract (Tool + Approval added to securityTestServer).
func withApprovalModels(t *testing.T) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/appr.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Session{}, &models.Harness{}, &models.Tool{},
		&models.Approval{}, &models.AuditEvent{},
		&identity.AdminCredentials{}, &models.ServiceSigningKey{}, &models.UsageRecord{},
		&models.InferenceEndpoint{}, &models.OrgSetting{}, &models.PromptExchange{},
		&models.ModelPackage{}, &models.Project{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(fmt.Errorf("migrate %T: %w", m, err))
		}
	}
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	srv.db = db
	return srv
}
