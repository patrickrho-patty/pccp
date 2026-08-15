package scheduler

import (
	"sort"
	"sync"
)

// gang.go implements S3 expert/parallelism-aware readiness (spec §14 row
// 16): a request is served only by worker groups whose full rank set is
// present and healthy. TP/EP ranks form one gang; DP ranks are
// independent gangs.

// GangRegistry tracks worker groups by (model, parallel config).
type GangRegistry struct {
	mu      sync.RWMutex
	workers map[string]WorkerEntry
}

// NewGangRegistry builds an empty registry.
func NewGangRegistry() *GangRegistry {
	return &GangRegistry{workers: make(map[string]WorkerEntry)}
}

// Upsert installs or refreshes a worker.
func (g *GangRegistry) Upsert(e WorkerEntry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.workers[e.Card.WorkerID] = e
}

// Remove drops a worker.
func (g *GangRegistry) Remove(workerID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.workers, workerID)
}

// Ready reports whether the model's gang is complete and healthy: every
// rank of the parallel config is present, servable, and unquarantined.
// Single-rank cards (TP=DP=1) are always individually ready.
func (g *GangRegistry) Ready(model string) bool {
	return g.ReadyCount(model) > 0
}

// ReadyCount returns how many ready workers serve the model. For a
// TP/EP gang, this is the full gang size (all ranks) or zero.
func (g *GangRegistry) ReadyCount(model string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	type gangKey struct{ tp, dp, ep uint32 }
	gangs := map[gangKey]map[string]bool{}
	var order []gangKey

	for id, e := range g.workers {
		if e.Card.ModelName != model {
			continue
		}
		if e.Quarantined || e.Lapsed || !e.Card.Servable() {
			continue
		}
		key := gangKey{tp: e.Card.TP, dp: e.Card.DP, ep: e.Card.EP}
		if key.tp == 0 {
			key.tp = 1
		}
		if key.dp == 0 {
			key.dp = 1
		}
		if key.ep == 0 {
			key.ep = 1
		}
		if _, ok := gangs[key]; !ok {
			gangs[key] = map[string]bool{}
			order = append(order, key)
		}
		gangs[key][id] = true
	}

	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.tp != b.tp {
			return a.tp < b.tp
		}
		if a.ep != b.ep {
			return a.ep < b.ep
		}
		return a.dp < b.dp
	})

	total := 0
	for _, key := range order {
		members := gangs[key]
		needed := int(key.tp * key.ep)
		if len(members) >= needed {
			// The gang is complete. DP ranks are independent gangs: the
			// member count across dp ranks contributes as full gangs.
			total += len(members)
		}
	}
	return total
}

// MemberBlocked reports whether the worker's own parallel group is
// incomplete (or the worker itself unhealthy) — the per-member form of
// the gang gate used by the router.
func (g *GangRegistry) MemberBlocked(workerID string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	e, ok := g.workers[workerID]
	if !ok {
		return true
	}
	if e.Quarantined || e.Lapsed || !e.Card.Servable() {
		return true
	}
	tp, dp, ep := e.Card.TP, e.Card.DP, e.Card.EP
	if tp == 0 {
		tp = 1
	}
	if dp == 0 {
		dp = 1
	}
	if ep == 0 {
		ep = 1
	}
	needed := int(tp * ep)
	count := 0
	for id, o := range g.workers {
		if o.Card.ModelName != e.Card.ModelName {
			continue
		}
		if o.Quarantined || o.Lapsed || !o.Card.Servable() {
			continue
		}
		otp, odp, oep := o.Card.TP, o.Card.DP, o.Card.EP
		if otp == 0 {
			otp = 1
		}
		if odp == 0 {
			odp = 1
		}
		if oep == 0 {
			oep = 1
		}
		if otp == tp && oep == ep && odp == dp {
			count++
		}
		_ = id
	}
	return count < needed
}
