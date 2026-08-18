package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
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
	encoded, kekID, err := keymgmt.SealEncoded(provider, "https://example.test/path")
	if err != nil {
		t.Fatal(err)
	}
	// Flip one byte in the base64 payload.
	raw, _ := base64.StdEncoding.DecodeString(encoded)
	raw[len(raw)-1] ^= 0x01
	encoded = base64.StdEncoding.EncodeToString(raw)
	if _, err := keymgmt.OpenEncoded(provider, encoded, kekID); err == nil {
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
	encoded, kekID, err := keymgmt.SealEncoded(prov1, "https://example.test/path")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keymgmt.OpenEncoded(prov2, encoded, kekID); err == nil {
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
	encoded, kekID, err := keymgmt.SealEncoded(prov1, "https://example.test/path")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keymgmt.OpenEncoded(prov2, encoded, kekID); err == nil {
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
		Base:           models.Base{ID: "legacy-row"},
		OrganizationID: org.ID, Name: "legacy",
		Type: "slack", Target: plainURL, Enabled: true,
		SeveritiesJSON: `["high"]`,
	}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	resolved, err := keymgmt.OpenAlertSecret(srv.KeyProvider(), ep.TargetEnc, ep.TargetKEKID, ep.Target,
		ep.TargetBindingVersion, ep.CredentialID, keymgmt.AlertSecretContext{OrganizationID: org.ID, EndpointID: ep.ID, ProviderType: ep.Type})
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
	if strings.Contains(body, "/A/B/original") {
		t.Fatalf("rotate response leaked the old URL: %s", body)
	}
	if strings.Contains(body, "/A/B/rotated") {
		t.Fatalf("rotate response leaked the new URL: %s", body)
	}
	var rotateResponse AlertEndpointResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &rotateResponse); err != nil {
		t.Fatal(err)
	}
	if !rotateResponse.ProviderRevocationRequired {
		t.Fatal("replacing a different upstream credential must state that provider-side revocation remains required")
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
	srv.alertNow = clk.Now

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

func (f *fakeClock) Now() time.Time          { return f.t }
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

func TestAlertEndpointRejectsUnknownSeverity(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-severity-validation", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts",
		`{"name":"x","type":"webhook","target":"https://example.com/hook","severities":["critical","urgent"]}`,
		org.ID, "admin")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown severity must be rejected, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlertEndpointTestThrottleIsSharedAcrossServerReplicas(t *testing.T) {
	srv1, db := securityTestServer(t)
	srv2, err := New(db, "test-jwt-secret")
	if err != nil {
		t.Fatal(err)
	}
	srv2.SetKeyProvider(srv1.KeyProvider())
	doer := alertDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	srv1.SetAlertHTTPClient(doer)
	srv2.SetAlertHTTPClient(doer)
	org := models.Organization{Name: "o", Slug: "o-shared-rate", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv1, http.MethodPost, "/api/security/alerts",
		`{"name":"x","type":"webhook","target":"https://example.com/hook"}`, org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)
	if rec = doJSONAsRole(t, srv1, http.MethodPost, "/api/security/alerts/"+created.ID+"/test", "", org.ID, "admin"); rec.Code != http.StatusOK {
		t.Fatalf("first replica test: %d %s", rec.Code, rec.Body.String())
	}
	if rec = doJSONAsRole(t, srv2, http.MethodPost, "/api/security/alerts/"+created.ID+"/test", "", org.ID, "admin"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second replica bypassed shared throttle: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlertEndpointRotationCannotRaceReservedTest(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-test-rotate-cas", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts",
		`{"name":"x","type":"webhook","target":"https://example.com/original"}`, org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)
	var ep models.AlertEndpoint
	if err := db.First(&ep, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/security/alerts/"+ep.ID+"/test", nil)
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{Email: "admin@example.com", OrganizationID: org.ID, Role: "admin"}))
	reservedAt := time.Now().UTC()
	if err := srv.reserveAlertTest(req, ep, reservedAt); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	rec = doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts/"+ep.ID+"/rotate",
		`{"target":"https://example.com/rotated"}`, org.ID, "admin")
	if rec.Code != http.StatusConflict {
		t.Fatalf("rotation must not change a credential while its test is pending: %d %s", rec.Code, rec.Body.String())
	}
	var unchanged models.AlertEndpoint
	db.First(&unchanged, "id = ?", ep.ID)
	if unchanged.CredentialID != ep.CredentialID || unchanged.TargetEnc != ep.TargetEnc {
		t.Fatal("pending test credential was rotated")
	}
	if err := srv.finishAlertTest(req, ep, reservedAt, "2xx", "success", map[string]interface{}{"credential_id": ep.CredentialID, "status_class": "2xx"}); err != nil {
		t.Fatalf("finish: %v", err)
	}
	rec = doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts/"+ep.ID+"/rotate",
		`{"target":"https://example.com/rotated"}`, org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotation should proceed after test completion: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlertEndpointRotationDoesNotUndoConcurrentDisable(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-rotate-disable-cas", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts",
		`{"name":"x","type":"webhook","target":"https://example.com/original"}`, org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)
	var stale models.AlertEndpoint
	if err := db.First(&stale, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AlertEndpoint{}).Where("id = ?", stale.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	enc, kekID, credentialID, bindingVersion, err := keymgmt.SealAlertSecret(srv.KeyProvider(), "https://example.com/rotated", keymgmt.AlertSecretContext{
		OrganizationID: org.ID, EndpointID: stale.ID, ProviderType: stale.Type,
	})
	if err != nil {
		t.Fatal(err)
	}
	var enabled bool
	if err := db.Transaction(func(tx *gorm.DB) error {
		enabled, err = applyAlertRotation(tx, stale, enc, kekID, credentialID, bindingVersion, time.Now().UTC(), nil)
		return err
	}); err != nil {
		t.Fatalf("rotation with stale pre-disable read failed: %v", err)
	}
	if enabled {
		t.Fatal("rotation without an explicit enable request resurrected a disabled route")
	}
	var stored models.AlertEndpoint
	db.First(&stored, "id = ?", stale.ID)
	if stored.Enabled {
		t.Fatal("concurrent disable was overwritten by rotation")
	}
}

func TestAlertOperatorPermissionGrantFeedsSSOTokenIssuance(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-operator-grants", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, http.MethodPut, "/api/security/alert-operators/permissions",
		`{"email":"operator@example.com","role":"security_operator","permissions":["security.alert_endpoint.read","security.alert_endpoint.disable"]}`,
		org.ID, "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("grant: %d %s", rec.Code, rec.Body.String())
	}
	token, err := srv.auth.IssueToken("operator@example.com", org.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := srv.auth.VerifyToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Role != "security_operator" || len(claims.Permissions) != 2 {
		t.Fatalf("SSO token did not receive durable alert grants: %+v", claims)
	}
}

func doJSONWithAlertPermissions(t *testing.T, srv *Server, method, path, body, orgID, role string, permissions ...string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{
		Email:          "security-operator@pccp.test",
		OrganizationID: orgID,
		Role:           role,
		Permissions:    permissions,
	}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestAlertEndpointDisableHasSeparatelyGrantablePermissionAndAudit(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-disable", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts",
		`{"name":"x","type":"slack","target":"https://hooks.slack.com/services/A/B/c"}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)

	rec = doJSONWithAlertPermissions(t, srv, http.MethodPost, "/api/security/alerts/"+created.ID+"/disable", "", org.ID, "security_operator", AlertPermissionDisable)
	if rec.Code != http.StatusOK {
		t.Fatalf("explicit disable permission must authorize only disable: %d %s", rec.Code, rec.Body.String())
	}
	var stored models.AlertEndpoint
	db.First(&stored, "id = ?", created.ID)
	if stored.Enabled {
		t.Fatal("disable action did not persist")
	}

	rec = doJSONWithAlertPermissions(t, srv, http.MethodDelete, "/api/security/alerts/"+created.ID, "", org.ID, "security_operator", AlertPermissionDisable)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disable grant must not grant delete, got %d", rec.Code)
	}
	var audits []models.AuditEvent
	db.Where("organization_id = ? AND resource_id = ?", org.ID, created.ID).Find(&audits)
	joined, _ := json.Marshal(audits)
	if !strings.Contains(string(joined), "security.alert_endpoint.disable") || !strings.Contains(string(joined), "authorization_denied") {
		t.Fatalf("disable success and delete denial must both be audited: %s", joined)
	}
}

func TestAlertEndpointCredentialIDStableAcrossReseal(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-stable-fingerprint", Status: "active"}
	db.Create(&org)
	target := "https://hooks.slack.com/services/A/B/stable"
	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts",
		`{"name":"x","type":"slack","target":"`+target+`"}`, org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)
	var before models.AlertEndpoint
	db.First(&before, "id = ?", created.ID)

	rec = doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts/"+created.ID+"/rotate", `{"target":"`+target+`"}`, org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	var rotated AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &rotated)
	var after models.AlertEndpoint
	db.First(&after, "id = ?", created.ID)
	if before.TargetEnc == after.TargetEnc {
		t.Fatal("test requires a fresh envelope nonce")
	}
	if created.CredentialID == "" || created.CredentialID != rotated.CredentialID || before.CredentialID != after.CredentialID || !strings.HasPrefix(after.CredentialID, created.CredentialID) {
		t.Fatalf("plaintext-derived credential id changed across reseal: before=%q after=%q stored=%q", created.CredentialID, rotated.CredentialID, after.CredentialID)
	}
}

type alertDoerFunc func(*http.Request) (*http.Response, error)

func (f alertDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestAlertEndpointTestScrubsTransportErrorFromAudit(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-scrub", Status: "active"}
	db.Create(&org)
	target := "https://hooks.slack.com/services/A/B/never-log-this"
	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts",
		`{"name":"x","type":"slack","target":"`+target+`"}`, org.ID, "admin")
	var created AlertEndpointResponse
	json.Unmarshal(rec.Body.Bytes(), &created)
	srv.SetAlertHTTPClient(alertDoerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed for " + target)
	}))

	rec = doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts/"+created.ID+"/test", "", org.ID, "admin")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("test: %d %s", rec.Code, rec.Body.String())
	}
	var event models.AuditEvent
	db.Where("organization_id = ? AND event_type = ?", org.ID, "security.alert_endpoint.test").Order("created_at DESC").First(&event)
	if strings.Contains(event.Details, target) || strings.Contains(event.Details, "never-log-this") || strings.Contains(event.Details, "dial failed") {
		t.Fatalf("transport error leaked into audit: %s", event.Details)
	}
	if !strings.Contains(event.Details, "delivery_failed") {
		t.Fatalf("audit should preserve bounded reason code: %s", event.Details)
	}
}

func TestSetKeyProviderAlsoWiresBackgroundDispatch(t *testing.T) {
	srv, db := securityTestServer(t)
	org := models.Organization{Name: "o", Slug: "o-dispatch-provider", Status: "active"}
	db.Create(&org)
	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/security/alerts",
		`{"name":"x","type":"slack","target":"https://hooks.slack.com/services/A/B/c","severities":["critical"]}`,
		org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	srv.SetAlertHTTPClient(alertDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	}))
	finding := models.SecurityFinding{OrganizationID: org.ID, FindingType: "secret", Severity: "critical", Title: "x"}
	if delivered := srv.security.DispatchAlerts(org.ID, finding); delivered != 1 {
		t.Fatalf("background dispatcher could not use API provider, delivered=%d", delivered)
	}
}

var _ = context.Background
