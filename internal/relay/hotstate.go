package relay

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// hotstate.go implements the governed-pipeline hot-state cache and
// backpressure (master plan Task 15): per-harness resolution of
// lease/epoch/package/endpoint is cached with revocation-aware
// invalidation, and governed exchanges run under a bounded concurrency
// limiter that sheds load instead of unbounded queueing.

// ---------------------------------------------------------------------------
// Hot-state cache.
// ---------------------------------------------------------------------------

// GovernanceSnapshot is one harness's resolved governance context.
type GovernanceSnapshot struct {
	Harness       models.Harness
	Lease         models.CapabilityLease
	Epoch         models.PolicyEpoch
	Package       models.ModelPackage
	Endpoint      models.InferenceEndpoint
	EndpointLease models.EndpointLease
	ResolvedAt    time.Time
}

// HotStateCache caches governance resolution per harness. Entries are
// validated against the identity revocation epoch: any revocation
// bumps the epoch and invalidates every entry atomically (fail-closed
// freshness).
type HotStateCache struct {
	mu      sync.RWMutex
	entries map[string]*GovernanceSnapshot
	epoch   uint64 // revocation high-water at capture time
	ttl     time.Duration
	hits    atomic.Uint64
	misses  atomic.Uint64
}

// NewHotStateCache builds a cache with the given TTL.
func NewHotStateCache(ttl time.Duration) *HotStateCache {
	return &HotStateCache{entries: map[string]*GovernanceSnapshot{}, ttl: ttl}
}

// ErrRevokedSnapshot marks a cached entry captured before a revocation.
var ErrRevokedSnapshot = errors.New("relay: cached governance state predates a revocation")

// GovCacheKey is the hot-state cache key: governance resolution is
// per (harness, model).
func GovCacheKey(harnessID, model string) string { return harnessID + "|" + model }

// Get returns a cached snapshot only when fresh by BOTH TTL and the
// current revocation epoch.
func (c *HotStateCache) Get(key string, now time.Time, revocationEpoch uint64) (*GovernanceSnapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snap, ok := c.entries[key]
	if !ok {
		c.misses.Add(1)
		return nil, nil
	}
	if now.Sub(snap.ResolvedAt) > c.ttl {
		c.misses.Add(1)
		return nil, nil
	}
	if revocationEpoch > c.epoch {
		// A revocation happened after this cache was captured — the
		// whole cache is invalid, fail closed.
		return nil, ErrRevokedSnapshot
	}
	c.hits.Add(1)
	out := *snap
	return &out, nil
}

// Put stores a snapshot under the current revocation epoch. A
// snapshot captured BEFORE the cache's current epoch is dropped — it
// may predate a revocation and must never be served.
func (c *HotStateCache) Put(key string, snap *GovernanceSnapshot, revocationEpoch uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if revocationEpoch > c.epoch {
		// A newer revocation epoch means all prior entries are stale —
		// drop them and rebase.
		c.entries = map[string]*GovernanceSnapshot{}
		c.epoch = revocationEpoch
	} else if revocationEpoch < c.epoch {
		// Stale resolver: its snapshot predates a revocation.
		return
	}
	cp := *snap
	cp.ResolvedAt = time.Now()
	c.entries[key] = &cp
}

// Invalidate drops one harness (revocation, lease change, epoch bump).
func (c *HotStateCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// InvalidateAll drops every entry (catalog publish, policy change).
func (c *HotStateCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]*GovernanceSnapshot{}
}

// Stats reports hits/misses for the pipeline observability.
func (c *HotStateCache) Stats() (hits, misses uint64, entries int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits.Load(), c.misses.Load(), len(c.entries)
}

// ---------------------------------------------------------------------------
// Backpressure: bounded exchange concurrency.
// ---------------------------------------------------------------------------

// ErrLoadShed is the fail-closed backpressure signal.
var ErrLoadShed = errors.New("relay: load shed — governed exchange concurrency at limit")

// ConcurrencyGate is a bounded, shed-on-full limiter for governed
// exchanges (no unbounded queue: an over-limit request fails fast).
type ConcurrencyGate struct {
	mu          sync.Mutex
	inFlight    int
	limit       int
	shed        uint64
	maxObserved int
}

