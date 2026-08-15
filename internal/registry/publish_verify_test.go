package registry

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func registryDB(t *testing.T) (*gorm.DB, *Service) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/r.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(&models.ModelPackage{}, &models.InferenceEndpoint{}, &models.EndpointLease{}, &models.ServiceSigningKey{})
	svc, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return db, svc
}

func TestPublishVerifiesManifestSignature(t *testing.T) {
	db, svc := registryDB(t)
	if err := svc.RegisterModelPackage(&models.ModelPackage{
		PackageID: "pmp-v", ModelID: "m-v", Name: "V", Version: "1",
		WeightsMerkleRoot: "wm", TokenizerDigest: "td",
	}); err != nil {
		t.Fatal(err)
	}
	pkg := "pmp-v"
	// Clean package publishes.
	if err := svc.PublishModelPackage(pkg); err != nil {
		t.Fatal(err)
	}

	// Tampered content after registration fails closed.
	if err := svc.RegisterModelPackage(&models.ModelPackage{
		PackageID: "pmp-t", ModelID: "m-t", Name: "T", Version: "1",
		WeightsMerkleRoot: "wm", TokenizerDigest: "td",
	}); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.ModelPackage{}).Where("package_id = ?", "pmp-t").
		Update("weights_merkle_root", "tampered")
	if err := svc.PublishModelPackage("pmp-t"); err == nil || !strings.Contains(err.Error(), "manifest digest mismatch") {
		t.Fatalf("tampered package published: %v", err)
	}

	// Forged signature fails closed.
	if err := svc.RegisterModelPackage(&models.ModelPackage{
		PackageID: "pmp-f", ModelID: "m-f", Name: "F", Version: "1",
	}); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.ModelPackage{}).Where("package_id = ?", "pmp-f").
		Update("signature", "00")
	if err := svc.PublishModelPackage("pmp-f"); err == nil {
		t.Fatal("forged signature published")
	}
}

func TestEndpointLeaseRequiresFreshAttestation(t *testing.T) {
	db, svc := registryDB(t)
	if err := svc.RegisterModelPackage(&models.ModelPackage{
		PackageID: "pmp-att", ModelID: "m-att", Name: "A", Version: "1",
	}); err != nil {
		t.Fatal(err)
	}
	_, pub, _ := ed25519.GenerateKey(nil)
	ep, err := svc.EnrollEndpoint("org", "pia-1", "pmp-att", "vllm", "0.1",
		hex.EncodeToString(pub), "node-1", "L1")
	if err != nil {
		t.Fatal(err)
	}

	// L1 endpoint WITHOUT an attestation: lease refused (fail closed).
	if _, err := svc.IssueEndpointLease("org", ep.EndpointID, time.Hour); err == nil || !strings.Contains(err.Error(), "attestation") {
		t.Fatalf("expected attestation-required error, got %v", err)
	}

	// Enroll an unattested (none) endpoint: leases allowed for dev.
	ep2, err := svc.EnrollEndpoint("org", "pia-2", "pmp-att", "vllm", "0.1",
		hex.EncodeToString(pub), "node-2", "none")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IssueEndpointLease("org", ep2.EndpointID, time.Hour); err != nil {
		t.Fatalf("dev endpoint lease refused: %v", err)
	}
	_ = db
}
