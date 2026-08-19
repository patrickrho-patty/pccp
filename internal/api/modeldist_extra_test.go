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

func mdTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("PCCP_BOOTSTRAP_TOKEN", "test-bootstrap-token")
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/md.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.OrgSetting{}, &models.ModelPackage{}, &models.InferenceEndpoint{},
		&models.ModelPackageEntitlement{}, &models.ModelDistributionCampaign{}, &models.ModelCampaignTarget{},
		&models.ModelArtifactLease{}, &models.ModelCampaignHealthEvidence{},
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

// mdJSON issues a request as orgA's admin unless role/org overridden.
func mdJSON(t *testing.T, srv *Server, method, path, body, role, org string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: org, Email: "model@patty.dev", Role: role}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func mdSeedPackage(t *testing.T, db *gorm.DB, pkgID string) {
	t.Helper()
	db.Create(&models.ModelPackage{
		PackageID: pkgID, ModelID: "patty-kocoder-35b", Name: "KoCoder", Version: "2026.08.1",
		WeightsMerkleRoot: "sha256:merkle-" + pkgID, WeightsShardsJSON: `[{"name":"a.bin","sha256":"deadbeef"}]`,
	})
}

// Entitlement isolation: an org sees only its entitled packages; leases
// are org-bound and expire.
func TestMDEntitlementIsolationAndLeases(t *testing.T) {
	srv, db := mdTestServer(t)
	mdSeedPackage(t, db, "pmp_a")
	mdSeedPackage(t, db, "pmp_b")
	// Grant pmp_a to orgA only.
	if w := mdJSON(t, srv, "POST", "/api/models/distribution/entitlements",
		`{"organization_id":"orgA","package_id":"pmp_a","reason":"계약"}`, "admin", "patty"); w.Code != http.StatusCreated {
		t.Fatalf("entitle: %d %s", w.Code, w.Body.String())
	}
	// orgA sees exactly pmp_a.
	w := mdJSON(t, srv, "GET", "/api/models/distribution/entitled", "", "viewer", "orgA")
	var pkgs []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &pkgs)
	if len(pkgs) != 1 || pkgs[0]["package_id"] != "pmp_a" {
		t.Fatalf("orgA entitled list: %v", pkgs)
	}
	// orgB sees nothing and cannot get a lease for pmp_a.
	w = mdJSON(t, srv, "GET", "/api/models/distribution/entitled", "", "viewer", "orgB")
	json.Unmarshal(w.Body.Bytes(), &pkgs)
	if len(pkgs) != 0 {
		t.Fatalf("orgB saw entitled packages: %v", pkgs)
	}
	if w := mdJSON(t, srv, "POST", "/api/models/distribution/agent/lease",
		`{"package_id":"pmp_a"}`, "viewer", "orgB"); w.Code != http.StatusForbidden {
		t.Fatalf("orgB lease for pmp_a allowed: %d", w.Code)
	}
	// orgA gets a lease; transfer works; expiry rejects.
	w = mdJSON(t, srv, "POST", "/api/models/distribution/agent/lease", `{"package_id":"pmp_a"}`, "viewer", "orgA")
	if w.Code != http.StatusCreated {
		t.Fatalf("orgA lease: %d", w.Code)
	}
	var lease map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &lease)
	w = mdJSON(t, srv, "GET", "/api/models/distribution/transfer/"+lease["lease_token"].(string), "", "viewer", "orgA")
	if w.Code != http.StatusOK {
		t.Fatalf("transfer: %d", w.Code)
	}
	// Cross-org use of the same lease token is rejected.
	if w := mdJSON(t, srv, "GET", "/api/models/distribution/transfer/"+lease["lease_token"].(string), "", "viewer", "orgB"); w.Code != http.StatusForbidden {
		t.Fatalf("cross-org lease accepted: %d", w.Code)
	}
	// Expire it.
	var row models.ModelArtifactLease
	db.Where("token = ?", lease["lease_token"]).First(&row)
	db.Model(&row).Update("expires_at", time.Now().Add(-time.Minute).Format(time.RFC3339))
	if w := mdJSON(t, srv, "GET", "/api/models/distribution/transfer/"+row.Token, "", "viewer", "orgA"); w.Code != http.StatusForbidden {
		t.Fatalf("expired lease accepted: %d", w.Code)
	}
}

