package relay

import (
	"context"
	"encoding/hex"
	"regexp"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

func hexLower(d [32]byte) string { return hex.EncodeToString(d[:]) }

// close_test.go pins the Task 10 Step 3 fix: the live receipt's chain
// root is the deterministic F.9 linear-chain root bound to the
// exchange identity — never a random identifier.

var randomIDPattern = regexp.MustCompile(`^(chainroot|id)_[0-9a-f]{10,}$`)

func TestCloseExchangeChainRootIsDeterministic(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID    = "org-t10"
		harness  = "h-t10"
		sess     = "sess-t10"
		leaseID  = "lease-t10"
		epochID  = "epoch-t10"
		modelPkg = "pmp-t10"
		endpoint = "ep-t10"
	)
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	db.Create(&models.Harness{OrganizationID: orgID, HarnessID: harness, Status: "enrolled"})
	db.Create(&models.CapabilityLease{OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harness,
		UserID: "u", SessionID: sess, PolicyEpochID: epochID,
		AllowedModelPackages: `["` + modelPkg + `"]`, NotBefore: past, NotAfter: future, Status: "active"})
	db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: epochID, AllowedModelsJSON: `["` + modelPkg + `"]`, Status: "active"})
	db.Create(&models.ModelPackage{PackageID: modelPkg, ModelID: "m-t10", Name: "T10", State: "published"})
	db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: endpoint, ModelPackageID: modelPkg, Status: "active"})
	db.Create(&models.EndpointLease{EndpointID: endpoint, OrganizationID: orgID, ModelPackageID: modelPkg, LeaseID: "epl-t10", NotAfter: future, Status: "active", IssuedAt: past})

	svc, err := New(db, "", "relay-close-test")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ex, _, err := svc.OpenExchange(ctx, OpenExchangeRequest{
		OrganizationID: orgID, SessionID: sess, UserID: "u", HarnessID: harness,
		LeaseID: leaseID, PolicyEpochID: epochID, ModelPackageID: modelPkg,
	})
	if err != nil {
		t.Fatal(err)
	}

	r1, err := svc.CloseExchange(ctx, ex.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r1 == nil || r1.ChainRoot == "" {
		t.Fatal("receipt must carry a chain root")
	}
	if randomIDPattern.MatchString(r1.ChainRoot) {
		t.Fatalf("chain root %q looks like a random ID — must be the F.9 root", r1.ChainRoot)
	}
	// The stored root is hex of the computed digest (64 chars).
	if len(r1.ChainRoot) != 64 {
		t.Fatalf("chain root len = %d, want 64-hex digest", len(r1.ChainRoot))
	}

	// Determinism: recomputing the SAME exchange's root yields the
	// identical digest (pure function of the exchange identity).
	if again := svc.evidenceChainRoot(ex); again != svc.evidenceChainRoot(ex) {
		t.Fatal("root computation must be deterministic")
	}
	// The stored receipt root matches the computed digest.
	if want := svc.evidenceChainRoot(ex).String(); r1.ChainRoot != want && len(want) > 0 {
		// ChainRoot stored as hex — compare case-insensitively.
		if r1.ChainRoot != hexLower(svc.evidenceChainRoot(ex)) {
			t.Fatalf("receipt root %s != computed %s", r1.ChainRoot, hexLower(svc.evidenceChainRoot(ex)))
		}
	}
}
