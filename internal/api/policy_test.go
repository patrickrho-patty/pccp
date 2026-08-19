package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func policyTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/pol.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.PolicyRule{}, &models.PolicyEpoch{},
		&models.PolicyPack{}, &models.PolicyTemplate{}, &models.PolicyAcknowledgement{},
		&models.PolicyException{}, &models.AuditEvent{}, &models.ServiceSigningKey{},
		&models.Session{}, &models.Harness{}, &models.Project{}, &models.CapabilityLease{},
		&models.Role{}, &models.UserRole{},
		&models.SecurityLockdown{}, &models.FleetDesiredState{},
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

func createRule(t *testing.T, srv *Server, orgID, domain, name, scope, scopeName, config string) map[string]interface{} {
	t.Helper()
	body := `{"domain":"` + domain + `","name":"` + name + `","scope":"` + scope + `","scopeName":"` + scopeName + `","enabled":true,"config":` + config + `}`
	rec := doJSON(t, srv, "POST", "/api/policy/rules", body, orgID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create rule failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	rule := resp["rule"].(map[string]interface{})
	if rule["status"] != "draft" {
		t.Fatalf("new rule should be a draft, got %v", rule["status"])
	}
	return rule
}

func approveRule(t *testing.T, srv *Server, orgID, ruleID string) map[string]interface{} {
	t.Helper()
	rec := doJSON(t, srv, "POST", "/api/policy/rules/"+ruleID+"/approve", "", orgID)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp
}

func TestRuleApprovalPublishesModelEpoch(t *testing.T) {
	srv, db := policyTestServer(t)
	org := models.Organization{Name: "o", Slug: "o", Status: "active"}
	db.Create(&org)

	// Draft creation must NOT touch epochs.
	rule := createRule(t, srv, org.ID, "models", "restrict", "org", "전체 조직", `{"allowed_models":["patty-code-standard"]}`)
	var epochCount int64
	db.Model(&models.PolicyEpoch{}).Count(&epochCount)
	if epochCount != 0 {
		t.Fatalf("draft rule must not create an epoch, got %d", epochCount)
	}

	// Approval publishes: epoch created with the model allow-list.
	approveRule(t, srv, org.ID, rule["id"].(string))
	db.Model(&models.PolicyEpoch{}).Count(&epochCount)
	if epochCount != 1 {
		t.Fatalf("approval should rebuild the epoch, got %d", epochCount)
	}
	var epoch models.PolicyEpoch
	db.First(&epoch)
	var allowed []string
	json.Unmarshal([]byte(epoch.AllowedModelsJSON), &allowed)
	if len(allowed) != 1 || allowed[0] != "patty-code-standard" {
		t.Fatalf("epoch allowed models wrong: %v", allowed)
	}

	// A network-domain rule approval must NOT touch the model list.
	rule2 := createRule(t, srv, org.ID, "network", "allowlist", "org", "전체 조직", `{"mode":"allowlist","allowed":["npmjs.org"]}`)
	approveRule(t, srv, org.ID, rule2["id"].(string))
	var epoch2 models.PolicyEpoch
	db.Where("status = ?", "active").First(&epoch2)
	var allowed2 []string
	json.Unmarshal([]byte(epoch2.AllowedModelsJSON), &allowed2)
	if len(allowed2) != 1 || allowed2[0] != "patty-code-standard" {
		t.Fatalf("network rule changed the model allow-list: %v", allowed2)
	}
	if !epoch2RequiresDomainJSON(epoch2.DomainPoliciesJSON, "network") {
		t.Fatalf("network domain config not in epoch: %s", epoch2.DomainPoliciesJSON)
	}
}

func epoch2RequiresDomainJSON(raw, domain string) bool {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return false
	}
	_, ok := m[domain]
	return ok
}

func TestEffectivePolicyIntersectsModelLayers(t *testing.T) {
	srv, db := policyTestServer(t)
	org := models.Organization{Name: "o", Slug: "o2", Status: "active"}
	db.Create(&org)
	proj := models.Project{Name: "p", Slug: "p", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)

	r1 := createRule(t, srv, org.ID, "models", "org-wide", "org", "", `{"allowed_models":["patty-code-standard","patty-code-fast"]}`)
	approveRule(t, srv, org.ID, r1["id"].(string))
	r2 := createRule(t, srv, org.ID, "models", "proj-narrow", "project", proj.ID, `{"allowed_models":["patty-code-standard"]}`)
	approveRule(t, srv, org.ID, r2["id"].(string))

	rec := doJSON(t, srv, "GET", "/api/policy/effective?project_id="+proj.ID, "", org.ID)
	var effective map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &effective)
	allowed := effective["allowed_models"].([]interface{})
	if len(allowed) != 1 || allowed[0] != "patty-code-standard" {
		t.Fatalf("effective policy must intersect layers (only strengthen): %v", allowed)
	}
}

