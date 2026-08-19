package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// PAT-1509: governed tool changes must record the admin's reason in the
// immutable audit trail — both for approval-gate updates and for project
// allowlist replacements.
func TestToolGovernanceAuditReason(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "otg", Status: "active"}
	db.Create(&org)
	proj := models.Project{Name: "p", NameKo: "p", Slug: "p", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)

	rec := doJSON(t, srv, "POST", "/api/tools",
		`{"name":"shell.write","name_ko":"셸 쓰기","category":"execute","tool_class":"shell","danger_level":"high","requires_approval":true}`, org.ID)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("register failed: %d %s", rec.Code, rec.Body.String())
	}
	var tool map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &tool)
	toolID := tool["id"].(string)

	rec = doJSON(t, srv, "PUT", "/api/tools/"+toolID,
		`{"requires_approval":false,"reason":"긴급 장애 대응"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("update failed: %d %s", rec.Code, rec.Body.String())
	}
	var ev models.AuditEvent
	if err := db.Where("organization_id = ? AND event_type = ?", org.ID, "cp.tools.updated").First(&ev).Error; err != nil {
		t.Fatalf("update audit event missing: %v", err)
	}
	if !strings.Contains(ev.Details, "긴급 장애 대응") {
		t.Fatalf("update audit should record reason: %s", ev.Details)
	}

	rec = doJSON(t, srv, "PUT", "/api/projects/"+proj.ID+"/tool-allowlist",
		`{"tool_names":["shell.write"],"granted_by":"admin","reason":"보안 검토 완료"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowlist put failed: %d %s", rec.Code, rec.Body.String())
	}
	var aev models.AuditEvent
	if err := db.Where("organization_id = ? AND event_type = ?", org.ID, "cp.tools.allowlist_replaced").First(&aev).Error; err != nil {
		t.Fatalf("allowlist audit event missing: %v", err)
	}
	if !strings.Contains(aev.Details, "보안 검토 완료") || !strings.Contains(aev.Details, "shell.write") {
		t.Fatalf("allowlist audit should record reason and names: %s", aev.Details)
	}
}
