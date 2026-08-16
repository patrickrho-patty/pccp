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

// P0-6: bootstrap is idempotent per org name (repeat bootstraps must
// not mint new orgs) and honors the deployment profile.
func TestBootstrapIdempotentOrgAndProfile(t *testing.T) {
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

	// Second bootstrap with the SAME org name: no new org, same ID.
	code2, second := call(map[string]string{"email": "a@patty.io", "password": "pw123456", "org_name": "Patty", "profile": "sovereign"})
	if code2 != http.StatusCreated {
		t.Fatalf("second bootstrap: %d %v", code2, second)
	}
	var orgs int64
	db.Model(&models.Organization{}).Where("name = ?", "Patty").Count(&orgs)
	if orgs != 1 {
		t.Fatalf("org spam: %d orgs after repeat bootstrap", orgs)
	}
	if second["organization_id"] != orgID {
		t.Fatalf("org id drifted: %v vs %v", second["organization_id"], orgID)
	}

	// Profile honored on the fresh org.
	var org models.Organization
	db.Where("name = ?", "Patty").First(&org)
	if org.Profile != "sovereign" {
		t.Fatalf("profile = %q, want sovereign", org.Profile)
	}
}
