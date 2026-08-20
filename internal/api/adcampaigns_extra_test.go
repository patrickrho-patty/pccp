package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto/ed25519"
	"encoding/base64"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func adTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/ad.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.AdCampaign{}, &models.AdMeasurementEvent{}, &models.AdCatalogSnapshot{},
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

func adJSON(t *testing.T, srv *Server, method, path, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-ad", Email: "ops@patty.dev", Role: role}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func adPublic(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func adCreateCampaign(t *testing.T, srv *Server, ceiling int, cpm, budget int64) uint {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"advertiser": "Acme", "category": "dev-tools",
		"headline_en": "Ship faster with Acme", "body_en": "Try Acme Pro today",
		"destination_url": "https://acme.example.com/pro",
		"weight":          1, "impression_ceiling": ceiling,
		"cpm_minor": cpm, "budget_minor": budget, "currency": "KRW",
	})
	w := adJSON(t, srv, "POST", "/api/adcampaigns/", string(body), "super_admin")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var view map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &view)
	return uint(view["id"].(float64))
}

func adEvent(t *testing.T, srv *Server, eventID string, campaign uint, rev int, typ string) *httptest.ResponseRecorder {
	t.Helper()
	return adPublic(t, srv, "POST", "/api/public/ads/events", fmt.Sprintf(
		`{"event_id":%q,"campaign_id":%d,"creative_revision":%d,"type":%q,"timestamp":"2026-08-20T00:00:00Z","catalog_revision":1}`,
		eventID, campaign, rev, typ))
}

// Operator boundary: ordinary tenant admin/owner cannot mutate; only
// super_admin (patty_ops) can.
func TestADOperatorBoundary(t *testing.T) {
	srv, _ := adTestServer(t)
	body := `{"advertiser":"X","headline_en":"H","body_en":"B","destination_url":"https://x.example.com","weight":1,"cpm_minor":1000,"budget_minor":10000}`
	if w := adJSON(t, srv, "POST", "/api/adcampaigns/", body, "admin"); w.Code != http.StatusForbidden {
		t.Fatalf("tenant admin created campaign: %d", w.Code)
	}
	if w := adJSON(t, srv, "POST", "/api/adcampaigns/", body, "owner"); w.Code != http.StatusForbidden {
		t.Fatalf("owner created campaign: %d", w.Code)
	}
	if w := adJSON(t, srv, "POST", "/api/adcampaigns/", body, "super_admin"); w.Code != http.StatusCreated {
		t.Fatalf("super_admin rejected: %d", w.Code)
	}
}

// Integer accounting: spend = impressions×CPM/1000 floored, capped by
// budget; expected = min(ceiling, budget capacity).
func TestADIntegerAccounting(t *testing.T) {
	srv, db := adTestServer(t)
	// CPM 1500 minor, budget 10000 minor → capacity 6666 impressions;
	// ceiling 10 → expected 10; spend at 10 = 15.
	id := adCreateCampaign(t, srv, 10, 1500, 10000)
	adJSON(t, srv, "POST", fmt.Sprintf("/api/adcampaigns/%d/lifecycle", id), `{"action":"activate","reason":"go"}`, "super_admin")
	for i := 0; i < 10; i++ {
		adEvent(t, srv, fmt.Sprintf("ev-%d", i), id, 1, "impression")
	}
	var c models.AdCampaign
	db.First(&c, "id = ?", id)
	if c.ValidatedImpressions != 10 {
		t.Fatalf("impressions = %d, want 10 (ceiling)", c.ValidatedImpressions)
	}
	if spend := adSpendMinor(c.ValidatedImpressions, c.CpmMinor); spend != 15 {
		t.Fatalf("spend = %d, want 15 (10×1500/1000)", spend)
	}
	// View fields consistent.
	w := adJSON(t, srv, "GET", "/api/adcampaigns/", "", "super_admin")
	var views []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &views)
	v := views[0]
	if v["expected_impressions"] != float64(10) {
		t.Fatalf("expected = %v, want 10", v["expected_impressions"])
	}
	if v["spend_minor"] != float64(15) || v["remaining_budget_minor"] != float64(9985) {
		t.Fatalf("spend/remaining: %v/%v", v["spend_minor"], v["remaining_budget_minor"])
	}
	// Budget-bound campaign (no ceiling): CPM 1000, budget 5000 →
	// capacity 5000 impressions; the 5001st is not billable.
	id2 := adCreateCampaign(t, srv, 0, 1000, 5000)
	adJSON(t, srv, "POST", fmt.Sprintf("/api/adcampaigns/%d/lifecycle", id2), `{"action":"activate","reason":"go"}`, "super_admin")
	for i := 0; i < 5001; i++ {
		adEvent(t, srv, fmt.Sprintf("b-%d", i), id2, 1, "impression")
	}
	var c2 models.AdCampaign
	db.First(&c2, "id = ?", id2)
	if c2.ValidatedImpressions != 5000 {
		t.Fatalf("budget-bound impressions = %d, want 5000 (spend cap)", c2.ValidatedImpressions)
	}
	if spend := adSpendMinor(c2.ValidatedImpressions, c2.CpmMinor); spend != 5000 {
		t.Fatalf("spend = %d, want 5000 = budget", spend)
	}
}

