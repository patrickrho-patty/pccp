package scheduler

import "sync"

// regions.go implements the PAT-1445 global routing stage: region health
// and the preauthorized failover map, consulted before cluster/worker
// selection. Cross-region failover occurs ONLY to preauthorized regions —
// never a silent crossing (issue §security). An empty registry is a no-op:
// no health data and no failover policy means the requested region is
// used unchanged (no behavior change for single-region deployments).

// RegionRegistry tracks region health and permitted failover.
type RegionRegistry struct {
	mu       sync.RWMutex
	healthy  map[string]bool
	failover map[string][]string // region → preauthorized failover regions, ordered
}

// NewRegionRegistry builds an empty registry (no-op global stage).
func NewRegionRegistry() *RegionRegistry {
	return &RegionRegistry{
		healthy:  make(map[string]bool),
		failover: make(map[string][]string),
	}
}

// SetHealth records a region's health (absent = unknown = treated as
// healthy: health data is advisory until it exists).
func (r *RegionRegistry) SetHealth(region string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healthy[region] = healthy
}

// SetFailover installs a region's preauthorized failover list (ordered by
// preference). The list is the ONLY set of regions a request may fail
// over to.
func (r *RegionRegistry) SetFailover(region string, allowed []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failover[region] = append([]string(nil), allowed...)
}

// Healthy reports a region's health (unknown = healthy).
func (r *RegionRegistry) Healthy(region string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.healthy[region]
	return !ok || h
}

// SelectRegion applies the global stage: a healthy (or unknown-health)
// wanted region serves; an unhealthy wanted region fails over to the
// first preauthorized healthy region; anything else is a clear
// availability failure (never silently reroute). An unconstrained request
// (empty want) passes through unchanged.
func (r *RegionRegistry) SelectRegion(want string) (string, bool) {
	if want == "" {
		return "", true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	health := func(region string) bool {
		h, ok := r.healthy[region]
		return !ok || h
	}
	if health(want) {
		return want, true
	}
	for _, alt := range r.failover[want] {
		if health(alt) {
			return alt, true
		}
	}
	return "", false
}
