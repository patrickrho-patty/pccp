package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func publicOpsTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
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
		&models.UsageRecord{},
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

func publicOpsOperatorToken(t *testing.T, srv *Server, db *gorm.DB) string {
	t.Helper()
	org := models.Organization{Name: "Patty Operations", Slug: "patty-operations", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	admin := identity.AdminCredentials{
		Email: "operator@patty.dev", Password: "unused", OrganizationID: org.ID,
		Role: "super_admin", Name: "Public Operations Test",
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	token, err := srv.auth.IssueToken(admin.Email, org.ID, admin.Role)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func doPublicOpsJSON(t *testing.T, srv *Server, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var requestBody *strings.Reader
	if body == "" {
		requestBody = strings.NewReader("")
	} else {
		requestBody = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, requestBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestPublicAccountsUsageIsServerAggregatedBeyondLedgerPageAndExact(t *testing.T) {
	srv, db := publicOpsTestServer(t)
	token := publicOpsOperatorToken(t, srv, db)
	acct := models.Account{Email: "usage@corp.kr", DisplayName: "Usage", SubscriptionStatus: "active"}
	if err := db.Create(&acct).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	records := make([]models.UsageRecord, 0, 203)
	records = append(records, models.UsageRecord{
		OrganizationID: acct.ID, MetricType: "tokens_in", Unit: "tokens",
		Quantity: 9_007_199_254_740_993, OccurredAt: now.Format(time.RFC3339),
	})
	for i := 0; i < 200; i++ {
		records = append(records, models.UsageRecord{
			OrganizationID: acct.ID, MetricType: "tokens_in", Unit: "tokens",
			Quantity: 1, OccurredAt: now.Format(time.RFC3339),
		})
	}
	records = append(records, models.UsageRecord{
		OrganizationID: acct.ID, MetricType: "tokens_out", Unit: "tokens",
		Quantity: 7, OccurredAt: now.Format(time.RFC3339),
	})
	// The account summary is a fixed 30-day window, not a lifetime counter.
	records = append(records, models.UsageRecord{
		OrganizationID: acct.ID, MetricType: "tokens_in", Unit: "tokens",
		Quantity: 99, OccurredAt: now.Add(-31 * 24 * time.Hour).Format(time.RFC3339),
	})
	if err := db.Create(&records).Error; err != nil {
		t.Fatal(err)
	}

	rec := doPublicOpsJSON(t, srv, "GET", "/api/public/accounts", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("accounts failed: %d %s", rec.Code, rec.Body.String())
	}
	var rows []struct {
		ID    string `json:"id"`
		Usage struct {
			InputTokens  string `json:"input_tokens"`
			OutputTokens string `json:"output_tokens"`
			RecordCount  string `json:"record_count"`
			State        string `json:"state"`
			Complete     bool   `json:"complete"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != acct.ID {
		t.Fatalf("unexpected accounts: %+v", rows)
	}
	usage := rows[0].Usage
	if usage.InputTokens != "9007199254741193" || usage.OutputTokens != "7" || usage.RecordCount != "202" {
		t.Fatalf("uncapped exact usage summary mismatch: %+v", usage)
	}
	if usage.State != string(MeterStateRecorded) || !usage.Complete {
		t.Fatalf("usage summary state mismatch: %+v", usage)
	}
}

func TestPublicAccountsUsageStatesAreExplicit(t *testing.T) {
	srv, db := publicOpsTestServer(t)
	token := publicOpsOperatorToken(t, srv, db)
	empty := models.Account{Email: "empty@corp.kr", DisplayName: "Empty"}
	invalid := models.Account{Email: "invalid@corp.kr", DisplayName: "Invalid"}
	delayed := models.Account{Email: "delayed@corp.kr", DisplayName: "Delayed"}
	zero := models.Account{Email: "zero@corp.kr", DisplayName: "Zero"}
	for _, account := range []*models.Account{&empty, &invalid, &delayed, &zero} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if err := db.Create(&models.UsageRecord{OrganizationID: invalid.ID, MetricType: "tokens_in", Unit: "seconds", Quantity: 4, OccurredAt: now.Format(time.RFC3339)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UsageRecord{Base: models.Base{CreatedAt: now}, OrganizationID: delayed.ID, MetricType: "tokens_out", Unit: "tokens", Quantity: 5, OccurredAt: now.Add(-time.Hour).Format(time.RFC3339)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UsageRecord{OrganizationID: zero.ID, MetricType: "tokens_in", Unit: "tokens", Quantity: 0, OccurredAt: now.Format(time.RFC3339)}).Error; err != nil {
		t.Fatal(err)
	}

	rec := doPublicOpsJSON(t, srv, "GET", "/api/public/accounts", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("accounts failed: %d %s", rec.Code, rec.Body.String())
	}
	var rows []struct {
		ID    string             `json:"id"`
		Usage publicAccountUsage `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	states := map[string]publicAccountUsage{}
	for _, row := range rows {
		states[row.ID] = row.Usage
	}
	if usage := states[empty.ID]; usage.State != MeterStateUnavailable || usage.ReasonCode != "no_meter_event" || !usage.Complete {
		t.Fatalf("empty usage state mismatch: %+v", usage)
	}
	if usage := states[invalid.ID]; usage.State != MeterStateError || usage.ReasonCode != "invalid_meter_unit" || usage.Complete || usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.RecordCount != 0 {
		t.Fatalf("invalid usage state mismatch: %+v", usage)
	}
	if usage := states[delayed.ID]; usage.State != MeterStateDelayed || usage.ReasonCode != "meter_delayed" || !usage.Complete || usage.OutputTokens != 5 {
		t.Fatalf("delayed usage state mismatch: %+v", usage)
	}
	if usage := states[zero.ID]; usage.State != MeterStateZero || !usage.Complete || usage.RecordCount != 1 {
		t.Fatalf("zero usage state mismatch: %+v", usage)
	}
}

func TestPublicAccountsGraduatedResponse(t *testing.T) {
	srv, db := publicOpsTestServer(t)
	token := publicOpsOperatorToken(t, srv, db)
	acct := models.Account{Email: "a@corp.kr", DisplayName: "A", SubscriptionStatus: "active"}
	db.Create(&acct)

	rec := doPublicOpsJSON(t, srv, "GET", "/api/public/accounts", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("accounts failed: %d", rec.Code)
	}
	var rows []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0]["ladder_rung"] != "normal" {
		t.Fatalf("accounts wrong: %v", rows)
	}

	// Graduated response requires a reason.
	rec = doPublicOpsJSON(t, srv, "POST", "/api/public/accounts/"+acct.ID+"/action",
		`{"dimension":"trust_safety","state":"restricted"}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reason should be required: %d", rec.Code)
	}
	rec = doPublicOpsJSON(t, srv, "POST", "/api/public/accounts/"+acct.ID+"/action",
		`{"dimension":"trust_safety","state":"restricted","reason":"abuse confirmed"}`, token)
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
	token := publicOpsOperatorToken(t, srv, db)
	acct := models.Account{Email: "b@corp.kr", DisplayName: "B"}
	db.Create(&acct)

	rec := doPublicOpsJSON(t, srv, "POST", "/api/public/support-cases",
		`{"account_id":"`+acct.ID+`","subject":"billing","priority":"high"}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("support case failed: %d %s", rec.Code, rec.Body.String())
	}
	var sc models.SupportCase
	json.Unmarshal(rec.Body.Bytes(), &sc)
	rec = doPublicOpsJSON(t, srv, "PUT", "/api/public/support-cases/"+sc.ID,
		`{"status":"in_progress","note":"assigned"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("case update failed: %d", rec.Code)
	}
	rec = doPublicOpsJSON(t, srv, "POST", "/api/public/abuse-cases",
		`{"account_id":"`+acct.ID+`","category":"account_sharing","severity":"high"}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("abuse case failed: %d %s", rec.Code, rec.Body.String())
	}
	var ab models.AbuseCase
	json.Unmarshal(rec.Body.Bytes(), &ab)
	rec = doPublicOpsJSON(t, srv, "PUT", "/api/public/abuse-cases/"+ab.ID,
		`{"status":"action_taken","decision":"restricted 30d"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("abuse update failed: %d", rec.Code)
	}
	// Segment tagging (12 A8).
	rec = doPublicOpsJSON(t, srv, "PUT", "/api/public/segments",
		`{"account_id":"`+acct.ID+`","segment":"enterprise-pilot"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("segment failed: %d", rec.Code)
	}
	rec = doPublicOpsJSON(t, srv, "GET", "/api/public/segments", "", token)
	var segs []string
	json.Unmarshal(rec.Body.Bytes(), &segs)
	if len(segs) != 1 || segs[0] != "enterprise-pilot" {
		t.Fatalf("segments wrong: %v", segs)
	}
}
