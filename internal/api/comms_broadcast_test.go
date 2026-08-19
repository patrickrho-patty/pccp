package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// commsBroadcastFixture builds an org with one active, one suspended and
// one offboarded user plus a project roster (active + suspended members).
func commsBroadcastFixture(t *testing.T, srv *Server) (org models.Organization, active, suspended, offboarded models.User, proj models.Project) {
	t.Helper()
	srvDB := srv.db
	if err := srvDB.AutoMigrate(&models.ProjectMember{}); err != nil {
		t.Fatal(err)
	}
	org = models.Organization{Name: "o", Slug: "obc", Status: "active"}
	srvDB.Create(&org)
	active = models.User{Email: "a@corp.kr", Name: "active", NameKo: "활성", Status: "active"}
	active.OrganizationID = org.ID
	srvDB.Create(&active)
	suspended = models.User{Email: "s@corp.kr", Name: "susp", Status: "suspended"}
	suspended.OrganizationID = org.ID
	srvDB.Create(&suspended)
	offboarded = models.User{Email: "o@corp.kr", Name: "off", Status: "offboarded"}
	offboarded.OrganizationID = org.ID
	srvDB.Create(&offboarded)
	proj = models.Project{Name: "p1", Slug: "p1"}
	proj.OrganizationID = org.ID
	srvDB.Create(&proj)
	srvDB.Create(&models.ProjectMember{OrganizationID: org.ID, ProjectID: proj.ID, UserID: active.ID})
	srvDB.Create(&models.ProjectMember{OrganizationID: org.ID, ProjectID: proj.ID, UserID: suspended.ID})
	return org, active, suspended, offboarded, proj
}

func TestBroadcastGovernedRequiresExplicitAudience(t *testing.T) {
	srv, _ := commsTestServer(t)
	org, _, _, _, _ := commsBroadcastFixture(t, srv)

	// No target_type → rejected (no silent whole-org default).
	rec := doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t","severity":"info"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing target_type accepted: %d %s", rec.Code, rec.Body.String())
	}
	// Scoped target without target_id → rejected.
	rec = doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t","target_type":"user"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing target_id accepted: %d", rec.Code)
	}
	// Unsupported scope → rejected.
	rec = doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t","target_type":"group","target_id":"g1"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported target_type accepted: %d", rec.Code)
	}
}

