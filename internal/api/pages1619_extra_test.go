package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func pages1619TestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/p1619.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.AuditEvent{}, &models.LegalHold{},
		&models.UsageRecord{}, &models.ModelPackage{}, &models.InferenceEndpoint{},
		&models.Session{}, &models.ProvenanceSpan{}, &models.ChangeSet{}, &models.Repository{},
		&models.OrgSetting{}, &models.ServiceSigningKey{}, &models.PolicyEpoch{},
		&models.CapabilityLease{}, &models.Harness{}, &models.Project{},
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

func TestAuditServerQueryAndHolds(t *testing.T) {
	srv, db := pages1619TestServer(t)
	org := models.Organization{Name: "o", Slug: "oaud", Status: "active"}
	db.Create(&org)
	for i := 0; i < 5; i++ {
		ev := models.AuditEvent{EventType: "cp.test", ActorType: "admin", Action: "test", ResourceType: "org",
			ResourceID: org.ID, Result: "success"}
		ev.OrganizationID = org.ID
		db.Create(&ev)
	}
	rec := doJSON(t, srv, "GET", "/api/audit?page=1&size=3&type=cp.test", "", org.ID)
	var page map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &page)
	if page["total"].(float64) != 5 || len(page["data"].([]interface{})) != 3 {
		t.Fatalf("audit page wrong: %v", page)
	}
	// Legal hold place/lift.
	rec = doJSON(t, srv, "POST", "/api/audit/holds",
		`{"resource_type":"session","resource_id":"s1","reason":"litigation"}`, org.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("hold failed: %d %s", rec.Code, rec.Body.String())
	}
	var hold models.LegalHold
	json.Unmarshal(rec.Body.Bytes(), &hold)
	rec = doJSON(t, srv, "GET", "/api/audit/holds", "", org.ID)
	var holds []models.LegalHold
	json.Unmarshal(rec.Body.Bytes(), &holds)
	if len(holds) != 1 {
		t.Fatalf("holds wrong: %v", holds)
	}
	rec = doJSON(t, srv, "DELETE", "/api/audit/holds/"+hold.ID, `{"reason":"settled"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("lift failed: %d", rec.Code)
	}
}

func TestAuditEvidenceBundleAndSIEMConfig(t *testing.T) {
	srv, db := pages1619TestServer(t)
	org := models.Organization{Name: "o", Slug: "oev", Status: "active"}
	db.Create(&org)
	var ids []string
	for i := 0; i < 3; i++ {
		ev := models.AuditEvent{EventType: "cp.test", ActorType: "admin", Action: "t", ResourceType: "org", ResourceID: org.ID, Result: "success"}
		ev.OrganizationID = org.ID
		db.Create(&ev)
		ids = append(ids, ev.ID)
	}
	rec := doJSON(t, srv, "POST", "/api/audit/evidence-bundle",
		`{"ids":[`+jsonList(ids)+`]}`, org.ID)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Disposition") == "" {
		t.Fatalf("bundle failed: %d", rec.Code)
	}
	rec = doJSON(t, srv, "PUT", "/api/audit/siem",
		`{"webhook":"http://siem.local/hook","secret":"s3cr3t"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("siem config failed: %d", rec.Code)
	}
	rec = doJSON(t, srv, "GET", "/api/audit/siem", "", org.ID)
	var cfg map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &cfg)
	if cfg["configured"] != true || cfg["secret_set"] != true {
		t.Fatalf("siem config wrong: %v", cfg)
	}
}

func jsonList(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += `"` + id + `"`
	}
	return out
}

func TestModelPublishVerificationAndRecallImpact(t *testing.T) {
	srv, db := pages1619TestServer(t)
	org := models.Organization{Name: "o", Slug: "omp", Status: "active"}
	db.Create(&org)
	// Register via the registry so Signature + ManifestDigest exist.
	pkg := models.ModelPackage{PackageID: "pmp_test_v", ModelID: "m1", Name: "M", Version: "1"}
	if err := srv.registry.RegisterModelPackage(&pkg); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, srv, "POST", "/api/models/"+pkg.PackageID+"/publish", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("verified publish failed: %d %s", rec.Code, rec.Body.String())
	}
	// Tamper: corrupt the manifest digest → publish must refuse.
	db.Model(&models.ModelPackage{}).Where("package_id = ?", pkg.PackageID).Update("manifest_digest", "deadbeef")
	rec = doJSON(t, srv, "POST", "/api/models/"+pkg.PackageID+"/publish", "", org.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tampered publish should be refused: %d", rec.Code)
	}
	// Recall impact.
	rec = doJSON(t, srv, "GET", "/api/models/"+pkg.PackageID+"/recall-impact", "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("impact failed: %d", rec.Code)
	}
	// Ring assign.
	rec = doJSON(t, srv, "PUT", "/api/models/"+pkg.PackageID+"/ring", `{"release_ring":"canary"}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring failed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCodeExplorerAttribution(t *testing.T) {
	srv, db := pages1619TestServer(t)
	org := models.Organization{Name: "o", Slug: "oce", Status: "active"}
	db.Create(&org)
	repo := models.Repository{Name: "r", FullName: "r", Status: "active"}
	repo.OrganizationID = org.ID
	db.Create(&repo)
	sp1 := models.ProvenanceSpan{RepositoryID: repo.ID, FilePath: "a.go", AttributionState: "AI_GENERATED", Confidence: 0.9, StartLine: 1, EndLine: 5}
	sp1.OrganizationID = org.ID
	db.Create(&sp1)
	sp2 := models.ProvenanceSpan{RepositoryID: repo.ID, FilePath: "a.go", AttributionState: "HUMAN_WRITTEN", Confidence: 0.8, StartLine: 6, EndLine: 8}
	sp2.OrganizationID = org.ID
	db.Create(&sp2)

	rec := doJSON(t, srv, "GET", "/api/code-explorer/attribution?repository="+repo.ID, "", org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("attribution failed: %d", rec.Code)
	}
	var attrs []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &attrs)
	if len(attrs) != 1 || attrs[0]["state"] != "AI_THEN_HUMAN_EDITED" {
		t.Fatalf("attribution wrong: %v", attrs)
	}
}
