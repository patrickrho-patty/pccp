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

func csTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/cs.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.CloudSchedule{}, &models.ScheduleOccurrence{},
		&models.AccountCapability{}, &models.CapabilityConnection{}, &models.ScheduleDelegation{},
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

func csJSON(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-cs", Email: "user@pub.dev", Role: "viewer"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func csCreateSchedule(t *testing.T, srv *Server, objective string) uint {
	t.Helper()
	spec := fmt.Sprintf(`{"objective":%q,"success_criteria":"요약 완료","delivery":"account"}`, objective)
	body := fmt.Sprintf(`{"task_spec":%s,"trigger":{"kind":"once","at":"%s","timezone":"Asia/Seoul"},"context_snapshot":{}}`,
		spec, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339))
	w := csJSON(t, srv, "POST", "/api/schedules/", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return uint(resp["id"].(float64))
}

// Malicious-use classes are denied at registration with policy evidence.
func TestCSDenyClasses(t *testing.T) {
	srv, db := csTestServer(t)
	spec := `{"objective":"경쟁사 계정 비밀번호 탈취 스크립트 작성"}`
	w := csJSON(t, srv, "POST", "/api/schedules/",
		fmt.Sprintf(`{"task_spec":%s,"trigger":{"kind":"once","at":"%s"}}`, spec, time.Now().Add(time.Hour).Format(time.RFC3339)))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("malicious schedule accepted: %d", w.Code)
	}
	var denied int64
	db.Model(&models.AuditEvent{}).Where("event_type = ?", "cp.schedule.denied").Count(&denied)
	if denied != 1 {
		t.Fatalf("policy evidence missing: %d", denied)
	}
	// Benign objective passes.
	if id := csCreateSchedule(t, srv, "매일 아침 뉴스 요약"); id == 0 {
		t.Fatal("benign schedule rejected")
	}
}

// Frozen snapshot semantics: an edit bumps the revision; the occurrence
// idempotency key includes the revision so old snapshots can't repeat.
func TestCSFrozenSnapshotRevision(t *testing.T) {
	srv, db := csTestServer(t)
	id := csCreateSchedule(t, srv, "금요일마다 이슈 리뷰")
	w := csJSON(t, srv, "POST", fmt.Sprintf("/api/schedules/%d/mutate", id),
		`{"action":"edit","task_spec":{"objective":"수정된 목표"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("edit: %d", w.Code)
	}
	var sched models.CloudSchedule
	db.First(&sched, "id = ?", id)
	if sched.Revision != 2 {
		t.Fatalf("revision after edit: %d", sched.Revision)
	}
	// Malicious edit is screened too.
	if w := csJSON(t, srv, "POST", fmt.Sprintf("/api/schedules/%d/mutate", id),
		`{"action":"edit","task_spec":{"objective":"스팸 대량 발송"}}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("malicious edit accepted: %d", w.Code)
	}
}

