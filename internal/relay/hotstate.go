package relay

import (
	"errors"
	"fmt"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
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

// Limit reports the gate's concurrency bound.
func (g *ConcurrencyGate) Limit() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.limit
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

// fairAdmitWait bounds how long a saturated governed request waits in
// the fair scheduler before failing closed.
const fairAdmitWait = 750 * time.Millisecond

// admitGoverned takes an exchange slot, queueing fairly on saturation.
// Saturated requests enter the per-account fair scheduler and wait
// (bounded) to be admitted by ITS weighted priority (§10C.7) — the
// winning request proceeds; the rest keep queuing per account so one
// hot harness cannot starve the fleet.
func (s *Service) admitGoverned(accountID, _ string) bool {
	if err := s.exchangeGate.Acquire(); err == nil {
		return true
	}
	s.fairSched.Enqueue(scheduler.QueuedRequest{
		AccountID:    accountID,
		Class:        "INTERACTIVE",
		CatalogModel: "governed-exchange",
	})
	defer s.fairSched.Release(accountID)
	deadline := time.Now().Add(fairAdmitWait)
	for time.Now().Before(deadline) {
		if won := s.fairSched.Admit(s.exchangeGate.Limit()); won != nil && won.AccountID == accountID {
			if err := s.exchangeGate.Acquire(); err == nil {
				return true
			}
			// The slot vanished before we took it — keep waiting.
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// releaseGoverned returns a slot; the scheduler's accounting follows.
func (s *Service) releaseGoverned(accountID string) {
	s.exchangeGate.Release()
}

// ---------------------------------------------------------------------------
// §42.1 idempotency + record replay window (per-connection, bounded).
// ---------------------------------------------------------------------------

// aiOpenCacheEntry is one cached governed response by idempotency key.
type aiOpenCacheEntry struct {
	response []byte
	at       time.Time
}

// aiOpenCache is a bounded per-connection idempotency-key → response
// store (§42.1): a retransmitted AI_OPEN with the same key replays the
// cached response instead of re-governing. Entries expire past TTL so
// the map stays bounded under key churn.
type aiOpenCache struct {
	mu      sync.Mutex
	byConn  map[string]map[string]aiOpenCacheEntry
	ttl     time.Duration
	maxKeys int
}

func newAIOpenCache() *aiOpenCache {
	return &aiOpenCache{
		byConn:  map[string]map[string]aiOpenCacheEntry{},
		ttl:     10 * time.Minute,
		maxKeys: 1024,
	}
}

func (c *aiOpenCache) get(connID, key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.byConn[connID]
	if m == nil {
		return nil, false
	}
	e, ok := m[key]
	if !ok || time.Since(e.at) > c.ttl {
		return nil, false
	}
	return e.response, true
}

func (c *aiOpenCache) put(connID, key string, response []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.byConn[connID]
	if m == nil {
		m = map[string]aiOpenCacheEntry{}
		c.byConn[connID] = m
	}
	if len(m) >= c.maxKeys {
		// Drop the oldest tenth (approximate LRU by timestamp).
		var oldest []string
		for k, e := range m {
			_ = e
			oldest = append(oldest, k)
			if len(oldest) > c.maxKeys/10 {
				break
			}
		}
		for _, k := range oldest {
			delete(m, k)
		}
	}
	m[key] = aiOpenCacheEntry{response: response, at: time.Now()}
}

// dropConn frees a closed connection's cache.
func (c *aiOpenCache) dropConn(connID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byConn, connID)
}

// replayWindow is the per-connection bounded record replay guard
// (§42.1): records whose LaneSequence was already observed inside the
// window are replays and are dropped. Sequences move forward; the
// window bounds how far BACK a legitimate retransmission may land.
type replayWindow struct {
	mu     sync.Mutex
	byConn map[string]*seqRing
	size   int
}

type seqRing struct {
	seen map[uint64]struct{}
	max  uint64 // highest observed
}

func newReplayWindow() *replayWindow {
	return &replayWindow{byConn: map[string]*seqRing{}, size: 512}
}

// observe records a sequence and reports whether it was ALREADY seen
// (a replay). Sequences above the ring's max are new; sequences below
// max that are not in the set are outside the window (stale
// reordering) and treated as new-but-flagged, not dropped.
func (w *replayWindow) observe(connID string, seq uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := w.byConn[connID]
	if r == nil {
		r = &seqRing{seen: map[uint64]struct{}{}}
		w.byConn[connID] = r
	}
	if _, dup := r.seen[seq]; dup {
		return true
	}
	if seq > r.max {
		r.max = seq
	}
	r.seen[seq] = struct{}{}
	if len(r.seen) > w.size {
		// Evict sequences more than the window below max.
		for s := range r.seen {
			if r.max-s > uint64(w.size) {
				delete(r.seen, s)
				if len(r.seen) <= w.size {
					break
				}
			}
		}
	}
	return false
}

// dropConn frees a closed connection's window.
func (w *replayWindow) dropConn(connID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.byConn, connID)
}

// headerString extracts one header value as a string ("" when absent).
func headerString(raw []byte, key dari.HeaderKey) string {
	m, err := dari.DecodeHeader(raw)
	if err != nil || m == nil {
		return ""
	}
	return string(m[key])
}

// Note: aiOpenCache/replayWindow regression pins live in hotstate_test.go.
