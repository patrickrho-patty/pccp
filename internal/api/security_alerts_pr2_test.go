package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// PAT-1502 PR 2 — durable secret-reference, rotate, test, SSRF,
// tampered-ciphertext, wrong-key, legacy dual-read. Uses the
// securityTestServer helper, which wires a deterministic local
// KeyProvider.

func TestAlertEndpointStoredEncrypted(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-enc", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack","target":"https://hooks.slack.com/services/T1/B1/secret"}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var stored models.AlertEndpoint
	db.First(&stored, "organization_id = ?", org.ID)
	if stored.TargetEnc == "" {
		t.Fatalf("TargetEnc must be populated after create")
	}
	if stored.Target != "" {
		t.Fatalf("legacy Target column must be empty on new writes (got %q)", stored.Target)
	}
	if stored.TargetKEKID == "" {
		t.Fatalf("TargetKEKID must be set to the provider's KEKID")
	}
	// The envelope is base64(JSON). It must not contain the plaintext URL.
	envJSON, err := base64.StdEncoding.DecodeString(stored.TargetEnc)
	if err != nil {
		t.Fatalf("TargetEnc must be base64: %v", err)
	}
	if strings.Contains(string(envJSON), "hooks.slack.com") {
		t.Fatalf("encrypted envelope must not contain the URL host")
	}
	if strings.Contains(string(envJSON), "secret") {
		t.Fatalf("encrypted envelope must not contain the URL path tail")
	}
}

func TestAlertEndpointCreateFailClosedWithoutProvider(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-no-kek", Status: "active"}
	db.Create(&org)
	srv.SetKeyProvider(nil) // PAT-1502 PR 2: production path must fail closed.
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack","target":"https://example.test/webhook"}`,
		org.ID, "admin")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("create without KeyProvider must fail closed (503), got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlertEndpointOpenFailsOnTamperedCiphertext(t *testing.T) {
	// Seal a target, flip one byte in the envelope, expect Open to
	// fail with a tamper error. PAT-1502 PR 2.
	provider, err := keymgmt.NewLocalProvider(make([]byte, 32), "tamper-kek")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		// populate
	}
	// Rebuild with random bytes so we don't all-zeros.
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	provider, _ = keymgmt.NewLocalProvider(kek, "tamper-kek")
	ref, err := SealTarget(provider, "https://example.test/path")
	if err != nil {
		t.Fatal(err)
	}
	// Flip one byte in the base64 payload.
	raw, _ := base64.StdEncoding.DecodeString(ref.Encoded)
	raw[len(raw)-1] ^= 0x01
	ref.Encoded = base64.StdEncoding.EncodeToString(raw)
	if _, err := OpenTarget(provider, ref); err == nil {
		t.Fatalf("tampered envelope must fail to open")
	}
}

func TestAlertEndpointOpenFailsOnWrongKey(t *testing.T) {
	kek1 := make([]byte, 32)
	for i := range kek1 {
		kek1[i] = byte(i)
	}
	kek2 := make([]byte, 32)
	for i := range kek2 {
		kek2[i] = byte(255 - i)
	}
	prov1, _ := keymgmt.NewLocalProvider(kek1, "k1")
	prov2, _ := keymgmt.NewLocalProvider(kek2, "k2")
	ref, err := SealTarget(prov1, "https://example.test/path")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenTarget(prov2, ref); err == nil {
		t.Fatalf("opening with a different KEK must fail")
	}
}

func TestAlertEndpointOpenFailsOnKEKMismatch(t *testing.T) {
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	prov1, _ := keymgmt.NewLocalProvider(kek, "k1")
	prov2, _ := keymgmt.NewLocalProvider(kek, "k2") // same KEK bytes, different ID
	ref, err := SealTarget(prov1, "https://example.test/path")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenTarget(prov2, ref); err == nil {
		t.Fatalf("KEKID mismatch must fail closed")
	}
}

func TestAlertEndpointLegacyDualReadRoundTrip(t *testing.T) {
	// A row with only the legacy plaintext column must still dispatch
	// during the dual-read window. PAT-1502 PR 2.
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-legacy", Status: "active"}
	db.Create(&org)
	plainURL := "https://hooks.slack.com/services/LEGACY/X/Y"
	ep := models.AlertEndpoint{
		Base: models.Base{ID: "legacy-row"},
		OrganizationID: org.ID, Name: "legacy",
		Type: "slack", Target: plainURL, Enabled: true,
		SeveritiesJSON: `["high"]`,
	}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveTarget(srv.KeyProvider(), ep.TargetEnc, ep.TargetKEKID, ep.Target)
	if err != nil {
		t.Fatalf("legacy row must resolve: %v", err)
	}
	if resolved != plainURL {
		t.Fatalf("legacy dual-read returned %q, want %q", resolved, plainURL)
	}
	// Response DTO must still report secret_configured without
	// leaking the URL.
	rec := doJSONAsRole(t, srv, "GET", "/api/security/alerts", "", org.ID, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), plainURL) {
		t.Fatalf("legacy row still leaks the URL: %s", rec.Body.String())
	}
}

