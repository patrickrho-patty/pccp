package scheduler

import (
	"fmt"
	"sync"
)

// lifecycle.go implements S7 engine lifecycle management (spec §14 rows
// 20–21, 39): a strict per-worker state machine for start/readiness/
// drain/sleep/wake/pause/resume/reload/terminate, the warm-pool
// inventory, LoRA adapter residency with popularity-based eviction, and
// heterogeneous cost-aware family selection.

// EngineLifecycle is the per-worker state machine. Transitions follow
// the locked sequence; terminal states refuse everything except restart.
type EngineLifecycle struct {
	mu     sync.RWMutex
	states map[string]string
}

// NewEngineLifecycle builds the state machine.
func NewEngineLifecycle() *EngineLifecycle {
	return &EngineLifecycle{states: make(map[string]string)}
}

// allowed transitions from → set of to states.
var lifecycleAllowed = map[string]map[string]bool{
	"":           {"starting": true},
	"starting":   {"ready": true, "terminated": true},
	"ready":      {"draining": true, "paused": true, "reloading": true},
	"draining":   {"sleeping": true, "terminated": true},
	"sleeping":   {"starting": true}, // wake
	"paused":     {"ready": true},    // resume
	"reloading":  {"ready": true},
	"terminated": {"starting": true}, // restart
}

// Transition moves a worker between states, refusing illegal moves.
func (l *EngineLifecycle) Transition(workerID, from, to, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, known := l.states[workerID]
	if !known && from != "" {
		return fmt.Errorf("lifecycle: worker %s unknown", workerID)
	}
	if known && cur != from {
		return fmt.Errorf("lifecycle: worker %s is %s, not %s", workerID, cur, from)
	}
	allowed, ok := lifecycleAllowed[from]
	if !ok || !allowed[to] {
		return fmt.Errorf("lifecycle: illegal transition %s→%s", from, to)
	}
	l.states[workerID] = to
	return nil
}

// State returns a worker's lifecycle state ("" when unknown).
func (l *EngineLifecycle) State(workerID string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.states[workerID]
}

// WarmPool tracks standby workers per model (spec §13.7: residency and
// pre-warming are core paths, not preview).
type WarmPool struct {
	mu      sync.RWMutex
	workers map[string]string // workerID → model
}

// NewWarmPool builds an empty pool.
func NewWarmPool() *WarmPool {
	return &WarmPool{workers: make(map[string]string)}
}

// Add registers a warm worker.
func (w *WarmPool) Add(workerID, model string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.workers[workerID] = model
}

// Remove drops a worker.
func (w *WarmPool) Remove(workerID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.workers, workerID)
}

// ReadyCount returns warm workers for a model.
func (w *WarmPool) ReadyCount(model string) int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	n := 0
	for _, m := range w.workers {
		if m == model {
			n++
		}
	}
	return n
}

// Need returns how many more warm workers a model needs to hit target.
func (w *WarmPool) Need(model string, target int) int {
	have := w.ReadyCount(model)
	if have >= target {
		return 0
	}
	return target - have
}

// LoRaLifecycle tracks adapter residency with popularity-based eviction
// (spec §14 row 18).
type LoRaLifecycle struct {
	mu         sync.Mutex
	capacity   int
	loaded     map[string]map[string]bool // worker → set of adapters
	popularity map[string]map[string]int  // worker → adapter → touches
}

// NewLoRaLifecycle builds a residency tracker with the given per-worker
// adapter capacity.
func NewLoRaLifecycle(capacity int) *LoRaLifecycle {
	if capacity <= 0 {
		capacity = 4
	}
	return &LoRaLifecycle{
		capacity:   capacity,
		loaded:     make(map[string]map[string]bool),
		popularity: make(map[string]map[string]int),
	}
}

// Touch records one use of an adapter (popularity signal).
func (l *LoRaLifecycle) Touch(workerID, adapter string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.popularity[workerID]
	if !ok {
		p = make(map[string]int)
		l.popularity[workerID] = p
	}
	p[adapter]++
}

// Loads the adapter into the worker, evicting the least popular when at
// capacity.
func (l *LoRaLifecycle) Load(workerID, adapter string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	loaded, ok := l.loaded[workerID]
	if !ok {
		loaded = make(map[string]bool)
		l.loaded[workerID] = loaded
	}
	if loaded[adapter] {
		return nil
	}
	if len(loaded) >= l.capacity {
		// Evict the least popular resident adapter (ties: first seen).
		victim := ""
		lowest := int(1 << 30)
		for a := range loaded {
			pop := l.popularity[workerID][a]
			if pop < lowest {
				lowest = pop
				victim = a
			}
		}
		if victim == "" {
			return fmt.Errorf("lora: worker %s at capacity with no evictable adapter", workerID)
		}
		delete(loaded, victim)
	}
	loaded[adapter] = true
	return nil
}

// Loaded reports whether the adapter is resident.
func (l *LoRaLifecycle) Loaded(workerID, adapter string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loaded[workerID][adapter]
}

// CostOptimizer picks accelerator families by workload class (spec §14
// row 39: cheaper HW for background; premium for interactive).
type CostOptimizer struct {
	mu    sync.RWMutex
	costs map[string]float64 // family → relative cost per token
}

// NewCostOptimizer builds the optimizer.
func NewCostOptimizer() *CostOptimizer {
	return &CostOptimizer{costs: make(map[string]float64)}
}

// SetFamilyCost registers a family's relative cost.
func (c *CostOptimizer) SetFamilyCost(family string, cost float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.costs[family] = cost
}

// PickFamily selects the family for a traffic class: cheapest for
// batch/background, premium (most expensive = fastest assumption) for
// interactive.
func (c *CostOptimizer) PickFamily(class string, available []string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(available) == 0 {
		return ""
	}
	best := available[0]
	bestCost := c.costs[best]
	for _, f := range available[1:] {
		cost := c.costs[f]
		switch class {
		case "batch", "background-agent":
			if cost < bestCost {
				best, bestCost = f, cost
			}
		default:
			if cost > bestCost {
				best, bestCost = f, cost
			}
		}
	}
	return best
}
