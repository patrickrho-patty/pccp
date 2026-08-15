package scheduler

import (
	"context"
	"sync"
	"time"
)

// resilience.go implements S8: health probing with flap protection,
// budgeted request migration, cancellation propagation, and shadow
// failover (spec §14 rows 22–25).

// HealthConfig tunes active probing.
type HealthConfig struct {
	FailureThreshold int
	ProbeInterval    time.Duration
}

// DefaultHealthConfig returns reference parameters.
func DefaultHealthConfig() HealthConfig {
	return HealthConfig{FailureThreshold: 3, ProbeInterval: 5 * time.Second}
}

// ProbeFn checks one worker's health (engine reachability, lease state).
type ProbeFn func(ctx context.Context) error

// HealthProber actively probes workers and flips health state after
// consecutive failures (spec §14 row 24: worker/process/GPU/network
// failure management).
type HealthProber struct {
	mu       sync.Mutex
	cfg      HealthConfig
	probes   map[string]ProbeFn
	failures map[string]int
	healthy  map[string]bool
}

// NewHealthProber builds a prober.
func NewHealthProber(cfg HealthConfig) *HealthProber {
	return &HealthProber{
		cfg:      cfg,
		probes:   make(map[string]ProbeFn),
		failures: make(map[string]int),
		healthy:  make(map[string]bool),
	}
}

// Register binds a probe function to a worker.
func (h *HealthProber) Register(workerID string, fn ProbeFn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.probes[workerID] = fn
	h.healthy[workerID] = true
}

// ProbeAll runs one probe round across all workers.
func (h *HealthProber) ProbeAll(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, fn := range h.probes {
		if err := fn(ctx); err != nil {
			h.failures[id]++
			if h.failures[id] >= h.cfg.FailureThreshold {
				h.healthy[id] = false
			}
		} else {
			h.failures[id] = 0
			h.healthy[id] = true
		}
	}
}

// Healthy reports the worker's current health state.
func (h *HealthProber) Healthy(workerID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.healthy[workerID]
}

// Unregister removes a worker.
func (h *HealthProber) Unregister(workerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.probes, workerID)
	delete(h.failures, workerID)
	delete(h.healthy, workerID)
}

// MigrationManager budgets request migrations (spec §14 row 22: failure-
// and load-triggered, budgeted — a request cannot migrate forever).
type MigrationManager struct {
	mu       sync.Mutex
	budget   int
	migrated map[string]int
}

// NewMigrationManager builds a manager with the per-request budget.
func NewMigrationManager(budget int) *MigrationManager {
	return &MigrationManager{budget: budget, migrated: make(map[string]int)}
}

// Migrate attempts to move a request to another worker. Returns false
// when the budget is exhausted.
func (m *MigrationManager) Migrate(requestID, from, to, reason string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.migrated[requestID] >= m.budget {
		return false
	}
	m.migrated[requestID]++
	return true
}

// Reset clears a request's migration count (new request identity).
func (m *MigrationManager) Reset(requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.migrated, requestID)
}

// CancellationHub propagates cancellations to in-flight request
// contexts (spec §14 row 23: disconnect detection, propagation, KV
// reservation cleanup).
type CancellationHub struct {
	mu       sync.Mutex
	requests map[string]*cancelEntry
}

type cancelEntry struct {
	cancel    context.CancelFunc
	reason    string
	cancelled bool
}

// NewCancellationHub builds the hub.
func NewCancellationHub() *CancellationHub {
	return &CancellationHub{requests: make(map[string]*cancelEntry)}
}

// Register binds a request ID to a cancellable context.
func (c *CancellationHub) Register(requestID string) (context.Context, context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	c.requests[requestID] = &cancelEntry{cancel: cancel}
	return ctx, func() { c.Unregister(requestID); cancel() }
}

// Cancel terminates a request's context and records the reason. The
// entry stays queryable for Reason() until the request unregisters.
func (c *CancellationHub) Cancel(requestID, reason string) {
	c.mu.Lock()
	e, ok := c.requests[requestID]
	if ok {
		e.reason = reason
		e.cancelled = true
	}
	c.mu.Unlock()
	if ok {
		e.cancel()
	}
}

// Reason returns the recorded cancellation reason ("" when the request
// is unknown or never cancelled).
func (c *CancellationHub) Reason(requestID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.requests[requestID]; ok && e.cancelled {
		return e.reason
	}
	return ""
}

// Unregister removes a request without cancelling (normal completion).
func (c *CancellationHub) Unregister(requestID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.requests, requestID)
}

// ShadowTracker pairs requests with shadow workers for rapid failover
// (spec §14 row 25: optional HA tier).
type ShadowTracker struct {
	mu    sync.Mutex
	pairs map[string]shadowPair
}

type shadowPair struct {
	primary string
	shadow  string
}

// NewShadowTracker builds the tracker.
func NewShadowTracker() *ShadowTracker {
	return &ShadowTracker{pairs: make(map[string]shadowPair)}
}

// Begin records the primary/shadow pairing.
func (s *ShadowTracker) Begin(requestID, primary, shadow string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairs[requestID] = shadowPair{primary: primary, shadow: shadow}
}

// Primary returns the request's primary worker.
func (s *ShadowTracker) Primary(requestID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pairs[requestID]
	return p.primary, ok
}

// Shadow returns the request's shadow worker.
func (s *ShadowTracker) Shadow(requestID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pairs[requestID]
	return p.shadow, ok
}

// Failover switches the pairing to the shadow when the primary fails.
func (s *ShadowTracker) Failover(requestID, failedPrimary string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pairs[requestID]
	if !ok || p.primary != failedPrimary {
		return "", false
	}
	return p.shadow, true
}

// Complete removes the pairing.
func (s *ShadowTracker) Complete(requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pairs, requestID)
}
