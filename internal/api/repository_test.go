package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func repoTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/r.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.Project{}, &models.Repository{},
		&models.RepoBaseline{}, &models.Branch{}, &models.AuditEvent{},
		&models.ServiceSigningKey{},
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

func TestRegisterRepositoryValidatesCloneURL(t *testing.T) {
	srv, db := repoTestServer(t)
	org := models.Organization{Name: "o", Slug: "o", Status: "active"}
	db.Create(&org)
	rec := doJSON(t, srv, "POST", "/api/repositories", `{"name":"r","clone_url":"not a url","default_branch":"main"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad clone_url, got %d", rec.Code)
	}
	rec = doJSON(t, srv, "POST", "/api/repositories", `{"name":"r","clone_url":"https://github.com/o/r.git","default_branch":"main","scm_provider":"github","sensitivity":"confidential"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var repo models.Repository
	db.First(&repo, "name = ?", "r")
	if repo.CloneURL != "https://github.com/o/r.git" || repo.SCMProvider != "github" || repo.Sensitivity != "confidential" {
		t.Fatalf("fields dropped: %+v", repo)
	}
}

func TestUpdateRepositoryChangesProjectBinding(t *testing.T) {
	srv, db := repoTestServer(t)
	org := models.Organization{Name: "o", Slug: "o2", Status: "active"}
	db.Create(&org)
	proj := models.Project{Name: "p", Slug: "p", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)
	repo := models.Repository{Name: "r2", ProjectID: "", CloneURL: "https://github.com/o/r2.git", Status: "active"}
	repo.OrganizationID = org.ID
	db.Create(&repo)

	rec := doJSON(t, srv, "PUT", "/api/repositories/"+repo.ID, `{"project_id":"`+proj.ID+`","default_branch":"develop"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("update failed: %d %s", rec.Code, rec.Body.String())
	}
	db.First(&repo, "id = ?", repo.ID)
	if repo.ProjectID != proj.ID || repo.DefaultBranch != "develop" {
		t.Fatalf("update not persisted: %+v", repo)
	}
}

func TestScmWebhookRequiresValidSignature(t *testing.T) {
	srv, db := repoTestServer(t)
	org := models.Organization{Name: "o", Slug: "o3", Status: "active"}
	db.Create(&org)
	repo := models.Repository{Name: "r3", CloneURL: "https://github.com/o/r3.git", Status: "active", WebhookSecret: "topsecret"}
	repo.OrganizationID = org.ID
	db.Create(&repo)

	payload := []byte(`{"ref":"refs/heads/main","after":"abc123","head_commit":{"id":"abc123"}}`)
	// No signature → 401
	rec := doJSONBody(t, srv, "POST", "/webhooks/scm/"+repo.ID, payload, map[string]string{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without signature, got %d", rec.Code)
	}
	// Wrong signature → 401
	rec = doJSONBody(t, srv, "POST", "/webhooks/scm/"+repo.ID, payload, map[string]string{"X-PCCP-Signature": "deadbeef"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with bad signature, got %d", rec.Code)
	}
	// Valid HMAC → 200 + audit event.
	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))
	rec = doJSONBody(t, srv, "POST", "/webhooks/scm/"+repo.ID, payload, map[string]string{"X-PCCP-Signature": sig, "X-GitHub-Event": "push"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid signature, got %d: %s", rec.Code, rec.Body.String())
	}
	var count int64
	db.Model(&models.AuditEvent{}).Where("action = ?", "scm.webhook_received").Count(&count)
	if count != 1 {
		t.Fatalf("expected webhook audit event, got %d", count)
	}
}

func TestSyncRequiresCloneURL(t *testing.T) {
	srv, db := repoTestServer(t)
	org := models.Organization{Name: "o", Slug: "o4", Status: "active"}
	db.Create(&org)
	repo := models.Repository{Name: "r4", Status: "active"}
	repo.OrganizationID = org.ID
	db.Create(&repo)
	rec := doJSON(t, srv, "POST", "/api/repositories/"+repo.ID+"/sync", "", org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without clone_url, got %d", rec.Code)
	}
}

func doJSONBody(t *testing.T, srv *Server, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}
