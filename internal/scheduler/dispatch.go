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
		if !e.Card.Servable() {
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

// RequestPayload is what a queued gateway request carries: the resolved
// model plus the raw message JSON for transparent passthrough to the PIA.
type RequestPayload struct {
	Model    string
	Messages []byte
}

// Dispatcher owns the global queue and worker selection, and implements
// the overload gate on fleet signals. Workers call Assign when a slot
// frees; the dispatcher releases the next eligible request or nothing.
// Submit/Cancel manage the pending-waiter side of the dispatch loop.
// The S3 cost router supersedes capability-only selection when installed.
type Dispatcher struct {
	mu       sync.Mutex
	queue    *queue.Queue
	selector *WorkerSelector
	policy   OverloadPolicy
	est      *OutputEstimator
	router   *CostRouter
	planner  *StagePlanner
	programs *ProgramRegistry

	fwMu      sync.Mutex
	forwarder Forwarder
	pending   map[string]*pendingWaiter
	trace     *TraceRecorder
}

// NewDispatcher builds a dispatcher with default overload policy and a
// fresh estimator. A nil queue is replaced with default limits.
func NewDispatcher(q *queue.Queue) *Dispatcher {
	if q == nil {
		q = queue.New(queue.DefaultLimits())
	}
	return &Dispatcher{
		queue:   q,
		policy:  DefaultOverloadPolicy(),
		est:     NewOutputEstimator(DefaultEstimatorConfig()),
		pending: make(map[string]*pendingWaiter),
	}
}

// Queue exposes the underlying global queue (enqueue path).
func (d *Dispatcher) Queue() *queue.Queue { return d.queue }

// SetSelector installs the worker selector and wakes the dispatch loop.
// The write takes the dispatcher lock: the dispatch loop reads
// d.selector concurrently (worker registration can race an in-flight
// drain).
func (d *Dispatcher) SetSelector(s *WorkerSelector) {
	d.mu.Lock()
	d.selector = s
	d.mu.Unlock()
	d.wakeDispatch()
}

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
	return d.assignLocked(workerID)
}

// assignLocked is Assign with the dispatcher lock already held.
func (d *Dispatcher) assignLocked(workerID string) *Dispatch {
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
		// The dequeued head expired (or was drained): complete its
		// waiter immediately so the parked HTTP handler unblocks with
		// the right error instead of waiting out its own timer.
		if out.Request != nil {
			switch out.Outcome {
			case queue.OutcomeExpiredTTL:
				d.recordTrace(traceEventFor(*out.Request, TraceExpired))
				d.submitResult(out.Request.ID, InferenceResult{Err: "scheduler: queued request expired; retry", Cancelled: true})
			case queue.OutcomeDrained:
				d.recordTrace(traceEventFor(*out.Request, TraceDrained))
				d.submitResult(out.Request.ID, InferenceResult{Err: "scheduler: shutting down", Cancelled: true})
			}
		}
		return nil
	}
	rp, ok := out.Request.Payload.(RequestPayload)
	if !ok || rp.Model == "" {
		return nil
	}
	model := rp.Model
	if d.selector == nil {
		return nil
	}
	// S3 cost-model routing when installed; capability matching otherwise.
	var selected string
	if d.router != nil {
		decision, err := d.router.Route(RouteRequest{
			Model:                model,
			Namespace:            out.Request.Tenant,
			Region:               out.Request.Region,
			InputTokens:          out.Request.InputTokens,
			CachedTokens:         out.Request.CachedInputTokens,
			ExpectedOutputTokens: out.Request.ExpectedOutputTokens,
			RequestClass:         requestClassFor(out.Request.Class),
		})
		if err != nil {
			// No eligible worker for the head request: requeue and wait
			// (late binding).
			d.queue.Enqueue(*out.Request)
			return nil
		}
		selected = decision.WorkerID
	} else {
		var ok bool
		selected, ok = d.selector.Select(model)
		if !ok {
			// Late binding: no worker can take this request yet — requeue
			// at the head of its flow and wait for a compatible slot.
			d.queue.Enqueue(*out.Request)
			return nil
		}
	}
	if selected != workerID {
		// The freed worker is not the router's choice; requeue and let
		// the chosen worker's free-slot event trigger binding.
		d.queue.Enqueue(*out.Request)
		return nil
	}
	// Optimistic reservation at bind (spec §12.3.7 layer 2): between
	// heartbeats the load picture is stale, so successive binds must
	// each consume a slot — otherwise one drain could stack unbounded
	// in-flight work onto a "free" worker. execute() releases on
	// completion; the next heartbeat's engine truth overwrites both.
	if !d.selector.ReserveLoad(selected) {
		d.queue.Enqueue(*out.Request)
		return nil
	}
	boundEvent := traceEventFor(*out.Request, TraceBound)
	boundEvent.WorkerID = selected
	boundEvent.QueueWaitMs = time.Since(out.Request.ArrivedAt).Milliseconds()
	if d.planner != nil {
		plan := d.planner.Plan(model, selected, out.Request.InputTokens)
		boundEvent.PlanMode = plan.Mode
		boundEvent.TransferMs = plan.TransferMs
	}
	d.recordTrace(boundEvent)
	return &Dispatch{Request: *out.Request, WorkerID: selected, Model: model}
}