// Manual approval is the default: activation without customer approval
// or explicit delegation is refused.
func TestMDManualApprovalDefault(t *testing.T) {
	srv, db := mdTestServer(t)
	mdSeedPackage(t, db, "pmp_c")
	mdJSON(t, srv, "POST", "/api/models/distribution/entitlements",
		`{"organization_id":"orgA","package_id":"pmp_c","reason":"계약"}`, "admin", "patty")
	w := mdJSON(t, srv, "POST", "/api/models/distribution/campaigns",
		`{"package_id":"pmp_c","targets_json":"[{\"organization_id\":\"orgA\",\"environments\":[\"prod\"]}]","reason":"단계 배포"}`, "admin", "patty")
	if w.Code != http.StatusCreated {
		t.Fatalf("campaign: %d %s", w.Code, w.Body.String())
	}
	var c models.ModelDistributionCampaign
	db.First(&c)
	var t1 models.ModelCampaignTarget
	db.Where("campaign_id = ?", fmt.Sprint(c.ID)).First(&t1)
	if t1.ObservedState != mdAwaitingApproval {
		t.Fatalf("target default state: %s, want awaiting_customer_approval", t1.ObservedState)
	}
	// Agent cannot jump to active without approval/delegation.
	if w := mdJSON(t, srv, "POST", "/api/models/distribution/agent/report",
		fmt.Sprintf(`{"campaign_id":"%d","organization_id":"orgA","environment":"prod","observed_state":"active"}`, c.ID), "viewer", "orgA"); w.Code != http.StatusForbidden {
		t.Fatalf("unattended activation allowed: %d %s", w.Code, w.Body.String())
	}
	// Customer approves → entitled → agent walks downloading → verifying → staged.
	if w := mdJSON(t, srv, "POST", fmt.Sprintf("/api/models/distribution/campaigns/%d/approve", c.ID),
		`{"environment":"prod","approve":true}`, "admin", "orgA"); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	for _, st := range []string{"downloading", "verifying", "staged", "loading", "active"} {
		if w := mdJSON(t, srv, "POST", "/api/models/distribution/agent/report",
			fmt.Sprintf(`{"campaign_id":"%d","organization_id":"orgA","environment":"prod","observed_state":"%s"}`, c.ID, st), "viewer", "orgA"); w.Code != http.StatusOK {
			t.Fatalf("report %s: %d %s", st, w.Code, w.Body.String())
		}
	}
}

// Illegal observed-state transitions are rejected.
func TestMDIllegalTransitions(t *testing.T) {
	srv, db := mdTestServer(t)
	mdSeedPackage(t, db, "pmp_d")
	mdJSON(t, srv, "POST", "/api/models/distribution/entitlements",
		`{"organization_id":"orgA","package_id":"pmp_d","reason":"x"}`, "admin", "patty")
	mdJSON(t, srv, "POST", "/api/models/distribution/campaigns",
		`{"package_id":"pmp_d","targets_json":"[{\"organization_id\":\"orgA\"}]","reason":"x"}`, "admin", "patty")
	var c models.ModelDistributionCampaign
	db.First(&c)
	var t1 models.ModelCampaignTarget
	db.Where("campaign_id = ?", fmt.Sprint(c.ID)).First(&t1)
	// entitled → staged skips downloading/verifying: illegal (409).
	if w := mdJSON(t, srv, "POST", "/api/models/distribution/agent/report",
		fmt.Sprintf(`{"campaign_id":"%d","organization_id":"orgA","environment":"default","observed_state":"staged"}`, c.ID), "viewer", "orgA"); w.Code != http.StatusConflict {
		t.Fatalf("entitled→staged accepted: %d", w.Code)
	}
	// entitled → active without delegation: forbidden (403).
	if w := mdJSON(t, srv, "POST", "/api/models/distribution/agent/report",
		fmt.Sprintf(`{"campaign_id":"%d","organization_id":"orgA","environment":"default","observed_state":"active"}`, c.ID), "viewer", "orgA"); w.Code != http.StatusForbidden {
		t.Fatalf("unattended activation accepted: %d", w.Code)
	}
	_ = t1
}

// Stale agents become offline_unknown — silence is never success.
func TestMDReconcileStaleUnknown(t *testing.T) {
	srv, db := mdTestServer(t)
	mdSeedPackage(t, db, "pmp_e")
	mdJSON(t, srv, "POST", "/api/models/distribution/entitlements",
		`{"organization_id":"orgA","package_id":"pmp_e","reason":"x"}`, "admin", "patty")
	mdJSON(t, srv, "POST", "/api/models/distribution/campaigns",
		`{"package_id":"pmp_e","targets_json":"[{\"organization_id\":\"orgA\"}]","reason":"x"}`, "admin", "patty")
	var c models.ModelDistributionCampaign
	db.First(&c)
	var t1 models.ModelCampaignTarget
	db.Where("campaign_id = ?", fmt.Sprint(c.ID)).First(&t1)
	// Simulate a stale last_contact.
	db.Model(&t1).Updates(map[string]interface{}{"observed_state": mdDownloading, "last_contact": time.Now().Add(-2 * time.Hour).Format(time.RFC3339)})
	if w := mdJSON(t, srv, "POST", "/api/models/distribution/reconcile-sweep", `{}`, "admin", "patty"); w.Code != http.StatusOK {
		t.Fatalf("sweep: %d", w.Code)
	}
	db.First(&t1)
	if t1.ObservedState != mdOfflineUnknown || t1.ReasonCode != "agent_stale" {
		t.Fatalf("stale target: %+v", t1)
	}
}

