package scheduler

import "sync"

// fleet.go implements the single worker-state module (PAT-1445 B1): one
// authoritative copy of live worker entries + load, fed by the signed
// card feed (SyncRouter) and read by every placement consumer (router,
// P/D planner, selector, topology). Removal propagates to all readers.

// FleetWorker is one worker's entry plus live state.
type FleetWorker struct {
	Entry WorkerEntry
	State RouterWorkerState
}

// WorkerFleet is the fleet-wide worker view. Safe for concurrent use.
type WorkerFleet struct {
	mu      sync.RWMutex
	workers map[string]FleetWorker
}

// NewWorkerFleet builds an empty fleet.
func NewWorkerFleet() *WorkerFleet {
	return &WorkerFleet{workers: make(map[string]FleetWorker)}
}

// Upsert installs or refreshes a worker.
func (f *WorkerFleet) Upsert(e WorkerEntry, s RouterWorkerState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workers[e.Card.WorkerID] = FleetWorker{Entry: e, State: s}
}

// Remove drops a worker (eviction, lease lapse).
func (f *WorkerFleet) Remove(workerID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.workers, workerID)
}

// Get returns one worker.
func (f *WorkerFleet) Get(workerID string) (FleetWorker, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	w, ok := f.workers[workerID]
	return w, ok
}

// List returns all workers (map iteration order — callers sort when
// deterministic order matters).
func (f *WorkerFleet) List() []FleetWorker {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]FleetWorker, 0, len(f.workers))
	for _, w := range f.workers {
		out = append(out, w)
	}
	return out
}

// Mutate applies fn to one worker's state atomically. Reports whether
// the worker exists (load updates on vanished workers are dropped, not
// resurrected).
func (f *WorkerFleet) Mutate(workerID string, fn func(*FleetWorker)) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.workers[workerID]
	if !ok {
		return false
	}
	fn(&w)
	f.workers[workerID] = w
	return true
}
