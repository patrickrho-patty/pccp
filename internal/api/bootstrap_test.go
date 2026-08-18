package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func apiDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/api.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(models.AllModels()...)
	db.Table("admin_credentials").AutoMigrate(&identity.AdminCredentials{})
	return db
}

func TestBootstrapIsOneTimeAuthorizedAndCannotResetPassword(t *testing.T) {
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "one-time-deployment-secret")
	db := apiDB(t)
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.handleBootstrap(w, r)
	})

	call := func(body map[string]string) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/bootstrap", bytes.NewReader(raw))
		req.Header.Set("X-PCCP-Bootstrap-Token", "one-time-deployment-secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	code, first := call(map[string]string{"email": "a@patty.io", "password": "pw123456", "org_name": "Patty", "profile": "sovereign"})
	if code != http.StatusCreated {
		t.Fatalf("first bootstrap: %d %v", code, first)
	}
	orgID, _ := first["organization_id"].(string)

	// A public replay can neither create another org nor reset the password.
	code2, second := call(map[string]string{"email": "a@patty.io", "password": "attacker-password", "org_name": "Patty", "profile": "enterprise"})
	if code2 != http.StatusConflict {
		t.Fatalf("second bootstrap: %d %v", code2, second)
	}
	var orgs int64
	db.Model(&models.Organization{}).Where("name = ?", "Patty").Count(&orgs)
	if orgs != 1 {
		t.Fatalf("org spam: %d orgs after repeat bootstrap", orgs)
	}
	if _, err := srv.auth.Login("a@patty.io", "pw123456"); err != nil {
		t.Fatalf("original password stopped working after bootstrap replay: %v", err)
	}
	if _, err := srv.auth.Login("a@patty.io", "attacker-password"); err == nil {
		t.Fatal("bootstrap replay reset the existing administrator password")
	}

	// Profile honored on the fresh org.
	var org models.Organization
	db.Where("name = ?", "Patty").First(&org)
	if org.Profile != "sovereign" {
		t.Fatalf("profile = %q, want sovereign", org.Profile)
	}
	if org.ID != orgID {
		t.Fatalf("organization id drifted: %s vs %s", org.ID, orgID)
	}
}