// Idempotency: the same event_id is counted once even on retry; stale
// revisions and unknown campaigns are never counted.
func TestADMeasurementIdempotency(t *testing.T) {
	srv, db := adTestServer(t)
	id := adCreateCampaign(t, srv, 0, 1000, 100000)
	adJSON(t, srv, "POST", fmt.Sprintf("/api/adcampaigns/%d/lifecycle", id), `{"action":"activate","reason":"go"}`, "super_admin")
	// Counted.
	if w := adEvent(t, srv, "dup-1", id, 1, "impression"); w.Code != http.StatusOK {
		t.Fatalf("event: %d", w.Code)
	}
	// Retry with the same id → duplicate, not counted again.
	if w := adEvent(t, srv, "dup-1", id, 1, "impression"); w.Code != http.StatusOK {
		t.Fatalf("retry: %d", w.Code)
	}
	var c models.AdCampaign
	db.First(&c, "id = ?", id)
	if c.ValidatedImpressions != 1 {
		t.Fatalf("impressions after retry = %d, want 1", c.ValidatedImpressions)
	}
	// Stale creative revision → not counted.
	adEvent(t, srv, "stale-1", id, 99, "impression")
	db.First(&c, "id = ?", id)
	if c.ValidatedImpressions != 1 {
		t.Fatalf("stale revision counted: %d", c.ValidatedImpressions)
	}
	// Unknown campaign → acknowledged, no panic.
	if w := adEvent(t, srv, "ghost-1", 9999, 1, "impression"); w.Code != http.StatusOK {
		t.Fatalf("unknown campaign: %d", w.Code)
	}
}

// Privacy: the measurement record carries no identity fields; the
// stored row is exactly the allowed field set.
func TestADMeasurementPrivacy(t *testing.T) {
	srv, db := adTestServer(t)
	id := adCreateCampaign(t, srv, 0, 1000, 100000)
	adJSON(t, srv, "POST", fmt.Sprintf("/api/adcampaigns/%d/lifecycle", id), `{"action":"activate","reason":"go"}`, "super_admin")
	// A client tries to smuggle identity fields — the handler ignores
	// everything outside the contract.
	adPublic(t, srv, "POST", "/api/public/ads/events", fmt.Sprintf(
		`{"event_id":"p-1","campaign_id":%d,"creative_revision":1,"type":"impression","user_id":"u1","harness_id":"h1","device":"mac","session":"s1"}`, id))
	var ev models.AdMeasurementEvent
	db.First(&ev, "event_id = ?", "p-1")
	raw, _ := json.Marshal(ev)
	for _, banned := range []string{`"user_id"`, `"harness_id"`, `"device"`, `"session"`, `"organization"`} {
		if bytes.Contains(raw, []byte(banned)) {
			t.Fatalf("measurement row leaks %s: %s", banned, raw)
		}
	}
	if ev.CampaignID != id || ev.Type != "impression" {
		t.Fatalf("measurement row wrong: %+v", ev)
	}
}

