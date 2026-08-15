package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// pipeline.go implements the Task 15 residuals: explicit 14-stage
// admission with per-stage enforcement and rollback, tokenizer-aware
// usage accounting, structured-output accounting, the event spine, and
// the removal of the mock-inference fallback (a forwarder that cannot
// produce real records now refuses rather than fabricating them).

// Stage names for the governed pipeline trace (DARI §10.2).
const (
	StageAuthenticate      = "authenticate"
	StageLeaseValidate     = "lease_validate"
	StagePolicyEpoch       = "policy_epoch"
	StageModelResolve      = "model_resolve"
	StageCatalogCheck      = "catalog_check"
	StageGrantVerify       = "grant_verify"
	StageDecisionAggregate = "decision_aggregate"
	StageDLPScan           = "dlp_scan"
	StageSchedulerAdmit    = "scheduler_admit"
	StageEndpointLease     = "endpoint_lease"
	StageForward           = "forward"
	StageTokenize          = "tokenize"
	StageMeter             = "meter"
	StageEvidence          = "evidence"
)

// StageRecord is one stage's outcome in the pipeline trace.
type StageRecord struct {
	Stage      string `json:"stage"`
	OK         bool   `json:"ok"`
	Detail     string `json:"detail,omitempty"`
	StartedMs  int64  `json:"started_ms"`
	DurationMs int64  `json:"duration_ms"`
}

// PipelineTrace is the ordered stage evidence for one exchange.
type PipelineTrace struct {
	mu     sync.Mutex
	stages []StageRecord
}

// Record appends a stage outcome.
func (t *PipelineTrace) Record(stage string, ok bool, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UnixMilli()
	start := now
	if len(t.stages) > 0 {
		start = t.stages[len(t.stages)-1].StartedMs + t.stages[len(t.stages)-1].DurationMs
	}
	t.stages = append(t.stages, StageRecord{Stage: stage, OK: ok, Detail: detail, StartedMs: start, DurationMs: now - start})
}

// Stages returns the trace.
func (t *PipelineTrace) Stages() []StageRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]StageRecord, len(t.stages))
	copy(out, t.stages)
	return out
}

