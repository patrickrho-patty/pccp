package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

func mkSession(t *testing.T, db *gorm.DB, orgID, status string) *models.Session {
	t.Helper()
	sess := models.Session{
		UserID: "u1", HarnessID: "hrn-1", SessionID: fmt.Sprintf("ws_%s", status), Status: status,
	}
	sess.OrganizationID = orgID
	if err := db.Create(&sess).Error; err != nil {
		t.Fatal(err)
	}
	return &sess
}

func TestSessionLifecycleTransitionsAndBroadcast(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgsl", Status: "active"}
	db.Create(&org)
	sess := mkSession(t, db, org.ID, "active")

	// active → paused: allowed.
	rec := doJSON(t, srv, "POST", "/api/sessions/"+sess.ID+"/pause", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause active → %d: %s", rec.Code, rec.Body.String())
	}
	var reloaded models.Session
	db.First(&reloaded, "id = ?", sess.ID)
	if reloaded.Status != "paused" || reloaded.LastActivityAt == "" {
		t.Fatalf("after pause: status=%s last_activity_at=%q", reloaded.Status, reloaded.LastActivityAt)
	}

	// paused → paused: invalid transition.
	rec = doJSON(t, srv, "POST", "/api/sessions/"+sess.ID+"/pause", "", org.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("re-pause → %d, want 409", rec.Code)
	}

	// paused → active (resume): allowed; last_activity refreshed.
	rec = doJSON(t, srv, "POST", "/api/sessions/"+sess.ID+"/resume", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume paused → %d: %s", rec.Code, rec.Body.String())
	}

	// active → closed: allowed; closed is terminal.
	rec = doJSON(t, srv, "POST", "/api/sessions/"+sess.ID+"/close", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("close active → %d: %s", rec.Code, rec.Body.String())
	}
	for _, action := range []string{"pause", "resume", "close"} {
		rec = doJSON(t, srv, "POST", "/api/sessions/"+sess.ID+"/"+action, "", org.ID)
		if rec.Code != http.StatusConflict {
			t.Fatalf("terminal %s → %d, want 409", action, rec.Code)
		}
	}
}

func TestSessionLifecycleCrossOrgAndUnknown(t *testing.T) {
	srv, db := harnessTestServer(t)
	orgA := models.Organization{Name: "a", Slug: "sla", Status: "active"}
	orgB := models.Organization{Name: "b", Slug: "slb", Status: "active"}
	db.Create(&orgA)
	db.Create(&orgB)
	sess := mkSession(t, db, orgA.ID, "active")

	// Cross-org mutation must 404, never mutate.
	rec := doJSON(t, srv, "POST", "/api/sessions/"+sess.ID+"/pause", "", orgB.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org pause → %d, want 404", rec.Code)
	}
	var reloaded models.Session
	db.First(&reloaded, "id = ?", sess.ID)
	if reloaded.Status != "active" {
		t.Fatalf("cross-org call mutated status: %s", reloaded.Status)
	}

	// Unknown session 404s.
	rec = doJSON(t, srv, "POST", "/api/sessions/nope/pause", "", orgA.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session → %d, want 404", rec.Code)
	}
}

func TestSessionsListExposesCanonicalFields(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgsf", Status: "active"}
	db.Create(&org)
	active := mkSession(t, db, org.ID, "active")
	mkSession(t, db, org.ID, "paused")
	mkSession(t, db, org.ID, "closed")

	rec := doJSON(t, srv, "GET", "/api/sessions?page=1&size=50", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("list → %d", rec.Code)
	}
	var page struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 3 {
		t.Fatalf("rows = %d, want 3", len(page.Data))
	}
	for _, row := range page.Data {
		for _, field := range []string{"id", "status", "opened_at"} {
			if _, ok := row[field]; !ok {
				t.Fatalf("row missing canonical field %q: %v", field, row)
			}
		}
	}
	_ = active
}

func TestBulkSessionsSkipInvalidTransitions(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgbs", Status: "active"}
	db.Create(&org)
	live := mkSession(t, db, org.ID, "active")
	done := mkSession(t, db, org.ID, "closed")

	// Bulk pause: the live row moves, the terminal row is skipped — bulk
	// can never resurrect a closed session (PAT-1496).
	rec := doJSON(t, srv, "POST", "/api/sessions/bulk",
		fmt.Sprintf(`{"ids":["%s","%s"],"action":"pause"}`, live.ID, done.ID), org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk → %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Affected int64    `json:"affected"`
		Skipped  []string `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Affected != 1 || len(resp.Skipped) != 1 {
		t.Fatalf("bulk result: affected=%d skipped=%v", resp.Affected, resp.Skipped)
	}
	var reloadedDone models.Session
	db.First(&reloadedDone, "id = ?", done.ID)
	if reloadedDone.Status != "closed" {
		t.Fatalf("terminal session mutated: %s", reloadedDone.Status)
	}
	var reloadedLive models.Session
	db.First(&reloadedLive, "id = ?", live.ID)
	if reloadedLive.Status != "paused" || reloadedLive.LastActivityAt == "" {
		t.Fatalf("live session after bulk: status=%s last_activity_at=%q", reloadedLive.Status, reloadedLive.LastActivityAt)
	}
}