func TestAlertEndpointRotateReplacesSecret(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-rot", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack","target":"https://hooks.slack.com/services/A/B/original"}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)

	rec = doJSONAsRole(t, srv, "POST", "/api/security/alerts/"+created.ID+"/rotate",
		`{"target":"https://hooks.slack.com/services/A/B/rotated"}`,
		org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "original") {
		t.Fatalf("rotate response leaked the old URL: %s", body)
	}
	if strings.Contains(body, "rotated") {
		t.Fatalf("rotate response leaked the new URL: %s", body)
	}
	var rotated models.AlertEndpoint
	db.First(&rotated, "id = ?", created.ID)
	if rotated.Target != "" {
		t.Fatalf("legacy Target must be cleared on rotate, got %q", rotated.Target)
	}
	if rotated.TargetEnc == "" {
		t.Fatalf("TargetEnc must hold the rotated envelope")
	}
	// Audit row must include both old and new credential ids.
	var audits []models.AuditEvent
	db.Where("organization_id = ? AND event_type = ?", org.ID, "security.alert_endpoint.rotate").Find(&audits)
	if len(audits) != 1 {
		t.Fatalf("rotate must record one audit row, got %d", len(audits))
	}
	if !strings.Contains(audits[0].Details, "old_credential_id") || !strings.Contains(audits[0].Details, "new_credential_id") {
		t.Fatalf("rotate audit must include both ids: %s", audits[0].Details)
	}
	if strings.Contains(audits[0].Details, "hooks.slack.com") {
		t.Fatalf("rotate audit leaked a URL: %s", audits[0].Details)
	}
}

func TestAlertEndpointTestRequiresProvider(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-test-no-kek", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack","target":"https://hooks.slack.com/services/A/B/c"}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)
	srv.SetKeyProvider(nil)
	rec = doJSONAsRole(t, srv, "POST", "/api/security/alerts/"+created.ID+"/test", "", org.ID, "admin")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("test must fail closed without provider, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlertEndpointTestRejectsPrivateHost(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-ssrf", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack","target":"https://hooks.slack.com/services/A/B/c"}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed create: %d %s", rec.Code, rec.Body.String())
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)

	// Rotate to a loopback URL — must be rejected by SSRF guard.
	rec = doJSONAsRole(t, srv, "POST", "/api/security/alerts/"+created.ID+"/rotate",
		`{"target":"http://127.0.0.1:9000/internal"}`,
		org.ID, "admin")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rotate to loopback must be rejected, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlertEndpointTestRateLimited(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-rate", Status: "active"}
	db.Create(&org)
	// Inject a deterministic clock for the rate limiter.
	clk := &fakeClock{t: time.Now()}
	srv.testAlert = newTestAlertState()
	srv.testAlert.now = clk.Now

	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"webhook","target":"https://1.1.1.1/path"}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)

	// We can't easily stand up an HTTP server inside this test, so we
	// assert only the rate-limit outcome for the second call without
	// asserting dispatch success.
	rec = doJSONAsRole(t, srv, "POST", "/api/security/alerts/"+created.ID+"/test", "", org.ID, "admin")
	if rec.Code != http.StatusBadGateway && rec.Code != http.StatusOK {
		t.Fatalf("first test call should reach the network, got %d %s", rec.Code, rec.Body.String())
	}
	clk.advance(alertTestCooldown - time.Second) // still inside cooldown
	rec = doJSONAsRole(t, srv, "POST", "/api/security/alerts/"+created.ID+"/test", "", org.ID, "admin")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second test inside cooldown must be 429, got %d %s", rec.Code, rec.Body.String())
	}
	clk.advance(alertTestCooldown + 2*time.Second) // outside cooldown
	rec = doJSONAsRole(t, srv, "POST", "/api/security/alerts/"+created.ID+"/test", "", org.ID, "admin")
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("test after cooldown must not be 429, got %d", rec.Code)
	}
}

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time { return f.t }
func (f *fakeClock) advance(d time.Duration) { f.t = f.t.Add(d) }

func TestAlertEndpointTestTenantScoped(t *testing.T) {
	srv, db := securityTestServer(t)
	orgA := models.Organization{Name: "A", Slug: "A-iso-test", Status: "active"}
	orgB := models.Organization{Name: "B", Slug: "B-iso-test", Status: "active"}
	db.Create(&orgA)
	db.Create(&orgB)
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack","target":"https://hooks.slack.com/services/A/B/c"}`,
		orgA.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rec.Code)
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)
	rec = doJSONAsRole(t, srv, "POST", "/api/security/alerts/"+created.ID+"/test", "", orgB.ID, "admin")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("test on cross-tenant endpoint must 404, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlertEndpointTestRBAC(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-rbac-test", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, "POST", "/api/security/alerts",
		`{"name":"x","type":"slack","target":"https://hooks.slack.com/services/A/B/c"}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rec.Code)
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)
	rec = doJSONAsRole(t, srv, "POST", "/api/security/alerts/"+created.ID+"/test", "", org.ID, "viewer")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer must be 403 on test, got %d %s", rec.Code, rec.Body.String())
	}
}
