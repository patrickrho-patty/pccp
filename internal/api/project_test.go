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

func projectTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/p.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Project{}, &models.ProjectMember{},
		&models.Repository{}, &models.Session{}, &models.ChangeRequest{},
		&models.AuditEvent{}, &models.ServiceSigningKey{}, &models.PolicyEpoch{},
		&models.PolicyPack{}, &models.UsageRecord{}, &models.ChangeSet{},
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

func TestProjectMemberLifecycle(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgp", Status: "active"}
	db.Create(&org)
	user := models.User{Email: "m@corp.kr", Name: "member", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	proj := models.Project{Name: "p", NameKo: "p", Slug: "p", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)

	rec := doJSON(t, srv, "POST", "/api/projects/"+proj.ID+"/members", `{"user_id":"`+user.ID+`","role":"admin"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/projects/"+proj.ID+"/members", "", org.ID)
	var members []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &members)
	if len(members) != 1 || members[0]["role"] != "admin" || members[0]["user"] == nil {
		t.Fatalf("unexpected roster: %v", members)
	}
	rec = doJSON(t, srv, "DELETE", "/api/projects/"+proj.ID+"/members/"+user.ID, "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove member failed: %d", rec.Code)
	}
	var count int64
	db.Model(&models.ProjectMember{}).Count(&count)
	if count != 0 {
		t.Fatalf("member not removed: %d", count)
	}
}

func TestArchivedProjectBlocksNewSessionsAndRestores(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "orga", Status: "active"}
	db.Create(&org)
	user := models.User{Email: "a@corp.kr", Name: "a", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	proj := models.Project{Name: "p2", Slug: "p2", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)

	// Archive
	rec := doJSON(t, srv, "DELETE", "/api/projects/"+proj.ID, "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive failed: %d", rec.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "archived" || resp["impact"] == nil {
		t.Fatalf("archive should report impact: %v", resp)
	}

	// New session rejected
	rec = doJSON(t, srv, "POST", "/api/sessions", `{"project_id":"`+proj.ID+`","user_id":"`+user.ID+`"}`, org.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for archived project, got %d: %s", rec.Code, rec.Body.String())
	}

	// Restore → session accepted
	rec = doJSON(t, srv, "POST", "/api/projects/"+proj.ID+"/restore", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore failed: %d", rec.Code)
	}
	rec = doJSON(t, srv, "POST", "/api/sessions", `{"project_id":"`+proj.ID+`","user_id":"`+user.ID+`"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 after restore, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHighRiskChangeSetQueuesChangeRequest(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgq", Status: "active"}
	db.Create(&org)
	proj := models.Project{Name: "p3", Slug: "p3", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)
	repo := models.Repository{Name: "r", ProjectID: proj.ID, Status: "active"}
	repo.OrganizationID = org.ID
	db.Create(&repo)

	// An auth-touching change must require approval and queue.
	body := `{"session_id":"ses-1","repository_id":"` + repo.ID + `","branch":"main","files_changed":["src/auth/login.go","src/crypto/sign.go","config/prod.yaml"],"lines_added":120,"lines_removed":10,"attribution_state":"AI_GENERATED","confidence":0.95}`
	rec := doJSON(t, srv, "POST", "/api/provenance/changeset", body, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("changeset failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["queued"] != true || resp["change_request"] == nil {
		t.Fatalf("high-risk change should queue a change request: %v", resp)
	}

	// The queue item must be pending and bound to the project.
	rec = doJSON(t, srv, "GET", "/api/projects/"+proj.ID+"/change-requests", "", org.ID)
	var crs []models.ChangeRequest
	json.Unmarshal(rec.Body.Bytes(), &crs)
	if len(crs) != 1 || crs[0].Status != "pending" || crs[0].RiskLevel == "low" {
		t.Fatalf("unexpected queue: %+v", crs)
	}

	// Decide it.
	rec = doJSON(t, srv, "POST", "/api/change-requests/"+crs[0].ID+"/decide", `{"approve":true,"reason":"reviewed"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("decide failed: %d", rec.Code)
	}
	db.First(&crs[0], "id = ?", crs[0].ID)
	if crs[0].Status != "approved" {
		t.Fatalf("expected approved, got %s", crs[0].Status)
	}
}
