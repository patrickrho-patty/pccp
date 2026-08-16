package relay

import (
	"testing"
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
