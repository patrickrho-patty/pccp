package api

import (
	"bytes"
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

func mkTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/mk.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.MarketPublisher{}, &models.MarketListing{},
		&models.MarketListingVersion{}, &models.MarketReport{}, &models.MarketInstallRecord{},
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

func mkJSON(t *testing.T, srv *Server, method, path, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: "org-mk", Email: "mk@patty.dev", Role: role}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func mkPublisher(t *testing.T, srv *Server) string {
	t.Helper()
	w := mkJSON(t, srv, "POST", "/api/marketplace/publishers", `{"display_name":"Acme Tools","email":"dev@acme.io"}`, "viewer")
	var pub models.MarketPublisher
	json.Unmarshal(w.Body.Bytes(), &pub)
	return pub.PublisherID
}

func mkPublish(t *testing.T, srv *Server, pub, slug, manifest string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"publisher_id": pub, "slug": slug, "name": slug, "type": "skill",
		"category": "productivity", "version": "1.0.0",
		"content_hash": "sha256:" + slug + "hash", "manifest_json": manifest,
	})
	w := mkJSON(t, srv, "POST", "/api/marketplace/publish", string(body), "viewer")
	if w.Code != http.StatusCreated {
		t.Fatalf("publish %s: %d %s", slug, w.Code, w.Body.String())
	}
}

// Immutability: the same version with a different hash is rejected;
// identical (version, hash) is a duplicate; a new version is fine.
func TestMKImmutableVersions(t *testing.T) {
	srv, _ := mkTestServer(t)
	pub := mkPublisher(t, srv)
	mkPublish(t, srv, pub, "tool-a", `{"capabilities":["filesystem"]}`)
	// Same version, different hash → 409 (bytes changed must be a new version).
	w := mkJSON(t, srv, "POST", "/api/marketplace/versions",
		`{"slug":"tool-a","version":"1.0.0","content_hash":"sha256:different","manifest_json":"{}"}`, "viewer")
	if w.Code != http.StatusConflict {
		t.Fatalf("bytes-changed same version accepted: %d", w.Code)
	}
	// New version → created.
	w = mkJSON(t, srv, "POST", "/api/marketplace/versions",
		`{"slug":"tool-a","version":"1.1.0","content_hash":"sha256:newhash","manifest_json":"{\"capabilities\":[\"filesystem\"]}"}`, "viewer")
	if w.Code != http.StatusCreated {
		t.Fatalf("new version rejected: %d %s", w.Code, w.Body.String())
	}
}

// Automated checks gate discovery: secrets/impersonation fail → pending,
// not listed in search.
func TestMKChecksGateDiscovery(t *testing.T) {
	srv, db := mkTestServer(t)
	pub := mkPublisher(t, srv)
	// Secret-bearing manifest → checks fail.
	leakyBody, _ := json.Marshal(map[string]interface{}{
		"publisher_id": pub, "slug": "leaky", "name": "Leaky", "type": "skill",
		"version": "1.0.0", "content_hash": "sha256:leak",
		"manifest_json": `{"api_key":"ghp_abcdefghijklmnopqrstuvwxy"}`,
	})
	mkJSON(t, srv, "POST", "/api/marketplace/publish", string(leakyBody), "viewer")
	var leaky models.MarketListingVersion
	db.Where("slug = ?", "leaky").First(&leaky)
	if leaky.State != "pending" {
		t.Fatalf("leaky version listed: %s", leaky.State)
	}
	// Impersonation.
	impBody, _ := json.Marshal(map[string]interface{}{
		"publisher_id": pub, "slug": "patty", "name": "Patty", "type": "skill",
		"version": "1.0.0", "content_hash": "sha256:imp", "manifest_json": `{}`,
	})
	mkJSON(t, srv, "POST", "/api/marketplace/publish", string(impBody), "viewer")
	var imp models.MarketListingVersion
	db.Where("slug = ?", "patty").First(&imp)
	if imp.State != "pending" {
		t.Fatalf("impersonation listed: %s", imp.State)
	}
	// Clean package → active and discoverable.
	mkPublish(t, srv, pub, "clean-tool", `{"capabilities":["filesystem"]}`)
	w := mkJSON(t, srv, "GET", "/api/marketplace/search?query=clean", "", "viewer")
	var results []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &results)
	if len(results) != 1 || results[0]["slug"] != "clean-tool" {
		t.Fatalf("clean tool not discoverable: %v", results)
	}
	// Pending-only package NOT in search.
	w = mkJSON(t, srv, "GET", "/api/marketplace/search?query=leaky", "", "viewer")
	json.Unmarshal(w.Body.Bytes(), &results)
	if len(results) != 0 {
		t.Fatalf("pending package discovered: %v", results)
	}
}

