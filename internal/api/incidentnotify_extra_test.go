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

func inTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/in.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.IncidentNotifyPolicy{}, &models.IncidentNotifyRecipientGroup{}, &models.IncidentNotifyChannel{},
		&models.IncidentNotifyIncident{}, &models.IncidentNotifyJob{}, &models.IncidentNotifyReceipt{},
		&models.IncidentNotifyAcknowledgement{}, &models.IncidentNotifyAudit{}, &models.IncidentNotifyHealthSum{},
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

func inJSON(t *testing.T, srv *Server, method, path, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-in", Email: "sec@patty.dev", Role: role}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func inSetupTenant(t *testing.T, srv *Server, db *gorm.DB) {
	t.Helper()
	org := models.Organization{Name: "테스트 테넌트", Slug: "in-test", Status: "active"}
	db.Create(&org)
	// db rows default org id "" — patch org id on created rows via claims
	// org-in. Create org with fixed id.
	db.Model(&org).Update("id", "org-in")
	// Verified recipient group with all three channels.
	db.Create(&models.IncidentNotifyRecipientGroup{
		OrganizationID: "org-in", Name: "1차 온콜", EscalationOrder: 1, Timezone: "Asia/Seoul",
		MembersJSON: `[{"kind":"email","target":"oncall@a.io","verified":true},{"kind":"sms","target":"+821012345678","verified":true},{"kind":"slack","target":"T000/C000-alerts","verified":true}]`,
	})
}

// Locked routing: critical must reach SMS+email+Slack; weakening the
// critical floor is rejected.
func TestINPolicyCriticalFloorNotWeakenable(t *testing.T) {
	srv, db := inTestServer(t)
	inSetupTenant(t, srv, db)
	weakenedRouting := `{"critical":{"channels":["email"]},"high":{"channels":["email"]},"medium":{"channels":["email"]},"low":{"channels":[]}}`
	weakened := fmt.Sprintf(`{"routing_json":%q,"managed_by_json":"{\"email\":\"customer\",\"sms\":\"customer\",\"slack\":\"customer\"}"}`, weakenedRouting)
	if w := inJSON(t, srv, "PUT", "/api/incidentnotify/policy", weakened, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("weakened critical accepted: %d %s", w.Code, w.Body.String())
	}
	strengthenedRouting := `{"critical":{"channels":["sms","email","slack"],"ack_required":true},"high":{"channels":["email","slack"]},"medium":{"channels":["email"]},"low":{"channels":[]}}`
	strengthened := fmt.Sprintf(`{"routing_json":%q,"managed_by_json":"{\"email\":\"customer\",\"sms\":\"customer\",\"slack\":\"customer\"}"}`, strengthenedRouting)
	if w := inJSON(t, srv, "PUT", "/api/incidentnotify/policy", strengthened, "admin"); w.Code != http.StatusOK {
		t.Fatalf("strengthened policy rejected: %d %s", w.Code, w.Body.String())
	}
	// Non-admin cannot mutate policy.
	if w := inJSON(t, srv, "PUT", "/api/incidentnotify/policy", strengthened, "viewer"); w.Code != http.StatusForbidden {
		t.Fatalf("viewer policy mutation allowed: %d", w.Code)
	}
}

// Air-gapped tenants cannot opt into Patty-managed delivery.
func TestINAirGapBlocksPattyManaged(t *testing.T) {
	srv, db := inTestServer(t)
	inSetupTenant(t, srv, db)
	// Set air-gapped policy.
	routing := `{"critical":{"channels":["sms","email","slack"],"ack_required":true},"high":{"channels":["email"]},"medium":{"channels":["email"]},"low":{"channels":[]}}`
	pol := fmt.Sprintf(`{"routing_json":%q,"managed_by_json":"{\"email\":\"customer\",\"sms\":\"customer\",\"slack\":\"customer\"}"}`, routing)
	inJSON(t, srv, "PUT", "/api/incidentnotify/policy", pol, "admin")
	var p models.IncidentNotifyPolicy
	db.First(&p)
	db.Model(&p).Update("air_gapped", true)
	if w := inJSON(t, srv, "POST", "/api/incidentnotify/channels",
		`{"channel":"email","managed_by":"patty","endpoint":"ops@a.io"}`, "admin"); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("air-gapped patty-managed accepted: %d %s", w.Code, w.Body.String())
	}
}

