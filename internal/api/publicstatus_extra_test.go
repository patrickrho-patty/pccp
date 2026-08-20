package api

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func psTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/ps.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Account{}, &models.AuditEvent{},
		&models.ServiceSigningKey{}, &models.OrgSetting{}, &models.PublicStatusComponent{},
		&models.PublicStatusObservation{}, &models.PublicIncident{}, &models.PublicIncidentUpdate{},
		&models.PublicStatusDailyRollup{}, &models.PublicStatusSnapshot{},
		&models.PublicStatusSubscriber{}, &models.PublicStatusNotification{},
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

func psAdminJSON(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-ps", Email: "ops@patty.dev", Role: "admin"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func psPublic(t *testing.T, srv *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

// Anti-flapping: sustained partial impact → yellow; single success does
// not recover; sustained success → green; stale feed → gray (never green).
func TestPSEvaluatorAntiFlap(t *testing.T) {
	th := psThresholdsCurrent
	now := time.Now().UTC()
	c := models.PublicStatusComponent{MeasuredColor: psColorGreen, ConsecutiveSuccesses: 10, LastObservationAt: now.Format(time.RFC3339)}
	c.ConsecutiveFailures = th.DegradePartialAfter
	c.LastImpact = "partial"
	if got := psEvaluateComponent(&c, th, now); got != psColorYellow {
		t.Fatalf("partial×3 = %s, want yellow", got)
	}
	c.ConsecutiveFailures = th.EscalateWidespread
	c.LastImpact = "widespread"
	if got := psEvaluateComponent(&c, th, now); got != psColorRed {
		t.Fatalf("widespread×2 = %s, want red (immediate escalation)", got)
	}
	// One clean sample after red: hysteresis keeps red.
	c.ConsecutiveFailures = 0
	c.ConsecutiveSuccesses = 1
	c.MeasuredColor = psColorRed
	if got := psEvaluateComponent(&c, th, now); got != psColorRed {
		t.Fatalf("1 success after red = %s, want red (recovery window)", got)
	}
	c.ConsecutiveSuccesses = th.RecoverAfter
	if got := psEvaluateComponent(&c, th, now); got != psColorGreen {
		t.Fatalf("5 successes = %s, want green", got)
	}
	// Stale data must never present as operational.
	c.LastObservationAt = now.Add(-time.Hour).Format(time.RFC3339)
	if got := psEvaluateComponent(&c, th, now); got != psColorGray {
		t.Fatalf("stale = %s, want gray", got)
	}
}

// Rollup rules: partial impact half weight, maintenance half weight,
// no-data excluded from denominator.
func TestPSDailyRollupRules(t *testing.T) {
	obs := []models.PublicStatusObservation{
		{Success: true, Impact: "none", WindowSeconds: 40000},       // up
		{Success: false, Impact: "partial", WindowSeconds: 10000},   // 50% weighted down
		{Success: false, Impact: "widespread", WindowSeconds: 5000}, // full down
		{Maintenance: true, WindowSeconds: 20000},                   // 50% weighted
	}
	av, impacted, maint, noData := psDeriveDailyRollup(obs)
	if impacted != 15000 || maint != 20000 {
		t.Fatalf("impacted=%d maint=%d, want 15000/20000", impacted, maint)
	}
	if noData != 86400-75000 {
		t.Fatalf("noData=%d, want %d", noData, 86400-75000)
	}
	// measured=75000, down = 5000 + 5000 + 10000 = 20000
	want := (75000.0 - 20000.0) / 75000.0 * 100
	if av < want-0.001 || av > want+0.001 {
		t.Fatalf("availability=%v, want %v", av, want)
	}
}

// Public API: anonymous read of the last snapshot; stale snapshot serves
// gray components rather than failing.
func TestPSPublicSnapshotStaleServesGray(t *testing.T) {
	srv, db := psTestServer(t)
	// Seed a component + publish a snapshot.
	db.Create(&models.PublicStatusComponent{ID: "patty_code", NameKo: "Patty Code", Active: true, MeasuredColor: psColorGreen, RegistryVersion: 1})
	if w := psAdminJSON(t, srv, "POST", "/api/publicstatus/snapshot/publish", `{}`); w.Code != http.StatusCreated {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	var snap models.PublicStatusSnapshot
	db.Order("id DESC").First(&snap)
	// Backdate the snapshot beyond TTL.
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	db.Model(&snap).Update("generated_at", old)

	w := psPublic(t, srv, "GET", "/api/public/status")
	if w.Code != http.StatusOK {
		t.Fatalf("public status: %d", w.Code)
	}
	var payload map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &payload)
	if payload["stale"] != true {
		t.Fatalf("expected stale=true")
	}
	comps := payload["components"].([]interface{})
	c0 := comps[0].(map[string]interface{})
	if c0["color"] != psColorGray || c0["state_ko"] != psColorLabelKo[psColorGray] {
		t.Fatalf("stale snapshot must serve gray, got %v", c0)
	}
}

// Automatic detection: degraded observation creates a PRIVATE incident
// draft + on-call page audit, and publishes no machine narrative.
func TestPSAutoDetectionPrivateDraftNoNarrative(t *testing.T) {
	srv, db := psTestServer(t)
	// Warm the component to green first.
	for i := 0; i < 5; i++ {
		psAdminJSON(t, srv, "POST", "/api/publicstatus/observations",
			`[{"component_id":"patty_code","success":true,"impact":"none","window_seconds":60}]`)
	}
	// Sustained severe failure.
	for i := 0; i < psThresholdsCurrent.DegradeSevereAfter; i++ {
		psAdminJSON(t, srv, "POST", "/api/publicstatus/observations",
			`[{"component_id":"patty_code","success":false,"impact":"severe","window_seconds":60}]`)
	}
	var drafts []models.PublicIncident
	db.Where("published = ?", false).Find(&drafts)
	if len(drafts) != 1 {
		t.Fatalf("auto draft incidents = %d, want 1", len(drafts))
	}
	if drafts[0].State != "investigating" || drafts[0].TitleKo != "서비스 상태 확인 중" {
		t.Fatalf("draft state/title = %s/%s", drafts[0].State, drafts[0].TitleKo)
	}
	var paged int64
	db.Model(&models.AuditEvent{}).Where("event_type = ?", "cp.publicstatus.oncall_paged").Count(&paged)
	if paged == 0 {
		t.Fatal("on-call page audit event missing")
	}
	// More failures must not duplicate the open draft.
	for i := 0; i < 3; i++ {
		psAdminJSON(t, srv, "POST", "/api/publicstatus/observations",
			`[{"component_id":"patty_code","success":false,"impact":"severe","window_seconds":60}]`)
	}
	db.Where("published = ?", false).Find(&drafts)
	if len(drafts) != 1 {
		t.Fatalf("duplicate drafts: %d", len(drafts))
	}
}

// Overrides: worsening immediate; healthier requires reason+expiry;
// green-over-failing requires explicit false-positive ack.
func TestPSOverrideRules(t *testing.T) {
	srv, db := psTestServer(t)
	db.Create(&models.PublicStatusComponent{ID: "patty_code", NameKo: "Patty Code", Active: true, MeasuredColor: psColorRed, LastObservationAt: time.Now().Format(time.RFC3339)})
	expiry := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)

	// Healthier without reason → 422.
	if w := psAdminJSON(t, srv, "POST", "/api/publicstatus/components/patty_code/override",
		fmt.Sprintf(`{"color":"green","expires_at":"%s"}`, expiry)); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no-reason override: %d", w.Code)
	}
	// Green over red without false-positive ack → 409.
	if w := psAdminJSON(t, srv, "POST", "/api/publicstatus/components/patty_code/override",
		fmt.Sprintf(`{"color":"green","reason":"monitor false positive","expires_at":"%s"}`, expiry)); w.Code != http.StatusConflict {
		t.Fatalf("green-over-red without ack: %d", w.Code)
	}
	// Green over red WITH ack + reason + expiry → allowed, measured
	// disagreement remains visible to the console.
	if w := psAdminJSON(t, srv, "POST", "/api/publicstatus/components/patty_code/override",
		fmt.Sprintf(`{"color":"green","reason":"monitor false positive","expires_at":"%s","false_positive_ack":true}`, expiry)); w.Code != http.StatusOK {
		t.Fatalf("acked override: %d %s", w.Code, w.Body.String())
	}
	// Worsening applies immediately with no expiry required.
	if w := psAdminJSON(t, srv, "POST", "/api/publicstatus/components/patty_code/override",
		`{"color":"orange"}`); w.Code != http.StatusOK {
		t.Fatalf("worsening override: %d", w.Code)
	}
	var audited int64
	db.Model(&models.AuditEvent{}).Where("event_type = ?", "cp.publicstatus.override").Count(&audited)
	// Only the two successful overrides audit; the 422/409 rejections do not.
	if audited != 2 {
		t.Fatalf("override audit events = %d, want 2", audited)
	}
}

