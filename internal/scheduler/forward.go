package scheduler

import (
	"context"
	"time"

	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// forward.go implements the S2 dispatch execution: submitted requests wait
// in the global queue; a dispatch loop binds them to workers when capacity
// frees (Assign), forwards them to the worker's PIA via the Forwarder, and
// completes the submitter. Cancellation propagates from the wire to the
// pending waiter.

// InferencePayload is the normalized request handed to a worker's PIA.
type InferencePayload struct {
	Model       string
	Messages    []byte // raw message JSON (transparent passthrough)
	MaxTokens   int
	Temperature float64
}

// InferenceResult is the completion handed back to the submitter.
type InferenceResult struct {
	Text      string
	Finish    string
	Usage     map[string]int
	Cancelled bool
	Retryable bool // the error is safe to retry later (bounded early rejection)
	Err       string
}

// Forwarder sends an inference payload to a worker's PIA and blocks for
// the completion. The scheduler is the only caller; the worker's DARI
// address comes from the signed capability card (never from headers).
type Forwarder interface {
	Send(workerAddr string, payload InferencePayload) (InferenceResult, error)
}

// StreamForwarder is a Forwarder that also emits token deltas as they
// arrive (spec §14 row 1 streaming; relay F1 pattern).
type StreamForwarder interface {
	Forwarder
	SendStream(workerAddr string, payload InferencePayload, onDelta func(string)) (InferenceResult, error)
}

// StageForwarder executes a two-stage disaggregated plan (WS2): prefill
// on one worker returns an opaque KV handle; decode consumes it on the
// router's chosen worker. The transfer between stages is engine-internal
// and priced by the NetworkOracle. A plan that fails mid-stage falls
// back to co-located execution on the decode worker (conservative).
type StageForwarder interface {
	Forwarder
	SendPrefill(workerAddr string, payload InferencePayload) (kvHandle string, err error)
	SendDecode(workerAddr, kvHandle string, payload InferencePayload) (InferenceResult, error)
}

// pendingWaiter is a submitter blocked on completion or cancellation.
// deltas (optional) delivers streamed token deltas; it is closed exactly
// once when the request terminates.
type pendingWaiter struct {
	ch         chan InferenceResult
	cancel     chan struct{}
	deltas     chan string
	deltasDone bool // guarded by Dispatcher.fwMu
}

// dispatchWake is a non-blocking signal that the dispatch loop should run.
var dispatchWake = make(chan struct{}, 1)

// wakeDispatch nudges the dispatch loop (coalesced; the loop drains).
func (d *Dispatcher) wakeDispatch() {
	select {
	case dispatchWake <- struct{}{}:
	default:
	}
}

// Submit enqueues a request and returns a channel delivering exactly one
// terminal result (completion, forward error, or cancellation). The
// waiter is registered BEFORE the request enters the queue: the dispatch
// loop also drains on the 500ms reap ticker, so a request enqueued first
// could be completed before its waiter existed — the result would drop
// silently and the caller would strand for the full TTL. Registration
// first, enqueue second, roll back on failure.
func (d *Dispatcher) Submit(qr queue.Request) (<-chan InferenceResult, error) {
	w := &pendingWaiter{
		ch:     make(chan InferenceResult, 1),
		cancel: make(chan struct{}, 1),
	}
	d.fwMu.Lock()
	d.pending[qr.ID] = w
	d.fwMu.Unlock()
	if err := d.queue.Enqueue(qr); err != nil {
		d.fwMu.Lock()
		delete(d.pending, qr.ID)
		d.fwMu.Unlock()
		return nil, err
	}
	d.recordTrace(traceEventFor(qr, TraceArrived))
	if q := d.stageQueuesFor(); q != nil {
		q.Enter(StageLookup, qr.ID)
	}
	if p := d.programsFor(); p != nil && qr.ProgramID != "" {
		p.Turn(qr.ProgramID, qr.Tenant, "", CacheIdentity{}, "", qr.TurnSeq, qr.SLOBudget)
	}
	d.wakeDispatch()
	return w.ch, nil
}

// SubmitStream is Submit with streaming: token deltas flow to the caller
// as the forwarder emits them; the result channel still delivers exactly
// one terminal result. The deltas channel closes when the request ends.
// Same registration-first ordering as Submit.
func (d *Dispatcher) SubmitStream(qr queue.Request) (<-chan InferenceResult, <-chan string, error) {
	w := &pendingWaiter{
		ch:     make(chan InferenceResult, 1),
		cancel: make(chan struct{}, 1),
		deltas: make(chan string, 64),
	}
	d.fwMu.Lock()
	d.pending[qr.ID] = w
	d.fwMu.Unlock()
	if err := d.queue.Enqueue(qr); err != nil {
		d.fwMu.Lock()
		delete(d.pending, qr.ID)
		d.fwMu.Unlock()
		return nil, nil, err
	}
	d.recordTrace(traceEventFor(qr, TraceArrived))
	d.wakeDispatch()
	return w.ch, w.deltas, nil
}

// Cancel removes a queued or pending request and completes its waiter with
// a cancellation result. Reports whether the request existed.
func (d *Dispatcher) Cancel(id string) bool {
	removed := d.queue.Cancel(id)
	d.fwMu.Lock()
	w, ok := d.pending[id]
	if ok {
		delete(d.pending, id)
	}
	d.fwMu.Unlock()
	if removed || ok {
		d.recordTrace(TraceEvent{RequestID: id, Stage: TraceCancelled})
		if q := d.stageQueuesFor(); q != nil {
			q.Leave(StageLookup, id)
		}
	}
	if w != nil {
		select {
		case w.ch <- InferenceResult{Cancelled: true}:
		default:
		}
		d.closeDeltas(w)
	}
	return removed || ok
}

// closeDeltas closes the waiter's delta stream exactly once.
func (d *Dispatcher) closeDeltas(w *pendingWaiter) {
	if w.deltas == nil {
		return
	}
	d.fwMu.Lock()
	done := w.deltasDone
	if !done {
		w.deltasDone = true
	}
	d.fwMu.Unlock()
	if !done {
		close(w.deltas)
	}
}

// RunDispatchLoop processes free-worker events until ctx ends. Each event
// asks the selector which workers have capacity, binds queued requests to
// them, forwards to the PIA, and completes waiters. Forwarding runs with
// bounded concurrency (one goroutine per in-flight dispatch, capped by
// worker capacity in practice).
func (d *Dispatcher) RunDispatchLoop(ctx context.Context) {
	// Periodic reap: TTL expiry and drain outcomes complete waiters when
	// the queue pops them, but pops only happen on wake events. An idle
	// fleet would otherwise hold expired heads (and their parked
	// waiters) until the next Submit/Assign; a slow tick keeps expiry
	// bounded without busy-waiting.
	reap := time.NewTicker(500 * time.Millisecond)
	defer reap.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-dispatchWake:
		case <-reap.C:
		}
		if d.queue.Pending() > 0 {
		}
		d.drainDispatch(ctx)
	}
}

