package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func commsTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/cm.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Conversation{}, &models.Message{},
		&models.Presence{}, &models.FileTransfer{}, &models.Broadcast{}, &models.AuditEvent{},
		&models.ProjectMember{},
		&models.ServiceSigningKey{}, &models.SecurityRule{}, &models.PolicyEpoch{}, &models.CapabilityLease{},
		&models.Harness{}, &models.Session{}, &models.SandboxImage{}, &models.SandboxRecord{},
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

func TestCommsDMThreadReactionRead(t *testing.T) {
	srv, db := commsTestServer(t)
	org := models.Organization{Name: "o", Slug: "ocm", Status: "active"}
	db.Create(&org)
	dev := models.User{Email: "d@corp.kr", Name: "dev", Status: "active"}
	dev.OrganizationID = org.ID
	db.Create(&dev)

	// 1:1 DM find-or-create (C1).
	rec := doJSON(t, srv, "POST", "/api/communications/conversations/dm", `{"user_id":"`+dev.ID+`"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("dm failed: %d %s", rec.Code, rec.Body.String())
	}
	var conv models.Conversation
	json.Unmarshal(rec.Body.Bytes(), &conv)

	// Threaded message with mention (B1/B2).
	rec = doJSON(t, srv, "POST", "/api/communications/conversations/"+conv.ID+"/messages",
		`{"sender_id":"operator:x","content":"hello","mentions":["`+dev.ID+`"]}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("message failed: %d %s", rec.Code, rec.Body.String())
	}
	var msg models.Message
	json.Unmarshal(rec.Body.Bytes(), &msg)
	if !msg.Edited && msg.MentionsJSON == "" {
		t.Fatalf("mentions missing")
	}
	rec = doJSON(t, srv, "POST", "/api/communications/conversations/"+conv.ID+"/messages",
		`{"sender_id":"operator:x","content":"reply","parent_id":"`+msg.ID+`"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("thread reply failed: %d", rec.Code)
	}
	var reply models.Message
	json.Unmarshal(rec.Body.Bytes(), &reply)
	if reply.ParentMessageID != msg.ID {
		t.Fatalf("thread parent wrong: %s", reply.ParentMessageID)
	}

	// Reaction + read receipt (B2).
	rec = doJSON(t, srv, "POST", "/api/communications/messages/"+msg.ID+"/react",
		`{"emoji":"👍","user_id":"`+dev.ID+`"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("react failed: %d", rec.Code)
	}
	rec = doJSON(t, srv, "POST", "/api/communications/messages/"+msg.ID+"/read",
		`{"user_id":"`+dev.ID+`"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("read failed: %d", rec.Code)
	}

	// System command rejected for non-admin (C3) — the test harness
	// context has no role (empty), which is not admin/owner.
	rec = doJSON(t, srv, "POST", "/api/communications/conversations/"+conv.ID+"/messages",
		`{"sender_id":"operator:x","sender_type":"system","content_type":"command","content":"kill"}`, org.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("system command should be forbidden, got %d", rec.Code)
	}
}

func TestCommsFileTransferUploadScanDownload(t *testing.T) {
	srv, db := commsTestServer(t)
	org := models.Organization{Name: "o", Slug: "oft", Status: "active"}
	db.Create(&org)
	rec := doJSON(t, srv, "POST", "/api/communications/file-transfers",
		`{"sender_id":"u1","recipient_id":"u2","file_name":"notes.txt","file_size":10,"file_type":"text","classification":"internal","expires_at":"2027-01-01T00:00:00Z"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("transfer create failed: %d %s", rec.Code, rec.Body.String())
	}
	var tr models.FileTransfer
	json.Unmarshal(rec.Body.Bytes(), &tr)

	rec = doMultipart(t, srv, "POST", "/api/communications/file-transfers/"+tr.ID+"/content",
		"file", "notes.txt", "hello world", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}
	var up map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &up)
	if up["scan_status"] != "clean" {
		t.Fatalf("clean file should pass scan: %v", up)
	}
	rec = doJSON(t, srv, "GET", "/api/communications/file-transfers/"+tr.ID+"/download", "", org.ID)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello world" {
		t.Fatalf("download failed: %d %q", rec.Code, rec.Body.String())
	}

	// Blocked content: a secret-looking string must fail the scan.
	rec = doJSON(t, srv, "POST", "/api/communications/file-transfers",
		`{"sender_id":"u1","recipient_id":"u2","file_name":"s.txt","file_size":10,"file_type":"text","classification":"internal"}`, org.ID)
	json.Unmarshal(rec.Body.Bytes(), &tr)
	rec = doMultipart(t, srv, "POST", "/api/communications/file-transfers/"+tr.ID+"/content",
		"file", "s.txt", "AKIAIOSFODNN7EXAMPLE aws_secret=deadbeef", org.ID)
	json.Unmarshal(rec.Body.Bytes(), &up)
	if up["scan_status"] != "blocked" {
		t.Fatalf("secret content should be blocked: %v", up)
	}
	rec = doJSON(t, srv, "GET", "/api/communications/file-transfers/"+tr.ID+"/download", "", org.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("blocked file must not download: %d", rec.Code)
	}
}

