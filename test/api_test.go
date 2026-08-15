package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/api"
	"github.com/patrickrho-patty/pccp/internal/db"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAPITest(t *testing.T) (*httptest.Server, *gorm.DB) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(database)
	server, err := api.New(database, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	authSvc := identity.NewAuthService(database, "test-secret")
	authSvc.BootstrapAdmin("admin@patty.dev", "admin123", "")
	return httptest.NewServer(server), database
}

func getTestToken(t *testing.T, ts *httptest.Server) string {
	body := bytes.NewBufferString(`{"email":"admin@patty.dev","password":"admin123"}`)
	resp, err := http.Post(ts.URL+"/api/auth/login", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	return result["token"]
}

func authedPost(ts *httptest.Server, path, token string, body []byte) (*http.Response, error) {
	req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func TestAPIHealth(t *testing.T) {
	ts, _ := setupAPITest(t)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAPILogin(t *testing.T) {
	ts, _ := setupAPITest(t)
	defer ts.Close()
	body := bytes.NewBufferString(`{"email":"admin@patty.dev","password":"admin123"}`)
	resp, err := http.Post(ts.URL+"/api/auth/login", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAPISessionLifecycle(t *testing.T) {
	ts, database := setupAPITest(t)
	defer ts.Close()
	token := getTestToken(t, ts)

	org := models.Organization{Name: "Test", Slug: "test-api", Status: "active"}
	database.Create(&org)

	user := models.User{
		AuditBase: models.AuditBase{OrganizationID: org.ID},
		Email:     "test@test.com", Name: "Test", NameKo: "테스트",
		Status: "active", Locale: "ko-KR",
	}
	database.Create(&user)

	// Governed open requires an active policy epoch (web/02 A1).
	polSvc, err := policy.New(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := polSvc.CreatePolicyEpochFull(policy.EpochRequest{OrganizationID: org.ID, AllowedModels: []string{"pmp_test"}}); err != nil {
		t.Fatalf("seed epoch: %v", err)
	}

	body := map[string]interface{}{
		"organization_id": org.ID,
		"harness_id":      "hrn-test",
		"user_id":         user.ID,
		"title":           "테스트 세션",
		"model_class":     "pmp_test",
	}
	bodyBytes, _ := json.Marshal(body)
	resp, err := authedPost(ts, "/api/sessions", token, bodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var session models.Session
	json.NewDecoder(resp.Body).Decode(&session)
	if session.SessionID == "" {
		t.Fatal("expected session ID")
	}
	if session.Title != "테스트 세션" {
		t.Fatalf("expected Korean title, got %s", session.Title)
	}
}

func TestAPIModelRegistration(t *testing.T) {
	ts, _ := setupAPITest(t)
	defer ts.Close()
	token := getTestToken(t, ts)

	body := map[string]interface{}{
		"package_id": "pmp_api_test",
		"model_id":   "test-model",
		"name":       "Test Model API",
		"version":    "1.0",
		"state":      "draft",
	}
	bodyBytes, _ := json.Marshal(body)
	resp, err := authedPost(ts, "/api/models", token, bodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}