// Major-incident cadence anchors: 15-minute first update, then 30-minute
// cadence, with overdue surfaced by the console list.
func TestPSMajorIncidentCadence(t *testing.T) {
	srv, db := psTestServer(t)
	w := psAdminJSON(t, srv, "POST", "/api/publicstatus/incidents",
		`{"title_ko":"모델 응답 지연 발생","components":["patty_code"],"impact":"severe"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var inc models.PublicIncident
	db.First(&inc)
	// Confirm major + publish.
	psAdminJSON(t, srv, "PUT", fmt.Sprintf("/api/publicstatus/incidents/%d", inc.ID), `{"major":true,"publish":true}`)
	db.First(&inc)
	if inc.ConfirmedMajorAt == "" || inc.NextUpdateDueAt == "" {
		t.Fatal("cadence anchors missing after major confirm")
	}
	due, _ := time.Parse(time.RFC3339, inc.NextUpdateDueAt)
	anchor, _ := time.Parse(time.RFC3339, inc.ConfirmedMajorAt)
	if due.Sub(anchor) != 15*time.Minute {
		t.Fatalf("first update due = %v after anchor, want 15m", due.Sub(anchor))
	}
	// Posting an update resets the cadence to 30 minutes.
	if w := psAdminJSON(t, srv, "POST", fmt.Sprintf("/api/publicstatus/incidents/%d/updates", inc.ID),
		`{"body_ko":"원인을 확인했습니다. 조치 중입니다."}`); w.Code != http.StatusCreated {
		t.Fatalf("update: %d", w.Code)
	}
	db.First(&inc)
	due2, _ := time.Parse(time.RFC3339, inc.NextUpdateDueAt)
	last, _ := time.Parse(time.RFC3339, inc.LastUpdateAt)
	if due2.Sub(last) != 30*time.Minute {
		t.Fatalf("cadence after update = %v, want 30m", due2.Sub(last))
	}
	// Backdate the due time; console list must flag overdue.
	db.Model(&inc).Update("next_update_due_at", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339))
	list := psAdminJSON(t, srv, "GET", "/api/publicstatus/incidents?published=true", "")
	var arr []map[string]interface{}
	json.Unmarshal(list.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["update_overdue"] != true {
		t.Fatalf("overdue flag missing: %v", arr)
	}
}

// Subscriptions: create unverified → verify → incident update enqueues
// exactly one idempotent notification per transition.
func TestPSSubscriberVerificationAndIdempotentNotify(t *testing.T) {
	srv, db := psTestServer(t)
	w := psAdminJSON(t, srv, "POST", "/api/public/status/subscribers",
		`{"component_id":"patty_code","channel":"email","destination":"user@example.com"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body.String())
	}
	var sub models.PublicStatusSubscriber
	db.First(&sub)
	if sub.Verified {
		t.Fatal("subscription must start unverified")
	}
	// Abuse rate limit: >5 creations/hour for same destination → 429.
	for i := 0; i < 5; i++ {
		psAdminJSON(t, srv, "POST", "/api/public/status/subscribers",
			`{"component_id":"patty_code","channel":"email","destination":"user@example.com"}`)
	}
	if w := psAdminJSON(t, srv, "POST", "/api/public/status/subscribers",
		`{"component_id":"patty_code","channel":"email","destination":"user@example.com"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit: %d", w.Code)
	}
	if w := psPublic(t, srv, "GET", "/api/public/status/subscribers/verify?token="+sub.VerifyToken); w.Code != http.StatusOK {
		t.Fatalf("verify: %d", w.Code)
	}
	// Create + publish an incident, post an update → notification queued
	// once per (subscriber, incident, transition).
	psAdminJSON(t, srv, "POST", "/api/publicstatus/incidents",
		`{"title_ko":"일부 기능 오류","components":["patty_code"],"impact":"partial"}`)
	var inc models.PublicIncident
	db.First(&inc)
	psAdminJSON(t, srv, "PUT", fmt.Sprintf("/api/publicstatus/incidents/%d", inc.ID), `{"publish":true}`)
	psAdminJSON(t, srv, "POST", fmt.Sprintf("/api/publicstatus/incidents/%d/updates", inc.ID), `{"body_ko":"조치 완료했습니다."}`)
	psAdminJSON(t, srv, "POST", fmt.Sprintf("/api/publicstatus/incidents/%d/updates", inc.ID), `{"body_ko":"안정화 확인 중입니다."}`)
	var notes []models.PublicStatusNotification
	db.Where("subscriber_id = ?", sub.ID).Find(&notes)
	if len(notes) != 1 || notes[0].Transition != "update" {
		t.Fatalf("notifications = %d (transition %s), want 1 idempotent update", len(notes), notes[0].Transition)
	}
}

// Snapshot signature verifies against the status-publisher key.
func TestPSSnapshotSignature(t *testing.T) {
	srv, db := psTestServer(t)
	db.Create(&models.PublicStatusComponent{ID: "patty_code", NameKo: "Patty Code", Active: true, MeasuredColor: psColorGreen, LastObservationAt: time.Now().Format(time.RFC3339)})
	psAdminJSON(t, srv, "POST", "/api/publicstatus/snapshot/publish", `{}`)
	var snap models.PublicStatusSnapshot
	db.Order("id DESC").First(&snap)
	if snap.Signature == "" {
		t.Fatal("snapshot unsigned")
	}
	priv, err := keys.LoadOrCreate(db, "status-publisher")
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	sig, _ := base64.StdEncoding.DecodeString(snap.Signature)
	if !ed25519.Verify(pub, []byte(snap.PayloadJSON), sig) {
		t.Fatal("snapshot signature does not verify")
	}
}

// Rollup correction: rebuilding after new data appends a new version row
// instead of silently rewriting published history.
func TestPSRollupCorrectionAppendsVersion(t *testing.T) {
	srv, db := psTestServer(t)
	psAdminJSON(t, srv, "POST", "/api/publicstatus/observations",
		`[{"component_id":"patty_code","success":true,"impact":"none","window_seconds":86400}]`)
	psAdminJSON(t, srv, "POST", "/api/publicstatus/components/patty_code/rollups/rebuild", `{}`)
	var v1 []models.PublicStatusDailyRollup
	db.Where("component_id = ?", "patty_code").Find(&v1)
	if len(v1) != 1 || v1[0].Version != 1 || v1[0].CorrectedBy != 0 {
		t.Fatalf("initial rollup: %+v", v1)
	}
	// New late-arriving observation changes the day.
	psAdminJSON(t, srv, "POST", "/api/publicstatus/observations",
		`[{"component_id":"patty_code","success":false,"impact":"widespread","window_seconds":3600}]`)
	psAdminJSON(t, srv, "POST", "/api/publicstatus/components/patty_code/rollups/rebuild", `{}`)
	var rows []models.PublicStatusDailyRollup
	db.Where("component_id = ?", "patty_code").Order("version ASC").Find(&rows)
	if len(rows) != 2 {
		t.Fatalf("correction rows = %d, want 2", len(rows))
	}
	if rows[0].CorrectedBy == 0 || rows[1].Version != 2 {
		t.Fatalf("correction trail wrong: %+v", rows)
	}
}