// drainDispatch binds and executes all currently assignable work.
func (d *Dispatcher) drainDispatch(ctx context.Context) {
	for {
		bound := d.AssignAnyFreeWorker()
		if bound == nil {
			return
		}
		go d.execute(ctx, bound)
	}
}

// AssignAnyFreeWorker binds the next eligible queued request to a worker
// with a free slot. When the queue head's model is not served by the
// first free worker, the request is requeued and the next free worker is
// tried, so a free slot never deadlocks the queue head. Returns nil when
// no free worker can serve the head (the queue waits for a compatible
// slot — late binding).
func (d *Dispatcher) AssignAnyFreeWorker() *Dispatch {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.selector == nil {
		return nil
	}
	free := d.selector.FreeWorkers()
	if len(free) == 0 && d.queue.Pending() > 0 {
	}
	for _, workerID := range free {
		if bound := d.assignLocked(workerID); bound != nil {
			return bound
		}
	}
	return nil
}

// execute forwards one bound request and completes its waiter. Decode
// accounting wraps admission+execution on every path (co-located or
// staged); prefill/transfer are measured inside the staged path.
func (d *Dispatcher) execute(ctx context.Context, bound *Dispatch) {
	req := bound.Request
	workerID := bound.WorkerID
	if q := d.stageQueuesFor(); q != nil {
		q.Enter(StageDecode, req.ID)
		defer q.Leave(StageDecode, req.ID)
	}

	fw := d.forwarderFor()
	if fw == nil {
		// No forwarder: complete the waiter with an explicit error and
		// DROP the request. Requeueing after the client has already
		// received the error would execute (and bill) the work twice.
		d.selector.ReleaseLoad(workerID)
		d.submitResult(req.ID, InferenceResult{Err: "scheduler: no forwarder configured"})
		return
	}

	addr, ok := d.selector.WorkerAddr(workerID)
	if !ok {
		// Same rule: error the waiter, drop the request. The selector
		// is drifting (worker vanished mid-binding); the client retries.
		d.selector.ReleaseLoad(workerID)
		d.submitResult(req.ID, InferenceResult{Err: "scheduler: worker " + workerID + " has no dispatch address"})
		return
	}

	rp, _ := req.Payload.(RequestPayload)
	payload := InferencePayload{
		Model:     bound.Model,
		Messages:  rp.Messages,
		MaxTokens: req.MaxOutputTokens,
	}

	// Streaming when the submitter wants deltas AND the forwarder speaks
	// StreamForwarder; otherwise one-shot Send. A disaggregated plan runs
	// the two-stage path first; any stage failure falls back to
	// co-located execution on the decode worker.
	waiter := d.waiterFor(req.ID)
	var res InferenceResult
	var err error
	staged := false
	if bound.Plan.Mode == StageDisaggregated {
		if stagedRes, ok := d.executeStaged(bound, fw, payload); ok {
			res = stagedRes
			staged = true
		}
	}
	if !staged {
		if sf, ok := fw.(StreamForwarder); ok && waiter != nil && waiter.deltas != nil {
			res, err = sf.SendStream(addr, payload, func(delta string) {
				select {
				case waiter.deltas <- delta:
				default: // bounded buffer full: drop, keep streaming
				}
			})
		} else {
			res, err = fw.Send(addr, payload)
		}
	}
	d.selector.ReleaseLoad(workerID)
	if err != nil {
		res = InferenceResult{Err: err.Error()}
	}
	completed := traceEventFor(req, TraceCompleted)
	completed.WorkerID = workerID
	completed.Err = res.Err
	if res.Usage != nil {
		if in, ok := res.Usage["prompt_tokens"]; ok {
			if out, ok2 := res.Usage["completion_tokens"]; ok2 {
				d.est.ObserveCompletion(in, out)
				completed.OutputTokens = out
			}
		}
	}
	d.recordTrace(completed)
	// A tool pause is only a pause when the request actually completed —
	// an errored turn did not end waiting on a tool.
	if p := d.programsFor(); p != nil && req.ProgramID != "" && req.ToolPaused && res.Err == "" {
		p.ToolPaused(req.ProgramID)
	}
	d.submitResult(req.ID, res)
	if waiter != nil {
		d.closeDeltas(waiter)
	}
}