func TestEpochDiffReportsModelChanges(t *testing.T) {
	srv, db := policyTestServer(t)
	org := models.Organization{Name: "o", Slug: "o3", Status: "active"}
	db.Create(&org)
	r1 := createRule(t, srv, org.ID, "models", "first", "org", "", `{"allowed_models":["patty-code-standard","patty-code-pro"]}`)
	approveRule(t, srv, org.ID, r1["id"].(string))
	var e1 models.PolicyEpoch
	db.Where("status = ?", "active").First(&e1)

	// Lower (later) layer can only strengthen: narrowing the list
	// removes patty-code-pro.
	r2 := createRule(t, srv, org.ID, "models", "second", "org", "", `{"allowed_models":["patty-code-standard"]}`)
	approveRule(t, srv, org.ID, r2["id"].(string))
	var e2 models.PolicyEpoch
	db.Where("status = ?", "active").First(&e2)

	rec := doJSON(t, srv, "GET", "/api/policy/epochs/"+e2.EpochID+"/diff?against="+e1.EpochID, "", org.ID)
	var diff map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &diff)
	domains := diff["domains"].(map[string]interface{})
	modelsDiff := domains["allowed_models"].(map[string]interface{})
	removed := modelsDiff["removed"].([]interface{})
	if len(removed) != 1 || removed[0] != "patty-code-pro" {
		t.Fatalf("diff should report patty-code-pro removed: %v", removed)
	}
}

func TestAckGateBlocksSessionsUntilAcked(t *testing.T) {
	srv, db := policyTestServer(t)
	org := models.Organization{Name: "o", Slug: "o4", Status: "active"}
	db.Create(&org)
	user := models.User{Email: "u@corp.kr", Name: "u", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	role := models.Role{OrganizationID: org.ID, Name: "session-user", Permissions: `["session:open","inference:use"]`}
	db.Create(&role)
	db.Create(&models.UserRole{OrganizationID: org.ID, UserID: user.ID, RoleID: role.ID, Scope: "org"})

	epoch, err := srv.policy.CreatePolicyEpochFull(policy.EpochRequest{OrganizationID: org.ID, AllowedModels: []string{"patty-code-standard"}})
	if err != nil {
		t.Fatal(err)
	}
	adminClaims := &identity.Claims{Email: user.Email, OrganizationID: org.ID, Role: "admin"}
	adminClaims.Subject = user.ID
	rec := doUserJSONWithClaims(t, srv, "POST", "/api/policy/epochs/"+epoch.EpochID+"/require-ack", "", adminClaims)
	if rec.Code != http.StatusOK {
		t.Fatalf("require-ack failed: %d", rec.Code)
	}

	// Session blocked before ack
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{"user_id":"`+user.ID+`"}`, adminClaims)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "acknowledgement") {
		t.Fatalf("expected 403 without ack, got %d: %s", rec.Code, rec.Body.String())
	}
	// Ack → session opens
	ackClaims := &identity.Claims{Email: user.Email, OrganizationID: org.ID, Role: "member"}
	ackClaims.Subject = user.ID
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/policy/epochs/"+epoch.EpochID+"/ack", `{"user_id":"`+user.ID+`"}`,
		ackClaims)
	if rec.Code != http.StatusOK {
		t.Fatalf("ack failed: %d", rec.Code)
	}
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{"user_id":"`+user.ID+`"}`, adminClaims)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 after ack, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPolicyPackCreateAssignAndExport(t *testing.T) {
	srv, db := policyTestServer(t)
	org := models.Organization{Name: "o", Slug: "o5", Status: "active"}
	db.Create(&org)
	proj := models.Project{Name: "p", Slug: "p2", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)

	r1 := createRule(t, srv, org.ID, "tools", "strict", "org", "", `{"require_approval_for":["shell.execute"]}`)
	approveRule(t, srv, org.ID, r1["id"].(string))

	rec := doJSON(t, srv, "POST", "/api/policy/packs", `{"name":"base-pack","version":"3","profile":"enterprise"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("pack create failed: %d %s", rec.Code, rec.Body.String())
	}
	var pack models.PolicyPack
	json.Unmarshal(rec.Body.Bytes(), &pack)
	if pack.Version != "3" || pack.Digest == "" {
		t.Fatalf("pack fields wrong: %+v", pack)
	}

	rec = doJSON(t, srv, "POST", "/api/policy/packs/"+pack.ID+"/assign", `{"scope":"project","scope_id":"`+proj.ID+`"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign failed: %d", rec.Code)
	}
	db.First(&proj, "id = ?", proj.ID)
	if proj.PolicyPackID != pack.ID {
		t.Fatalf("project pack binding not set: %s", proj.PolicyPackID)
	}

	// Export → import round trip.
	rec = doJSON(t, srv, "GET", "/api/policy/packs/"+pack.ID+"/export", "", org.ID)
	var doc map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &doc)
	docBytes, _ := json.Marshal(doc)
	rec = doJSON(t, srv, "POST", "/api/policy/packs/import", string(docBytes), org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import failed: %d %s", rec.Code, rec.Body.String())
	}
	var count int64
	db.Model(&models.PolicyPack{}).Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 packs after import, got %d", count)
	}
}

