package scheduler

import (
	"crypto/ed25519"
	"fmt"
	"sort"
	"sync"
	"time"
)

// WorkerEntry is a registry record: the latest signed card, the worker's PPC
// public key, and lease state. The card IS the health signal — an engine that
// died fails its next heartbeat's introspection and arrives with a degraded
// card; the lease tracks liveness independently.
type WorkerEntry struct {
	Card        WorkerCard
	PPCPubKey   ed25519.PublicKey
	AdmittedAt  time.Time
	LastSeen    time.Time
	LeasedUntil time.Time
	Lapsed      bool
}

// Registry is the in-memory fleet registry. State is ephemeral by design:
// workers re-register on restart and stale leases expire; there is no restore
// code (DARI scheduler §6).
type Registry struct {
	mu      sync.RWMutex
	ttl     time.Duration
	grace   time.Duration
	workers map[string]*WorkerEntry
}

// NewRegistry creates a registry with the given lease TTL and post-lease
// grace period. Defaults per spec: TTL 30s, grace 2×TTL.
func NewRegistry(ttl, grace time.Duration) *Registry {
	return &Registry{
		ttl:     ttl,
		grace:   grace,
		workers: make(map[string]*WorkerEntry),
	}
}

// Register stores a worker and issues a fresh lease. Signature verification
// and policy admission happen before this call (admission ladder); the
// registry is dumb storage.
func (r *Registry) Register(card WorkerCard, pub ed25519.PublicKey, now time.Time) (WorkerEntry, error) {
	if card.WorkerID == "" {
		return WorkerEntry{}, fmt.Errorf("scheduler: register: empty worker ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := &WorkerEntry{
		Card:        card,
		PPCPubKey:   append(ed25519.PublicKey(nil), pub...),
		AdmittedAt:  now,
		LastSeen:    now,
		LeasedUntil: now.Add(r.ttl),
	}
	if existing, ok := r.workers[card.WorkerID]; ok {
		entry.AdmittedAt = existing.AdmittedAt
	}
	r.workers[card.WorkerID] = entry
	return *entry, nil
}

// Heartbeat renews the lease for a known worker, or re-populates the registry
// for an unknown one (scheduler restart case — workers' heartbeats rebuild
// the in-memory state without restore code).
func (r *Registry) Heartbeat(card WorkerCard, pub ed25519.PublicKey, now time.Time) (WorkerEntry, error) {
	if card.WorkerID == "" {
		return WorkerEntry{}, fmt.Errorf("scheduler: heartbeat: empty worker ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.workers[card.WorkerID]
	if !ok {
		entry = &WorkerEntry{
			AdmittedAt: now,
		}
		r.workers[card.WorkerID] = entry
	}
	entry.Card = card
	entry.PPCPubKey = append(ed25519.PublicKey(nil), pub...)
	entry.LastSeen = now
	entry.LeasedUntil = now.Add(r.ttl)
	entry.Lapsed = false
	return *entry, nil
}

// Get returns a worker entry by ID.
func (r *Registry) Get(workerID string) (WorkerEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.workers[workerID]
	if !ok {
		return WorkerEntry{}, false
	}
	return *entry, true
}

// List returns all workers sorted by ID for deterministic output.
func (r *Registry) List() []WorkerEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WorkerEntry, 0, len(r.workers))
	for _, e := range r.workers {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Card.WorkerID < out[j].Card.WorkerID })
	return out
}

// Sweep marks workers past their lease as lapsed and evicts those past the
// grace period. It returns the evicted worker IDs (sorted) so the caller can
// emit evidence events.
func (r *Registry) Sweep(now time.Time) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var evicted []string
	for id, entry := range r.workers {
		age := now.Sub(entry.LastSeen)
		switch {
		case age > r.ttl+r.grace:
			delete(r.workers, id)
			evicted = append(evicted, id)
		case age > r.ttl:
			entry.Lapsed = true
		}
	}
	sort.Strings(evicted)
	return evicted
}