// executeStaged runs a disaggregated plan: prefill on the plan's prefill
// worker, KV transfer (engine-internal, priced on the plan), decode on
// the router's chosen worker. Returns ok=false when the forwarder cannot
// stage or any stage fails — the caller then falls back to co-located
// execution on the decode worker (conservative, never a misroute).
func (d *Dispatcher) executeStaged(bound *Dispatch, fw Forwarder, payload InferencePayload) (InferenceResult, bool) {
	sf, ok := fw.(StageForwarder)
	if !ok {
		return InferenceResult{}, false
	}
	plan := bound.Plan
	prefillAddr, ok := d.selector.WorkerAddr(plan.PrefillWorker)
	if !ok || plan.PrefillWorker == "" {
		return InferenceResult{}, false
	}
	d.recordTrace(stageEventFor(bound, TracePrefill, plan.PrefillWorker))
	if q := d.stageQueuesFor(); q != nil {
		q.Enter(StagePrefill, bound.Request.ID)
	}
	kvHandle, err := sf.SendPrefill(prefillAddr, payload)
	if q := d.stageQueuesFor(); q != nil {
		q.Leave(StagePrefill, bound.Request.ID)
	}
	if err != nil {
		return InferenceResult{}, false
	}
	decodeAddr, ok := d.selector.WorkerAddr(plan.DecodeWorker)
	if !ok {
		return InferenceResult{}, false
	}
	d.recordTrace(stageEventFor(bound, TraceTransfer, plan.DecodeWorker))
	if q := d.stageQueuesFor(); q != nil {
		q.Enter(StageTransfer, bound.Request.ID)
	}
	res, err := sf.SendDecode(decodeAddr, kvHandle, payload)
	if q := d.stageQueuesFor(); q != nil {
		q.Leave(StageTransfer, bound.Request.ID)
	}
	if err != nil {
		return InferenceResult{}, false
	}
	d.recordTrace(stageEventFor(bound, TraceDecode, plan.DecodeWorker))
	return res, true
}

// stageEventFor builds a stage-boundary trace event for a plan step.
func stageEventFor(bound *Dispatch, stage TraceStage, workerID string) TraceEvent {
	e := traceEventFor(bound.Request, stage)
	e.WorkerID = workerID
	e.PlanMode = bound.Plan.Mode
	e.TransferMs = bound.Plan.TransferMs
	return e
}

// waiterFor returns the pending waiter for a request (nil when the
// submitter already cancelled).
func (d *Dispatcher) waiterFor(id string) *pendingWaiter {
	d.fwMu.Lock()
	defer d.fwMu.Unlock()
	return d.pending[id]
}

func (d *Dispatcher) forwarderFor() Forwarder {
	d.fwMu.Lock()
	defer d.fwMu.Unlock()
	return d.forwarder
}

// SetForwarder installs the worker-facing forwarder (DARI client in
// production) and wakes the loop.
func (d *Dispatcher) SetForwarder(f Forwarder) {
	d.fwMu.Lock()
	d.forwarder = f
	d.fwMu.Unlock()
	d.wakeDispatch()
}

// SetStreamForwarder installs a streaming-capable forwarder.
func (d *Dispatcher) SetStreamForwarder(sf StreamForwarder) {
	d.SetForwarder(sf)
}

// submitResult delivers (exactly once) a terminal result to the waiter.
func (d *Dispatcher) submitResult(id string, res InferenceResult) {
	d.fwMu.Lock()
	w, ok := d.pending[id]
	if ok {
		delete(d.pending, id)
	}
	d.fwMu.Unlock()
	if !ok {
	}
	if w != nil {
		select {
		case w.ch <- res:
		default:
		}
		d.closeDeltas(w)
	}
}
