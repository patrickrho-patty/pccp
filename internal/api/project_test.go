package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func projectTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/p.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Project{}, &models.ProjectMember{},
		&models.Repository{}, &models.Session{}, &models.ChangeRequest{},
		&models.AuditEvent{}, &models.ServiceSigningKey{}, &models.PolicyEpoch{},
		&models.PolicyPack{}, &models.UsageRecord{}, &models.ChangeSet{}, &models.CapabilityLease{},
		&models.CatalogModel{}, &models.ModelPackage{}, &models.Harness{}, &models.Device{},
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

func TestProjectMemberLifecycle(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgp", Status: "active"}
	db.Create(&org)
	user := models.User{Email: "m@corp.kr", Name: "member", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	proj := models.Project{Name: "p", NameKo: "p", Slug: "p", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)

	rec := doUserJSONAs(t, srv, "POST", "/api/projects/"+proj.ID+"/members", `{"user_id":"`+user.ID+`","role":"admin"}`, org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, "GET", "/api/projects/"+proj.ID+"/members", "", org.ID)
	var members []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &members)
	if len(members) != 1 || members[0]["role"] != "admin" || members[0]["user"] == nil {
		t.Fatalf("unexpected roster: %v", members)
	}
	rec = doUserJSONAs(t, srv, "DELETE", "/api/projects/"+proj.ID+"/members/"+user.ID, "", org.ID, "admin", "operator@corp.kr")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove member failed: %d", rec.Code)
	}
	var count int64
	db.Model(&models.ProjectMember{}).Count(&count)
	if count != 0 {
		t.Fatalf("member not removed: %d", count)
	}
}

func TestProjectMembershipIsTenantScopedAdminOnlyAndRejectsOffboardedUsers(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "org-member-scope", Status: "active"}
	other := models.Organization{Name: "other", Slug: "other-member-scope", Status: "active"}
	db.Create(&org)
	db.Create(&other)
	project := models.Project{Name: "p", Slug: "member-scope", Status: "active"}
	project.OrganizationID = org.ID
	db.Create(&project)
	offboarded := models.User{Email: "former@corp.kr", Name: "Former", Status: models.UserStatusOffboarded}
	offboarded.OrganizationID = org.ID
	db.Create(&offboarded)
	foreign := models.User{Email: "foreign@corp.kr", Name: "Foreign", Status: models.UserStatusActive}
	foreign.OrganizationID = other.ID
	db.Create(&foreign)

	for name, tc := range map[string]struct {
		userID, orgID, role string
		want                int
	}{
		"non-admin":       {offboarded.ID, org.ID, "member", http.StatusForbidden},
		"terminal":        {offboarded.ID, org.ID, "admin", http.StatusConflict},
		"foreign-user":    {foreign.ID, org.ID, "admin", http.StatusNotFound},
		"foreign-project": {offboarded.ID, other.ID, "admin", http.StatusNotFound},
	} {
		rec := doUserJSONAs(t, srv, "POST", "/api/projects/"+project.ID+"/members", `{"user_id":"`+tc.userID+`"}`, tc.orgID, tc.role, "operator@corp.kr")
		if rec.Code != tc.want {
			t.Errorf("%s got %d, want %d (%s)", name, rec.Code, tc.want, rec.Body.String())
		}
	}
	var count int64
	db.Model(&models.ProjectMember{}).Count(&count)
	if count != 0 {
		t.Fatalf("rejected membership mutations persisted %d rows", count)
	}
}

