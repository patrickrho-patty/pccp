package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func complianceTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/c.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.SecurityRule{}, &models.SecurityFinding{},
		&models.AuditEvent{}, &models.ServiceSigningKey{}, &models.ComplianceEvidence{},
		&models.ComplianceRemediation{}, &models.ComplianceAssessmentRecord{},
		&models.Session{}, &models.Harness{}, &models.PolicyEpoch{}, &models.CapabilityLease{},
		&models.EvidenceReceipt{}, &models.Conversation{}, &models.Message{},
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

func TestComplianceMetaHasLevelsAndScopes(t *testing.T) {
	srv, _ := complianceTestServer(t)
	rec := doJSON(t, srv, "GET", "/api/compliance/meta", "", "org-meta")
	if rec.Code != http.StatusOK {
		t.Fatalf("meta failed: %d", rec.Code)
	}
	var meta []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &meta)
	if len(meta) < 5 {
		t.Fatalf("expected 5 certifications, got %d", len(meta))
	}
	found := false
	for _, m := range meta {
		if m["certification"] == "CSAP" {
			levels := m["levels"].([]interface{})
			scopes := m["scopes"].([]interface{})
			if len(levels) != 2 || len(scopes) != 3 {
				t.Fatalf("CSAP levels/scopes wrong: %v %v", levels, scopes)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("CSAP meta missing")
	}
}

func TestComplianceEvidenceVaultRoundtrip(t *testing.T) {
	srv, db := complianceTestServer(t)
	org := models.Organization{Name: "o", Slug: "oc", Status: "active"}
	db.Create(&org)

	rec := doJSON(t, srv, "POST", "/api/compliance/evidence",
		`{"certification":"CSAP","control_id":"CSAP-2.1","title":"RBAC config","source":"manual","reference":"/audit/12"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("evidence add failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/compliance/evidence?certification=CSAP", "", org.ID)
	var items []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 1 || items[0]["control_id"] != "CSAP-2.1" {
		t.Fatalf("evidence list wrong: %v", items)
	}
	rec = doJSON(t, srv, "DELETE", "/api/compliance/evidence/"+items[0]["id"].(string), "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("evidence delete failed: %d", rec.Code)
	}
}

func TestComplianceRemediationAndBulk(t *testing.T) {
	srv, db := complianceTestServer(t)
	org := models.Organization{Name: "o", Slug: "or", Status: "active"}
	db.Create(&org)

	rec := doJSON(t, srv, "POST", "/api/compliance/remediations",
		`{"certification":"ISMS-P","control_id":"ISMS-P-2.3","owner":"cs@patty.io","sla":"30d"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("remediation add failed: %d %s", rec.Code, rec.Body.String())
	}
	var task map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &task)
	rec = doJSON(t, srv, "PUT", "/api/compliance/remediations/"+task["id"].(string),
		`{"status":"in_progress"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("remediation update failed: %d", rec.Code)
	}
	// Bulk conversion from an honest assessment (gaps become tasks).
	rec = doJSON(t, srv, "POST", "/api/compliance/remediate-all",
		`{"certification":"CSAP","owner":"sec@patty.io","sla":"60d"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk remediate failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["created"].(float64) < 1 {
		t.Fatalf("bulk remediate created nothing: %v", resp)
	}
}

func TestComplianceAssessmentPersistsAndExports(t *testing.T) {
	srv, db := complianceTestServer(t)
	org := models.Organization{Name: "o", Slug: "ox", Status: "active"}
	db.Create(&org)

	rec := doJSON(t, srv, "POST", "/api/compliance/assess",
		`{"certification":"CSAP","scope":"SaaS","level":"simple"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("assess failed: %d %s", rec.Code, rec.Body.String())
	}
	var assessment map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &assessment)
	if assessment["overall_status"] == "" || assessment["open_gaps"] == nil {
		t.Fatalf("assessment shape wrong: %v", assessment)
	}
	rec = doJSON(t, srv, "GET", "/api/compliance/history", "", org.ID)
	var history []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &history)
	if len(history) != 1 || history[0]["scope"] != "SaaS" {
		t.Fatalf("history wrong: %v", history)
	}
	rec = doJSON(t, srv, "GET", "/api/compliance/export?certification=CSAP&format=csv", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("export failed: %d", rec.Code)
	}
	if rec.Body.Len() < 100 || rec.Header().Get("Content-Type") == "" {
		t.Fatalf("csv export malformed")
	}
}
