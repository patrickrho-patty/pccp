package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// sandbox_detail_test.go covers the PAT-1513 additions: the sandbox
// detail endpoint (joined session/user, audit evidence, valid actions),
// the retry recovery path, and state-machine enforcement on snapshot.

func TestSandboxDetailReturnsVerticalAndValidActions(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "osbd", Status: "active"}
	db.Create(&org)
	user := models.User{Email: "a@b.c", Name: "A", NameKo: "에이", Status: "active", AuthMethod: "local"}
	user.OrganizationID = org.ID
	db.Create(&user)
	rec := models.SandboxRecord{
		OrganizationID: org.ID, SessionID: "sess-1", UserID: user.ID,
		Mode: "local", BaseImage: "patty/sandbox-base:latest", ImageDigest: "sha256:abc",
		CPULimit: "1", MemoryLimitMB: 1024, NetworkPolicy: "none", Status: "running",
	}
	db.Create(&rec)
	db.Create(&models.AuditEvent{
		OrganizationID: org.ID, EventType: "cp.runtime.forensic_snapshot", Action: "forensic_snapshot",
		ResourceType: "sandbox", ResourceID: rec.ID, Details: `{"snapshot_id":"snap-1"}`,
		Result: "success", OccurredAt: "2026-08-18T00:00:00Z",
	})

	resp := doJSON(t, srv, "GET", "/api/sandboxes/"+rec.ID, "", org.ID)
	if resp.Code != http.StatusOK {
		t.Fatalf("detail failed: %d %s", resp.Code, resp.Body.String())
	}
	var detail map[string]interface{}
	json.Unmarshal(resp.Body.Bytes(), &detail)
	sb := detail["sandbox"].(map[string]interface{})
	if sb["image_digest"] != "sha256:abc" || sb["status"] != "running" {
		t.Fatalf("sandbox payload wrong: %v", sb)
	}
	if detail["user"].(map[string]interface{})["name_ko"] != "에이" {
		t.Fatalf("user join missing: %v", detail["user"])
	}
	if detail["created_at"] == nil || detail["created_at"] == "" {
		t.Fatal("created_at must be exposed for age/TTL display")
	}
	actions := detail["valid_actions"].([]interface{})
	if len(actions) != 2 {
		t.Fatalf("running sandbox must admit snapshot+destroy, got %v", actions)
	}
	snaps := detail["snapshots"].([]interface{})
	if len(snaps) != 1 || snaps[0].(map[string]interface{})["snapshot_id"] != "snap-1" {
		t.Fatalf("snapshot history wrong: %v", snaps)
	}
	if len(detail["audit_events"].([]interface{})) == 0 {
		t.Fatal("audit evidence must be included")
	}
}

func TestSandboxDetailOrgScoped(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "osbo", Status: "active"}
	db.Create(&org)
	other := models.Organization{Name: "o2", Slug: "osbo2", Status: "active"}
	db.Create(&other)
	rec := models.SandboxRecord{OrganizationID: other.ID, Mode: "local", BaseImage: "img", Status: "running"}
	db.Create(&rec)

	resp := doJSON(t, srv, "GET", "/api/sandboxes/"+rec.ID, "", org.ID)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant sandbox must be invisible: %d", resp.Code)
	}

	// Route precedence: the static image-allowlist route must still win
	// over /{id} now that a param route exists.
	resp = doJSON(t, srv, "GET", "/api/sandboxes/image-allowlist", "", org.ID)
	if resp.Code != http.StatusOK {
		t.Fatalf("image-allowlist must not be swallowed by /{id}: %d", resp.Code)
	}
}

func TestSandboxRetryRecoveryAndTransitionGuard(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "osbr", Status: "active"}
	db.Create(&org)
	rec := models.SandboxRecord{OrganizationID: org.ID, Mode: "local", BaseImage: "img", Status: "failed"}
	db.Create(&rec)

	// Retry on failed is admitted; with no runtime reachable the sandbox
	// lands in defined (honest outcome) rather than running.
	resp := doJSON(t, srv, "POST", "/api/sandboxes/"+rec.ID+"/retry", "", org.ID)
	if resp.Code != http.StatusOK {
		t.Fatalf("retry failed: %d %s", resp.Code, resp.Body.String())
	}
	var sb map[string]interface{}
	json.Unmarshal(resp.Body.Bytes(), &sb)
	if sb["status"] != "defined" && sb["status"] != "running" {
		t.Fatalf("retry outcome wrong: %v", sb)
	}
	var retryAudit []models.AuditEvent
	db.Where("resource_id = ? AND action = ?", rec.ID, "retry_sandbox_provision").Find(&retryAudit)
	if len(retryAudit) != 1 {
		t.Fatal("retry must be audited")
	}

	// Retry is not valid for a running sandbox — the state machine
	// rejects with 409.
	db.Model(&models.SandboxRecord{}).Where("id = ?", rec.ID).Update("status", "running")
	resp = doJSON(t, srv, "POST", "/api/sandboxes/"+rec.ID+"/retry", "", org.ID)
	if resp.Code != http.StatusConflict {
		t.Fatalf("retry on running must 409, got %d", resp.Code)
	}

	// Snapshot on a non-running sandbox is rejected by the service.
	db.Model(&models.SandboxRecord{}).Where("id = ?", rec.ID).Update("status", "defined")
	resp = doJSON(t, srv, "POST", "/api/sandboxes/"+rec.ID+"/snapshot", "", org.ID)
	if resp.Code == http.StatusOK {
		t.Fatal("snapshot on a defined sandbox must be rejected")
	}
}

func TestSandboxDestroyIdempotent(t *testing.T) {
	srv, db := toolsSandboxTestServer(t)
	org := models.Organization{Name: "o", Slug: "osbi", Status: "active"}
	db.Create(&org)
	rec := models.SandboxRecord{OrganizationID: org.ID, Mode: "local", BaseImage: "img", Status: "running"}
	db.Create(&rec)

	resp := doJSON(t, srv, "POST", "/api/sandboxes/"+rec.ID+"/destroy", "", org.ID)
	if resp.Code != http.StatusOK {
		t.Fatalf("destroy failed: %d %s", resp.Code, resp.Body.String())
	}
	// Second destroy is idempotent: 200 and no additional destruction
	// evidence events.
	resp = doJSON(t, srv, "POST", "/api/sandboxes/"+rec.ID+"/destroy", "", org.ID)
	if resp.Code != http.StatusOK {
		t.Fatalf("re-destroy must be idempotent: %d %s", resp.Code, resp.Body.String())
	}
	var evts []models.AuditEvent
	db.Where("resource_id = ? AND action = ?", rec.ID, "sandbox_destroy").Find(&evts)
	if len(evts) != 1 {
		t.Fatalf("re-destroy must not mint new evidence, got %d events", len(evts))
	}
}