func TestArchivedProjectBlocksNewSessionsAndRestores(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "orga", Status: "active"}
	db.Create(&org)
	user := models.User{Email: "a@corp.kr", Name: "a", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	proj := models.Project{Name: "p2", Slug: "p2", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)
	db.Create(&models.ProjectMember{OrganizationID: org.ID, ProjectID: proj.ID, UserID: user.ID, Role: "member"})
	for _, status := range []string{"active", "pending", "paused"} {
		sess := models.Session{ProjectID: proj.ID, SessionID: "archive-" + status, HarnessID: "harness-" + status, UserID: user.ID, Status: status}
		sess.OrganizationID = org.ID
		db.Create(&sess)
	}
	role := models.Role{OrganizationID: org.ID, Name: "project-session-user", Permissions: `["session:open","inference:use"]`}
	db.Create(&role)
	db.Create(&models.UserRole{OrganizationID: org.ID, UserID: user.ID, RoleID: role.ID, Scope: "project", ScopeID: proj.ID})
	// Governed open requires an active policy epoch (web/02 A1).
	if _, err := srv.policy.CreatePolicyEpochFull(policy.EpochRequest{OrganizationID: org.ID, AllowedModels: []string{"patty-code-standard"}}); err != nil {
		t.Fatalf("seed epoch: %v", err)
	}
	assertActiveEpoch := func(stage string) {
		t.Helper()
		var count int64
		if err := db.Model(&models.PolicyEpoch{}).Where("organization_id = ? AND status = ?", org.ID, "active").Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%s: active epoch count=%d err=%v", stage, count, err)
		}
	}
	assertActiveEpoch("before archive")
	adminClaims := &identity.Claims{Email: user.Email, OrganizationID: org.ID, Role: "admin"}
	adminClaims.Subject = user.ID

	// Archive
	rec := doUserJSONWithClaims(t, srv, "DELETE", "/api/projects/"+proj.ID, "", adminClaims)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "archived" || resp["impact"] == nil {
		t.Fatalf("archive should report impact: %v", resp)
	}
	var pausedCount int64
	db.Model(&models.Session{}).Where("organization_id = ? AND project_id = ? AND status = ?", org.ID, proj.ID, "paused").Count(&pausedCount)
	if pausedCount != 3 {
		t.Fatalf("archive did not freeze every nonterminal project session: paused=%d", pausedCount)
	}

	// New session rejected
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{"project_id":"`+proj.ID+`","user_id":"`+user.ID+`"}`, adminClaims)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for archived project, got %d: %s", rec.Code, rec.Body.String())
	}

	// Restore → session accepted
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/projects/"+proj.ID+"/restore", "", adminClaims)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore failed: %d", rec.Code)
	}
	assertActiveEpoch("after restore")
	rec = doUserJSONWithClaims(t, srv, "POST", "/api/sessions", `{"project_id":"`+proj.ID+`","user_id":"`+user.ID+`"}`, adminClaims)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 after restore, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProjectPolicyPackBindingEnforcesTenantAndAdminScope(t *testing.T) {
	srv, db := projectTestServer(t)
	orgA := models.Organization{Name: "A", Slug: "pack-binding-a", Status: "active"}
	orgB := models.Organization{Name: "B", Slug: "pack-binding-b", Status: "active"}
	db.Create(&orgA)
	db.Create(&orgB)
	projectA := models.Project{Name: "A", Slug: "project-a", Status: "active"}
	projectA.OrganizationID = orgA.ID
	projectB := models.Project{Name: "B", Slug: "project-b", Status: "active"}
	projectB.OrganizationID = orgB.ID
	packA := models.PolicyPack{OrganizationID: orgA.ID, Name: "pack-a"}
	packB := models.PolicyPack{OrganizationID: orgB.ID, Name: "pack-b"}
	db.Create(&projectA)
	db.Create(&projectB)
	db.Create(&packA)
	db.Create(&packB)

	viewer := doUserJSONAs(t, srv, http.MethodPost, "/api/projects/"+projectA.ID+"/policy-pack", `{"policy_pack_id":"`+packA.ID+`"}`, orgA.ID, "viewer", "viewer@a.kr")
	if viewer.Code != http.StatusForbidden {
		t.Fatalf("viewer bind = %d, want 403", viewer.Code)
	}
	foreignPack := doUserJSONAs(t, srv, http.MethodPost, "/api/projects/"+projectA.ID+"/policy-pack", `{"policy_pack_id":"`+packB.ID+`"}`, orgA.ID, "admin", "admin@a.kr")
	if foreignPack.Code != http.StatusNotFound {
		t.Fatalf("foreign pack bind = %d, want 404: %s", foreignPack.Code, foreignPack.Body.String())
	}
	foreignProject := doUserJSONAs(t, srv, http.MethodPost, "/api/projects/"+projectB.ID+"/policy-pack", `{"policy_pack_id":"`+packA.ID+`"}`, orgA.ID, "admin", "admin@a.kr")
	if foreignProject.Code != http.StatusNotFound {
		t.Fatalf("foreign project bind = %d, want 404: %s", foreignProject.Code, foreignProject.Body.String())
	}
	allowed := doUserJSONAs(t, srv, http.MethodPost, "/api/projects/"+projectA.ID+"/policy-pack", `{"policy_pack_id":"`+packA.ID+`"}`, orgA.ID, "admin", "admin@a.kr")
	if allowed.Code != http.StatusOK {
		t.Fatalf("own pack bind = %d: %s", allowed.Code, allowed.Body.String())
	}
	var storedA, storedB models.Project
	db.First(&storedA, "id = ?", projectA.ID)
	db.First(&storedB, "id = ?", projectB.ID)
	if storedA.PolicyPackID != packA.ID || storedB.PolicyPackID != "" {
		t.Fatalf("binding results own=%q foreign=%q", storedA.PolicyPackID, storedB.PolicyPackID)
	}
}

func TestProjectArchiveRestoreAndImpactAreTenantScoped(t *testing.T) {
	srv, db := projectTestServer(t)
	orgA := models.Organization{Name: "Org A", Slug: "archive-a", Status: "active"}
	orgB := models.Organization{Name: "Org B", Slug: "archive-b", Status: "active"}
	db.Create(&orgA)
	db.Create(&orgB)
	project := models.Project{Name: "Victim", Slug: "victim", Status: "active"}
	project.OrganizationID = orgB.ID
	db.Create(&project)
	repository := models.Repository{Name: "repo", ProjectID: project.ID, Status: "active"}
	repository.OrganizationID = orgB.ID
	db.Create(&repository)
	session := models.Session{ProjectID: project.ID, SessionID: "ses-tenant-project", HarnessID: "hrn-b", UserID: "user-b", Status: "active"}
	session.OrganizationID = orgB.ID
	db.Create(&session)
	db.Create(&models.ProjectMember{OrganizationID: orgB.ID, ProjectID: project.ID, UserID: "user-b", Role: "member"})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/projects/" + project.ID + "/archive-impact"},
		{http.MethodDelete, "/api/projects/" + project.ID},
		{http.MethodPost, "/api/projects/" + project.ID + "/restore"},
	} {
		rec := doUserJSONAs(t, srv, tc.method, tc.path, "", orgA.ID, "admin", "operator@a.kr")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s cross-tenant status=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	var persisted models.Project
	if err := db.First(&persisted, "id = ?", project.ID).Error; err != nil || persisted.Status != "active" {
		t.Fatalf("cross-tenant mutation changed project: status=%s err=%v", persisted.Status, err)
	}
	var foreignAudits int64
	db.Model(&models.AuditEvent{}).Where("organization_id = ? AND resource_id = ?", orgA.ID, project.ID).Count(&foreignAudits)
	if foreignAudits != 0 {
		t.Fatalf("cross-tenant attempts wrote %d audit rows", foreignAudits)
	}

	archive := doUserJSONAs(t, srv, http.MethodDelete, "/api/projects/"+project.ID, "", orgB.ID, "admin", "operator@b.kr")
	if archive.Code != http.StatusOK {
		t.Fatalf("owner archive failed: %d %s", archive.Code, archive.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(archive.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	impact, _ := body["impact"].(map[string]interface{})
	if impact["in_progress_sessions"] != float64(1) || impact["repositories"] != float64(1) || impact["members"] != float64(1) {
		t.Fatalf("archive impact does not resolve exact tenant graph: %v", impact)
	}
	foreignRestore := doUserJSONAs(t, srv, http.MethodPost, "/api/projects/"+project.ID+"/restore", "", orgA.ID, "admin", "operator@a.kr")
	if foreignRestore.Code != http.StatusNotFound {
		t.Fatalf("foreign restore status=%d body=%s", foreignRestore.Code, foreignRestore.Body.String())
	}
	if err := db.First(&persisted, "id = ?", project.ID).Error; err != nil || persisted.Status != "archived" {
		t.Fatalf("foreign restore changed archived project: status=%s err=%v", persisted.Status, err)
	}
}

func TestHighRiskChangeSetQueuesChangeRequest(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgq", Status: "active"}
	db.Create(&org)
	proj := models.Project{Name: "p3", Slug: "p3", Status: "active"}
	proj.OrganizationID = org.ID
	db.Create(&proj)
	repo := models.Repository{Name: "r", ProjectID: proj.ID, Status: "active"}
	repo.OrganizationID = org.ID
	db.Create(&repo)

	// An auth-touching change must require approval and queue.
	body := `{"session_id":"ses-1","repository_id":"` + repo.ID + `","branch":"main","files_changed":["src/auth/login.go","src/crypto/sign.go","config/prod.yaml"],"lines_added":120,"lines_removed":10,"attribution_state":"AI_GENERATED","confidence":0.95}`
	rec := doJSON(t, srv, "POST", "/api/provenance/changeset", body, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("changeset failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["queued"] != true || resp["change_request"] == nil {
		t.Fatalf("high-risk change should queue a change request: %v", resp)
	}

	// The queue item must be pending and bound to the project.
	rec = doJSON(t, srv, "GET", "/api/projects/"+proj.ID+"/change-requests", "", org.ID)
	var crs []models.ChangeRequest
	json.Unmarshal(rec.Body.Bytes(), &crs)
	if len(crs) != 1 || crs[0].Status != "pending" || crs[0].RiskLevel == "low" {
		t.Fatalf("unexpected queue: %+v", crs)
	}

	// Decide it.
	rec = doJSON(t, srv, "POST", "/api/change-requests/"+crs[0].ID+"/decide", `{"approve":true,"reason":"reviewed"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("decide failed: %d", rec.Code)
	}
	db.First(&crs[0], "id = ?", crs[0].ID)
	if crs[0].Status != "approved" {
		t.Fatalf("expected approved, got %s", crs[0].Status)
	}
}

