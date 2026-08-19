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

func toolsSandboxTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/ts.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Tool{}, &models.Approval{},
		&models.Project{}, &models.ProjectToolAllowlist{}, &models.AuditEvent{},
		&models.ServiceSigningKey{}, &models.OrgSetting{}, &models.SandboxRecord{},
		&models.Session{}, &models.CapabilityLease{}, &models.Harness{},
		&models.EnterpriseFeatureViolation{}, &models.SandboxImage{},
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

func TestToolSeedCountAndPresets(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "ots", Status: "active"}
	db.Create(&org)

	rec := doJSON(t, srv, "GET", "/api/tools/presets", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("presets failed: %d", rec.Code)
	}
	var presets map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &presets)
	if len(presets["categories"].([]interface{})) != 4 {
		t.Fatalf("preset categories wrong: %v", presets["categories"])
	}
	rec = doJSON(t, srv, "POST", "/api/tools/seed-defaults", "", org.ID)
	var seed map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &seed)
	if seed["added"].(float64) < 1 {
		t.Fatalf("seed should add tools: %v", seed)
	}
	rec = doJSON(t, srv, "POST", "/api/tools/seed-defaults", "", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &seed)
	if seed["added"].(float64) != 0 {
		t.Fatalf("second seed should add 0: %v", seed)
	}
}

func TestProjectToolAllowlist(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "opt", Status: "active"}
	db.Create(&org)
	proj := models.Project{Name: "p", NameKo: "p", Slug: "p", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)

	rec := doJSON(t, srv, "PUT", "/api/projects/"+proj.ID+"/tool-allowlist",
		`{"tool_names":["file.read","file.write"],"granted_by":"admin"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowlist put failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/projects/"+proj.ID+"/tool-allowlist", "", org.ID)
	var rows []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 2 {
		t.Fatalf("allowlist rows wrong: %v", rows)
	}
}

func TestSandboxImageAllowlistEnforcement(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "osb", Status: "active"}
	db.Create(&org)

	rec := doJSON(t, srv, "PUT", "/api/sandboxes/image-allowlist",
		`{"images":["patty/sandbox-base"]}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowlist put failed: %d %s", rec.Code, rec.Body.String())
	}
	// Allowed prefix.
	rec = doJSON(t, srv, "POST", "/api/sandboxes",
		`{"mode":"local","base_image":"patty/sandbox-base:latest","network_policy":"none"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("allowed image rejected: %d %s", rec.Code, rec.Body.String())
	}
	// Denied image.
	rec = doJSON(t, srv, "POST", "/api/sandboxes",
		`{"mode":"local","base_image":"evil/img:latest","network_policy":"none"}`, org.ID)
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest && rec.Code != http.StatusInternalServerError {
		t.Fatalf("denied image should fail: %d %s", rec.Code, rec.Body.String())
	}
	// Allowlist off → any image allowed again.
	rec = doJSON(t, srv, "PUT", "/api/sandboxes/image-allowlist", `{"images":[]}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear failed: %d", rec.Code)
	}
	rec = doJSON(t, srv, "POST", "/api/sandboxes",
		`{"mode":"local","base_image":"evil/img:latest","network_policy":"none"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cleared allowlist should allow: %d %s", rec.Code, rec.Body.String())
	}
}