func TestBroadcastGovernedEmptyAndCriticalGuards(t *testing.T) {
	srv, _ := commsTestServer(t)
	org, _, _, _, _ := commsBroadcastFixture(t, srv)

	// Empty audience (project with no eligible members) → rejected unless confirmed.
	emptyProj := models.Project{Name: "empty", Slug: "empty"}
	emptyProj.OrganizationID = org.ID
	srv.db.Create(&emptyProj)
	rec := doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t","target_type":"project","target_id":"`+emptyProj.ID+`"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero-audience send accepted: %d", rec.Code)
	}
	rec = doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t","target_type":"project","target_id":"`+emptyProj.ID+`","allow_empty":true}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("allow_empty send failed: %d %s", rec.Code, rec.Body.String())
	}

	// Critical without a confirmation reason → rejected; with reason → sent and audited.
	rec = doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t","severity":"critical","target_type":"org"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("critical without reason accepted: %d", rec.Code)
	}
	rec = doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t","severity":"emergency","target_type":"org","confirm_reason":"보안 패치 긴급 적용"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("emergency with reason failed: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Broadcast models.Broadcast `json:"broadcast"`
		Audience  struct {
			Eligible int `json:"eligible"`
			Excluded int `json:"excluded"`
		} `json:"audience"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Audience.Eligible != 1 || out.Audience.Excluded != 2 {
		t.Fatalf("audience split wrong: %+v", out.Audience)
	}
	if out.Broadcast.AudienceJSON == "" {
		t.Fatalf("audience snapshot missing: %+v", out.Broadcast)
	}
	var ev models.AuditEvent
	if err := srv.db.Where("resource_id = ? AND action = 'send_broadcast'", out.Broadcast.ID).First(&ev).Error; err != nil {
		t.Fatalf("audit event missing: %v", err)
	}
}

func TestBroadcastGovernedIdempotencyAndSnapshot(t *testing.T) {
	srv, _ := commsTestServer(t)
	org, active, _, _, proj := commsBroadcastFixture(t, srv)

	body := `{"title":"t","target_type":"project","target_id":"` + proj.ID + `","client_token":"tok-1"}`
	rec := doJSON(t, srv, "POST", "/api/communications/broadcasts/send", body, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("send failed: %d %s", rec.Code, rec.Body.String())
	}
	var first struct {
		Broadcast models.Broadcast `json:"broadcast"`
	}
	json.Unmarshal(rec.Body.Bytes(), &first)
	// Snapshot froze exactly the active member; the suspended member is excluded.
	var snap broadcastAudienceSnapshot
	if err := json.Unmarshal([]byte(first.Broadcast.AudienceJSON), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.EligibleIDs) != 1 || snap.EligibleIDs[0] != active.ID {
		t.Fatalf("snapshot eligible wrong: %+v", snap)
	}
	if len(snap.Excluded) != 1 || snap.Excluded[0].Reason != "suspended" {
		t.Fatalf("snapshot excluded wrong: %+v", snap.Excluded)
	}
	// Retry with the same token returns the same broadcast, no duplicate row.
	rec = doJSON(t, srv, "POST", "/api/communications/broadcasts/send", body, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent retry failed: %d", rec.Code)
	}
	var retry struct {
		Broadcast models.Broadcast `json:"broadcast"`
		Duplicate bool             `json:"duplicate"`
	}
	json.Unmarshal(rec.Body.Bytes(), &retry)
	if !retry.Duplicate || retry.Broadcast.ID != first.Broadcast.ID {
		t.Fatalf("retry did not return original: %+v", retry)
	}
	var count int64
	srv.db.Model(&models.Broadcast{}).Where("organization_id = ? AND client_token = 'tok-1'", org.ID).Count(&count)
	if count != 1 {
		t.Fatalf("duplicate broadcast rows: %d", count)
	}
	// The partial unique index enforces idempotency at the DB level: a
	// second insert with the same non-empty token fails outright, while
	// legacy rows with an empty token are unaffected.
	dup := models.Broadcast{Severity: "info", Title: "dup", ClientToken: "tok-1"}
	dup.OrganizationID = org.ID
	if err := srv.db.Create(&dup).Error; err == nil {
		t.Fatal("duplicate client_token insert bypassed the unique index")
	}
	legacy := models.Broadcast{Severity: "info", Title: "legacy"}
	legacy.OrganizationID = org.ID
	if err := srv.db.Create(&legacy).Error; err != nil {
		t.Fatalf("empty client_token insert blocked: %v", err)
	}
}

func TestBroadcastAcksTrackFrozenAudience(t *testing.T) {
	srv, _ := commsTestServer(t)
	org, active, suspended, offboarded, proj := commsBroadcastFixture(t, srv)

	// Presence: active user online.
	srv.db.Create(&models.Presence{OrganizationID: org.ID, UserID: active.ID, Status: "online"})

	rec := doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t","target_type":"project","target_id":"`+proj.ID+`","requires_ack":true}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("send failed: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Broadcast models.Broadcast `json:"broadcast"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	bcID := out.Broadcast.ID

	// Ack by the active member.
	rec = doJSON(t, srv, "POST", "/api/communications/broadcasts/"+bcID+"/ack",
		`{"user_id":"`+active.ID+`"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("ack failed: %d", rec.Code)
	}

	rec = doJSON(t, srv, "GET", "/api/communications/broadcasts/"+bcID+"/acks", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("acks failed: %d", rec.Code)
	}
	var dash struct {
		TotalUsers int  `json:"total_users"`
		Acked      int  `json:"acked"`
		Expired    bool `json:"expired"`
		Recipients []struct {
			UserID   string `json:"user_id"`
			Acked    bool   `json:"acked"`
			Presence string `json:"presence"`
		} `json:"recipients"`
		Excluded []audienceExclusion `json:"excluded"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dash); err != nil {
		t.Fatal(err)
	}
	// Snapshot had only the active member: total must not count the whole org.
	if dash.TotalUsers != 1 || dash.Acked != 1 {
		t.Fatalf("ack counts wrong: %+v", dash)
	}
	if len(dash.Recipients) != 1 || !dash.Recipients[0].Acked || dash.Recipients[0].Presence != "online" {
		t.Fatalf("recipient drill-down wrong: %+v", dash.Recipients)
	}
	if len(dash.Excluded) != 1 || dash.Excluded[0].UserID != suspended.ID {
		t.Fatalf("excluded wrong: %+v", dash.Excluded)
	}
	if dash.Expired {
		t.Fatalf("unexpected expired flag")
	}
	_ = offboarded

	// Expired broadcast reports expired=true.
	srv.db.Model(&models.Broadcast{}).Where("id = ?", bcID).
		Update("expires_at", time.Now().Add(-time.Hour).Format(time.RFC3339))
	rec = doJSON(t, srv, "GET", "/api/communications/broadcasts/"+bcID+"/acks", "", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &dash)
	if !dash.Expired {
		t.Fatalf("expired broadcast not flagged")
	}
}