// --- PAT-1491: allowed-model classes as a typed view model ---

func TestProjectAllowedModelClassesParsedInView(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "orgam", Status: "active"}
	db.Create(&org)

	cases := []struct {
		name   string
		stored string
		want   []string
	}{
		{"one", `["code"]`, []string{"code"}},
		{"many", `["code","reasoning"]`, []string{"code", "reasoning"}},
		{"empty", `[]`, []string{}},
		{"empty-legacy", ``, []string{}},
		{"duplicates deduped in order", `["code","reasoning","code"]`, []string{"code", "reasoning"}},
		{"special characters stay separate items", `["클래스/특수:값","a b&c=d"]`, []string{"클래스/특수:값", "a b&c=d"}},
		{"malformed stored JSON yields empty, never raw", `["code",`, []string{}},
	}
	for _, tc := range cases {
		proj := models.Project{Name: "p-" + tc.name, Slug: "p-" + tc.name}
		proj.OrganizationID = org.ID
		proj.AllowedModelClasses = tc.stored
		if err := db.Create(&proj).Error; err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"/api/projects/" + proj.ID, "/api/projects/" + proj.ID + "/detail", "/api/projects?page=1&size=50"} {
			rec := doJSON(t, srv, "GET", path, "", org.ID)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %s → %d", tc.name, path, rec.Code)
			}
			var body map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s: %s: %v", tc.name, path, err)
			}
			row, ok := body["project"].(map[string]interface{})
			if !ok {
				row = body // detail nests under "project"; get/list return the row
			}
			if path == "/api/projects?page=1&size=50" {
				rows, ok := body["data"].([]interface{})
				if !ok || len(rows) == 0 {
					t.Fatalf("%s: list returned no data", tc.name)
				}
				found := false
				for _, r := range rows {
					rm := r.(map[string]interface{})
					if rm["id"] == proj.ID {
						row = rm
						found = true
					}
				}
				if !found {
					t.Fatalf("%s: project missing from list", tc.name)
				}
			}
			got, ok := row["allowed_model_classes"].([]interface{})
			if !ok {
				t.Fatalf("%s: %s: allowed_model_classes is %T, want array", tc.name, path, row["allowed_model_classes"])
			}
			if len(got) != len(tc.want) {
				t.Fatalf("%s: %s: got %v, want %v", tc.name, path, got, tc.want)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Fatalf("%s: %s: got %v, want %v", tc.name, path, got, tc.want)
				}
			}
		}
	}
}