// NewConcurrencyGate builds a gate.
func NewConcurrencyGate(limit int) *ConcurrencyGate {
	if limit <= 0 {
		limit = 64
	}
	return &ConcurrencyGate{limit: limit}
}

// Acquire takes a slot or fails with ErrLoadShed.
func (g *ConcurrencyGate) Acquire() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight >= g.limit {
		g.shed++
		return ErrLoadShed
	}
	g.inFlight++
	if g.inFlight > g.maxObserved {
		g.maxObserved = g.inFlight
	}
	return nil
}

// Release returns a slot.
func (g *ConcurrencyGate) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight > 0 {
		g.inFlight--
	}
}

// Do runs fn under the gate.
func (g *ConcurrencyGate) Do(fn func() error) error {
	if err := g.Acquire(); err != nil {
		return err
	}
	defer g.Release()
	return fn()
}

// Stats reports the backpressure observability.
func (g *ConcurrencyGate) Stats() (inFlight, limit int, shed uint64, maxObserved int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inFlight, g.limit, g.shed, g.maxObserved
}

// ---------------------------------------------------------------------------
// Pipeline wiring: snapshot resolution + cache use in GovernInference.
// ---------------------------------------------------------------------------

// ResolveGovernanceSnapshot performs the fail-closed DB resolution
// (harness → lease → package → endpoint → endpoint lease) for a model.
func (s *Service) ResolveGovernanceSnapshot(harnessID, model string) (*GovernanceSnapshot, error) {
	var snap GovernanceSnapshot
	if err := s.db.Where("harness_id = ?", harnessID).First(&snap.Harness).Error; err != nil {
		return nil, fmt.Errorf("relay: harness %s not enrolled: %w", harnessID, err)
	}
	if err := s.db.Where("subject_peer_id = ? AND status = 'active'", harnessID).
		Order("not_after DESC").First(&snap.Lease).Error; err != nil {
		return nil, fmt.Errorf("relay: no active capability lease for harness %s: %w", harnessID, err)
	}
	if err := s.db.Where("model_id = ?", model).First(&snap.Package).Error; err != nil {
		return nil, fmt.Errorf("relay: model %s not in registry: %w", model, err)
	}
	if err := s.db.Where("epoch_id = ?", snap.Lease.PolicyEpochID).First(&snap.Epoch).Error; err != nil {
		return nil, fmt.Errorf("relay: policy epoch not found: %w", err)
	}
	if err := s.db.Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
		snap.Harness.OrganizationID, snap.Package.PackageID).First(&snap.Endpoint).Error; err != nil {
		return nil, fmt.Errorf("relay: no active endpoint for model %s: %w", snap.Package.PackageID, err)
	}
	if err := s.db.Where("endpoint_id = ? AND status = 'active' AND not_after > ?",
		snap.Endpoint.EndpointID, time.Now().Format(time.RFC3339)).
		Order("issued_at DESC").First(&snap.EndpointLease).Error; err != nil {
		return nil, fmt.Errorf("relay: no valid endpoint lease for endpoint %s: %w", snap.Endpoint.EndpointID, err)
	}
	return &snap, nil
}

// heartbeatThrottle bounds harness heartbeat writes to one per minute.
var heartbeatThrottle struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func init() { heartbeatThrottle.last = map[string]time.Time{} }

// recordHeartbeat stamps the harness's live-path heartbeat, throttled
// to one write per harness per minute (fleet liveness needs
// seconds-level freshness, not per-request write amplification).
func (s *Service) recordHeartbeat(harnessID string) {
	heartbeatThrottle.mu.Lock()
	if time.Since(heartbeatThrottle.last[harnessID]) < time.Minute {
		heartbeatThrottle.mu.Unlock()
		return
	}
	heartbeatThrottle.last[harnessID] = time.Now()
	heartbeatThrottle.mu.Unlock()

	s.db.Model(&models.Harness{}).Where("harness_id = ?", harnessID).
		Update("last_heartbeat", time.Now().Format(time.RFC3339))
}