// Idempotent dispatch: sweeping twice for the same due occurrence
// admits exactly one; overlapping pending coalesces; the 24h catch-up
// window expires older misses.
func TestCSDispatchIdempotencyCoalescingCatchup(t *testing.T) {
	srv, db := csTestServer(t)
	id := csCreateSchedule(t, srv, "배포 상태 점검")
	// Sweep twice — only one admission.
	for i := 0; i < 2; i++ {
		if w := csJSON(t, srv, "POST", "/api/schedules/dispatch-sweep", `{}`); w.Code != http.StatusOK {
			t.Fatalf("sweep: %d", w.Code)
		}
	}
	var admitted []models.ScheduleOccurrence
	db.Where("schedule_id = ? AND state = ?", id, soAdmitted).Find(&admitted)
	if len(admitted) != 1 {
		t.Fatalf("admitted = %d, want exactly 1 (idempotent)", len(admitted))
	}
	// Manually add an overlapping pending occurrence, then a report of
	// failure on the active one → pending coalesces on next sweep.
	pending := models.ScheduleOccurrence{
		ScheduleID: id, Revision: 1, IntendedAt: time.Now().UTC().Format(time.RFC3339),
		IdempotencyKey: "sch-manual", State: soPending,
	}
	db.Create(&pending)
	// Backdate a missed pending occurrence beyond 24h → expires.
	old := models.ScheduleOccurrence{
		ScheduleID: id, Revision: 1, IntendedAt: time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339),
		IdempotencyKey: "sch-old", State: soPending,
	}
	db.Create(&old)
	csJSON(t, srv, "POST", "/api/schedules/dispatch-sweep", `{}`)
	var state string
	db.Model(&models.ScheduleOccurrence{}).Where("id = ?", pending.ID).Pluck("state", &state)
	if state != soCoalesced {
		t.Fatalf("overlap not coalesced: %s", state)
	}
	db.Model(&models.ScheduleOccurrence{}).Where("id = ?", old.ID).Pluck("state", &state)
	if state != soExpired {
		t.Fatalf("pre-24h miss not expired: %s", state)
	}
}

// Retry policy: transient failures retry at most three times; policy
// denials never retry and finish immediately.
func TestCSRetryPolicy(t *testing.T) {
	srv, db := csTestServer(t)
	id := csCreateSchedule(t, srv, "리포트 생성")
	csJSON(t, srv, "POST", "/api/schedules/dispatch-sweep", `{}`)
	var occ models.ScheduleOccurrence
	db.Where("schedule_id = ? AND state = ?", id, soAdmitted).First(&occ)
	// Three transient failures schedule retries.
	for i := 1; i <= 3; i++ {
		w := csJSON(t, srv, "POST", "/api/schedules/report",
			fmt.Sprintf(`{"occurrence_id":%d,"state":"failed","result_summary_ko":"일시 오류"}`, occ.ID))
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "retry_scheduled" {
			t.Fatalf("transient failure %d did not retry: %v", i, resp)
		}
	}
	db.First(&occ, "id = ?", occ.ID)
	if occ.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", occ.Attempts)
	}
	// Fourth failure is terminal.
	w := csJSON(t, srv, "POST", "/api/schedules/report",
		fmt.Sprintf(`{"occurrence_id":%d,"state":"failed"}`, occ.ID))
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != soFailed {
		t.Fatalf("fourth failure should be terminal: %v", resp)
	}
	// Policy denial on a fresh occurrence is terminal immediately.
	occ2 := models.ScheduleOccurrence{ScheduleID: id, Revision: 1, IdempotencyKey: "k2", State: soAdmitted}
	db.Create(&occ2)
	w = csJSON(t, srv, "POST", "/api/schedules/report",
		fmt.Sprintf(`{"occurrence_id":%d,"state":"denied","deny_reason":"금지된 용도"}`, occ2.ID))
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != soDenied {
		t.Fatalf("denial not terminal: %v", resp)
	}
	var fin string
	db.Model(&models.ScheduleOccurrence{}).Where("id = ?", occ2.ID).Pluck("state", &fin)
	if fin != soDenied || occ2.Attempts != 0 {
		t.Fatalf("denial retried: state=%s attempts=%d", fin, occ2.Attempts)
	}
}