// Signed catalog: eligible-only, bounded, versioned, ed25519 signature
// verifies; expired catalog serves empty.
func TestADSignedCatalog(t *testing.T) {
	srv, db := adTestServer(t)
	id1 := adCreateCampaign(t, srv, 0, 1000, 100000)
	id2 := adCreateCampaign(t, srv, 0, 1000, 100000)
	// Activate only #1; #2 stays draft → excluded.
	adJSON(t, srv, "POST", fmt.Sprintf("/api/adcampaigns/%d/lifecycle", id1), `{"action":"activate","reason":"go"}`, "super_admin")
	if w := adJSON(t, srv, "POST", "/api/adcampaigns/catalog/publish", `{}`, "super_admin"); w.Code != http.StatusCreated {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	w := adPublic(t, srv, "GET", "/api/public/ads/catalog", "")
	if w.Code != http.StatusOK {
		t.Fatalf("catalog: %d", w.Code)
	}
	var envelope struct {
		Catalog   json.RawMessage `json:"catalog"`
		Signature string          `json:"signature"`
		KeyID     string          `json:"key_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	var payload struct {
		Schema    string                   `json:"schema"`
		Revision  int                      `json:"catalog_revision"`
		Campaigns []map[string]interface{} `json:"campaigns"`
	}
	if err := json.Unmarshal(envelope.Catalog, &payload); err != nil {
		t.Fatalf("catalog payload: %v", err)
	}
	if payload.Schema != "patty.ads.catalog.v1" || len(payload.Campaigns) != 1 {
		t.Fatalf("catalog wrong: %s", envelope.Catalog)
	}
	if float64(payload.Campaigns[0]["campaign_id"].(float64)) != float64(id1) {
		t.Fatalf("draft campaign leaked into catalog")
	}
	// The signature verifies over the EXACT served catalog bytes —
	// no re-canonicalization round-trip.
	priv, err := keys.LoadOrCreate(db, "ad-catalog")
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	sig, _ := base64.StdEncoding.DecodeString(envelope.Signature)
	if !ed25519.Verify(pub, []byte(envelope.Catalog), sig) {
		t.Fatal("catalog signature does not verify over the served bytes")
	}
	// Expired catalog → empty + expired flag.
	var snap models.AdCatalogSnapshot
	db.Order("revision DESC").First(&snap)
	db.Model(&snap).Update("expires_at", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339))
	w = adPublic(t, srv, "GET", "/api/public/ads/catalog", "")
	var expired struct {
		Campaigns []interface{} `json:"campaigns"`
		Expired   bool          `json:"expired"`
	}
	json.Unmarshal(w.Body.Bytes(), &expired)
	if !expired.Expired || len(expired.Campaigns) != 0 {
		t.Fatalf("expired catalog served campaigns: %s", w.Body.String())
	}
	_ = id2
}

// Creative validation + budget floor + lifecycle gating.
func TestADValidationAndLifecycle(t *testing.T) {
	srv, db := adTestServer(t)
	// http URL rejected.
	bad, _ := json.Marshal(map[string]interface{}{
		"advertiser": "X", "headline_en": "H", "body_en": "B",
		"destination_url": "http://insecure.example.com", "cpm_minor": 1000, "budget_minor": 10000,
	})
	if w := adJSON(t, srv, "POST", "/api/adcampaigns/", string(bad), "super_admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("http URL accepted: %d", w.Code)
	}
	// Missing English creative rejected.
	noEn, _ := json.Marshal(map[string]interface{}{
		"advertiser": "X", "headline_ko": "한글", "body_ko": "한글본문",
		"destination_url": "https://x.example.com", "cpm_minor": 1000, "budget_minor": 10000,
	})
	if w := adJSON(t, srv, "POST", "/api/adcampaigns/", string(noEn), "super_admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("EN-less creative accepted: %d", w.Code)
	}
	// Budget below spent cannot be set.
	id := adCreateCampaign(t, srv, 0, 1000, 100000)
	adJSON(t, srv, "POST", fmt.Sprintf("/api/adcampaigns/%d/lifecycle", id), `{"action":"activate","reason":"go"}`, "super_admin")
	for i := 0; i < 20; i++ {
		adEvent(t, srv, fmt.Sprintf("c-%d", i), id, 1, "impression")
	}
	var c models.AdCampaign
	db.First(&c, "id = ?", id)
	spent := adSpendMinor(c.ValidatedImpressions, c.CpmMinor)
	cut, _ := json.Marshal(map[string]interface{}{"budget_minor": spent - 1})
	if w := adJSON(t, srv, "PUT", fmt.Sprintf("/api/adcampaigns/%d", id), string(cut), "super_admin"); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("budget cut below spend accepted: %d", w.Code)
	}
	// Paused campaign impressions are not billable.
	adJSON(t, srv, "POST", fmt.Sprintf("/api/adcampaigns/%d/lifecycle", id), `{"action":"pause","reason":"hold"}`, "super_admin")
	adEvent(t, srv, "paused-1", id, 1, "impression")
	db.First(&c, "id = ?", id)
	before := c.ValidatedImpressions
	if before != 20 {
		t.Fatalf("baseline drifted: %d", before)
	}
}

// Window normalization: non-UTC offsets are normalized on write so the
// lexicographic SQL gate agrees with parsed-time eligibility.
func TestADWindowNormalization(t *testing.T) {
	srv, db := adTestServer(t)
	body, _ := json.Marshal(map[string]interface{}{
		"advertiser": "Acme", "headline_en": "H", "body_en": "B",
		"destination_url": "https://acme.example.com", "weight": 1,
		"cpm_minor": 1000, "budget_minor": 100000,
		// +09:00 offset: 2026-08-20T12:00+09:00 = 03:00Z — in the past.
		"end_at": "2026-08-20T12:00:00+09:00",
	})
	w := adJSON(t, srv, "POST", "/api/adcampaigns/", string(body), "super_admin")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var c models.AdCampaign
	db.First(&c)
	if c.EndAt != "2026-08-20T03:00:00Z" {
		t.Fatalf("end_at not normalized to UTC Z-form: %q", c.EndAt)
	}
	// Unparseable timestamps rejected.
	bad, _ := json.Marshal(map[string]interface{}{
		"advertiser": "Acme", "headline_en": "H", "body_en": "B",
		"destination_url": "https://acme.example.com", "weight": 1,
		"cpm_minor": 1000, "budget_minor": 100000, "start_at": "not-a-time",
	})
	if w := adJSON(t, srv, "POST", "/api/adcampaigns/", string(bad), "super_admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("garbage timestamp accepted: %d", w.Code)
	}
}

// Post-increment budget bound: with CPM 1999 and budget 1000 the 501st
// impression must be refused (pre-increment logic would overshoot to
// 1001 > 1000).
func TestADPostIncrementBudgetBound(t *testing.T) {
	srv, db := adTestServer(t)
	id := adCreateCampaign(t, srv, 0, 1999, 1000)
	adJSON(t, srv, "POST", fmt.Sprintf("/api/adcampaigns/%d/lifecycle", id), `{"action":"activate","reason":"go"}`, "super_admin")
	for i := 0; i < 501; i++ {
		adEvent(t, srv, fmt.Sprintf("pi-%d", i), id, 1, "impression")
	}
	var c models.AdCampaign
	db.First(&c, "id = ?", id)
	if spend := adSpendMinor(c.ValidatedImpressions, c.CpmMinor); spend > c.BudgetMinor {
		t.Fatalf("spend %d exceeded budget %d (post-increment bound broken)", spend, c.BudgetMinor)
	}
	if c.ValidatedImpressions != 500 {
		t.Fatalf("impressions = %d, want 500 (501st must be refused)", c.ValidatedImpressions)
	}
}

// Click redirect: active campaigns increment atomically and forward;
// inactive campaigns 404 with no increment.
func TestADClickRedirectGating(t *testing.T) {
	srv, db := adTestServer(t)
	id := adCreateCampaign(t, srv, 0, 1000, 100000)
	// Draft → 404, no click.
	if w := adPublic(t, srv, "GET", fmt.Sprintf("/api/public/ads/go/%d", id), ""); w.Code != http.StatusNotFound {
		t.Fatalf("draft campaign redirected: %d", w.Code)
	}
	var c models.AdCampaign
	db.First(&c, "id = ?", id)
	if c.Clicks != 0 {
		t.Fatalf("draft campaign click counted: %d", c.Clicks)
	}
	adJSON(t, srv, "POST", fmt.Sprintf("/api/adcampaigns/%d/lifecycle", id), `{"action":"activate","reason":"go"}`, "super_admin")
	if w := adPublic(t, srv, "GET", fmt.Sprintf("/api/public/ads/go/%d", id), ""); w.Code != http.StatusFound {
		t.Fatalf("active redirect: %d", w.Code)
	}
	db.First(&c, "id = ?", id)
	if c.Clicks != 1 {
		t.Fatalf("click not counted: %d", c.Clicks)
	}
}

// Campaigns list is operator-only.
func TestADCampaignsListOperatorOnly(t *testing.T) {
	srv, _ := adTestServer(t)
	if w := adJSON(t, srv, "GET", "/api/adcampaigns/", "", "admin"); w.Code != http.StatusForbidden {
		t.Fatalf("tenant admin listed campaigns: %d", w.Code)
	}
	if w := adJSON(t, srv, "GET", "/api/adcampaigns/", "", "super_admin"); w.Code != http.StatusOK {
		t.Fatalf("super_admin list rejected: %d", w.Code)
	}
}
