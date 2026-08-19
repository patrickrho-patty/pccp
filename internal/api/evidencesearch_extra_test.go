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

func esTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/es.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.Session{}, &models.PromptExchange{}, &models.ProvenanceSpan{},
		&models.ChangeSet{}, &models.TrailNode{}, &models.EvidenceSearchGrant{}, &models.EvidenceSearchAudit{},
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

func esJSON(t *testing.T, srv *Server, method, path, body, role, org, email string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: org, Email: email, Role: role}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func esSeed(t *testing.T, db *gorm.DB) models.Session {
	t.Helper()
	sess := models.Session{AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: "org-es"}}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&models.PromptExchange{
		SessionID: sess.ID, ExchangeID: "ex-1",
		PromptText: "sk-SECRET123 결제 모듈을 검토해줘", ResponseText: "검토했습니다",
	})
	return sess
}

// Generic admin status without the dedicated grant is denied; ordinary
// users are denied; a granted admin searches successfully.
func TestESPermissionGate(t *testing.T) {
	srv, db := esTestServer(t)
	sess := esSeed(t, db)
	body := fmt.Sprintf(`{"query":"결제"}`)
	// Plain admin role: no grant → 403.
	if w := esJSON(t, srv, "POST", "/api/evidence-search/query", body, "admin", "org-es", "a@x"); w.Code != http.StatusForbidden {
		t.Fatalf("grantless admin searched: %d", w.Code)
	}
	if w := esJSON(t, srv, "POST", "/api/evidence-search/query", body, "viewer", "org-es", "v@x"); w.Code != http.StatusForbidden {
		t.Fatalf("viewer searched: %d", w.Code)
	}
	// Grant → search works.
	db.Create(&models.EvidenceSearchGrant{OrganizationID: "org-es", AdminEmail: "inv@x", ScopeKind: "organization"})
	w := esJSON(t, srv, "POST", "/api/evidence-search/query", body, "admin", "org-es", "inv@x")
	if w.Code != http.StatusOK {
		t.Fatalf("granted admin: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["export_available"] != false {
		t.Fatal("export must be advertised as unavailable")
	}
	// Revocation is immediate.
	var g models.EvidenceSearchGrant
	db.First(&g)
	db.Model(&g).Update("revoked", true)
	if w := esJSON(t, srv, "POST", "/api/evidence-search/query", body, "admin", "org-es", "inv@x"); w.Code != http.StatusForbidden {
		t.Fatalf("revoked grant still searches: %d", w.Code)
	}
	// Expiry is honored per call.
	db.Model(&g).Updates(map[string]interface{}{"revoked": false, "expires_at": time.Now().Add(-time.Minute).Format(time.RFC3339)})
	if w := esJSON(t, srv, "POST", "/api/evidence-search/query", body, "admin", "org-es", "inv@x"); w.Code != http.StatusForbidden {
		t.Fatalf("expired grant still searches: %d", w.Code)
	}
	_ = sess
}

// Masking: snippets mask secret-looking prefixes; reveal requires the
// separate permission + reason and audits.
func TestESMaskingAndReveal(t *testing.T) {
	srv, db := esTestServer(t)
	esSeed(t, db)
	db.Create(&models.EvidenceSearchGrant{OrganizationID: "org-es", AdminEmail: "inv@x"})
	w := esJSON(t, srv, "POST", "/api/evidence-search/query", `{"query":"결제"}`, "admin", "org-es", "inv@x")
	var resp struct {
		Results map[string][]struct {
			Label  string `json:"label"`
			Masked bool   `json:"masked"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Results["conversations"]) == 0 {
		t.Fatal("no conversation hit")
	}
	hit := resp.Results["conversations"][0]
	if !hit.Masked {
		t.Fatal("conversation hit not flagged masked")
	}
	if bytes.Contains([]byte(hit.Label), []byte("sk-SECRET123")) {
		t.Fatalf("excerpt leaks secret: %s", hit.Label)
	}
	// Reveal without CanReveal → 403.
	var ex models.PromptExchange
	db.First(&ex)
	if w := esJSON(t, srv, "POST", "/api/evidence-search/reveal",
		fmt.Sprintf(`{"domain":"conversations","source_id":"%s","reason":"침해 조사"}`, ex.ID), "admin", "org-es", "inv@x"); w.Code != http.StatusForbidden {
		t.Fatalf("reveal without permission: %d", w.Code)
	}
	// Reasonless reveal → 400 even with permission.
	var g models.EvidenceSearchGrant
	db.First(&g)
	db.Model(&g).Update("can_reveal", true)
	if w := esJSON(t, srv, "POST", "/api/evidence-search/reveal",
		fmt.Sprintf(`{"domain":"conversations","source_id":"%s"}`, ex.ID), "admin", "org-es", "inv@x"); w.Code != http.StatusBadRequest {
		t.Fatalf("reasonless reveal: %d", w.Code)
	}
	if w := esJSON(t, srv, "POST", "/api/evidence-search/reveal",
		fmt.Sprintf(`{"domain":"conversations","source_id":"%s","reason":"침해 조사"}`, ex.ID), "admin", "org-es", "inv@x"); w.Code != http.StatusOK {
		t.Fatalf("authorized reveal: %d %s", w.Code, w.Body.String())
	}
	var audits int64
	db.Model(&models.EvidenceSearchAudit{}).Where("kind = ?", "reveal").Count(&audits)
	if audits != 1 {
		t.Fatalf("reveal audits = %d", audits)
	}
}

// Cross-tenant isolation: another org's records never appear, even with
// an identical query.
func TestESTenantIsolation(t *testing.T) {
	srv, db := esTestServer(t)
	sess := esSeed(t, db)
	// Foreign org session + exchange.
	foreign := models.Session{AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: "org-other"}}
	db.Create(&foreign)
	db.Create(&models.PromptExchange{SessionID: foreign.ID, PromptText: "결제 키 검토"})
	db.Create(&models.EvidenceSearchGrant{OrganizationID: "org-es", AdminEmail: "inv@x"})
	db.Create(&models.EvidenceSearchGrant{OrganizationID: "org-other", AdminEmail: "inv@x"})
	w := esJSON(t, srv, "POST", "/api/evidence-search/query", `{"query":"결제"}`, "admin", "org-es", "inv@x")
	var resp struct {
		Results map[string][]map[string]interface{} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	for _, hits := range resp.Results {
		for _, h := range hits {
			if ref, _ := h["scope_ref"].(string); ref == "session:"+foreign.ID {
				t.Fatal("cross-tenant record leaked into results")
			}
		}
	}
	_ = sess
}

// Exact-identifier priority: a session ID resolves as exact-match ahead
// of lexical results.
func TestESExactIDPriority(t *testing.T) {
	srv, db := esTestServer(t)
	sess := esSeed(t, db)
	db.Create(&models.EvidenceSearchGrant{OrganizationID: "org-es", AdminEmail: "inv@x"})
	w := esJSON(t, srv, "POST", "/api/evidence-search/query",
		fmt.Sprintf(`{"query":"%s"}`, sess.ID), "admin", "org-es", "inv@x")
	var resp struct {
		Results map[string][]struct {
			RankKind string `json:"rank_kind"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Results["conversations"]) == 0 || resp.Results["conversations"][0].RankKind != "exact" {
		t.Fatalf("exact match missing or not first: %+v", resp.Results["conversations"])
	}
}