func TestProjectAllowedModelsResolveAgainstScopedCanonicalCatalog(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "catalog-org", Status: "active"}
	otherOrg := models.Organization{Name: "other", Slug: "catalog-other", Status: "active"}
	db.Create(&org)
	db.Create(&otherOrg)

	packages := []models.ModelPackage{
		{PackageID: "pkg-org", ModelID: "model-org", Name: "Org Package", Family: "code", State: "published"},
		{PackageID: "pkg-global", ModelID: "model-global", Name: "Global Package", Family: "code", State: "published"},
		{PackageID: "pkg-retired", ModelID: "model-retired", Name: "Retired Package", Family: "code", State: "deprecated"},
	}
	for i := range packages {
		if err := db.Create(&packages[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	catalogModels := []models.CatalogModel{
		{CatalogModelID: "catalog-org", OrganizationID: org.ID, DisplayName: "Org Model", DisplayNameKo: "조직 모델", Family: "code", EntitlementClass: "enterprise-code", ProductionPackageID: "pkg-org", Availability: "available", Status: "active"},
		{CatalogModelID: "catalog-global", DisplayName: "Global Model", DisplayNameKo: "공용 모델", Family: "code", EntitlementClass: "enterprise-code", ProductionPackageID: "pkg-global", Availability: "available", Status: "active"},
		{CatalogModelID: "catalog-retired", OrganizationID: org.ID, DisplayName: "Retired Model", DisplayNameKo: "사용 중단 모델", Family: "legacy", EntitlementClass: "legacy", ProductionPackageID: "pkg-retired", Availability: "withdrawn", Status: "deprecated"},
		{CatalogModelID: "catalog-other", OrganizationID: otherOrg.ID, DisplayName: "Other Tenant Secret", DisplayNameKo: "타 조직 비공개 모델", Family: "private", EntitlementClass: "private", Availability: "available", Status: "active"},
	}
	for i := range catalogModels {
		if err := db.Create(&catalogModels[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	proj := models.Project{Name: "catalog project", Slug: "catalog-project", Status: "active", AllowedModelClasses: `["catalog-org","code","catalog-retired","catalog-other","missing","catalog-org"]`}
	proj.OrganizationID = org.ID
	db.Create(&proj)

	rec := doJSON(t, srv, "GET", "/api/projects/"+proj.ID, "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get project: %d %s", rec.Code, rec.Body.String())
	}
	var row struct {
		PolicyState string `json:"allowed_model_policy_state"`
		Items       []struct {
			ID             string `json:"id"`
			Label          string `json:"label"`
			State          string `json:"state"`
			EntityKind     string `json:"entity_kind"`
			CatalogModelID string `json:"catalog_model_id"`
			PackageID      string `json:"package_id"`
		} `json:"allowed_model_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row.PolicyState != "configured" || len(row.Items) != 5 {
		t.Fatalf("unexpected allowed-model projection: %+v", row)
	}
	wantStates := []string{"single", "many", "retired", "restricted", "unknown"}
	for i, want := range wantStates {
		if row.Items[i].State != want {
			t.Fatalf("item %d state = %q, want %q: %+v", i, row.Items[i].State, want, row.Items[i])
		}
	}
	if row.Items[0].Label != "조직 모델" || row.Items[0].CatalogModelID != "catalog-org" || row.Items[0].PackageID != "pkg-org" {
		t.Fatalf("canonical model did not resolve to its package: %+v", row.Items[0])
	}
	if row.Items[2].Label != "사용 중단 모델" || row.Items[2].PackageID != "pkg-retired" {
		t.Fatalf("retired model lost canonical metadata: %+v", row.Items[2])
	}
	if row.Items[3].Label == "타 조직 비공개 모델" || row.Items[3].EntityKind != "model" || row.Items[3].CatalogModelID != "" || row.Items[3].PackageID != "" {
		t.Fatalf("restricted model leaked another tenant's metadata: %+v", row.Items[3])
	}
}

func TestProjectAllowedModelPolicyDistinguishesMalformedFromUnrestricted(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "policy-state-org", Status: "active"}
	db.Create(&org)
	for _, tc := range []struct {
		name   string
		stored string
		state  string
	}{
		{name: "unrestricted", stored: `[]`, state: "unrestricted"},
		{name: "malformed", stored: `["code",`, state: "invalid"},
	} {
		proj := models.Project{Name: tc.name, Slug: tc.name, Status: "active", AllowedModelClasses: tc.stored}
		proj.OrganizationID = org.ID
		db.Create(&proj)
		rec := doJSON(t, srv, "GET", "/api/projects/"+proj.ID, "", org.ID)
		var row map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &row)
		if row["allowed_model_policy_state"] != tc.state {
			t.Fatalf("%s state = %v, want %s", tc.name, row["allowed_model_policy_state"], tc.state)
		}
	}
}

func TestAllowedModelResolverRequiresEffectivePublishedMapping(t *testing.T) {
	resolver := &allowedModelResolver{
		packages: map[string]models.ModelPackage{
			"published":  {PackageID: "published", State: "published"},
			"draft":      {PackageID: "draft", State: "draft"},
			"unexpected": {PackageID: "unexpected", State: "uploading"},
		},
	}
	for _, tc := range []struct {
		name  string
		model models.CatalogModel
		state string
	}{
		{
			name:  "maintenance catalog is unavailable",
			model: models.CatalogModel{CatalogModelID: "maintenance", DisplayName: "Maintenance", Status: "active", Availability: "maintenance", ProductionPackageID: "published"},
			state: "unavailable",
		},
		{
			name:  "draft package is unavailable",
			model: models.CatalogModel{CatalogModelID: "draft", DisplayName: "Draft", Status: "active", Availability: "available", ProductionPackageID: "draft"},
			state: "unavailable",
		},
		{
			name:  "unexpected package lifecycle is unavailable",
			model: models.CatalogModel{CatalogModelID: "unexpected", DisplayName: "Unexpected", Status: "active", Availability: "available", ProductionPackageID: "unexpected"},
			state: "unavailable",
		},
		{
			name:  "published effective mapping is single",
			model: models.CatalogModel{CatalogModelID: "published", DisplayName: "Published", Status: "active", Availability: "degraded", ProductionPackageID: "published"},
			state: "single",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolver.catalogItem(tc.model.CatalogModelID, tc.model).State; got != tc.state {
				t.Fatalf("state = %q, want %q", got, tc.state)
			}
		})
	}

	missing := resolver.catalogItem("missing", models.CatalogModel{
		CatalogModelID: "missing", DisplayName: "Missing", Status: "active",
		Availability: "available", ProductionPackageID: "does-not-exist",
	})
	if missing.State != "unavailable" || missing.PackageID != "" || missing.CatalogModelID != "missing" {
		t.Fatalf("missing package must use catalog fallback without a dead package link: %+v", missing)
	}
}

func TestAllowedModelResolverDeduplicatesSameFamilyAndEntitlementClass(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "dedupe-catalog-org", Status: "active"}
	db.Create(&org)
	db.Create(&models.ModelPackage{PackageID: "pkg-dual", ModelID: "model-dual", Name: "Dual", State: "published"})
	db.Create(&models.CatalogModel{
		OrganizationID: org.ID, CatalogModelID: "catalog-dual", DisplayName: "Dual",
		Family: "dual", EntitlementClass: "dual", ProductionPackageID: "pkg-dual",
		Availability: "available", Status: "active",
	})
	resolver, err := srv.newAllowedModelResolver(org.ID, []string{"dual"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolver.resolve("dual"); got.State != "single" || got.CatalogModelID != "catalog-dual" {
		t.Fatalf("same model matched by family and entitlement must resolve once: %+v", got)
	}
}

func TestAllowedModelResolverRestrictsForeignFamilyAndEntitlementClass(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "restricted-class-org", Status: "active"}
	otherOrg := models.Organization{Name: "other", Slug: "restricted-class-other", Status: "active"}
	db.Create(&org)
	db.Create(&otherOrg)
	db.Create(&models.CatalogModel{
		OrganizationID: otherOrg.ID, CatalogModelID: "foreign-catalog", DisplayNameKo: "타 조직 비공개 모델",
		Family: "foreign-family", EntitlementClass: "foreign-class", EntitlementLabelKo: "타 조직 비공개 클래스",
		Availability: "available", Status: "active",
	})

	resolver, err := srv.newAllowedModelResolver(org.ID, []string{"foreign-family", "foreign-class"})
	if err != nil {
		t.Fatal(err)
	}
	for _, identifier := range []string{"foreign-family", "foreign-class"} {
		item := resolver.resolve(identifier)
		if item.State != "restricted" || item.EntityKind != "class" || item.Label != identifier {
			t.Fatalf("foreign-only %q leaked or was not restricted: %+v", identifier, item)
		}
	}

	rec := doJSON(t, srv, "POST", "/api/projects", `{"name":"blocked","slug":"blocked","allowed_models":["foreign-family"]}`, org.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create with foreign family = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	project := models.Project{Name: "editable", Slug: "editable", Status: "active", AllowedModelClasses: `[]`}
	project.OrganizationID = org.ID
	db.Create(&project)
	rec = doJSON(t, srv, "PUT", "/api/projects/"+project.ID, `{"allowed_models":["foreign-class"]}`, org.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("update with foreign entitlement class = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestAllowedModelResolverUsesRegistryKoreanClassLabel(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "canonical-class-label", Status: "active"}
	db.Create(&org)
	for _, suffix := range []string{"a", "b"} {
		pkg := models.ModelPackage{PackageID: "pkg-label-" + suffix, ModelID: "model-label-" + suffix, Name: "Label " + suffix, State: "published"}
		db.Create(&pkg)
		db.Create(&models.CatalogModel{
			OrganizationID: org.ID, CatalogModelID: "catalog-label-" + suffix, DisplayName: "Label " + suffix,
			Family: "custom-family", EntitlementClass: "custom-entitlement", EntitlementLabelKo: "맞춤형 추론",
			ProductionPackageID: pkg.PackageID, Availability: "available", Status: "active",
		})
	}
	resolver, err := srv.newAllowedModelResolver(org.ID, []string{"custom-entitlement"})
	if err != nil {
		t.Fatal(err)
	}
	item := resolver.resolve("custom-entitlement")
	if item.State != "many" || item.EntityKind != "class" || item.Label != "맞춤형 추론" {
		t.Fatalf("class projection did not use registry Korean label: %+v", item)
	}
}

func TestProjectMutationResponsesUseTypedAllowedModelProjection(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "mutation-projection", Status: "active"}
	db.Create(&org)
	db.Create(&models.ModelPackage{PackageID: "pkg-mutation", ModelID: "model-mutation", Name: "Mutation", State: "published"})
	db.Create(&models.CatalogModel{
		OrganizationID: org.ID, CatalogModelID: "catalog-mutation", DisplayNameKo: "변경 모델",
		ProductionPackageID: "pkg-mutation", Availability: "available", Status: "active",
	})

	rec := doJSON(t, srv, "POST", "/api/projects", `{"name":"mutation","slug":"mutation","allowed_models":["catalog-mutation"]}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID      string                 `json:"id"`
		Classes []string               `json:"allowed_model_classes"`
		Items   []allowedModelItemView `json:"allowed_model_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.Classes) != 1 || created.Classes[0] != "catalog-mutation" || len(created.Items) != 1 || created.Items[0].CatalogModelID != "catalog-mutation" {
		t.Fatalf("create projection is not canonical: %+v", created)
	}

	rec = doJSON(t, srv, "PUT", "/api/projects/"+created.ID, `{"allowed_models":["catalog-mutation","future-model"]}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Classes []string               `json:"allowed_model_classes"`
		Items   []allowedModelItemView `json:"allowed_model_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Classes) != 2 || len(updated.Items) != 2 || updated.Items[1].State != "unknown" {
		t.Fatalf("update projection is not canonical: %+v", updated)
	}
}

func TestProjectUpdateCannotBypassArchiveWorkflow(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "status-workflow", Status: "active"}
	db.Create(&org)
	project := models.Project{Name: "protected", Slug: "protected", Status: "active", AllowedModelClasses: `[]`}
	project.OrganizationID = org.ID
	db.Create(&project)

	for _, role := range []string{"viewer", "admin"} {
		rec := doUserJSONAs(t, srv, "PUT", "/api/projects/"+project.ID, `{"status":"archived"}`, org.ID, role, role+"@corp.kr")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("direct status update as %s = %d, want 400: %s", role, rec.Code, rec.Body.String())
		}
	}
	if err := db.First(&project, "id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if project.Status != "active" {
		t.Fatalf("direct update bypassed archive workflow: %q", project.Status)
	}
}

func TestProjectMutationAllowedModelInputSemantics(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "mutation-semantics", Status: "active"}
	db.Create(&org)

	rec := doJSON(t, srv, "POST", "/api/projects", `{"name":"defaulted","slug":"defaulted"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("omitted create = %d: %s", rec.Code, rec.Body.String())
	}
	var defaulted struct {
		ID      string   `json:"id"`
		Classes []string `json:"allowed_model_classes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &defaulted); err != nil {
		t.Fatal(err)
	}
	if len(defaulted.Classes) != 1 || defaulted.Classes[0] != "patty-code-standard" {
		t.Fatalf("omitted create widened policy: %+v", defaulted.Classes)
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "null", body: `{"name":"null","slug":"null","allowed_models":null}`},
		{name: "blank", body: `{"name":"blank","slug":"blank","allowed_models":[" "]}`},
	} {
		t.Run("create rejects "+tc.name, func(t *testing.T) {
			rec := doJSON(t, srv, "POST", "/api/projects", tc.body, org.ID)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s create = %d, want 400: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}

	rec = doJSON(t, srv, "POST", "/api/projects", `{"name":"unrestricted","slug":"unrestricted","allowed_models":[]}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("explicit unrestricted create = %d: %s", rec.Code, rec.Body.String())
	}
	var unrestricted struct {
		PolicyState string `json:"allowed_model_policy_state"`
	}
	json.Unmarshal(rec.Body.Bytes(), &unrestricted)
	if unrestricted.PolicyState != modelPolicyUnrestricted {
		t.Fatalf("explicit empty create state = %q", unrestricted.PolicyState)
	}

	rec = doJSON(t, srv, "PUT", "/api/projects/"+defaulted.ID, `{"name":"renamed"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("omitted update = %d: %s", rec.Code, rec.Body.String())
	}
	for _, body := range []string{`{"allowed_models":null}`, `{"allowed_models":[" "]}`} {
		rec = doJSON(t, srv, "PUT", "/api/projects/"+defaulted.ID, body, org.ID)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid update %s = %d, want 400: %s", body, rec.Code, rec.Body.String())
		}
	}
	var stored models.Project
	db.First(&stored, "id = ?", defaulted.ID)
	if stored.AllowedModelClasses != `["patty-code-standard"]` {
		t.Fatalf("invalid update mutated allowed models: %s", stored.AllowedModelClasses)
	}
	rec = doJSON(t, srv, "PUT", "/api/projects/"+defaulted.ID, `{"allowed_models":[]}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("explicit unrestricted update = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDashboardRecentProjectsUseTypedAllowedModelProjection(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "dashboard-projection", Status: "active"}
	db.Create(&org)
	project := models.Project{Name: "dashboard", Slug: "dashboard", Status: "active", AllowedModelClasses: `["code","reasoning"]`}
	project.OrganizationID = org.ID
	db.Create(&project)

	rec := doJSON(t, srv, "GET", "/api/dashboard", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard = %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		RecentProjects []struct {
			AllowedModelClasses []string               `json:"allowed_model_classes"`
			AllowedModelItems   []allowedModelItemView `json:"allowed_model_items"`
		} `json:"recent_projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("dashboard leaked serialized project policy: %v", err)
	}
	if len(response.RecentProjects) != 1 || len(response.RecentProjects[0].AllowedModelClasses) != 2 || len(response.RecentProjects[0].AllowedModelItems) != 2 {
		t.Fatalf("dashboard project projection is incomplete: %+v", response.RecentProjects)
	}
}

func TestAllowedModelResolverBatchesLargeCollections(t *testing.T) {
	srv, db := projectTestServer(t)
	org := models.Organization{Name: "org", Slug: "large-model-policy", Status: "active"}
	db.Create(&org)
	identifiers := make([]string, 12000)
	for i := range identifiers {
		identifiers[i] = "future-model-" + strconv.Itoa(i)
	}
	stored, err := json.Marshal(identifiers)
	if err != nil {
		t.Fatal(err)
	}
	project := models.Project{Name: "large", Slug: "large", Status: "active", AllowedModelClasses: string(stored)}
	project.OrganizationID = org.ID
	db.Create(&project)

	rec := doJSON(t, srv, "GET", "/api/projects/"+project.ID, "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("large allowed-model policy = %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Classes []string               `json:"allowed_model_classes"`
		Items   []allowedModelItemView `json:"allowed_model_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Classes) != len(identifiers) || len(response.Items) != len(identifiers) {
		t.Fatalf("large projection truncated: classes=%d items=%d", len(response.Classes), len(response.Items))
	}
}

func TestProjectReadWriteRoutesEnforceTenantScope(t *testing.T) {
	srv, db := projectTestServer(t)
	orgA := models.Organization{Name: "A", Slug: "tenant-a", Status: "active"}
	orgB := models.Organization{Name: "B", Slug: "tenant-b", Status: "active"}
	db.Create(&orgA)
	db.Create(&orgB)
	foreign := models.Project{Name: "foreign", Slug: "foreign", Status: "active", AllowedModelClasses: `[]`}
	foreign.OrganizationID = orgB.ID
	db.Create(&foreign)
	foreignPack := models.PolicyPack{Name: "foreign pack", Status: "active"}
	foreignPack.OrganizationID = orgB.ID
	db.Create(&foreignPack)

	for _, path := range []string{"/api/projects/" + foreign.ID, "/api/projects/" + foreign.ID + "/detail"} {
		rec := doJSON(t, srv, "GET", path, "", orgA.ID)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("cross-tenant GET %s = %d, want 404", path, rec.Code)
		}
	}
	rec := doJSON(t, srv, "PUT", "/api/projects/"+foreign.ID, `{"name":"stolen"}`, orgA.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant update = %d, want 404", rec.Code)
	}
	db.First(&foreign, "id = ?", foreign.ID)
	if foreign.Name != "foreign" {
		t.Fatalf("cross-tenant update mutated project: %q", foreign.Name)
	}
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: "GET", path: "/api/projects/" + foreign.ID},
		{method: "GET", path: "/api/projects/" + foreign.ID + "/detail"},
		{method: "PUT", path: "/api/projects/" + foreign.ID, body: `{}`},
	} {
		rec := doJSON(t, srv, request.method, request.path, request.body, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("missing organization %s %s = %d, want 401", request.method, request.path, rec.Code)
		}
	}

	rec = doJSON(t, srv, "POST", "/api/projects", `{"name":"foreign pack create","slug":"foreign-pack-create","policy_pack_id":"`+foreignPack.ID+`"}`, orgA.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("create with foreign policy pack = %d, want 404", rec.Code)
	}

	rec = doJSON(t, srv, "POST", "/api/projects", `{"organization_id":"`+orgB.ID+`","name":"owned by claims","slug":"owned-by-claims","allowed_models":[]}`, orgA.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created models.Project
	if err := db.First(&created, "slug = ?", "owned-by-claims").Error; err != nil {
		t.Fatal(err)
	}
	if created.OrganizationID != orgA.ID {
		t.Fatalf("create trusted body tenant %q, want claims tenant %q", created.OrganizationID, orgA.ID)
	}
	rec = doJSON(t, srv, "PUT", "/api/projects/"+created.ID, `{"policy_pack_id":"`+foreignPack.ID+`"}`, orgA.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update with foreign policy pack = %d, want 404", rec.Code)
	}

	rec = doJSON(t, srv, "PUT", "/api/projects/"+created.ID, `{"allowed_models":[]}`, orgA.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear allowed models = %d: %s", rec.Code, rec.Body.String())
	}
	var cleared map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &cleared)
	if cleared["allowed_model_policy_state"] != "unrestricted" {
		t.Fatalf("empty clear state = %v", cleared["allowed_model_policy_state"])
	}
}
