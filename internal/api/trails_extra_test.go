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

func trTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/tr.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.Session{}, &models.ActionEnvelope{}, &models.ChangeSet{},
		&models.TrailNode{}, &models.TrailEdge{}, &models.TrailViewerScope{},
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

func trJSON(t *testing.T, srv *Server, method, path, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-tr", Email: "tr@patty.dev", Role: role}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

// Authorization before everything: viewer role gets 403 with zero
// information leaked (no counts, no clusters).
func TestTrailsRBACBeforeExpansion(t *testing.T) {
	srv, db := trTestServer(t)
	// Seed one session so org data exists.
	db.Create(&models.Session{AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: "org-tr"}})
	if w := trJSON(t, srv, "GET", "/api/trails/overview", "", "viewer"); w.Code != http.StatusForbidden {
		t.Fatalf("viewer overview: %d", w.Code)
	}
	if w := trJSON(t, srv, "GET", "/api/trails/graph", "", "viewer"); w.Code != http.StatusForbidden {
		t.Fatalf("viewer graph: %d", w.Code)
	}
	if w := trJSON(t, srv, "GET", "/api/trails/nodes/session/s1", "", "viewer"); w.Code != http.StatusForbidden {
		t.Fatalf("viewer node: %d", w.Code)
	}
}