// Correlation: repeated events reuse one incident identity; unchanged
// repeats never notify; severity growth is a material update.
func TestINCorrelationDedup(t *testing.T) {
	srv, db := inTestServer(t)
	inSetupTenant(t, srv, db)
	body := `{"source_type":"security_finding","service":"gateway","rule":"SOL-001","severity":"high","title_ko":"인증 우회 시도 탐지","safe_summary_ko":"게이트웨이에서 비정상 토큰 사용"}`
	if w := inJSON(t, srv, "POST", "/api/incidentnotify/sources", body, "admin"); w.Code != http.StatusCreated {
		t.Fatalf("first ingest: %d %s", w.Code, w.Body.String())
	}
	// Identical repeat: correlated, no new jobs.
	w := inJSON(t, srv, "POST", "/api/incidentnotify/sources", body, "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("repeat ingest: %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["correlated"] != true || resp["notified"] != false {
		t.Fatalf("repeat not deduped: %v", resp)
	}
	// Severity growth: material update → escalation of severity.
	esc := `{"source_type":"security_finding","service":"gateway","rule":"SOL-001","severity":"critical","title_ko":"인증 우회 시도 탐지","safe_summary_kro":"","safe_summary_ko":"악성 확산 징후"}`
	inJSON(t, srv, "POST", "/api/incidentnotify/sources", esc, "admin")
	var incidents []models.IncidentNotifyIncident
	db.Find(&incidents)
	if len(incidents) != 1 {
		t.Fatalf("incidents = %d, want 1 correlated identity", len(incidents))
	}
	if incidents[0].Severity != "critical" {
		t.Fatalf("severity not escalated: %s", incidents[0].Severity)
	}
}

// Content boundary: outbound envelopes contain only safe fields — no
// scope refs, evidence, or raw detector data ever appear.
func TestINEnvelopeContentBoundary(t *testing.T) {
	srv, db := inTestServer(t)
	inSetupTenant(t, srv, db)
	inJSON(t, srv, "POST", "/api/incidentnotify/sources",
		`{"source_type":"security_finding","service":"gateway","rule":"SOL-002","severity":"critical","title_ko":"자격증명 유출 의심","safe_summary_ko":"토큰 재사용 패턴","scope_ref":"evidence://finding/42?raw=BEGIN%20SECRET"}`, "admin")
	var jobs []models.IncidentNotifyJob
	db.Find(&jobs)
	if len(jobs) == 0 {
		t.Fatal("no jobs enqueued for critical")
	}
	channels := map[string]bool{}
	for _, j := range jobs {
		var env map[string]interface{}
		if err := json.Unmarshal([]byte(j.EnvelopeJSON), &env); err != nil {
			t.Fatalf("bad envelope: %v", err)
		}
		raw := j.EnvelopeJSON
		for _, banned := range []string{"evidence://", "SECRET", "scope_ref", "password", "token="} {
			if bytes.Contains([]byte(raw), []byte(banned)) {
				t.Fatalf("envelope leaks %q: %s", banned, raw)
			}
		}
		if env["schema"] != "patty.incident.v1" {
			t.Fatalf("envelope schema: %v", env["schema"])
		}
		// SMS jobs carry only the minimized field set.
		if j.Channel == "sms" {
			for _, k := range []string{"impact_summary_ko", "affected_scope", "next_action_ko"} {
				if _, present := env[k]; present {
					t.Fatalf("SMS envelope carries non-SMS field %s", k)
				}
			}
		}
		channels[j.Channel] = true
	}
	for _, need := range []string{"sms", "email", "slack"} {
		if !channels[need] {
			t.Fatalf("critical did not route to %s: %v", need, channels)
		}
	}
}

// Ack via single-use token; second use is rejected; escalation advances
// when the deadline passes unacked.
func TestINAckTokenAndEscalation(t *testing.T) {
	srv, db := inTestServer(t)
	inSetupTenant(t, srv, db)
	inJSON(t, srv, "POST", "/api/incidentnotify/sources",
		`{"source_type":"outage","service":"inference","rule":"SLO-5xx","severity":"critical","title_ko":"추론 5xx 비율 급증","safe_summary_ko":"가용성 영향"}`, "admin")
	var inc models.IncidentNotifyIncident
	db.First(&inc)
	var ack models.IncidentNotifyAcknowledgement
	db.Where("incident_id = ?", inc.ID).First(&ack)
	if ack.ActionToken == "" {
		t.Fatal("ack token not minted for critical")
	}
	// Wrong token rejected.
	if w := inJSON(t, srv, "POST", "/api/incidentnotify/ack",
		fmt.Sprintf(`{"incident_id":%d,"token":"wrong","via":"email"}`, inc.ID), "admin"); w.Code != http.StatusForbidden {
		t.Fatalf("wrong token accepted: %d", w.Code)
	}
	// Correct token acks.
	if w := inJSON(t, srv, "POST", "/api/incidentnotify/ack",
		fmt.Sprintf(`{"incident_id":%d,"token":"%s","via":"email"}`, inc.ID, ack.ActionToken), "admin"); w.Code != http.StatusOK {
		t.Fatalf("ack rejected: %d %s", w.Code, w.Body.String())
	}
	// Token is single-use.
	if w := inJSON(t, srv, "POST", "/api/incidentnotify/ack",
		fmt.Sprintf(`{"incident_id":%d,"token":"%s","via":"email"}`, inc.ID, ack.ActionToken), "admin"); w.Code != http.StatusConflict {
		t.Fatalf("token replay accepted: %d", w.Code)
	}

	// New unacked critical for escalation sweep.
	inJSON(t, srv, "POST", "/api/incidentnotify/sources",
		`{"source_type":"outage","service":"relay","rule":"SLO-latency","severity":"critical","title_ko":"릴레이 지연 임계 초과","safe_summary_ko":"영향 확대"}`, "admin")
	var inc2 models.IncidentNotifyIncident
	db.Where("severity = ? AND state = ?", "critical", "open").First(&inc2)
	// Backdate first-seen beyond the ack deadline.
	db.Model(&inc2).Update("first_seen_at", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	if w := inJSON(t, srv, "POST", "/api/incidentnotify/escalation-sweep", `{}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("sweep: %d", w.Code)
	}
	db.First(&inc2)
	if inc2.State != "escalated" || inc2.EscalationStep != 1 {
		t.Fatalf("escalation did not advance: %+v", inc2)
	}
	// Escalation enqueued an escalation-kind job.
	var escJobs int64
	db.Model(&models.IncidentNotifyJob{}).Where("incident_id = ? AND kind = ?", inc2.ID, "escalation").Count(&escJobs)
	if escJobs == 0 {
		t.Fatal("escalation job missing")
	}
}

// Dispatch: healthy channel delivers with receipt (accepted ≠ ack);
// invalid destination retries with backoff then dead-letters.
func TestINDispatchRetryAndDeadLetter(t *testing.T) {
	srv, db := inTestServer(t)
	inSetupTenant(t, srv, db)
	inJSON(t, srv, "POST", "/api/incidentnotify/sources",
		`{"source_type":"system_failure","service":"queue","rule":"F-1","severity":"high","title_ko":"큐 적체","safe_summary_ko":"처리 지연"}`, "admin")
	// Make every target invalid by rewriting to a bad sms destination.
	db.Model(&models.IncidentNotifyJob{}).Where("state = ?", "queued").Update("target", "x")
	for i := 0; i < inMaxAttempts+2; i++ {
		// Simulate time passing: backoff-scheduled retries become due.
		db.Model(&models.IncidentNotifyJob{}).Where("state = ?", "queued").Update("next_retry_at", "")
		inJSON(t, srv, "POST", "/api/incidentnotify/dispatch", `{}`, "admin")
	}
	var dead []models.IncidentNotifyJob
	db.Where("state = ?", "dead_letter").Find(&dead)
	if len(dead) == 0 {
		t.Fatal("no dead letters after max attempts")
	}
	for _, j := range dead {
		if j.Attempts > inMaxAttempts {
			t.Fatalf("attempts exceeded bound: %d", j.Attempts)
		}
	}
	// Now a healthy email delivery path.
	inJSON(t, srv, "POST", "/api/incidentnotify/test", `{"channel":"email","target":"ops@a.io"}`, "admin")
	if w := inJSON(t, srv, "POST", "/api/incidentnotify/dispatch", `{}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("dispatch: %d", w.Code)
	}
	var sent models.IncidentNotifyJob
	db.Where("kind = ? AND state = ?", "test", "sent").First(&sent)
	if sent.ID == 0 {
		t.Fatal("test job not sent")
	}
	if !bytes.Contains([]byte(sent.EnvelopeJSON), []byte("[테스트]")) {
		t.Fatalf("test notification not labeled: %s", sent.EnvelopeJSON)
	}
	var receipts []models.IncidentNotifyReceipt
	db.Where("job_id = ?", sent.ID).Find(&receipts)
	if len(receipts) == 0 || receipts[0].State != "accepted" {
		t.Fatalf("receipt missing or not accepted: %+v", receipts)
	}
}

// Resolution cancels obsolete pending jobs and notifies the same
// audience with a resolution-kind job.
func TestINResolveCancelsPending(t *testing.T) {
	srv, db := inTestServer(t)
	inSetupTenant(t, srv, db)
	inJSON(t, srv, "POST", "/api/incidentnotify/sources",
		`{"source_type":"degradation","service":"api","rule":"D-9","severity":"high","title_ko":"API 지연","safe_summary_ko":"p99 상승"}`, "admin")
	var inc models.IncidentNotifyIncident
	db.First(&inc)
	if w := inJSON(t, srv, "POST", fmt.Sprintf("/api/incidentnotify/incidents/%d/resolve", inc.ID), `{}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("resolve: %d", w.Code)
	}
	var cancelled int64
	db.Model(&models.IncidentNotifyJob{}).Where("incident_id = ? AND state = ?", inc.ID, "cancelled").Count(&cancelled)
	if cancelled == 0 {
		t.Fatal("pending jobs not cancelled on resolve")
	}
	var resolution int64
	db.Model(&models.IncidentNotifyJob{}).Where("incident_id = ? AND kind = ?", inc.ID, "resolution").Count(&resolution)
	if resolution == 0 {
		t.Fatal("resolution notification missing")
	}
}

// Tenant isolation: another org's incidents/jobs are invisible.
func TestINTenantIsolation(t *testing.T) {
	srv, db := inTestServer(t)
	inSetupTenant(t, srv, db)
	// Foreign org incident.
	db.Create(&models.IncidentNotifyIncident{
		OrganizationID: "org-other", Fingerprint: inFingerprint("org-other", "x", "y", "z"),
		Severity: "critical", TitleKo: "외부 테넌트", State: "open",
	})
	w := inJSON(t, srv, "GET", "/api/incidentnotify/incidents", "", "admin")
	var arr []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &arr)
	for _, row := range arr {
		if row["organization_id"] == "org-other" {
			t.Fatal("cross-tenant incident leaked")
		}
	}
}

// The critical floor includes ack_required — dropping it is a weakening.
func TestINCriticalAckFloorNotWeakenable(t *testing.T) {
	srv, _ := inTestServer(t)
	routing := `{"critical":{"channels":["sms","email","slack"],"ack_required":false},"high":{"channels":["email"]},"medium":{"channels":["email"]},"low":{"channels":[]}}`
	body := fmt.Sprintf(`{"routing_json":%q,"managed_by_json":"{\"email\":\"customer\"}"}`, routing)
	if w := inJSON(t, srv, "PUT", "/api/incidentnotify/policy", body, "admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("ack-less critical accepted: %d %s", w.Code, w.Body.String())
	}
}

// Escalation advances past step 1 on later sweeps.
func TestINEscalationMultiStep(t *testing.T) {
	srv, db := inTestServer(t)
	inSetupTenant(t, srv, db)
	// Two groups for two escalation steps.
	db.Create(&models.IncidentNotifyRecipientGroup{
		OrganizationID: "org-in", Name: "2차 온콜", EscalationOrder: 2,
		MembersJSON: `[{"kind":"email","target":"esc2@a.io","verified":true}]`,
	})
	inJSON(t, srv, "POST", "/api/incidentnotify/sources",
		`{"source_type":"outage","service":"core","rule":"R","severity":"critical","title_ko":"장애","safe_summary_ko":"x"}`, "admin")
	var inc models.IncidentNotifyIncident
	db.First(&inc)
	// Backdate far beyond two deadlines.
	db.Model(&inc).Update("first_seen_at", time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339))
	inJSON(t, srv, "POST", "/api/incidentnotify/escalation-sweep", `{}`, "admin")
	db.First(&inc)
	if inc.EscalationStep != 1 {
		t.Fatalf("first sweep step = %d", inc.EscalationStep)
	}
	inJSON(t, srv, "POST", "/api/incidentnotify/escalation-sweep", `{}`, "admin")
	db.First(&inc)
	if inc.EscalationStep != 2 {
		t.Fatalf("second sweep step = %d, want 2 (multi-step escalation)", inc.EscalationStep)
	}
	// Material severity-rose repeats re-notify (severity in the key).
	inJSON(t, srv, "POST", "/api/incidentnotify/sources",
		`{"source_type":"outage","service":"core2","rule":"R2","severity":"high","title_ko":"x","safe_summary_ko":"x"}`, "admin")
	var inc2 models.IncidentNotifyIncident
	db.Where("fingerprint = ?", inFingerprint("org-in", "outage", "core2", "R2")).First(&inc2)
	var jobs1 int64
	db.Model(&models.IncidentNotifyJob{}).Where("incident_id = ?", inc2.ID).Count(&jobs1)
	inJSON(t, srv, "POST", "/api/incidentnotify/sources",
		`{"source_type":"outage","service":"core2","rule":"R2","severity":"critical","title_ko":"x","safe_summary_ko":"x"}`, "admin")
	var jobs2 int64
	db.Model(&models.IncidentNotifyJob{}).Where("incident_id = ?", inc2.ID).Count(&jobs2)
	if jobs2 <= jobs1 {
		t.Fatalf("severity-rose repeat did not re-notify: %d → %d", jobs1, jobs2)
	}
}