// SetRouter installs the S3 cost-model router; selection switches from
// capability matching to lowest-cost placement with KV credits.
func (d *Dispatcher) SetRouter(r *CostRouter) {
	d.mu.Lock()
	d.router = r
	d.mu.Unlock()
}

// SetTraceRecorder installs the governed trace recorder (PAT-1445);
// arrival/binding/completion events are recorded content-free.
func (d *Dispatcher) SetTraceRecorder(t *TraceRecorder) {
	d.fwMu.Lock()
	d.trace = t
	d.fwMu.Unlock()
}

// SetStagePlanner installs the WS2 stage planner: each binding records
// its execution path (co-located or disaggregated plan) as trace
// evidence. Execution remains co-located until the PIA stage protocol
// lands — plans are shadow-grade decision evidence (PAT-1445 rollout).
func (d *Dispatcher) SetStagePlanner(p *StagePlanner) {
	d.mu.Lock()
	d.planner = p
	d.mu.Unlock()
}

// SetPrograms installs the WS3 program registry: turn arrivals and
// tool-pause completions drive pause-aware KV residency decisions.
func (d *Dispatcher) SetPrograms(p *ProgramRegistry) {
	d.mu.Lock()
	d.programs = p
	d.mu.Unlock()
}

// recordTrace appends one event when a recorder is installed.
func (d *Dispatcher) recordTrace(e TraceEvent) {
	d.fwMu.Lock()
	t := d.trace
	d.fwMu.Unlock()
	if t != nil {
		t.Add(e)
	}
}

// RejectSheddable removes queued requests of shed classes during overload
// (multi-level load shedding, spec §14 row 38). Called on gate transitions;
// each removed request maps to a retryable 429 on the wire.
func (d *Dispatcher) RejectSheddable() int {
	removed := d.queue.DropClass(queue.ClassBatch, queue.ClassBackgroundAgent)
	return len(removed)
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

// FreeWorkers returns every worker with at least one free slot.
func (s *WorkerSelector) FreeWorkers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	var out []string
	for id, e := range s.workers {
		if e.Quarantined || e.Lapsed || now.After(e.LeasedUntil) {
			continue
		}
		if !e.Card.Servable() {
			continue
		}
		if s.load[id].CanAccept() {
			out = append(out, id)
		}
	}
	return out
}

// WorkerAddr returns the card's DARI dispatch address for a worker.
func (s *WorkerSelector) WorkerAddr(workerID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.workers[workerID]
	if !ok {
		return "", false
	}
	return e.Card.DariAddr, e.Card.DariAddr != ""
}

// ReserveLoad atomically marks one more active sequence on a worker.
// Returns false when the worker has no capacity left (double-check the
// bind before forwarding).
func (s *WorkerSelector) ReserveLoad(workerID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.load[workerID]
	if !l.CanAccept() {
		return false
	}
	l.Active++
	s.load[workerID] = l
	return true
}

// ReleaseLoad marks one active sequence finished on a worker and wakes
// the dispatch loop (a slot freed).
func (s *WorkerSelector) ReleaseLoad(workerID string) {
	s.mu.Lock()
	l := s.load[workerID]
	if l.Active > 0 {
		l.Active--
	}
	s.load[workerID] = l
	s.mu.Unlock()
	dispatchWake <- struct{}{}
}

// requestClassFor maps the queue traffic class to the SLO-scoping class
// (agentic requests carry tighter objectives, spec §14 row 28).
func requestClassFor(c queue.Class) string {
	switch c {
	case queue.ClassBackgroundAgent:
		return "agentic"
	case queue.ClassBatch:
		return "batch"
	default:
		return "interactive"
	}
}