// Health-gate promotion fails closed on missing/stale evidence and
// promotes on objective evidence.
func TestMDPromoteGateFailClosed(t *testing.T) {
	srv, db := mdTestServer(t)
	mdSeedPackage(t, db, "pmp_f")
	mdJSON(t, srv, "POST", "/api/models/distribution/entitlements",
		`{"organization_id":"orgA","package_id":"pmp_f","reason":"x"}`, "admin", "patty")
	mdJSON(t, srv, "POST", "/api/models/distribution/campaigns",
		fmt.Sprintf(`{"package_id":"pmp_f","targets_json":"[{\"organization_id\":\"orgA\"}]","reason":"x","health_gates_json":"{\"error_rate\":0.02,\"load_success\":0.99,\"observation_minutes\":30}"}`), "admin", "patty")
	var c models.ModelDistributionCampaign
	db.First(&c)
	var t1 models.ModelCampaignTarget
	db.Where("campaign_id = ?", fmt.Sprint(c.ID)).First(&t1)
	db.Model(&t1).Update("observed_state", mdCanary)
	// No evidence at all → fail closed.
	w := mdJSON(t, srv, "POST", fmt.Sprintf("/api/models/distribution/campaigns/%d/promote-gate", c.ID), `{}`, "admin", "patty")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["blocked"] != float64(1) || resp["promoted"] != float64(0) {
		t.Fatalf("missing evidence did not fail closed: %v", resp)
	}
	// Stale evidence also fails closed.
	db.Create(&models.ModelCampaignHealthEvidence{
		CampaignID: fmt.Sprint(c.ID), OrganizationID: "orgA",
		LoadSuccess: 1, ErrorRate: 0, Attested: true, ObservationMinutes: 60,
		RecordedAt: time.Now().Add(-3 * time.Hour).Format(time.RFC3339),
	})
	w = mdJSON(t, srv, "POST", fmt.Sprintf("/api/models/distribution/campaigns/%d/promote-gate", c.ID), `{}`, "admin", "patty")
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["blocked"] != float64(1) {
		t.Fatalf("stale evidence did not fail closed: %v", resp)
	}
	// Fresh, objective evidence promotes.
	db.Create(&models.ModelCampaignHealthEvidence{
		CampaignID: fmt.Sprint(c.ID), OrganizationID: "orgA",
		LoadSuccess: 1, ErrorRate: 0.001, Attested: true, ObservationMinutes: 60,
		RecordedAt: time.Now().Format(time.RFC3339),
	})
	w = mdJSON(t, srv, "POST", fmt.Sprintf("/api/models/distribution/campaigns/%d/promote-gate", c.ID), `{}`, "admin", "patty")
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["promoted"] != float64(1) {
		t.Fatalf("fresh evidence did not promote: %v", resp)
	}
	db.First(&t1)
	if t1.ObservedState != mdActive {
		t.Fatalf("target not promoted: %s", t1.ObservedState)
	}
}

// Recall mid-rollout blocks targets and revokes entitlements; preview
// flags ineligible targets; activation with ineligible targets refuses.
func TestMDRecallAndPreview(t *testing.T) {
	srv, db := mdTestServer(t)
	mdSeedPackage(t, db, "pmp_g")
	mdSeedPackage(t, db, "pmp_h")
	mdJSON(t, srv, "POST", "/api/models/distribution/entitlements",
		`{"organization_id":"orgA","package_id":"pmp_g","reason":"x"}`, "admin", "patty")
	// Preview: orgA eligible, orgB not.
	w := mdJSON(t, srv, "POST", "/api/models/distribution/campaigns/preview",
		`{"package_id":"pmp_g","targets_json":"[{\"organization_id\":\"orgA\"},{\"organization_id\":\"orgB\"}]"}`, "admin", "patty")
	var pv map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &pv)
	if len(pv["eligible"].([]interface{})) != 1 || len(pv["ineligible"].([]interface{})) != 1 {
		t.Fatalf("preview: %v", pv)
	}
	// Campaign including orgB → activation refused while ineligible exists.
	mdJSON(t, srv, "POST", "/api/models/distribution/campaigns",
		`{"package_id":"pmp_g","targets_json":"[{\"organization_id\":\"orgA\"},{\"organization_id\":\"orgB\"}]","reason":"x"}`, "admin", "patty")
	var c models.ModelDistributionCampaign
	db.First(&c)
	if w := mdJSON(t, srv, "POST", fmt.Sprintf("/api/models/distribution/campaigns/%d/mutate", c.ID),
		`{"action":"activate","reason":"go","expected_epoch":0}`, "admin", "patty"); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("activation with ineligible target allowed: %d", w.Code)
	}
	// Recall pmp_g mid-rollout.
	if w := mdJSON(t, srv, "POST", "/api/models/distribution/recall",
		`{"package_id":"pmp_g","reason":"취약점 발견"}`, "admin", "patty"); w.Code != http.StatusOK {
		t.Fatalf("recall: %d", w.Code)
	}
	var t1 models.ModelCampaignTarget
	db.Where("organization_id = ?", "orgA").First(&t1)
	if t1.ObservedState != mdBlockedRecalled {
		t.Fatalf("target after recall: %s", t1.ObservedState)
	}
	var ent int64
	db.Model(&models.ModelPackageEntitlement{}).Where("package_id = ? AND revoked = ?", "pmp_g", true).Count(&ent)
	if ent != 1 {
		t.Fatal("entitlement not revoked on recall")
	}
}
