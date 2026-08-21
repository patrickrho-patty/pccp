package scheduler

import (
	"errors"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// dispatch.go implements S2 late binding: the global queue holds requests
// until a worker with matching capability and free capacity exists, then
// binds them (spec §14 row 2; llm-d "scheduling regret" avoidance). Worker
// selection is capability matching here; the cost model replaces it in S3.

// WorkerSelector picks a serving worker for a model, reading from the
// shared WorkerFleet (PAT-1445 B1: one worker-state module). With no
// fleet supplied it owns a private one (tests).
type WorkerSelector struct {
	fleet *WorkerFleet
}

// NewWorkerSelector builds a selector over the given fleet (or a private
// one when omitted). The first supplied fleet wins.
func NewWorkerSelector(fleets ...*WorkerFleet) *WorkerSelector {
	f := NewWorkerFleet()
	if len(fleets) > 0 && fleets[0] != nil {
		f = fleets[0]
	}
	return &WorkerSelector{fleet: f}
}

// Upsert installs or refreshes a worker's capability entry. Load state
// survives re-upserts (heartbeats refresh cards, not load).
func (s *WorkerSelector) Upsert(e WorkerEntry, _ int) {
	id := e.Card.WorkerID
	if !s.fleet.Mutate(id, func(w *FleetWorker) { w.Entry = e }) {
		s.fleet.Upsert(e, RouterWorkerState{
			Load: WorkerLoad{MaxConcurrent: int(e.Card.MaxConcurrentSeqs)},
		})
	}
}

// SetLoad updates a worker's layer-2 load picture (active seqs + local
// buffer) and recomputes the max-concurrency bound from the card.
func (s *WorkerSelector) SetLoad(workerID string, active, localQueued int) {
	s.fleet.Mutate(workerID, func(w *FleetWorker) {
		w.State.Load.MaxConcurrent = int(w.Entry.Card.MaxConcurrentSeqs)
		w.State.Load.Active = active
		w.State.Load.LocalQueued = localQueued
	})
}

// Select returns a worker serving the model with free capacity, skipping
// quarantined, lapsed, and degraded entries. S2 matching is
// capability-only; the S3 cost model supersedes the tie-break.
func (s *WorkerSelector) Select(model string) (string, bool) {
	now := time.Now()
	best := ""
	bestLoad := -1
	for _, w := range s.fleet.List() {
		e := w.Entry
		id := e.Card.WorkerID
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
		l := w.State.Load
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
	s.fleet.Remove(workerID)
}

// Count returns the number of tracked workers.
func (s *WorkerSelector) Count() int {
	return len(s.fleet.List())
}

// Dispatch is a binding of one queued request to one worker. Plan is the
// request's execution path (co-located by default; a disaggregated plan
// carries the prefill worker and priced transfer).
type Dispatch struct {
	Request  queue.Request
	WorkerID string
	Model    string
	Plan     StagePlan
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
	mu          sync.Mutex
	queue       *queue.Queue
	selector    *WorkerSelector
	policy      OverloadPolicy
	est         *OutputEstimator
	router      *CostRouter
	planner     *StagePlanner
	programs    *ProgramRegistry
	stageQueues *StageQueues

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
	d := &Dispatcher{
		queue:   q,
		policy:  DefaultOverloadPolicy(),
		est:     NewOutputEstimator(DefaultEstimatorConfig()),
		pending: make(map[string]*pendingWaiter),
	}
	return d
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
	var active, maxCap int
	now := time.Now()
	for _, w := range d.selector.fleet.List() {
		e := w.Entry
		if e.Quarantined || e.Lapsed || now.After(e.LeasedUntil) {
			continue
		}
		l := w.State.Load
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
				if q := d.stageQueues; q != nil {
					q.Leave(StageLookup, out.Request.ID)
				}
				d.recordTrace(traceEventFor(*out.Request, TraceExpired))
				d.submitResult(out.Request.ID, InferenceResult{Err: "scheduler: queued request expired; retry", Cancelled: true})
			case queue.OutcomeDrained:
				if q := d.stageQueues; q != nil {
					q.Leave(StageLookup, out.Request.ID)
				}
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
			Cache:                cacheIdentityFor(out.Request.Cache),
			PrefixHash:           out.Request.PrefixHash,
			InputTokens:          out.Request.InputTokens,
			CachedTokens:         out.Request.CachedInputTokens,
			ExpectedOutputTokens: out.Request.ExpectedOutputTokens,
			RequestClass:         requestClassFor(out.Request.Class),
		})
		if err != nil {
			// Bounded early rejection (WS3): a structurally unroutable
			// request (no model/region/pool path at any load level) gets an
			// honest retryable answer now instead of parking to TTL. A
			// transient miss keeps late binding and requeues.
			var rerr *RouteError
			if errors.As(err, &rerr) && rerr.Permanent {
				if q := d.stageQueues; q != nil {
					q.Leave(StageLookup, out.Request.ID)
				}
				d.recordTrace(traceEventFor(*out.Request, TraceRejected))
				d.submitResult(out.Request.ID, InferenceResult{Err: err.Error(), Retryable: true})
				return nil
			}
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
	if q := d.stageQueues; q != nil {
		q.Leave(StageLookup, out.Request.ID)
	}
	var plan StagePlan
	if d.planner != nil {
		plan = d.planner.Plan(model, selected, out.Request.InputTokens)
		boundEvent.PlanMode = plan.Mode
		boundEvent.TransferMs = plan.TransferMs
	}
	d.recordTrace(boundEvent)
	return &Dispatch{Request: *out.Request, WorkerID: selected, Model: model, Plan: plan}
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

// SetStageQueues installs the stage measurement plane (criterion 7):
// lookup/prefill/transfer/decode depths and waits are recorded at
// lifecycle boundaries.
func (d *Dispatcher) SetStageQueues(q *StageQueues) {
	d.mu.Lock()
	d.stageQueues = q
	d.mu.Unlock()
}

// stageQueuesFor returns the measurement plane (nil-safe).
func (d *Dispatcher) stageQueuesFor() *StageQueues {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stageQueues
}

// programsFor returns the installed registry (nil = WS3 off) with the
// dispatcher lock held — Submit/execute read it off the hot path.
func (d *Dispatcher) programsFor() *ProgramRegistry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.programs
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
	now := time.Now()
	seen := map[string]bool{}
	var out []string
	for _, w := range d.selector.fleet.List() {
		e := w.Entry
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
	now := time.Now()
	var out []string
	for _, w := range s.fleet.List() {
		e := w.Entry
		id := e.Card.WorkerID
		if e.Quarantined || e.Lapsed || now.After(e.LeasedUntil) {
			continue
		}
		if !e.Card.Servable() {
			continue
		}
		if w.State.Load.CanAccept() {
			out = append(out, id)
		}
	}
	return out
}

// WorkerAddr returns the card's DARI dispatch address for a worker.
func (s *WorkerSelector) WorkerAddr(workerID string) (string, bool) {
	w, ok := s.fleet.Get(workerID)
	if !ok {
		return "", false
	}
	return w.Entry.Card.DariAddr, w.Entry.Card.DariAddr != ""
}

// ReserveLoad atomically marks one more active sequence on a worker.
// Returns false when the worker has no capacity left (double-check the
// bind before forwarding).
func (s *WorkerSelector) ReserveLoad(workerID string) bool {
	reserved := false
	s.fleet.Mutate(workerID, func(w *FleetWorker) {
		if w.State.Load.CanAccept() {
			w.State.Load.Active++
			reserved = true
		}
	})
	return reserved
}

// ReleaseLoad marks one active sequence finished on a worker and wakes
// the dispatch loop (a slot freed).
func (s *WorkerSelector) ReleaseLoad(workerID string) {
	s.fleet.Mutate(workerID, func(w *FleetWorker) {
		if w.State.Load.Active > 0 {
			w.State.Load.Active--
		}
	})
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

// cacheIdentityFor converts the queue package's identity mirror to the
// scheduler's directory identity.
func cacheIdentityFor(c queue.CacheIdentity) CacheIdentity {
	return CacheIdentity{
		ModelPackage: c.ModelPackage,
		TokenizerID:  c.TokenizerID,
		TemplateID:   c.TemplateID,
		AdapterID:    c.AdapterID,
		PolicyEpoch:  c.PolicyEpoch,
	}
}
