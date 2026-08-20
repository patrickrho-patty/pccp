package scheduler

// stages.go implements PAT-1445 WS2 stage planning: inference represented
// as a multi-stage execution path (prefill → KV transfer → decode)
// instead of one undifferentiated worker assignment. The planner decides
// the path; co-located execution on the router's chosen worker is always
// the safe fallback when roles, estimates, or the transfer budget say no
// (WS2: preserve co-located execution as a safe fallback when transfer/
// network cost outweighs disaggregation).

// StagePlan is one request's execution path.
type StagePlan struct {
	Mode          string  `json:"mode"` // colocated | disaggregated
	PrefillWorker string  `json:"prefill_worker"`
	DecodeWorker  string  `json:"decode_worker"`
	TransferMs    float64 `json:"transfer_ms,omitempty"`
	KVBytes       int64   `json:"kv_bytes,omitempty"`
}

// StagePlan modes.
const (
	StageColocated     = "colocated"
	StageDisaggregated = "disaggregated"
)

// StagePlanner decides HOW a routed request executes. Disaggregation only
// ever adds a separate prefill stage upstream of the router's chosen
// decode worker — it never overrides the router's placement.
type StagePlanner struct {
	pd            *PDPlanner
	oracle        NetworkOracle
	controller    *PDController
	bytesPerToken int64   // KV size per token for transfer pricing
	maxTransferMs float64 // TTFT budget guard: transfers priced above this fall back co-located
}

// NewStagePlanner builds a planner over the P/D role view and the network
// oracle. A nil controller falls back to the planner's raw engagement
// threshold (no hysteresis).
func NewStagePlanner(pd *PDPlanner, oracle NetworkOracle, controller *PDController) *StagePlanner {
	return &StagePlanner{
		pd:            pd,
		oracle:        oracle,
		controller:    controller,
		bytesPerToken: 128,   // conservative KV bytes/token until engine-reported
		maxTransferMs: 250.0, // transfers beyond this never improve TTFT enough
	}
}

// disaggregating reports whether the model is engaged for disaggregation:
// the controller's hysteresis state when present, else the planner's raw
// sustained-share threshold.
func (p *StagePlanner) disaggregating(model string) bool {
	if p.controller != nil {
		return p.controller.Engaged(model)
	}
	return p.pd != nil && p.pd.ShouldDisaggregate(model)
}

// SetTransferBudget tunes the TTFT guard: priced transfers above this
// many milliseconds fall back to co-located execution (WS2: a KV
// transfer that would miss the TTFT budget never runs).
func (p *StagePlanner) SetTransferBudget(ms float64) { p.maxTransferMs = ms }

// Plan builds the execution path for a request the router placed on
// decodeWorker. Co-located is returned whenever disaggregation is not
// engaged, no distinct prefill-capable worker exists, the transfer
// cannot be priced, or the priced transfer exceeds the TTFT budget
// (WS2: a KV transfer that would miss the TTFT budget falls back before
// consuming scarce stage capacity).
func (p *StagePlanner) Plan(model, decodeWorker string, inputTokens int) StagePlan {
	plan := StagePlan{Mode: StageColocated, PrefillWorker: decodeWorker, DecodeWorker: decodeWorker}
	if p.pd == nil || p.oracle == nil || decodeWorker == "" {
		return plan
	}
	if !p.disaggregating(model) {
		return plan
	}
	prefill := ""
	for _, w := range p.pd.Place(model, PDPhasePrefill) {
		if w != decodeWorker {
			prefill = w
			break
		}
	}
	if prefill == "" {
		return plan
	}
	kvBytes := int64(inputTokens) * p.bytesPerToken
	ms, ok := p.oracle.TransferCostMs(prefill, decodeWorker, kvBytes)
	if !ok || ms > p.maxTransferMs {
		return plan
	}
	plan.Mode = StageDisaggregated
	plan.PrefillWorker = prefill
	plan.TransferMs = ms
	plan.KVBytes = kvBytes
	return plan
}
