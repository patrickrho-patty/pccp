package scheduler

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func signedTestCard(t *testing.T) (WorkerCard, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	card := testCard()
	if err := card.Sign(priv); err != nil {
		t.Fatal(err)
	}
	return card, pub, priv
}

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	return NewRegistry(30*time.Second, 60*time.Second)
}

func TestRegistryRegisterStoresCardAndLease(t *testing.T) {
	r := newTestRegistry(t)
	card, pub, _ := signedTestCard(t)
	now := time.Now()

	entry, err := r.Register(card, pub, now)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if entry.Card.WorkerID != card.WorkerID {
		t.Fatalf("stored worker %q, want %q", entry.Card.WorkerID, card.WorkerID)
	}
	if !entry.LeasedUntil.Equal(now.Add(30 * time.Second)) {
		t.Fatalf("lease expiry %v, want %v", entry.LeasedUntil, now.Add(30*time.Second))
	}
	if entry.Lapsed {
		t.Fatal("freshly registered worker must not be lapsed")
	}
}

func TestRegistryHeartbeatRenewsLease(t *testing.T) {
	r := newTestRegistry(t)
	card, pub, priv := signedTestCard(t)
	now := time.Now()
	if _, err := r.Register(card, pub, now); err != nil {
		t.Fatal(err)
	}

	later := now.Add(10 * time.Second)
	card.Status = "active"
	card.LeaseExpiryUnixMs = later.UnixMilli()
	if err := card.Sign(priv); err != nil {
		t.Fatal(err)
	}
	entry, err := r.Heartbeat(card, pub, later)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !entry.LeasedUntil.Equal(later.Add(30 * time.Second)) {
		t.Fatalf("lease not renewed: %v", entry.LeasedUntil)
	}
}

func TestRegistryHeartbeatRepopulatesAfterRestart(t *testing.T) {
	r := newTestRegistry(t)
	card, pub, _ := signedTestCard(t)
	now := time.Now()

	entry, err := r.Heartbeat(card, pub, now)
	if err != nil {
		t.Fatalf("heartbeat should re-register unknown worker: %v", err)
	}
	if entry.Card.WorkerID != card.WorkerID {
		t.Fatal("worker not re-populated")
	}
}

func TestRegistryGetAndList(t *testing.T) {
	r := newTestRegistry(t)
	card, pub, _ := signedTestCard(t)
	now := time.Now()
	if _, err := r.Register(card, pub, now); err != nil {
		t.Fatal(err)
	}

	if _, ok := r.Get(card.WorkerID); !ok {
		t.Fatal("Get should find registered worker")
	}
	if len(r.List()) != 1 {
		t.Fatalf("List length %d, want 1", len(r.List()))
	}
	if _, ok := r.Get("unknown-worker"); ok {
		t.Fatal("Get should not find unknown worker")
	}
}

func TestRegistrySweepKeepsWithinGraceAndEvictsAfter(t *testing.T) {
	r := newTestRegistry(t)
	card, pub, _ := signedTestCard(t)
	now := time.Now()
	if _, err := r.Register(card, pub, now); err != nil {
		t.Fatal(err)
	}

	// Within TTL: nothing evicted.
	if evicted := r.Sweep(now.Add(20 * time.Second)); len(evicted) != 0 {
		t.Fatalf("evicted %v within TTL", evicted)
	}

	// Past TTL but within grace: lapsed, not evicted.
	if evicted := r.Sweep(now.Add(40 * time.Second)); len(evicted) != 0 {
		t.Fatalf("evicted %v within grace", evicted)
	}
	entry, ok := r.Get(card.WorkerID)
	if !ok {
		t.Fatal("worker missing within grace")
	}
	if !entry.Lapsed {
		t.Fatal("worker past TTL within grace must be lapsed")
	}

	// Past TTL + grace: evicted.
	if evicted := r.Sweep(now.Add(100 * time.Second)); len(evicted) != 1 || evicted[0] != card.WorkerID {
		t.Fatalf("evicted %v, want [%s]", evicted, card.WorkerID)
	}
	if _, ok := r.Get(card.WorkerID); ok {
		t.Fatal("worker should be removed after grace")
	}
}

func TestRegistryHeartbeatResurrectsLapsedWorker(t *testing.T) {
	r := newTestRegistry(t)
	card, pub, priv := signedTestCard(t)
	now := time.Now()
	if _, err := r.Register(card, pub, now); err != nil {
		t.Fatal(err)
	}

	// Lapse it.
	r.Sweep(now.Add(40 * time.Second))

	later := now.Add(50 * time.Second)
	if err := card.Sign(priv); err != nil {
		t.Fatal(err)
	}
	entry, err := r.Heartbeat(card, pub, later)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if entry.Lapsed {
		t.Fatal("heartbeat should restore lapsed worker to leased state")
	}
}
