package relay

import (
	"testing"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// P0-5: sandbox governance rows are REPO-scoped (the old mapping put
// SessionIDs in RepositoryID — repo matching could never succeed and
// governed sessions fail-closed on all local shell).
func TestSandboxRowsAreRepoScoped(t *testing.T) {
	db := setupGovernedTestDB(t)
	// A repo-scoped row and a session-only row.
	db.Exec(`INSERT INTO sandbox_records (id, created_at, updated_at, organization_id, repository_id, session_id, mode, status)
		VALUES ('sb-1', datetime('now'), datetime('now'), 'org-sb', 'repo-x', '', 'remote_sandbox', 'running')`)
	db.Exec(`INSERT INTO sandbox_records (id, created_at, updated_at, organization_id, repository_id, session_id, mode, status)
		VALUES ('sb-2', datetime('now'), datetime('now'), 'org-sb', '', 'sess-1', 'docker', 'running')`)

	svc, err := New(db, "", "relay-sb")
	if err != nil {
		t.Fatal(err)
	}
	view := svc.GatherGovernanceState("org-sb", "repo-x", "m")
	for _, sb := range view.Sandboxes {
		if sb.RepositoryID == "sess-1" || sb.RepositoryID == "" {
			t.Fatalf("sandbox row leaked a non-repo scope: %+v", sb)
		}
		if sb.RepositoryID != "repo-x" {
			t.Fatalf("expected repo-x scope, got %+v", sb)
		}
	}
	if len(view.Sandboxes) != 1 {
		t.Fatalf("sandbox rows = %d, want 1 (session-only row skipped)", len(view.Sandboxes))
	}
}

// H5 (code review): resumption credentials are crypto-random and
// harness+org bound — two mints for the same inputs never collide, and
// the binding fields are populated.
func TestResumptionTokenRandomAndBound(t *testing.T) {
	a := dari.GenerateResumptionToken("sess-x", "org-1", "harness-1")
	b := dari.GenerateResumptionToken("sess-x", "org-1", "harness-1")
	if string(a.Token) == string(b.Token) {
		t.Fatal("tokens for identical inputs must not collide (crypto-rand)")
	}
	if a.HarnessID != "harness-1" || a.OrgID != "org-1" {
		t.Fatalf("binding fields = %q/%q", a.HarnessID, a.OrgID)
	}
	if !a.IsValid() {
		t.Fatal("fresh token must be valid")
	}
}

// M2 (code review): a nil reservation is a miss for replay purposes —
// never a replayable empty response.
func TestAIOpenCacheNilReservationNotReplayable(t *testing.T) {
	c := newAIOpenCache()
	c.put("conn", "k", nil) // in-flight reservation
	if got, ok := c.get("conn", "k"); ok && got != nil {
		t.Fatalf("reservation leaked as response: %q", got)
	}
	c.put("conn", "k", []byte("done"))
	if got, ok := c.get("conn", "k"); !ok || string(got) != "done" {
		t.Fatalf("filled entry must replay: %q %v", got, ok)
	}
}
