package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func lgTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/lg.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.ChangeSet{}, &models.ProvenanceSpan{},
		&models.SCMProviderConnection{}, &models.ObservedRepositoryEvent{}, &models.CommitAttribution{},
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

func lgJSON(t *testing.T, srv *Server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-lg", Email: "scm@patty.dev", Role: "admin"}))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func lgHMAC(secret string, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// Webhook: bad signature rejected; good signature accepted; duplicate
// delivery idempotently ignored; revoked connection rejected.
func TestLGWebhookVerifyDedupRevoke(t *testing.T) {
	srv, db := lgTestServer(t)
	// Create a Patty Git connection.
	w := lgJSON(t, srv, "POST", "/api/scm/observation/connections",
		`{"provider":"patty_git","webhook_secret":"whsec-1"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("connection: %d %s", w.Code, w.Body.String())
	}
	var connResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &connResp)
	hookURL := fmt.Sprintf("/api/scm/observation/webhooks/%v", connResp["id"])
	body := `{"event_id":"evt-1","type":"push","actor":"patrick","ref":"refs/heads/main","after":"abc123"}`

	// Bad signature → 400.
	if w := lgJSON(t, srv, "POST", hookURL, body, map[string]string{"X-Patty-Signature": "deadbeef"}); w.Code != http.StatusBadRequest {
		t.Fatalf("bad signature accepted: %d", w.Code)
	}
	// Good signature → created.
	sig := lgHMAC("whsec-1", body)
	if w := lgJSON(t, srv, "POST", hookURL, body, map[string]string{"X-Patty-Signature": sig}); w.Code != http.StatusCreated {
		t.Fatalf("good signature rejected: %d %s", w.Code, w.Body.String())
	}
	// Duplicate → ignored, no second row.
	if w := lgJSON(t, srv, "POST", hookURL, body, map[string]string{"X-Patty-Signature": sig}); w.Code != http.StatusOK {
		t.Fatalf("duplicate not acknowledged: %d", w.Code)
	}
	var events []models.ObservedRepositoryEvent
	db.Find(&events)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (idempotent)", len(events))
	}
	// GitHub-style adapter verifies too.
	gh := models.SCMProviderConnection{OrganizationID: "org-lg", Provider: "github", WebhookSecret: "ghsec", Health: "healthy"}
	db.Create(&gh)
	ghBody := `{"delivery_id":"d-9","after":"fff","ref":"refs/heads/x","sender":{"login":"octo"}}`
	ghSig := lgHMAC("ghsec", ghBody)
	if w := lgJSON(t, srv, "POST", fmt.Sprintf("/api/scm/observation/webhooks/%d", gh.ID), ghBody,
		map[string]string{"X-Hub-Signature-256": "sha256=" + ghSig}); w.Code != http.StatusCreated {
		t.Fatalf("github webhook: %d %s", w.Code, w.Body.String())
	}
	// Revoked connection rejects delivery.
	db.Model(&gh).Update("health", "revoked")
	if w := lgJSON(t, srv, "POST", fmt.Sprintf("/api/scm/observation/webhooks/%d", gh.ID), ghBody,
		map[string]string{"X-Hub-Signature-256": "sha256=" + ghSig}); w.Code != http.StatusForbidden {
		t.Fatalf("revoked connection accepted webhook: %d", w.Code)
	}
}

// Attribution binds ONLY on digest match; unmatched commits import as
// unverifiable; categories map from recorded change-set attribution and
// accumulate without rewriting history.
func TestLGAttributionDigestOnly(t *testing.T) {
	srv, db := lgTestServer(t)
	// Recorded change-set with a diff digest + AI attribution.
	cs := models.ChangeSet{
		Base: models.Base{}, OrganizationID: "org-lg", SessionID: "sess-1",
		RepositoryID: "r-1", Branch: "main", DiffDigest: "sha256:patch-1",
		AttributionState: "AI_GENERATED", UserID: "u1", HarnessID: "h1",
	}
	db.Create(&cs)

	// Digest-matching commit binds as ai_created, authoritative.
	w := lgJSON(t, srv, "POST", "/api/scm/observation/attribution",
		`{"provider_repo_id":"r-1","commit_sha":"c1","patch_digest":"sha256:patch-1","git_author":"patrick","git_committer":"ci-bot"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("bind: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["lineage"] != "ai_created" || resp["authoritative"] != true {
		t.Fatalf("bound attribution: %v", resp)
	}
	var a1 models.CommitAttribution
	db.Where("commit_sha = ?", "c1").First(&a1)
	if a1.GitAuthor != "patrick" || a1.GitCommitter != "ci-bot" {
		t.Fatal("author/committer collapsed")
	}

	// Digest-matching commit on a HUMAN_THEN_AI_ASSISTED change →
	// ai_modified_human — categories preserved distinctly.
	cs2 := models.ChangeSet{
		Base: models.Base{}, OrganizationID: "org-lg", SessionID: "sess-2",
		RepositoryID: "r-1", Branch: "main", DiffDigest: "sha256:patch-2",
		AttributionState: "HUMAN_THEN_AI_ASSISTED",
	}
	db.Create(&cs2)
	w = lgJSON(t, srv, "POST", "/api/scm/observation/attribution",
		`{"provider_repo_id":"r-1","commit_sha":"c2","patch_digest":"sha256:patch-2","git_author":"minji","git_committer":"minji"}`, nil)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["lineage"] != "ai_modified_human" {
		t.Fatalf("second category: %v", resp)
	}

	// Digest-matching but different repo commit with no evidence digest →
	// imported_unverifiable, NOT guessed from author/time/message.
	w = lgJSON(t, srv, "POST", "/api/scm/observation/attribution",
		`{"provider_repo_id":"r-1","commit_sha":"c3","patch_digest":"sha256:no-such-digest","git_author":"patrick"}`, nil)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["lineage"] != "imported_unverifiable" || resp["authoritative"] != false {
		t.Fatalf("unmatched commit must stay unverifiable: %v", resp)
	}
	// Earlier categories untouched by the later unverifiable record.
	db.Where("commit_sha = ?", "c1").First(&a1)
	if a1.Lineage != "ai_created" {
		t.Fatalf("later record rewrote history: %+v", a1)
	}
}

// Lineage view: distinct categories are all present with legend; author
// ≠ committer surfaced as a distinct fact.
func TestLGLineageView(t *testing.T) {
	srv, db := lgTestServer(t)
	db.Create(&models.CommitAttribution{
		OrganizationID: "org-lg", ProviderRepoID: "r-1", CommitSHA: "c1",
		GitAuthor: "a@x", GitCommitter: "b@y", Lineage: "ai_created", Authoritative: true,
		EvidenceDigest: "dd", ChangeSetID: "cs1", SessionID: "s1",
	})
	db.Create(&models.CommitAttribution{
		OrganizationID: "org-lg", ProviderRepoID: "r-1", CommitSHA: "c2",
		GitAuthor: "m@x", GitCommitter: "m@x", Lineage: "imported_unverifiable",
	})
	w := lgJSON(t, srv, "GET", "/api/scm/observation/lineage?repo=r-1", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("lineage: %d", w.Code)
	}
	var resp struct {
		Commits []map[string]interface{} `json:"commits"`
		Legend  map[string]string        `json:"legend"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Commits) != 2 {
		t.Fatalf("commits = %d", len(resp.Commits))
	}
	for _, c := range resp.Commits {
		if c["commit_sha"] == "c1" {
			if c["author_distinct"] != true {
				t.Fatal("author≠committer fact missing")
			}
			if c["session_id"] != "s1" {
				t.Fatal("evidence chain missing on authoritative row")
			}
		}
		if c["commit_sha"] == "c2" {
			if _, has := c["session_id"]; has {
				t.Fatal("unverifiable row must not carry an evidence chain")
			}
		}
	}
	for _, need := range []string{"ai_created", "human_created", "human_modified_ai", "ai_modified_human", "mixed", "imported_unverifiable"} {
		if resp.Legend[need] == "" {
			t.Fatalf("legend missing %s", need)
		}
	}
}

// Connections list never exposes credentials; reconciliation marks
// lagging connections stale.
func TestLGConnectionMaskingAndStaleness(t *testing.T) {
	srv, db := lgTestServer(t)
	db.Create(&models.SCMProviderConnection{
		OrganizationID: "org-lg", Provider: "gitlab", BaseURL: "https://gitlab.corp",
		CredentialRef: "secretstore:cred-99", WebhookSecret: "supersecret", Health: "healthy",
	})
	w := lgJSON(t, srv, "GET", "/api/scm/observation/connections", "", nil)
	body := w.Body.String()
	for _, banned := range []string{"supersecret", "secretstore:cred-99"} {
		if bytes.Contains([]byte(body), []byte(banned)) {
			t.Fatalf("connection list leaks %s", banned)
		}
	}
	// Reconcile: no last_reconciliation → stale.
	if w := lgJSON(t, srv, "POST", "/api/scm/observation/reconcile", `{}`, nil); w.Code != http.StatusOK {
		t.Fatalf("reconcile: %d", w.Code)
	}
	var conn models.SCMProviderConnection
	db.First(&conn)
	if conn.Health != "stale" {
		t.Fatalf("lagging connection not marked stale: %s", conn.Health)
	}
}

// The webhook endpoint is publicly routable (no console auth) and
// signature verification still applies.
func TestLGWebhookPublicRoute(t *testing.T) {
	srv, db := lgTestServer(t)
	conn := models.SCMProviderConnection{OrganizationID: "org-lg", Provider: "patty_git", WebhookSecret: "whsec-p", Health: "healthy"}
	db.Create(&conn)
	body := `{"event_id":"evt-pub","type":"push","actor":"patrick","ref":"refs/heads/main","after":"aaa"}`
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/scm/observation/webhooks/%d", conn.ID), bytes.NewReader([]byte(body)))
	// No claims attached — provider deliveries carry none.
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsigned public webhook: %d (want 400 signature reject)", w.Code)
	}
	req2 := httptest.NewRequest("POST", fmt.Sprintf("/api/scm/observation/webhooks/%d", conn.ID), bytes.NewReader([]byte(body)))
	req2.Header.Set("X-Patty-Signature", lgHMAC("whsec-p", body))
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("signed public webhook rejected: %d %s", w2.Code, w2.Body.String())
	}
	// Patty Git event type parsed (exported field fix).
	var ev models.ObservedRepositoryEvent
	db.Where("provider_event_id = ?", "evt-pub").First(&ev)
	if ev.EventType != "push" {
		t.Fatalf("patty git event type empty: %q", ev.EventType)
	}
}

// Cross-connection dedup: the same provider event ID on two connections
// is two legitimate events, not a duplicate (self-hosted instances
// share ID spaces).
func TestLGWebhookCrossConnectionNotDropped(t *testing.T) {
	srv, db := lgTestServer(t)
	conn1 := models.SCMProviderConnection{OrganizationID: "org-lg", Provider: "patty_git", WebhookSecret: "s1", Health: "healthy"}
	conn2 := models.SCMProviderConnection{OrganizationID: "org-lg", Provider: "patty_git", WebhookSecret: "s1", Health: "healthy"}
	db.Create(&conn1)
	db.Create(&conn2)
	body := `{"event_id":"evt-shared-7","type":"push","actor":"a","ref":"refs/heads/main","after":"zzz"}`
	sig := lgHMAC("s1", body)
	// Same event on both connections — both must be stored.
	if w := lgJSON(t, srv, "POST", fmt.Sprintf("/api/scm/observation/webhooks/%d", conn1.ID), body, map[string]string{"X-Patty-Signature": sig}); w.Code != http.StatusCreated {
		t.Fatalf("conn1: %d %s", w.Code, w.Body.String())
	}
	if w := lgJSON(t, srv, "POST", fmt.Sprintf("/api/scm/observation/webhooks/%d", conn2.ID), body, map[string]string{"X-Patty-Signature": sig}); w.Code != http.StatusCreated {
		t.Fatalf("conn2 cross-connection event dropped: %d %s", w.Code, w.Body.String())
	}
	var count int64
	db.Model(&models.ObservedRepositoryEvent{}).Where("provider_event_id = ?", "evt-shared-7").Count(&count)
	if count != 2 {
		t.Fatalf("stored %d events, want 2 (one per connection)", count)
	}
	// Same connection replay is still a duplicate.
	if w := lgJSON(t, srv, "POST", fmt.Sprintf("/api/scm/observation/webhooks/%d", conn1.ID), body, map[string]string{"X-Patty-Signature": sig}); w.Code != http.StatusOK {
		t.Fatalf("same-connection replay not deduped: %d", w.Code)
	}
}
