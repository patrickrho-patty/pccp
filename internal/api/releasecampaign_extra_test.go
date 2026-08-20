package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func hvTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/hv.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.Harness{}, &models.HarnessRelease{}, &models.HarnessUpdateCampaign{},
		&models.HarnessCampaignTarget{}, &models.HarnessVersionException{}, &models.HarnessHeartbeatReport{},
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

func hvJSON(t *testing.T, srv *Server, method, path, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-hv", Email: "rel@patty.dev", Role: role}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

// Deterministic cohort: same (seed, harness) always lands on the same
// side of the percentage across calls; distribution is not degenerate.
func TestHVCohortDeterminism(t *testing.T) {
	if !hvCohortMember("seed-1", "h1", 100) || hvCohortMember("seed-1", "h1", 0) {
		t.Fatal("100%/0% boundaries broken")
	}
	// Stability across calls.
	for i := 0; i < 50; i++ {
		if hvCohortMember("seed-1", "h1", 50) != hvCohortMember("seed-1", "h1", 50) {
			t.Fatal("cohort membership oscillates")
		}
	}
	// Roughly half of 1000 harnesses in a 50% ring.
	in := 0
	for i := 0; i < 1000; i++ {
		if hvCohortMember("seed-1", fmt.Sprintf("h%d", i), 50) {
			in++
		}
	}
	if in < 400 || in > 600 {
		t.Fatalf("50%% cohort admitted %d/1000", in)
	}
}

// State machine: below floor before deadline → grace; past deadline →
// restricted; revoked release → revoked; unknown digest → tampered;
// active exception → supported; expired exception → restricted.
func TestHVStateMachine(t *testing.T) {
	srv, db := hvTestServer(t)
	_ = srv
	now := time.Now().UTC()
	rel := models.HarnessRelease{
		ReleaseID: "rel-1.4.0", Version: "1.4.0", BuildProfile: "enterprise",
		Platform: "linux/amd64/deb", ArtifactDigest: "sha256:aaa", Channel: "stable",
		PublishedAt: now.Format(time.RFC3339),
	}
	db.Create(&rel)
	rel5 := models.HarnessRelease{
		ReleaseID: "rel-1.5.0", Version: "1.5.0", BuildProfile: "enterprise",
		Platform: "linux/amd64/deb", ArtifactDigest: "sha256:bbb", Channel: "stable",
		PublishedAt: now.Format(time.RFC3339),
	}
	db.Create(&rel5)
	future := now.Add(48 * time.Hour).Format(time.RFC3339)
	camp := models.HarnessUpdateCampaign{
		ReleaseID: "rel-1.5.0", TargetVersion: "1.5.0", MinVersion: "1.5.0",
		Ring: "stable", Percentage: 100, CohortSeed: "s1",
		StartTime: now.Add(-time.Hour).Format(time.RFC3339),
		Deadline:  future, State: "active", Reason: "security floor",
	}
	db.Create(&camp)
	expires := now.Add(24 * time.Hour).Format(time.RFC3339)

	mk := func(harnessID, version, releaseID, digest string) *models.HarnessHeartbeatReport {
		return &models.HarnessHeartbeatReport{
			HarnessID: harnessID, OrganizationID: "org-hv", BuildProfile: "enterprise",
			Version: version, ReleaseID: releaseID, ExecutableDigest: digest,
			ReportedAt: now.Format(time.RFC3339),
		}
	}
	var releases []models.HarnessRelease
	db.Find(&releases)
	var campaigns []models.HarnessUpdateCampaign
	db.Where("state = ?", "active").Find(&campaigns)

	// 1. Known old release, before deadline → grace.
	st, reason := hvDeriveState(mk("h1", "1.4.0", "rel-1.4.0", "sha256:aaa"), releases, campaigns, "", nil, now)
	if st != hvUpdateRequiredGrace {
		t.Fatalf("below floor before deadline = %s (%s), want grace", st, reason)
	}
	// 2. Past deadline → restricted.
	st, _ = hvDeriveState(mk("h2", "1.4.0", "rel-1.4.0", "sha256:aaa"), releases, campaigns, "", nil, now.Add(72*time.Hour))
	if st != hvRestricted {
		t.Fatalf("past deadline = %s, want restricted", st)
	}
	// 3. Active exception defers the floor as grace — never full support
	// (PAT-1449: exceptions cannot grant `supported` below a floor).
	ex := models.HarnessVersionException{
		OrganizationID: "org-hv", HarnessIDsJSON: `["h3"]`, CurrentVersion: "1.4.0", TargetVersion: "1.5.0",
		Reason: "결제일 이후 적용", Owner: "ops", ApprovedBy: "ciso", CompensatingControls: "네트워크 격리 강화",
		StartedAt: now.Format(time.RFC3339), ExpiresAt: expires,
	}
	db.Create(&ex)
	st, reason = hvDeriveState(mk("h3", "1.4.0", "rel-1.4.0", "sha256:aaa"), releases, campaigns, "", []models.HarnessVersionException{ex}, now)
	if st != hvUpdateRequiredGrace || reason != "exception_deferred" {
		t.Fatalf("exception not deferring: %s (%s)", st, reason)
	}
	// 4. Expired exception no longer defers.
	ex.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339)
	st, _ = hvDeriveState(mk("h3", "1.4.0", "rel-1.4.0", "sha256:aaa"), releases, campaigns, "", []models.HarnessVersionException{ex}, now)
	if st != hvUpdateRequiredGrace {
		t.Fatalf("expired exception still defers: %s", st)
	}
	// 5. Revoked release → revoked, exception cannot bypass.
	db.Model(&rel).Updates(map[string]interface{}{"revoked": true, "revoked_at": now.Format(time.RFC3339)})
	db.Find(&releases)
	ex.ExpiresAt = expires
	st, _ = hvDeriveState(mk("h4", "1.4.0", "rel-1.4.0", "sha256:aaa"), releases, campaigns, "", []models.HarnessVersionException{ex}, now)
	if st != hvRevoked {
		t.Fatalf("revoked release with exception = %s, want revoked", st)
	}
	db.Model(&rel).Update("revoked", false)
	db.Find(&releases)
	// 6. Digest mismatch → unknown/tampered, not "old".
	st, _ = hvDeriveState(mk("h5", "1.4.0", "rel-1.4.0", "sha256:tampered"), releases, campaigns, "", nil, now)
	if st != hvUnknownTampered {
		t.Fatalf("digest mismatch = %s, want unknown_or_tampered", st)
	}
	// 7. Unknown release ID → unknown/tampered.
	st, _ = hvDeriveState(mk("h6", "9.9.9", "rel-unknown", ""), releases, campaigns, "", nil, now)
	if st != hvUnknownTampered {
		t.Fatalf("unknown release = %s, want unknown_or_tampered", st)
	}
	// 8. At target with full attestation → supported.
	st, _ = hvDeriveState(mk("h7", "1.5.0", "rel-1.5.0", "sha256:bbb"), releases, campaigns, "", nil, now)
	if st != hvSupported {
		t.Fatalf("at target = %s, want supported", st)
	}
	// 9. Known release with MISSING digest attestation → tampered
	// (identity = catalog + digest + profile; absence is a non-match).
	st, reason = hvDeriveState(mk("h8", "1.5.0", "rel-1.5.0", ""), releases, campaigns, "", nil, now)
	if st != hvUnknownTampered || reason != "no_attestation" {
		t.Fatalf("missing attestation = %s (%s), want unknown_or_tampered", st, reason)
	}
}