// Trust labels derive from publisher trust and are never influenced by
// featured/sponsored placement.
func TestMKTrustIndependentOfPlacement(t *testing.T) {
	srv, db := mkTestServer(t)
	pub := mkPublisher(t, srv)
	mkPublish(t, srv, pub, "trust-tool", `{"capabilities":["none"]}`)
	// Sponsor + feature it.
	w := mkJSON(t, srv, "POST", "/api/marketplace/placement",
		`{"slug":"trust-tool","featured":true,"sponsored":true}`, "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("placement: %d", w.Code)
	}
	var listing models.MarketListing
	db.Where("slug = ?", "trust-tool").First(&listing)
	if listing.TrustLabel != "community" {
		t.Fatalf("sponsorship altered trust: %s", listing.TrustLabel)
	}
	// Publisher verified → label re-derives to verified_publisher.
	if w := mkJSON(t, srv, "POST", fmt.Sprintf("/api/marketplace/publishers/%s/trust", pub),
		`{"trust_state":"verified"}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("verify: %d", w.Code)
	}
	db.Where("slug = ?", "trust-tool").First(&listing)
	if listing.TrustLabel != "verified_publisher" {
		t.Fatalf("verified label: %s", listing.TrustLabel)
	}
	// Revocation downgrades everything.
	mkJSON(t, srv, "POST", fmt.Sprintf("/api/marketplace/publishers/%s/trust", pub), `{"trust_state":"revoked"}`, "admin")
	db.Where("slug = ?", "trust-tool").First(&listing)
	if listing.TrustLabel != "community" {
		t.Fatalf("revoked label: %s", listing.TrustLabel)
	}
}

// Install gating: hash mismatch rejected, quarantined version rejected,
// full_trust needs approval, pin blocks update, rollback restores.
func TestMKInstallLifecycle(t *testing.T) {
	srv, db := mkTestServer(t)
	pub := mkPublisher(t, srv)
	mkPublish(t, srv, pub, "life-tool", `{"capabilities":["mcp_tools"]}`)
	// Wrong hash → 409.
	if w := mkJSON(t, srv, "POST", "/api/marketplace/installs",
		`{"harness_id":"h1","slug":"life-tool","version":"1.0.0","content_hash":"sha256:wrong"}`, "viewer"); w.Code != http.StatusConflict {
		t.Fatalf("hash mismatch installed: %d", w.Code)
	}
	// Correct install.
	w := mkJSON(t, srv, "POST", "/api/marketplace/installs",
		`{"harness_id":"h1","slug":"life-tool","version":"1.0.0","content_hash":"sha256:life-toolhash"}`, "viewer")
	if w.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", w.Code, w.Body.String())
	}
	var rec models.MarketInstallRecord
	db.First(&rec, "slug = ?", "life-tool")
	if rec.State != "installed" {
		t.Fatalf("install state: %s", rec.State)
	}
	// Add a new version, record update, rollback restores previous.
	mkJSON(t, srv, "POST", "/api/marketplace/versions",
		`{"slug":"life-tool","version":"2.0.0","content_hash":"sha256:v2hash","manifest_json":"{\"capabilities\":[\"mcp_tools\"]}"}`, "viewer")
	if w := mkJSON(t, srv, "POST", "/api/marketplace/installs/update",
		fmt.Sprintf(`{"record_id":%d,"version":"2.0.0","hash":"sha256:v2hash"}`, rec.ID), "viewer"); w.Code != http.StatusOK {
		t.Fatalf("update: %d", w.Code)
	}
	// Pin + attempted update → conflict.
	mkJSON(t, srv, "POST", fmt.Sprintf("/api/marketplace/installs/%d/lifecycle", rec.ID), `{"action":"pin"}`, "viewer")
	if w := mkJSON(t, srv, "POST", "/api/marketplace/installs/update",
		fmt.Sprintf(`{"record_id":%d,"version":"3.0.0"}`, rec.ID), "viewer"); w.Code != http.StatusConflict {
		t.Fatalf("pinned update allowed: %d", w.Code)
	}
	// Rollback to the previous verified version.
	if w := mkJSON(t, srv, "POST", fmt.Sprintf("/api/marketplace/installs/%d/lifecycle", rec.ID), `{"action":"rollback"}`, "viewer"); w.Code != http.StatusOK {
		t.Fatalf("rollback: %d %s", w.Code, w.Body.String())
	}
	db.First(&rec, "id = ?", rec.ID)
	if rec.Version != "1.0.0" {
		t.Fatalf("rollback version: %s", rec.Version)
	}
}

// full_trust capability → needs_approval; approval installs.
func TestMKFullTrustNeedsApproval(t *testing.T) {
	srv, db := mkTestServer(t)
	pub := mkPublisher(t, srv)
	mkPublish(t, srv, pub, "full-tool", `{"capabilities":["full_trust"]}`)
	w := mkJSON(t, srv, "POST", "/api/marketplace/installs",
		`{"harness_id":"h1","slug":"full-tool","version":"1.0.0"}`, "viewer")
	if w.Code != http.StatusCreated {
		t.Fatalf("install: %d", w.Code)
	}
	var rec models.MarketInstallRecord
	db.First(&rec, "slug = ?", "full-tool")
	if rec.State != "needs_approval" {
		t.Fatalf("full_trust auto-installed: %s", rec.State)
	}
	if w := mkJSON(t, srv, "POST", fmt.Sprintf("/api/marketplace/installs/%d/lifecycle", rec.ID), `{"action":"approve"}`, "viewer"); w.Code != http.StatusOK {
		t.Fatalf("approve: %d", w.Code)
	}
	db.First(&rec, "id = ?", rec.ID)
	if rec.State != "installed" {
		t.Fatalf("approved state: %s", rec.State)
	}
}

// Auto-update envelope: capability expansion requires approval.
func TestMKAutoUpdateEnvelope(t *testing.T) {
	srv, _ := mkTestServer(t)
	pub := mkPublisher(t, srv)
	mkPublish(t, srv, pub, "upd-tool", `{"capabilities":["filesystem"]}`)
	mkJSON(t, srv, "POST", "/api/marketplace/versions",
		`{"slug":"upd-tool","version":"1.1.0","content_hash":"sha256:u11","manifest_json":"{\"capabilities\":[\"filesystem\"]}"}`, "viewer")
	mkJSON(t, srv, "POST", "/api/marketplace/versions",
		`{"slug":"upd-tool","version":"2.0.0","content_hash":"sha256:u20","manifest_json":"{\"capabilities\":[\"filesystem\",\"network\"]}"}`, "viewer")
	// Same envelope → auto eligible.
	w := mkJSON(t, srv, "POST", "/api/marketplace/update-eligibility",
		`{"slug":"upd-tool","from_version":"1.0.0","to_version":"1.1.0"}`, "viewer")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["auto_eligible"] != true {
		t.Fatalf("routine update not auto-eligible: %v", resp)
	}
	// Expanded envelope → approval required.
	w = mkJSON(t, srv, "POST", "/api/marketplace/update-eligibility",
		`{"slug":"upd-tool","from_version":"1.0.0","to_version":"2.0.0"}`, "viewer")
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["auto_eligible"] != false {
		t.Fatalf("expansion auto-eligible: %v", resp)
	}
}

// Moderation: critical disable blocks listing + quarantines versions +
// marks installed records warned; quarantined version installs rejected.
func TestMKModerationCriticalDisable(t *testing.T) {
	srv, db := mkTestServer(t)
	pub := mkPublisher(t, srv)
	mkPublish(t, srv, pub, "evil-tool", `{"capabilities":["none"]}`)
	mkJSON(t, srv, "POST", "/api/marketplace/installs",
		`{"harness_id":"h1","slug":"evil-tool","version":"1.0.0"}`, "viewer")
	// Viewer cannot moderate.
	if w := mkJSON(t, srv, "POST", "/api/marketplace/moderate",
		`{"action":"critical_disable","slug":"evil-tool","reason":"x"}`, "viewer"); w.Code != http.StatusForbidden {
		t.Fatalf("viewer moderated: %d", w.Code)
	}
	if w := mkJSON(t, srv, "POST", "/api/marketplace/moderate",
		`{"action":"critical_disable","slug":"evil-tool","reason":"악성코드 확인"}`, "admin"); w.Code != http.StatusOK {
		t.Fatalf("critical disable: %d %s", w.Code, w.Body.String())
	}
	var listing models.MarketListing
	db.Where("slug = ?", "evil-tool").First(&listing)
	if listing.Status != "blocked" {
		t.Fatalf("listing not blocked: %s", listing.Status)
	}
	var rec models.MarketInstallRecord
	db.First(&rec, "slug = ?", "evil-tool")
	if rec.State != "quarantined" || !rec.Warned {
		t.Fatalf("install not quarantined/warned: %+v", rec)
	}
	// Install attempt on the quarantined version → rejected.
	if w := mkJSON(t, srv, "POST", "/api/marketplace/installs",
		`{"harness_id":"h2","slug":"evil-tool","version":"1.0.0"}`, "viewer"); w.Code != http.StatusForbidden {
		t.Fatalf("quarantined install allowed: %d", w.Code)
	}
	// Quarantined package cannot be re-enabled by the user.
	if w := mkJSON(t, srv, "POST", fmt.Sprintf("/api/marketplace/installs/%d/lifecycle", rec.ID), `{"action":"enable"}`, "viewer"); w.Code != http.StatusForbidden {
		t.Fatalf("quarantine enable allowed: %d", w.Code)
	}
	var audits int64
	db.Model(&models.AuditEvent{}).Where("event_type = ?", "cp.marketplace.moderated").Count(&audits)
	if audits == 0 {
		t.Fatal("moderation not audited")
	}
}
