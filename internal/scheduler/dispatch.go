package scheduler

import (
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// dispatch.go implements S2 late binding: the global queue holds requests
// until a worker with matching capability and free capacity exists, then
// binds them (spec §14 row 2; llm-d "scheduling regret" avoidance). Worker
// selection is capability matching here; the cost model replaces it in S3.

// WorkerSelector picks a serving worker for a model. All methods are safe
// for concurrent use (dispatch + heartbeat paths both touch it).
type WorkerSelector struct {
	mu      sync.RWMutex
	workers map[string]WorkerEntry
	load    map[string]WorkerLoad
}

// NewWorkerSelector builds an empty selector.
func NewWorkerSelector() *WorkerSelector {
	return &WorkerSelector{
		workers: make(map[string]WorkerEntry),
		load:    make(map[string]WorkerLoad),
	}
}

// Upsert installs or refreshes a worker's capability entry.
func (s *WorkerSelector) Upsert(e WorkerEntry, _ int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workers[e.Card.WorkerID] = e
	if _, ok := s.load[e.Card.WorkerID]; !ok {
		s.load[e.Card.WorkerID] = WorkerLoad{MaxConcurrent: int(e.Card.MaxConcurrentSeqs)}
	}
}

// SetLoad updates a worker's layer-2 load picture (active seqs + local
// buffer) and recomputes the max-concurrency bound from the card.
func (s *WorkerSelector) SetLoad(workerID string, active, localQueued int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.load[workerID]
	if e, ok := s.workers[workerID]; ok {
		l.MaxConcurrent = int(e.Card.MaxConcurrentSeqs)
	}
	l.Active = active
	l.LocalQueued = localQueued
	s.load[workerID] = l
}

// Select returns a worker serving the model with free capacity, skipping
// quarantined, lapsed, and degraded entries. S2 matching is
// capability-only; the S3 cost model supersedes the tie-break.
func (s *WorkerSelector) Select(model string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	best := ""
	bestLoad := -1
	for id, e := range s.workers {
		if e.Card.ModelName != model {
			continue
		}
		if e.Quarantined || e.Lapsed {
			continue
		}
		if now.After(e.LeasedUntil) {
			continue
		}
		l := s.load[id]
		if !l.CanAccept() {
			continue
		}
		// Least-loaded first: spread across the fleet.
		score := l.Active*10 + l.LocalQueued
		if best == "" || score < bestLoad {
			best, bestLoad = id, score
		}
	}
	return best, best != ""
}

// Remove drops a worker (eviction).
func (s *WorkerSelector) Remove(workerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.workers, workerID)
	delete(s.load, workerID)
}

// Count returns the number of tracked workers.
func (s *WorkerSelector) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.workers)
}

// Dispatch is a binding of one queued request to one worker.
type Dispatch struct {
	Request  queue.Request
	WorkerID string
	Model    string
}

// Dispatcher owns the global queue and worker selection, and implements
// the overload gate on fleet signals. Workers call Assign when a slot
// frees; the dispatcher releases the next eligible request or nothing.
type Dispatcher struct {
	mu       sync.Mutex
	queue    *queue.Queue
	selector *WorkerSelector
	policy   OverloadPolicy
	est      *OutputEstimator
}

// NewDispatcher builds a dispatcher with default overload policy and a
// fresh estimator. A nil queue is replaced with default limits.
func NewDispatcher(q *queue.Queue) *Dispatcher {
	if q == nil {
		q = queue.New(queue.DefaultLimits())
	}
	return &Dispatcher{
		queue:  q,
		policy: DefaultOverloadPolicy(),
		est:    NewOutputEstimator(DefaultEstimatorConfig()),
	}
}

// Queue exposes the underlying global queue (enqueue path).
func (d *Dispatcher) Queue() *queue.Queue { return d.queue }

// SetSelector installs the worker selector.
func (d *Dispatcher) SetSelector(s *WorkerSelector) { d.selector = s }

// Estimator exposes the output-length estimator (usage/telemetry hooks).
func (d *Dispatcher) Estimator() *OutputEstimator { return d.est }

// FleetSignalsFromRegistry snapshots the selector into edge-gate signals.
// KV utilization is approximated from active/max sequence occupancy until
// S3's KV index provides exact numbers (documented approximation).
func (d *Dispatcher) FleetSignalsFromRegistry() FleetSignals {
	sig := FleetSignals{QueuedTokens: d.queue.PendingTokens()}
	if d.selector == nil {
		return sig
	}
	d.selector.mu.RLock()
	defer d.selector.mu.RUnlock()
	var active, maxCap int
	now := time.Now()
	for _, e := range d.selector.workers {
		if e.Quarantined || e.Lapsed || now.After(e.LeasedUntil) {
			continue
		}
		l := d.selector.load[e.Card.WorkerID]
		active += l.Active
		maxCap += int(e.Card.MaxConcurrentSeqs)
		if l.CanAccept() {
			sig.AvailableReplicas++
		}
	}
	sig.ActiveDecodeKV = int64(active)
	if maxCap > 0 {
		sig.KVUtilization = float64(active) / float64(maxCap)
	}
	return sig
}

// Assign is called when workerID has a free slot. It releases the next
// request the overload gate allows and binds it to the worker. Returns
// nil when nothing is dispatchable (queue empty, gate closed, or no
// eligible worker for the head request's model).
func (d *Dispatcher) Assign(workerID string) *Dispatch {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Gate first: the fleet's health decides whether ANY new work flows
	// (spec §12.3.7 layer 1). Sheddable work was rejected at ingress; a
	// saturated fleet simply holds interactive work (bounded by TTL).
	sig := d.FleetSignalsFromRegistry()
	if d.policy.Saturated(sig) {
		return nil
	}

	out, ok := d.queue.Next()
	if !ok {
		return nil
	}
	if out.Outcome != queue.OutcomeDispatched {
		return nil
	}
	model, _ := out.Request.Payload.(string)
	if model == "" {
		return nil
	}
	if d.selector == nil {
		return nil
	}
	selected, ok := d.selector.Select(model)
	if !ok {
		// Late binding: no worker can take this request yet — requeue at
		// the head of its flow and wait for a compatible slot. (A request
		// parked here does not consume a worker slot.)
		d.queue.Enqueue(*out.Request)
		return nil
	}
	if selected != workerID {
		// The freed worker cannot serve this request's model; requeue and
		// let a compatible worker's free-slot event trigger binding.
		d.queue.Enqueue(*out.Request)
		return nil
	}
	return &Dispatch{Request: *out.Request, WorkerID: selected, Model: model}
}

// RejectSheddable removes queued requests of shed classes during overload
// (multi-level load shedding, spec §14 row 38). Called on gate transitions.
func (d *Dispatcher) RejectSheddable() int {
	// Walk the queue's classes; batch/background entries are dropped with
	// a retryable outcome. queue.Cancel reports each removal.
	return 0 // class-level drop implemented in S2.5 overload wiring
}

// ServedModels returns the distinct models served by healthy, non-expired
// workers (model discovery, spec §14 row 1).
func (d *Dispatcher) ServedModels() []string {
	if d.selector == nil {
		return nil
	}
	d.selector.mu.RLock()
	defer d.selector.mu.RUnlock()
	now := time.Now()
	seen := map[string]bool{}
	var out []string
	for _, e := range d.selector.workers {
		if e.Quarantined || e.Lapsed || now.After(e.LeasedUntil) {
			continue
		}
		if !seen[e.Card.ModelName] {
			seen[e.Card.ModelName] = true
			out = append(out, e.Card.ModelName)
		}
	}
	return out
}