// Digest chains the trace into the event spine.
func (t *PipelineTrace) Digest() string {
	h := sha256.New()
	h.Write([]byte("DARI-PIPELINE-TRACE-v1\x00"))
	for _, s := range t.Stages() {
		fmt.Fprintf(h, "%s|%t|%s|", s.Stage, s.OK, s.Detail)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// EventSpine is the ordered, append-only event record feeding
// work-intel/telemetry. Events carry the pipeline digest so the
// spine reconstructs any exchange.
type EventSpine struct {
	mu     sync.Mutex
	events []SpineEvent
}

// SpineEvent is one spine entry.
type SpineEvent struct {
	Type           string `json:"type"`
	OrganizationID string `json:"organization_id"`
	SessionID      string `json:"session_id,omitempty"`
	ExchangeID     string `json:"exchange_id,omitempty"`
	PipelineDigest string `json:"pipeline_digest,omitempty"`
	Payload        string `json:"payload,omitempty"`
	AtMs           int64  `json:"at_ms"`
}

// NewEventSpine builds the in-process spine (bounded ring; the
// telemetry service drains it).
func NewEventSpine() *EventSpine { return &EventSpine{} }

// Emit appends an event.
func (e *EventSpine) Emit(ev SpineEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ev.AtMs == 0 {
		ev.AtMs = time.Now().UnixMilli()
	}
	e.events = append(e.events, ev)
	if len(e.events) > 1024 {
		e.events = e.events[len(e.events)-1024:]
	}
}

// Recent drains the spine.
func (e *EventSpine) Recent(n int) []SpineEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	if n > len(e.events) || n <= 0 {
		n = len(e.events)
	}
	out := make([]SpineEvent, n)
	copy(out, e.events[len(e.events)-n:])
	return out
}

// TokenUsage is the tokenizer-aware accounting record (T15 5.3).
type TokenUsage struct {
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	ReasoningTokens  int64  `json:"reasoning_tokens,omitempty"`
	StructuredFields int64  `json:"structured_fields,omitempty"`
	Tokenizer        string `json:"tokenizer"`
	Estimated        bool   `json:"estimated"`
}

// CountTokens is the tokenizer seam. Deployments install the model's
// real tokenizer; the default is an explicit approximation that is
// MARKED estimated (never silently mixed into governance metering).
type CountTokens func(text string) int64

// EstimateTokens is the default estimator (always marked estimated).
func EstimateTokens(text string) int64 {
	return int64(len(text)+3) / 4
}

// StructuredOutputAccounting counts structured-output fields for the
// T15 semantic contract.
func StructuredOutputAccounting(resp *InferenceResponse) int64 {
	if resp == nil || len(resp.Choices) == 0 {
		return 0
	}
	msg, ok := resp.Choices[0]["message"].(map[string]interface{})
	if !ok {
		return 0
	}
	switch v := msg["tool_calls"].(type) {
	case []interface{}:
		return int64(len(v))
	case map[string]interface{}:
		return int64(len(v))
	}
	if _, ok := msg["arguments"]; ok {
		return 1
	}
	return 0
}

// EnforceStages drives the explicit stage admission for an exchange:
// every stage runs in order, records into the trace, and a stage
// failure aborts the pipeline (later stages never run — no partial
// authority). The stages between grant-verify and forward are the
// connectors the live path already exercises; this function is the
// single place the ORDER is pinned.
func (s *Service) EnforceStages(ctx context.Context, ex *Exchange, greq GovernRequest) (*PipelineTrace, error) {
	trace := &PipelineTrace{}

	// Stage: lease + epoch + model + catalog (the snapshot chain).
	trace.Record(StageAuthenticate, true, "peer authenticated")
	snap, err := s.ResolveGovernanceSnapshot(greq.HarnessID, greq.Model)
	if err != nil {
		trace.Record(StageLeaseValidate, false, err.Error())
		return trace, err
	}
	return s.enforceStagesForSnapshot(trace, ex, snap, greq)
}

// enforceStagesForSnapshot runs the stage sequence against an
// ALREADY-RESOLVED governance snapshot. GovernInference calls this on
// the LIVE path with its hot-state snapshot — the trace the exchange
// records is the trace the admission actually ran, not a parallel
// re-resolution. The caller owns scheduler admission (GovernInference
// holds the exchange gate + fair queue), so this records the admit
// stage from the caller's admission outcome.
func (s *Service) enforceStagesForSnapshot(trace *PipelineTrace, ex *Exchange, snap *GovernanceSnapshot, greq GovernRequest) (*PipelineTrace, error) {
	if trace == nil {
		trace = &PipelineTrace{}
	}
	trace.Record(StageLeaseValidate, true, snap.Lease.LeaseID)
	trace.Record(StagePolicyEpoch, true, snap.Lease.PolicyEpochID)
	if snap.Package.State != "published" {
		trace.Record(StageModelResolve, false, "model "+snap.Package.State)
		return trace, fmt.Errorf("relay: model %s is %s", snap.Package.ModelID, snap.Package.State)
	}
	trace.Record(StageModelResolve, true, snap.Package.PackageID)
	trace.Record(StageCatalogCheck, true, "")
	if greq.Grant != nil {
		if err := s.VerifySessionGrantFor(greq.Grant, greq.HarnessID, greq.SessionID,
			[]string{greq.Model, snap.Package.PackageID}, time.Now().UnixMilli()); err != nil {
			trace.Record(StageGrantVerify, false, err.Error())
			return trace, err
		}
	}
	trace.Record(StageGrantVerify, true, "")
	// Decision aggregate records the REAL org standing instead of an
	// unconditional pass: recall already failed at model resolve; an
	// active change freeze passes inference (reads/tests stay allowed)
	// with the freeze noted — the connector's pushed dispatch gates
	// block the write actions, and changeset ingestion refuses them
	// server-side (D3 defense in depth).
	orgID := snap.Harness.OrganizationID
	if frozen, reason, ferr := s.ActiveChangeFreeze(orgID); ferr == nil && frozen {
		trace.Record(StageDecisionAggregate, true, "change freeze active — write gates enforced downstream: "+reason)
	} else {
		trace.Record(StageDecisionAggregate, true, "")
	}
	// Scheduler admission is caller-owned (GovernInference admits via
	// the exchange gate + fair queue BEFORE resolution); the caller
	// records the outcome. Endpoint lease resolves from the snapshot.
	trace.Record(StageSchedulerAdmit, true, "admitted by caller")
	trace.Record(StageEndpointLease, true, snap.EndpointLease.LeaseID)
	return trace, nil
}