// Cron + timezone: next occurrence honors Asia/Seoul and DST-safe math.
func TestCSCronTimezone(t *testing.T) {
	// 08:00 KST weekdays from 2026-08-19 15:00 UTC (= 2026-08-20 00:00 KST).
	next, err := csNextOccurrence(map[string]interface{}{
		"kind": "cron", "expr": "0 8 * * 1-5", "timezone": "Asia/Seoul",
	}, time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// Next weekday 08:00 KST = 2026-08-20 08:00 KST = 2026-08-19 23:00 UTC.
	want := time.Date(2026, 8, 19, 23, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("cron next = %s, want %s", next.UTC(), want.UTC())
	}
	if _, err := csNextCron("bad expr", time.Now()); err == nil {
		t.Fatal("invalid cron accepted")
	}
	if _, err := csNextCron("61 * * * *", time.Now()); err == nil {
		t.Fatal("out-of-range cron accepted")
	}
}

// Capabilities expose metadata only (no credential material exists in
// the response shape); delegation is narrow-only and consequential
// grants require the disclosure.
func TestCSCapabilityMetadataAndDelegation(t *testing.T) {
	srv, db := csTestServer(t)
	schedID := csCreateSchedule(t, srv, "이메일 요약 전달")
	// Connected capability with granted scopes.
	cap1 := models.AccountCapability{
		OwnerUserID: "user@pub.dev", CapabilityID: "communication.email.read",
		Kind: "mcp", DisplayKo: "이메일 읽기", State: "available", CloudExecutable: true, Version: "1.2",
	}
	db.Create(&cap1)
	conn := models.CapabilityConnection{
		OwnerUserID: "user@pub.dev", CapabilityID: "communication.email.read",
		State: "connected", ScopesJSON: `["read:mail","read:labels"]`,
		CredentialEnvelopeRef: "kms:envelope-7", Version: "1.2",
	}
	db.Create(&conn)
	w := csJSON(t, srv, "GET", "/api/schedules/capabilities", "")
	body := w.Body.String()
	for _, banned := range []string{"kms:envelope-7", "token", "refresh"} {
		if bytes.Contains([]byte(body), []byte(banned)) {
			t.Fatalf("capability list leaks %s: %s", banned, body)
		}
	}
	// Delegation: subset OK, expansion rejected.
	delegURL := "/api/schedules/delegations"
	if w := csJSON(t, srv, "POST", delegURL,
		fmt.Sprintf(`{"schedule_id":%d,"capability_id":"communication.email.read","scopes":["read:mail"]}`, schedID)); w.Code != http.StatusCreated {
		t.Fatalf("subset delegation rejected: %d %s", w.Code, w.Body.String())
	}
	if w := csJSON(t, srv, "POST", delegURL,
		fmt.Sprintf(`{"schedule_id":%d,"capability_id":"communication.email.read","scopes":["read:mail","write:mail"]}`, schedID)); w.Code != http.StatusForbidden {
		t.Fatalf("scope expansion accepted: %d", w.Code)
	}
	// Cross-service consequential without disclosure → 400.
	if w := csJSON(t, srv, "POST", delegURL,
		fmt.Sprintf(`{"schedule_id":%d,"capability_id":"communication.email.read","scopes":["read:mail"],"consequential":true}`, schedID)); w.Code != http.StatusBadRequest {
		t.Fatalf("undisclosed consequential delegation accepted: %d", w.Code)
	}
	// With full disclosure → granted.
	if w := csJSON(t, srv, "POST", delegURL,
		fmt.Sprintf(`{"schedule_id":%d,"capability_id":"communication.email.read","scopes":["read:mail"],"consequential":true,"disclosure":{"source":"Gmail","destination":"Telegram","data_class":"요약 텍스트","operation":"전송"}}`, schedID)); w.Code != http.StatusCreated {
		t.Fatalf("disclosed consequential delegation rejected: %d", w.Code)
	}
	// Connect flow reflects authorization_required for an unconnected one.
	cap2 := models.AccountCapability{OwnerUserID: "user@pub.dev", CapabilityID: "communication.telegram.send", State: "unavailable"}
	db.Create(&cap2)
	if w := csJSON(t, srv, "POST", "/api/schedules/capabilities/connect",
		`{"capability_id":"communication.telegram.send","initiated_from":"harness"}`); w.Code != http.StatusAccepted {
		t.Fatalf("connect flow: %d", w.Code)
	}
	db.First(&cap2)
	if cap2.State != "authorization_required" {
		t.Fatalf("connect flow state: %s", cap2.State)
	}
}
