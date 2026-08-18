package api

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/fleet"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func harnessTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/h.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Harness{}, &models.Device{},
		&models.EnrollmentCode{}, &models.OrgSetting{}, &models.Session{},
		&models.Approval{}, &models.SecurityFinding{}, &models.AuditEvent{}, &models.CredentialRevocationRecord{},
		&models.ActionEnvelope{}, &models.ChangeSet{}, &models.PromptExchange{}, &models.ProvenanceSpan{},
		&models.SandboxRecord{},
		&models.ServiceSigningKey{}, &models.RealtimeEvent{}, &models.RealtimeSequence{}, &models.RealtimeStreamTicket{}, &models.RealtimeTransientEvent{},
		&models.FleetBulkOperation{},
		&models.FleetBulkTargetOutcome{},
		&models.FleetDesiredState{},
		&models.SecurityLockdown{},
		&identity.AdminCredentials{},
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

func doJSON(t *testing.T, srv *Server, method, path, body string, orgID string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if path == "/api/auth/bootstrap" {
		req.Header.Set("X-PCCP-Bootstrap-Token", "test-bootstrap-token")
	}
	if orgID != "" {
		req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: orgID}))
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestEnrollRejectsMissingOrganization(t *testing.T) {
	srv, _ := harnessTestServer(t)
	pub := make([]byte, 32)
	body, _ := json.Marshal(map[string]string{
		"harness_id": "hrn_test", "public_key_hex": hex.EncodeToString(pub),
		"binary_version": "1.0.0", "user_id": "u1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/harnesses/enroll", strings.NewReader(string(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without organization, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEnrollWithCodeBurnsCode(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "org", Status: "active", MaxHarnessSeats: 100}
	db.Create(&org)
	user := models.User{Email: "dev@corp.kr", Name: "dev", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	code, err := srv.identity.GenerateEnrollmentCode(org.ID, user.ID, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	pub := make([]byte, 32)
	body, _ := json.Marshal(map[string]string{
		"harness_id": "hrn_code", "public_key_hex": hex.EncodeToString(pub),
		"binary_version": "1.2.0", "user_id": user.ID, "enrollment_code": code,
	})
	rec := doJSON(t, srv, "POST", "/api/harnesses/enroll", string(body), org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var ec models.EnrollmentCode
	db.Where("code = ?", code).First(&ec)
	if !ec.Used || ec.UsedBy != "hrn_code" {
		t.Fatalf("code not burned: used=%v used_by=%q", ec.Used, ec.UsedBy)
	}
	var h models.Harness
	db.Where("harness_id = ?", "hrn_code").First(&h)
	if h.CredentialJSON == "" {
		t.Fatal("expected issued PPC on harness")
	}
	// The issued PPC must be a valid COSE-Sign1 signed by the CA.
	raw, err := hex.DecodeString(h.CredentialJSON)
	if err != nil {
		t.Fatal(err)
	}
	sign1, err := dari.DecodeCOSESign1(raw)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := dari.DecodePeerCredential(sign1.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := cred.VerifySignature(srv.identity.CAPublicKeyRaw(), hex.EncodeToString(raw)); err != nil {
		t.Fatalf("issued PPC fails CA verification: %v", err)
	}
}

func TestEnrollBlocksBelowForcedVersion(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "org2", Status: "active", MaxHarnessSeats: 100}
	db.Create(&org)
	user := models.User{Email: "dev2@corp.kr", Name: "dev", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	if err := srv.ext().Korean.SetForcedHarnessVersion(org.ID, "1.5.0", "stable", "", "security"); err != nil {
		t.Fatal(err)
	}
	pub := make([]byte, 32)
	body, _ := json.Marshal(map[string]string{
		"harness_id": "hrn_old", "public_key_hex": hex.EncodeToString(pub),
		"binary_version": "1.2.0", "user_id": user.ID,
	})
	rec := doJSON(t, srv, "POST", "/api/harnesses/enroll", string(body), org.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 below floor, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevokeTerminatesSessionsAndRecordsRevocation(t *testing.T) {
	srv, db := harnessTestServer(t)
	srv.fleet.SetRevocationSender(func(fleet.ActionRequest) error { return nil })
	org := models.Organization{Name: "org", Slug: "org3", Status: "active", MaxHarnessSeats: 100}
	db.Create(&org)
	user := models.User{Email: "dev3@corp.kr", Name: "dev", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	pub := make([]byte, 32)
	body, _ := json.Marshal(map[string]string{
		"harness_id": "hrn_revoke", "public_key_hex": hex.EncodeToString(pub),
		"binary_version": "1.2.0", "user_id": user.ID,
	})
	rec := doJSON(t, srv, "POST", "/api/harnesses/enroll", string(body), org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll failed: %s", rec.Body.String())
	}
	var h models.Harness
	db.Where("harness_id = ?", "hrn_revoke").First(&h)
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, HarnessID: "hrn_revoke", UserID: user.ID, SessionID: "ses-1", Status: "active"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, HarnessID: "hrn_revoke", UserID: user.ID, SessionID: "ses-2", Status: "closed"})

	rec = doJSONAsRole(t, srv, "POST", "/api/harnesses/"+h.ID+"/revoke", `{"reason":"offboarded"}`, org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke failed: %d %s", rec.Code, rec.Body.String())
	}
	var s1 models.Session
	db.Where("session_id = ?", "ses-1").First(&s1)
	if s1.Status != "terminated" {
		t.Fatalf("active session not terminated: %s", s1.Status)
	}
	var s2 models.Session
	db.Where("session_id = ?", "ses-2").First(&s2)
	if s2.Status != "closed" {
		t.Fatalf("closed session should be untouched: %s", s2.Status)
	}
	var revCount int64
	db.Model(&models.CredentialRevocationRecord{}).Count(&revCount)
	if revCount == 0 {
		t.Fatal("expected credential revocation record")
	}
}

// doJSON2 issues a request with an explicit operator email claim.
func doJSON2(t *testing.T, srv *Server, method, path, body string, email string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{Email: email, OrganizationID: "org-mfa"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestReactivateHarnessRequiresManagementPermissionAndTenantScope(t *testing.T) {
	srv, db := harnessTestServer(t)
	foreign := models.Harness{OrganizationID: "org-b", HarnessID: "foreign-harness", Status: "quarantined", RiskState: "high"}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	denied := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/harnesses/"+foreign.ID+"/reactivate", "", "org-a", "viewer")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer reactivation = %d, want 403", denied.Code)
	}
	crossTenant := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/harnesses/"+foreign.ID+"/reactivate", "", "org-a", "admin")
	if crossTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant reactivation = %d, want 404: %s", crossTenant.Code, crossTenant.Body.String())
	}
	var got models.Harness
	db.First(&got, "id = ?", foreign.ID)
	if got.Status != "quarantined" || got.RiskState != "high" {
		t.Fatalf("foreign harness mutated: %+v", got)
	}
}