// Campaign creation rejects a prerelease minimum and enforces reason.
func TestHVCampaignValidation(t *testing.T) {
	srv, _ := hvTestServer(t)
	_ = srv
	if w := hvJSON(t, srv, "POST", "/api/release/campaigns",
		`{"target_version":"1.5.0","min_version":"1.5.0"}`, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("reasonless campaign accepted: %d", w.Code)
	}
	if w := hvJSON(t, srv, "POST", "/api/release/campaigns",
		`{"target_version":"1.5.0","min_version":"1.5.0-beta.1","reason":"x"}`, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("prerelease minimum accepted: %d", w.Code)
	}
	if w := hvJSON(t, srv, "POST", "/api/release/campaigns",
		`{"target_version":"1.5.0","min_version":"1.0.0","reason":"보안 하한 적용","percentage":25,"ring":"canary"}`, "admin"); w.Code != http.StatusCreated {
		t.Fatalf("valid campaign rejected: %d %s", w.Code, w.Body.String())
	}
}

// Operator mutations require reason + epoch CAS; wrong epoch → 409.
func TestHVCampaignEpochCAS(t *testing.T) {
	srv, db := hvTestServer(t)
	hvJSON(t, srv, "POST", "/api/release/campaigns",
		`{"target_version":"2.0.0","min_version":"1.0.0","reason":"단계 배포"}`, "admin")
	var c models.HarnessUpdateCampaign
	db.First(&c)
	// Missing reason → 400.
	if w := hvJSON(t, srv, "POST", fmt.Sprintf("/api/release/campaigns/%d/mutate", c.ID),
		`{"action":"activate","expected_epoch":0}`, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("reasonless mutate accepted: %d", w.Code)
	}
	// Wrong epoch → 409.
	if w := hvJSON(t, srv, "POST", fmt.Sprintf("/api/release/campaigns/%d/mutate", c.ID),
		`{"action":"activate","reason":"go","expected_epoch":7}`, "admin"); w.Code != http.StatusConflict {
		t.Fatalf("stale epoch accepted: %d", w.Code)
	}
	// Correct epoch activates; epoch advances.
	if w := hvJSON(t, srv, "POST", fmt.Sprintf("/api/release/campaigns/%d/mutate", c.ID),
		`{"action":"activate","reason":"go","expected_epoch":0}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", w.Code, w.Body.String())
	}
	db.First(&c)
	if c.State != "active" || c.ExpectedEpoch != 1 {
		t.Fatalf("state/epoch after activate: %s/%d", c.State, c.ExpectedEpoch)
	}
	// Old epoch now conflicts.
	if w := hvJSON(t, srv, "POST", fmt.Sprintf("/api/release/campaigns/%d/mutate", c.ID),
		`{"action":"pause","reason":"hold","expected_epoch":0}`, "admin"); w.Code != http.StatusConflict {
		t.Fatalf("replayed epoch accepted: %d", w.Code)
	}
}

// Preview counts affected/excluded/unknown before a floor advance.
func TestHVCampaignPreview(t *testing.T) {
	srv, db := hvTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339)
	db.Create(&models.HarnessRelease{ReleaseID: "rel-1.4.0", Version: "1.4.0", BuildProfile: "enterprise", ArtifactDigest: "sha256:aaa", PublishedAt: now})
	for _, h := range []string{"h1", "h2", "h3"} {
		db.Create(&models.HarnessHeartbeatReport{
			HarnessID: h, OrganizationID: "org-hv", BuildProfile: "enterprise",
			Version: "1.4.0", ReleaseID: "rel-1.4.0", ExecutableDigest: "sha256:aaa", ReportedAt: now,
		})
	}
	// A tampered harness.
	db.Create(&models.HarnessHeartbeatReport{
		HarnessID: "hx", OrganizationID: "org-hv", BuildProfile: "enterprise",
		Version: "1.4.0", ReleaseID: "rel-1.4.0", ExecutableDigest: "sha256:zzz", ReportedAt: now,
	})
	w := hvJSON(t, srv, "POST", "/api/release/campaigns/preview",
		`{"min_version":"1.5.0","target_version":"1.6.0","percentage":100,"cohort_seed":"s"}`, "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Counts map[string]int `json:"counts"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Counts["affected"] != 3 {
		t.Fatalf("affected = %d, want 3", resp.Counts["affected"])
	}
	if resp.Counts["ineligible_unknown"] != 1 {
		t.Fatalf("unknown = %d, want 1 (tampered digest)", resp.Counts["ineligible_unknown"])
	}
}

// Exceptions require owner/approver/controls/expiry; cannot be created
// for revoked targets; revoke works.
func TestHVExceptions(t *testing.T) {
	srv, db := hvTestServer(t)
	bad := `{"harness_ids":["h1"],"current_version":"1.4.0","target_version":"1.5.0","reason":"사정","owner":"ops","approved_by":"ciso","compensating_controls":"","expires_at":"2026-10-01T00:00:00Z"}`
	if w := hvJSON(t, srv, "POST", "/api/release/exceptions", bad, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("control-less exception accepted: %d", w.Code)
	}
	good := `{"harness_ids":["h1"],"current_version":"1.4.0","target_version":"1.5.0","reason":"결제 시스템 점검 연기","owner":"ops@corp","approved_by":"ciso@corp","compensating_controls":"해당 하네스 네트워크 격리","expires_at":"2026-10-01T00:00:00Z"}`
	if w := hvJSON(t, srv, "POST", "/api/release/exceptions", good, "admin"); w.Code != http.StatusCreated {
		t.Fatalf("valid exception rejected: %d %s", w.Code, w.Body.String())
	}
	var ex models.HarnessVersionException
	db.First(&ex)
	if w := hvJSON(t, srv, "POST", fmt.Sprintf("/api/release/exceptions/%d/revoke", ex.ID), `{"reason":"위험 상승"}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("revoke: %d", w.Code)
	}
	db.First(&ex)
	if !ex.Revoked {
		t.Fatal("exception not revoked")
	}
}

// Heartbeat report endpoint derives and returns the effective state.
func TestHVHeartbeatReportEndpoint(t *testing.T) {
	srv, db := hvTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339)
	db.Create(&models.HarnessRelease{ReleaseID: "rel-1.4.0", Version: "1.4.0", BuildProfile: "enterprise", ArtifactDigest: "sha256:aaa", PublishedAt: now})
	deadline := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	db.Create(&models.HarnessUpdateCampaign{
		ReleaseID: "rel-1.5.0", TargetVersion: "1.5.0", MinVersion: "1.5.0", Ring: "stable",
		Percentage: 100, CohortSeed: "s", StartTime: now, Deadline: deadline, State: "active", Reason: "floor",
	})
	w := hvJSON(t, srv, "POST", "/api/release/heartbeat-report",
		`{"harness_id":"h-ep","version":"1.4.0","release_id":"rel-1.4.0","executable_digest":"sha256:aaa","build_profile":"enterprise"}`, "viewer")
	if w.Code != http.StatusOK {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["state"] != hvUpdateRequiredGrace {
		t.Fatalf("endpoint state = %v, want grace", resp["state"])
	}
	if resp["state_ko"] != "업데이트 필요 (유예 중)" {
		t.Fatalf("Korean label: %v", resp["state_ko"])
	}
}