func TestBroadcastAcksLegacyFallback(t *testing.T) {
	srv, _ := commsTestServer(t)
	org, active, _, _, _ := commsBroadcastFixture(t, srv)

	// Legacy path (no snapshot, target all) still works via scope resolution.
	rec := doJSON(t, srv, "POST", "/api/communications/broadcasts",
		`{"title":"legacy","severity":"info"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("legacy send failed: %d %s", rec.Code, rec.Body.String())
	}
	var bc models.Broadcast
	json.Unmarshal(rec.Body.Bytes(), &bc)
	rec = doJSON(t, srv, "GET", "/api/communications/broadcasts/"+bc.ID+"/acks", "", org.ID)
	var dash struct {
		TotalUsers int `json:"total_users"`
	}
	json.Unmarshal(rec.Body.Bytes(), &dash)
	if dash.TotalUsers != 1 { // only the active user is eligible
		t.Fatalf("legacy fallback wrong: %+v (active=%s)", dash, active.ID)
	}
}

func TestBroadcastAcksSnapshotAuthoritativeWhenEmpty(t *testing.T) {
	srv, _ := commsTestServer(t)
	org, _, _, _, _ := commsBroadcastFixture(t, srv)

	// A snapshot frozen with zero eligible recipients stays zero — the ack
	// dashboard must not fall through to live scope or all org users.
	emptyProj := models.Project{Name: "empty", Slug: "empty"}
	emptyProj.OrganizationID = org.ID
	srv.db.Create(&emptyProj)
	rec := doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t","target_type":"project","target_id":"`+emptyProj.ID+`","allow_empty":true}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("allow_empty send failed: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Broadcast models.Broadcast `json:"broadcast"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	rec = doJSON(t, srv, "GET", "/api/communications/broadcasts/"+out.Broadcast.ID+"/acks", "", org.ID)
	var dash struct {
		TotalUsers int `json:"total_users"`
		Acked      int `json:"acked"`
	}
	json.Unmarshal(rec.Body.Bytes(), &dash)
	if dash.TotalUsers != 0 || dash.Acked != 0 {
		t.Fatalf("empty frozen snapshot not authoritative: %+v", dash)
	}

	// Recipients deleted after send: counts still derive from the frozen
	// snapshot (now resolving to zero rows), not from live re-resolution.
	rec = doJSON(t, srv, "POST", "/api/communications/broadcasts/send",
		`{"title":"t2","target_type":"org"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("org send failed: %d %s", rec.Code, rec.Body.String())
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	srv.db.Exec("DELETE FROM users")
	rec = doJSON(t, srv, "GET", "/api/communications/broadcasts/"+out.Broadcast.ID+"/acks", "", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &dash)
	if dash.TotalUsers != 0 || dash.Acked != 0 {
		t.Fatalf("snapshot with deleted recipients not authoritative: %+v", dash)
	}
}
