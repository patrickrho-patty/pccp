package relay

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// hotstate_test.go implements the Task 15 hot-state + backpressure
// conformance vectors.

func TestHotStateCacheFreshness(t *testing.T) {
	c := NewHotStateCache(time.Minute)
	snap := &GovernanceSnapshot{Lease: models.CapabilityLease{LeaseID: "l1"}}
	c.Put("h1", snap, 0)

	got, err := c.Get("h1", time.Now(), 0)
	if err != nil || got == nil || got.Lease.LeaseID != "l1" {
		t.Fatalf("fresh get failed: %v %+v", err, got)
	}
	// TTL expiry.
	c2 := NewHotStateCache(time.Millisecond)
	c2.Put("h1", snap, 0)
	time.Sleep(3 * time.Millisecond)
	if got, _ := c2.Get("h1", time.Now(), 0); got != nil {
		t.Fatal("stale entry returned")
	}
	// Revocation epoch bump invalidates EVERYTHING (fail-closed).
	c.Put("h2", snap, 0)
	if _, err := c.Get("h1", time.Now(), 1); !errors.Is(err, ErrRevokedSnapshot) {
		t.Fatalf("expected revoked-snapshot error, got %v", err)
	}
	// A put under the new epoch rebases the cache.
	c.Put("h3", snap, 1)
	if got, err := c.Get("h3", time.Now(), 1); err != nil || got == nil {
		t.Fatalf("rebased get failed: %v", err)
	}
	// Explicit invalidation.
	c.Invalidate("h3")
	if got, _ := c.Get("h3", time.Now(), 1); got != nil {
		t.Fatal("invalidated entry returned")
	}
	// Stats.
	hits, misses, entries := c.Stats()
	if hits == 0 || misses == 0 || entries == 0 {
		t.Logf("stats: hits=%d misses=%d entries=%d", hits, misses, entries)
	}
}

func TestConcurrencyGateSheds(t *testing.T) {
	g := NewConcurrencyGate(2)
	if err := g.Acquire(); err != nil {
		t.Fatal(err)
	}
	if err := g.Acquire(); err != nil {
		t.Fatal(err)
	}
	if err := g.Acquire(); !errors.Is(err, ErrLoadShed) {
		t.Fatalf("expected load shed, got %v", err)
	}
	g.Release()
	if err := g.Acquire(); err != nil {
		t.Fatal(err)
	}
	if _, limit, shed, _ := g.Stats(); limit != 2 || shed != 1 {
		t.Fatalf("stats: limit=%d shed=%d", limit, shed)
	}

	// Concurrency shed stress: 100 goroutines through a 8-slot gate
	// all complete; sheds counted.
	g2 := NewConcurrencyGate(8)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g2.Do(func() error {
				time.Sleep(time.Millisecond)
				return nil
			})
		}()
	}
	wg.Wait()
	_, _, shed, maxObserved := g2.Stats()
	if maxObserved > 8 {
		t.Fatalf("in-flight %d exceeded limit", maxObserved)
	}
	t.Logf("gate: shed=%d maxObserved=%d", shed, maxObserved)
}

func TestResolveGovernanceSnapshotFailClosed(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID     = "org-hot"
		harnessID = "hrn-hot"
		modelID   = "patty-hot"
	)
	svc, err := New(db, "", "relay-hot-test")
	if err != nil {
		t.Fatal(err)
	}
	// Empty DB → fail-closed at the first resolution step.
	if _, err := svc.ResolveGovernanceSnapshot(harnessID, modelID); err == nil {
		t.Fatal("unresolved governance context must fail closed")
	}

	// Fully-seeded chain resolves.
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	db.Create(&models.Harness{OrganizationID: orgID, HarnessID: harnessID, Status: "enrolled"})
	db.Create(&models.CapabilityLease{OrganizationID: orgID, LeaseID: "lease-hot", SubjectPeerID: harnessID,
		UserID: "u", SessionID: "sess-hot", PolicyEpochID: "epoch-hot",
		AllowedModelPackages: `["pmp-hot"]`, NotBefore: past, NotAfter: future, Status: "active"})
	db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: "epoch-hot", AllowedModelsJSON: `["pmp-hot"]`, Status: "active"})
	db.Create(&models.ModelPackage{PackageID: "pmp-hot", ModelID: modelID, Name: "Hot", State: "published"})
	db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: "ep-hot", ModelPackageID: "pmp-hot", Status: "active"})
	db.Create(&models.EndpointLease{EndpointID: "ep-hot", OrganizationID: orgID, ModelPackageID: "pmp-hot", LeaseID: "epl-hot", NotAfter: future, Status: "active", IssuedAt: past})

	snap, err := svc.ResolveGovernanceSnapshot(harnessID, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Endpoint.EndpointID != "ep-hot" || snap.Package.PackageID != "pmp-hot" {
		t.Fatalf("snapshot: %+v", snap)
	}
	_ = context.Background()
}
