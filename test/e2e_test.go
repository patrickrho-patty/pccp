package test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"github.com/patrickrho-patty/pccp/internal/provenance"
	"github.com/patrickrho-patty/pccp/internal/registry"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(append(models.AllModels(), &identity.AdminCredentials{})...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestEndToEndProvenance(t *testing.T) {
	db := setupTestDB(t)

	idSvc, err := identity.New(db)
	if err != nil {
		t.Fatal(err)
	}
	regSvc, err := registry.New(db)
	if err != nil {
		t.Fatal(err)
	}
	polSvc, err := policy.New(db)
	if err != nil {
		t.Fatal(err)
	}
	provSvc, err := provenance.New(db, "test-relay")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Create org and user
	org, err := idSvc.CreateOrganization("Test Org", "테스트 조직", "test-org", "enterprise")
	if err != nil {
		t.Fatal(err)
	}
	user, err := idSvc.CreateUser(org.ID, "test@patty.dev", "Test User", "테스트 사용자", "local", "")
	if err != nil {
		t.Fatal(err)
	}
	if user.NameKo != "테스트 사용자" {
		t.Fatalf("expected Korean name, got %s", user.NameKo)
	}

	// 2. Register model package
	pkg := &models.ModelPackage{
		PackageID: "pmp_test_v1",
		ModelID:   "test-model",
		Name:      "Test Model",
		Family:    "coder",
		Version:   "1.0",
		State:     "draft",
	}
	if err := regSvc.RegisterModelPackage(pkg); err != nil {
		t.Fatal(err)
	}
	if err := regSvc.PublishModelPackage(pkg.PackageID); err != nil {
		t.Fatal(err)
	}

	// 3. Enroll endpoint and issue lease
	ep, err := regSvc.EnrollEndpoint(org.ID, "pia-test", "pmp_test_v1", "vllm", "0.6.0",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"spiffe://test/node/pia-test", "none")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := regSvc.IssueEndpointLease(org.ID, ep.EndpointID, 1*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Status != "active" {
		t.Fatalf("expected active lease, got %s", lease.Status)
	}

	// 4. Create policy epoch
	epoch, err := polSvc.CreatePolicyEpoch(org.ID, []string{"pmp_test_v1"}, "immediate")
	if err != nil {
		t.Fatal(err)
	}

	// 5. Issue capability lease
	capLease, err := polSvc.IssueCapabilityLease(policy.IssueLeaseRequest{
		OrganizationID: org.ID,
		SubjectPeerID:  "hrn_test",
		UserID:         user.ID,
		SessionID:      "ses_test",
		PolicyEpochID:  epoch.EpochID,
		AllowedModels:  []string{"pmp_test_v1"},
		ToolClasses:    []string{"read", "write"},
		TokenBudget:    100000,
		Validity:       1 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 6. Validate lease
	validated, err := polSvc.ValidateCapabilityLease(capLease.LeaseID, "hrn_test", "ses_test")
	if err != nil {
		t.Fatal(err)
	}
	if validated.UserID != user.ID {
		t.Fatal("lease user mismatch")
	}

	// 7. Open session and record action
	sess, err := idSvc.OpenSession(org.ID, "hrn_test", user.ID, "", "", "", "",
		"테스트 작업", "test purpose", "pmp_test_v1")
	if err != nil {
		t.Fatal(err)
	}

	action, err := provSvc.RecordAction(provenance.RecordActionRequest{
		OrganizationID: org.ID,
		SessionID:      sess.SessionID,
		ExchangeID:     "exch_test",
		UserID:         user.ID,
		HarnessID:      "hrn_test",
		ModelPackageID: "pmp_test_v1",
		EndpointID:     ep.EndpointID,
		PolicyEpochID:  epoch.EpochID,
		LeaseID:        capLease.LeaseID,
		ActionType:     "ai.inference",
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.EnvelopeDigest == "" {
		t.Fatal("expected non-empty envelope digest")
	}

	// 8. Verify provenance chain
	chain, err := provSvc.GetProvenanceChain(org.ID, sess.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(chain.Actions))
	}

	// 9. Create ChangeSet
	cs, err := provSvc.CreateChangeSet(provenance.CreateChangeSetRequest{
		OrganizationID:   org.ID,
		SessionID:        sess.SessionID,
		RepositoryID:     "repo_test",
		Branch:           "main",
		UserID:           user.ID,
		HarnessID:        "hrn_test",
		ModelPackageID:   "pmp_test_v1",
		EndpointID:       ep.EndpointID,
		FilesChanged:     []string{"src/main.go"},
		DiffSummary:      "+func main() {}",
		LinesAdded:       1,
		AttributionState: "AI_GENERATED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cs.ChangeSetDigest == "" {
		t.Fatal("expected non-empty changeset digest")
	}

	// 10. Issue evidence receipt
	receipt, err := provSvc.IssueEvidenceReceipt(provenance.IssueReceiptRequest{
		OrganizationID: org.ID,
		ExchangeID:     "exch_test",
		SessionID:      sess.SessionID,
		FinalState:     "COMPLETED",
		ChainRoot:      "sha256:test_root",
		PolicyEpochID:  epoch.EpochID,
		ModelPackageID: "pmp_test_v1",
		EndpointID:     ep.EndpointID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Signature == "" {
		t.Fatal("expected non-empty receipt signature")
	}
}

func TestModelRecallInvalidatesLeases(t *testing.T) {
	db := setupTestDB(t)

	regSvc, _ := registry.New(db)
	idSvc, _ := identity.New(db)

	org, _ := idSvc.CreateOrganization("Test", "테스트", "test2", "enterprise")

	pkg := &models.ModelPackage{
		PackageID: "pmp_recall_test",
		ModelID:   "recall-model",
		Name:      "Recall Test",
		Version:   "1.0",
		State:     "draft",
	}
	regSvc.RegisterModelPackage(pkg)
	regSvc.PublishModelPackage(pkg.PackageID)

	ep, _ := regSvc.EnrollEndpoint(org.ID, "pia-test2", "pmp_recall_test", "vllm", "0.6",
		"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		"node-test", "none")
	lease, _ := regSvc.IssueEndpointLease(org.ID, ep.EndpointID, 1*time.Hour)
	if lease.Status != "active" {
		t.Fatal("expected active lease before recall")
	}

	regSvc.RecallModelPackage(pkg.PackageID, "security recall")

	var updated models.EndpointLease
	db.Where("lease_id = ?", lease.LeaseID).First(&updated)
	if updated.Status != "revoked" {
		t.Fatalf("expected revoked lease after recall, got %s", updated.Status)
	}

	_, err := regSvc.IssueEndpointLease(org.ID, ep.EndpointID, 1*time.Hour)
	if err == nil {
		t.Fatal("expected error issuing lease for recalled model")
	}
}