// Causality: edges exist only via recorded relationships. Derivation
// from a session + action + changeset yields session→action (initiated)
// and session→changeset (produced) — never adjacency-only edges.
func TestTrailsDerivedCausality(t *testing.T) {
	srv, db := trTestServer(t)
	sess := models.Session{AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: "org-tr"}}
	db.Create(&sess)
	now := time.Now().UTC().Format(time.RFC3339)
	// Two actions in one session — one plain execution, one policy deny.
	db.Create(&models.ActionEnvelope{
		OrganizationID: "org-tr", ActionID: "act-1", SessionID: sess.ID,
		ActionType: "tool_use", OccurredAt: now, EnvelopeDigest: "d1",
	})
	db.Create(&models.ActionEnvelope{
		OrganizationID: "org-tr", ActionID: "act-2", SessionID: sess.ID,
		ActionType: "file_write", VerdictResult: "deny", OccurredAt: now, EnvelopeDigest: "d2",
	})
	// Changeset bound to the same session.
	cs := models.ChangeSet{
		Base: models.Base{}, OrganizationID: "org-tr", SessionID: sess.ID,
		RepositoryID: "repo-1", Branch: "main", LinesAdded: 10, DiffDigest: "dd1",
	}
	db.Create(&cs)
	// An unrelated session with an action at the same time — must NOT
	// gain edges to the first session's nodes (adjacency ≠ causation).
	sess2 := models.Session{AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: "org-tr"}}
	db.Create(&sess2)
	db.Create(&models.ActionEnvelope{
		OrganizationID: "org-tr", ActionID: "act-3", SessionID: sess2.ID,
		ActionType: "tool_use", OccurredAt: now, EnvelopeDigest: "d3",
	})

	if w := trJSON(t, srv, "POST", "/api/trails/rebuild", `{}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("rebuild: %d %s", w.Code, w.Body.String())
	}
	var edges []models.TrailEdge
	db.Find(&edges)
	byType := map[string]int{}
	crossSession := 0
	for _, e := range edges {
		byType[e.EdgeType]++
		if e.FromSourceType == "session" && e.FromSourceID == sess2.ID && e.ToSourceID != "act-3" {
			crossSession++
		}
	}
	if byType["initiated"] < 2 || byType["produced"] < 1 {
		t.Fatalf("derived edges missing: %v", byType)
	}
	if crossSession != 0 {
		t.Fatalf("adjacency leaked causation across sessions: %d", crossSession)
	}
	// Every edge must cite its recorded evidence field.
	for _, e := range edges {
		if e.SourceEvidence == "" {
			t.Fatalf("edge without evidence: %+v", e)
		}
	}

	// Graph read for the session scope: bounded, decision node visible
	// with Korean label, collapsed execution groups counted.
	w := trJSON(t, srv, "GET", "/api/trails/graph?scope=session&ref="+sess.ID, "", "security_admin")
	if w.Code != http.StatusOK {
		t.Fatalf("graph: %d", w.Code)
	}
	var g map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &g)
	nodes := g["nodes"].([]interface{})
	if len(nodes) == 0 {
		t.Fatal("no nodes in session graph")
	}
	foundDecision := false
	for _, raw := range nodes {
		n := raw.(map[string]interface{})
		if n["node_type"] == "decision" && n["label_ko"] == "정책 결정 — 차단" {
			foundDecision = true
		}
		// Safe fields only: no payload content in graph output.
		for _, banned := range []string{"action_payload", "diff_summary", "content"} {
			if _, present := n[banned]; present {
				t.Fatalf("graph node leaks %s", banned)
			}
		}
	}
	if !foundDecision {
		t.Fatal("deny decision node missing")
	}
}

// Path finding uses explicit edges only: connected pair yields a path,
// disconnected pair reports found=false honestly.
func TestTrailsPathExplicitOnly(t *testing.T) {
	srv, db := trTestServer(t)
	sess := models.Session{AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: "org-tr"}}
	db.Create(&sess)
	now := time.Now().UTC().Format(time.RFC3339)
	db.Create(&models.ActionEnvelope{OrganizationID: "org-tr", ActionID: "act-p1", SessionID: sess.ID, ActionType: "tool_use", OccurredAt: now, EnvelopeDigest: "dp"})
	db.Create(&models.ChangeSet{Base: models.Base{}, OrganizationID: "org-tr", SessionID: sess.ID, RepositoryID: "r", Branch: "b", DiffDigest: "dc"})
	var csRow models.ChangeSet
	db.Where("session_id = ?", sess.ID).First(&csRow)
	trJSON(t, srv, "POST", "/api/trails/rebuild", `{}`, "admin")
	// Connected: session → changeset (produced).
	w := trJSON(t, srv, "POST", "/api/trails/path",
		fmt.Sprintf(`{"from_type":"session","from_id":"%s","to_type":"changeset","to_id":"%s"}`, sess.ID, csRow.ID), "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("path: %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["found"] != true {
		t.Fatalf("connected path not found: %v", resp)
	}
	// Disconnected: another session has no chain.
	sess2 := models.Session{AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: "org-tr"}}
	db.Create(&sess2)
	w = trJSON(t, srv, "POST", "/api/trails/path",
		fmt.Sprintf(`{"from_type":"session","from_id":"%s","to_type":"changeset","to_id":"%s"}`, sess2.ID, csRow.ID), "admin")
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["found"] != false || resp["path"] != nil {
		t.Fatalf("invented path for disconnected nodes: %v", resp)
	}
}

// Budget: oversized graphs truncate honestly with a flag.
func TestTrailsBudgetTruncation(t *testing.T) {
	srv, db := trTestServer(t)
	sess := models.Session{AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: "org-tr"}}
	db.Create(&sess)
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < trBudget+50; i++ {
		db.Create(&models.ActionEnvelope{
			OrganizationID: "org-tr", ActionID: fmt.Sprintf("act-b%d", i), SessionID: sess.ID,
			ActionType: "tool_use", OccurredAt: now, EnvelopeDigest: fmt.Sprintf("bd%d", i),
		})
	}
	trJSON(t, srv, "POST", "/api/trails/rebuild", `{}`, "admin")
	w := trJSON(t, srv, "GET", "/api/trails/graph", "", "admin")
	var g map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &g)
	if g["truncated"] != true {
		t.Fatalf("budget overrun not flagged: %v", g["truncated"])
	}
	nodes := g["nodes"].([]interface{})
	if len(nodes) > trBudget {
		t.Fatalf("nodes exceed budget: %d", len(nodes))
	}
}

// Node detail resolves typed metadata from the system of record —
// changeset detail includes attribution/digest, never diff content.
func TestTrailsNodeDetailTyped(t *testing.T) {
	srv, db := trTestServer(t)
	sess := models.Session{AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: "org-tr"}}
	db.Create(&sess)
	cs := models.ChangeSet{
		Base: models.Base{}, OrganizationID: "org-tr", SessionID: sess.ID,
		RepositoryID: "r1", Branch: "feat/x", LinesAdded: 3, LinesRemoved: 1,
		DiffDigest: "dd9", DiffSummary: "SECRET RAW DIFF CONTENT", AttributionState: "AI_GENERATED",
	}
	db.Create(&cs)
	trJSON(t, srv, "POST", "/api/trails/rebuild", `{}`, "admin")
	w := trJSON(t, srv, "GET", "/api/trails/nodes/changeset/"+cs.ID, "", "compliance_admin")
	if w.Code != http.StatusOK {
		t.Fatalf("detail: %d", w.Code)
	}
	body := w.Body.String()
	if bytes.Contains([]byte(body), []byte("SECRET RAW DIFF")) {
		t.Fatalf("node detail leaks raw diff: %s", body)
	}
	var d map[string]interface{}
	json.Unmarshal([]byte(body), &d)
	if d["attribution"] != "AI_GENERATED" || d["diff_digest"] != "dd9" {
		t.Fatalf("typed metadata missing: %v", d)
	}
}