func TestPolicyPackAssignmentCannotTargetAnotherOrganizationProject(t *testing.T) {
	srv, db := policyTestServer(t)
	org := models.Organization{Name: "o", Slug: "pack-owner", Status: "active"}
	other := models.Organization{Name: "other", Slug: "pack-target", Status: "active"}
	db.Create(&org)
	db.Create(&other)
	project := models.Project{Name: "foreign", Slug: "foreign-pack-target", Status: "active"}
	project.OrganizationID = other.ID
	db.Create(&project)
	pack := models.PolicyPack{OrganizationID: org.ID, Name: "owner-pack", Version: "1", Status: "active"}
	db.Create(&pack)

	rec := doJSON(t, srv, http.MethodPost, "/api/policy/packs/"+pack.ID+"/assign",
		`{"scope":"project","scope_id":"`+project.ID+`"}`, org.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-tenant assignment status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := db.First(&project, "id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if project.PolicyPackID != "" {
		t.Fatalf("cross-tenant assignment changed project policy pack to %q", project.PolicyPackID)
	}
}

func TestPolicyExceptionMarketplaceFlow(t *testing.T) {
	srv, db := policyTestServer(t)
	org := models.Organization{Name: "o", Slug: "o6", Status: "active"}
	db.Create(&org)
	rec := doJSON(t, srv, "POST", "/api/policy/exceptions", `{"scope":"project","scope_id":"p1","scopeName":"결제 서비스","reason":"레거시 테스트 필요","rule_ids":["r1"],"requested_by":"admin","justification_ko":"레거시 결제 모듈 임시 허용","expires_at":"2027-01-01T00:00:00Z"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("exception create failed: %d %s", rec.Code, rec.Body.String())
	}
	var ex models.PolicyException
	json.Unmarshal(rec.Body.Bytes(), &ex)
	if ex.Status != "pending" {
		t.Fatalf("exception should start pending: %s", ex.Status)
	}
	rec = doJSON(t, srv, "POST", "/api/policy/exceptions/"+ex.ID+"/decide", `{"approve":true,"decided_by":"sec-admin","reason":"ok"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("decide failed: %d", rec.Code)
	}
	db.First(&ex, "id = ?", ex.ID)
	if ex.Status != "approved" {
		t.Fatalf("expected approved, got %s", ex.Status)
	}
}

func TestTemplateSeedAndEdit(t *testing.T) {
	srv, db := policyTestServer(t)
	org := models.Organization{Name: "o", Slug: "o7", Status: "active"}
	db.Create(&org)
	rec := doJSON(t, srv, "GET", "/api/policy/templates", "", org.ID)
	var templates []models.PolicyTemplate
	json.Unmarshal(rec.Body.Bytes(), &templates)
	if len(templates) < 6 {
		t.Fatalf("expected seeded templates, got %d", len(templates))
	}
	rec = doJSON(t, srv, "POST", "/api/policy/templates", `{"template_id":"restrict-models","domain":"models","name":"모델 제한 v2","config":{"allowed_models":["patty-code-standard"]},"version":"2"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("template edit failed: %d", rec.Code)
	}
	var tpl models.PolicyTemplate
	db.Where("organization_id = ? AND template_id = ?", org.ID, "restrict-models").First(&tpl)
	if tpl.Version != "2" || tpl.Name != "모델 제한 v2" {
		t.Fatalf("template edit not persisted: %+v", tpl)
	}
}
