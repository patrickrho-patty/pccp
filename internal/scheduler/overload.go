package scheduler

import "time"

// overload.go implements S2 two-layer overload protection (spec §12.3.7,
// §14 row 38). Layer 1 (edge admission) gates new work on aggregate fleet
// signals with a short bounded wait budget; layer 2 (worker-local) keeps a
// small per-worker buffer so continuous batching never starves without
// parking long tails on the engine.

// FleetSignals is the aggregate telemetry snapshot the edge gate consumes.
// Signals arrive from worker heartbeats' introspection and the global
// queue's depth; there is no DB and no per-request lookup.
type FleetSignals struct {
	QueuedTokens      int64   // total token debit waiting in the global queue
	P95TTFTMs         float64 // fleet p95 time-to-first-token
	P95ITLMs          float64 // fleet p95 inter-token latency
	KVUtilization     float64 // 0..1 fleet KV cache utilization
	ActivePrefillTok  int64   // tokens currently in prefill across workers
	ActiveDecodeKV    int64   // decode KV slots in use across workers
	AvailableReplicas int     // workers with at least one free sequence slot
}

// OverloadPolicy are the edge-gate thresholds. Thresholds are operator-set
// per fleet; defaults are deliberately conservative (fail closed).
type OverloadPolicy struct {
	MaxQueuedTokens      int64
	MaxP95TTFTMs         float64
	MaxP95ITLMs          float64
	MaxKVUtilization     float64
	MaxActivePrefillTok  int64
	MaxActiveDecodeKV    int64
	MinAvailableReplicas int
	WaitBudget           time.Duration
}

// DefaultOverloadPolicy returns conservative fleet-wide thresholds.
func DefaultOverloadPolicy() OverloadPolicy {
	return OverloadPolicy{
		MaxQueuedTokens:      1_000_000,
		MaxP95TTFTMs:         2000,
		MaxP95ITLMs:          200,
		MaxKVUtilization:     0.95,
		MaxActivePrefillTok:  20_000,
		MaxActiveDecodeKV:    20_000,
		MinAvailableReplicas: 1,
		WaitBudget:           5 * time.Second,
	}
}

// Verdict is the edge gate's decision for a new request.
type Verdict string

const (
	VerdictAdmit  Verdict = "admit"  // healthy: proceed to the global queue
	VerdictWait   Verdict = "wait"   // saturated, non-sheddable: hold up to WaitBudget, then reject/retry
	VerdictShed   Verdict = "shed"   // saturated, sheddable class: reject immediately (retryable 429)
	VerdictReject Verdict = "reject" // wait budget exhausted: reject with retry metadata
)

// Saturated reports whether any fleet signal crosses its threshold.
func (p OverloadPolicy) Saturated(s FleetSignals) bool {
	if s.QueuedTokens > p.MaxQueuedTokens {
		return true
	}
	if s.P95TTFTMs > p.MaxP95TTFTMs {
		return true
	}
	if s.P95ITLMs > p.MaxP95ITLMs {
		return true
	}
	if s.KVUtilization > p.MaxKVUtilization {
		return true
	}
	if s.ActivePrefillTok > p.MaxActivePrefillTok {
		return true
	}
	if s.ActiveDecodeKV > p.MaxActiveDecodeKV {
		return true
	}
	if s.AvailableReplicas < p.MinAvailableReplicas {
		return true
	}
	return false
}

// Evaluate decides for a request with no class (defaults to non-sheddable).
func (p OverloadPolicy) Evaluate(s FleetSignals) Verdict {
	return p.EvaluateFor(s, "interactive-normal")
}

// EvaluateFor decides for a request of the given traffic class. Sheddable
// classes (batch, background-agent) are rejected immediately on saturation
// so they cannot hold capacity the higher bands need (llm-d in-flight
// eviction discipline). Non-sheddable classes wait out the budget.
func (p OverloadPolicy) EvaluateFor(s FleetSignals, class string) Verdict {
	if !p.Saturated(s) {
		return VerdictAdmit
	}
	if class == "batch" || class == "background-agent" {
		return VerdictShed
	}
	return VerdictWait
}

// WorkerLoad is the layer-2 signal: one worker's concurrency and its small
// local buffer. The buffer exists to keep continuous batching saturated
// (spec §12.3.7); it is capped at a couple of requests by construction.
type WorkerLoad struct {
	MaxConcurrent int // from the capability card
	Active        int // sequences currently on the engine
	LocalQueued   int // requests parked on the worker (small!)
}

// maxWorkerLocalBuffer is the hard cap on a worker's local queue. Bigger
// than this and we are re-introducing the engine-local queues the global
// queue exists to replace (llm-d "healthy buffer" principle).
const maxWorkerLocalBuffer = 2

// CanAccept reports whether the worker can take another request now.
func (w WorkerLoad) CanAccept() bool {
	if w.MaxConcurrent <= 0 {
		return false
	}
	if w.Active >= w.MaxConcurrent {
		return false
	}
	return w.LocalQueued < maxWorkerLocalBuffer
}