func TestBroadcastAckDashboard(t *testing.T) {
	srv, db := commsTestServer(t)
	org := models.Organization{Name: "o", Slug: "obc", Status: "active"}
	db.Create(&org)
	u1 := models.User{Email: "a@corp.kr", Name: "a", Status: "active"}
	u1.OrganizationID = org.ID
	db.Create(&u1)
	u2 := models.User{Email: "b@corp.kr", Name: "b", Status: "active"}
	u2.OrganizationID = org.ID
	db.Create(&u2)

	rec := doJSON(t, srv, "POST", "/api/communications/broadcasts",
		`{"severity":"emergency","title":"t","body":"b","target_type":"all","requires_ack":true}`, org.ID)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("broadcast failed: %d", rec.Code)
	}
	var bc models.Broadcast
	json.Unmarshal(rec.Body.Bytes(), &bc)

	rec = doJSON(t, srv, "POST", "/api/communications/broadcasts/"+bc.ID+"/ack",
		`{"user_id":"`+u1.ID+`"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("ack failed: %d", rec.Code)
	}
	rec = doJSON(t, srv, "GET", "/api/communications/broadcasts/"+bc.ID+"/acks", "", org.ID)
	var dash map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &dash)
	// pending is the acked=false subset of recipients — derived, not shipped.
	recipients, _ := dash["recipients"].([]interface{})
	if dash["acked"].(float64) != 1 || dash["total_users"].(float64) != 2 || len(recipients) != 2 {
		t.Fatalf("ack dashboard wrong: %v", dash)
	}
	pendingCount := 0
	for _, r := range recipients {
		if !r.(map[string]interface{})["acked"].(bool) {
			pendingCount++
		}
	}
	if pendingCount != 1 {
		t.Fatalf("expected 1 pending recipient, got %d: %v", pendingCount, dash)
	}
}

// TestBroadcastAcksLegacyEmptyScopeDoesNotFallBackToOrg (Q2): a legacy
// broadcast (no frozen snapshot) whose project scope resolves to ZERO
// eligible users — an all-suspended roster — must report zero recipients,
// not fall through to "all active org users".
func TestBroadcastAcksLegacyEmptyScopeDoesNotFallBackToOrg(t *testing.T) {
	srv, db := commsTestServer(t)
	org := models.Organization{Name: "o", Slug: "obc2", Status: "active"}
	db.Create(&org)
	suspended := models.User{Email: "s@corp.kr", Name: "s", Status: "suspended"}
	suspended.OrganizationID = org.ID
	db.Create(&suspended)
	active := models.User{Email: "a@corp.kr", Name: "a", Status: "active"}
	active.OrganizationID = org.ID
	db.Create(&active)
	db.Create(&models.ProjectMember{
		OrganizationID: org.ID, ProjectID: "p1", UserID: suspended.ID, Role: "member",
	})
	// Legacy broadcast: target scope recorded, no audience snapshot.
	bc := models.Broadcast{
		Title: "t", TargetType: "project", TargetID: "p1", Status: "active",
	}
	bc.OrganizationID = org.ID
	db.Create(&bc)

	rec := doJSON(t, srv, "GET", "/api/communications/broadcasts/"+bc.ID+"/acks", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("acks failed: %d %s", rec.Code, rec.Body.String())
	}
	var dash map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &dash)
	if dash["total_users"].(float64) != 0 {
		t.Fatalf("all-suspended project roster must resolve to zero recipients, not the org: %v", dash)
	}
	excluded, _ := dash["excluded"].([]interface{})
	if len(excluded) != 1 {
		t.Fatalf("suspended roster member should be listed as excluded: %v", dash)
	}
}
