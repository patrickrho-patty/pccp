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

func publicOpsTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/po.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Account{}, &models.Subscription{},
		&models.AccountCapacityLease{}, &models.SupportCase{}, &models.AbuseCase{},
		&models.AuditEvent{}, &models.ServiceSigningKey{}, &models.PolicyEpoch{},
		&models.CapabilityLease{}, &models.Harness{}, &models.Session{}, &models.SecurityFinding{},
		&models.SecurityRule{}, &models.ComplianceRemediation{}, &models.OrgSetting{},
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

func TestPublicAccountsGraduatedResponse(t *testing.T) {
	srv, db := publicOpsTestServer(t)
	acct := models.Account{Email: "a@corp.kr", DisplayName: "A", SubscriptionStatus: "active"}
	db.Create(&acct)

	rec := doJSON(t, srv, "GET", "/api/public/accounts", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("accounts failed: %d", rec.Code)
	}
	var rows []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0]["ladder_rung"] != "normal" {
		t.Fatalf("accounts wrong: %v", rows)
	}

	// Graduated response requires a reason.
	rec = doJSON(t, srv, "POST", "/api/public/accounts/"+acct.ID+"/action",
		`{"dimension":"trust_safety","state":"restricted"}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reason should be required: %d", rec.Code)
	}
	rec = doJSON(t, srv, "POST", "/api/public/accounts/"+acct.ID+"/action",
		`{"dimension":"trust_safety","state":"restricted","reason":"abuse confirmed"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("action failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["ladder_rung"] != "restrict" {
		t.Fatalf("ladder should advance to restrict: %v", resp)
	}
}

func TestSupportAndAbuseCaseLifecycle(t *testing.T) {
	srv, db := publicOpsTestServer(t)
	acct := models.Account{Email: "b@corp.kr", DisplayName: "B"}
	db.Create(&acct)

	rec := doJSON(t, srv, "POST", "/api/public/support-cases",
		`{"account_id":"`+acct.ID+`","subject":"billing","priority":"high"}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("support case failed: %d %s", rec.Code, rec.Body.String())
	}
	var sc models.SupportCase
	json.Unmarshal(rec.Body.Bytes(), &sc)
	rec = doJSON(t, srv, "PUT", "/api/public/support-cases/"+sc.ID,
		`{"status":"in_progress","note":"assigned"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("case update failed: %d", rec.Code)
	}
	rec = doJSON(t, srv, "POST", "/api/public/abuse-cases",
		`{"account_id":"`+acct.ID+`","category":"account_sharing","severity":"high"}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("abuse case failed: %d %s", rec.Code, rec.Body.String())
	}
	var ab models.AbuseCase
	json.Unmarshal(rec.Body.Bytes(), &ab)
	rec = doJSON(t, srv, "PUT", "/api/public/abuse-cases/"+ab.ID,
		`{"status":"action_taken","decision":"restricted 30d"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("abuse update failed: %d", rec.Code)
	}
	// Segment tagging (12 A8).
	rec = doJSON(t, srv, "PUT", "/api/public/segments",
		`{"account_id":"`+acct.ID+`","segment":"enterprise-pilot"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("segment failed: %d", rec.Code)
	}
	rec = doJSON(t, srv, "GET", "/api/public/segments", "", "")
	var segs []string
	json.Unmarshal(rec.Body.Bytes(), &segs)
	if len(segs) != 1 || segs[0] != "enterprise-pilot" {
		t.Fatalf("segments wrong: %v", segs)
	}
}
