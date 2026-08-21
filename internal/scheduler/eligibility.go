package scheduler

// eligibility.go separates the eligibility filter ladder from performance
// scoring (PAT-1445 Phase 1: hard governance, entitlement, residency, and
// assurance constraints always precede performance optimization). Every
// filter is a pure check — exclusion reasons are bounded, recorded on the
// routing receipt, and never mutate router state.

// IneligibilityReason is the bounded label for why a worker was excluded
// from scoring. The set is closed: new filters add a new constant here so
// receipt cardinality stays bounded.
type IneligibilityReason string

const (
	ReasonModelMismatch  IneligibilityReason = "model-mismatch"
	ReasonRegionMismatch IneligibilityReason = "region-mismatch"
	ReasonPoolMismatch   IneligibilityReason = "pool-mismatch"
	ReasonNotServable    IneligibilityReason = "not-servable"
	ReasonGangIncomplete IneligibilityReason = "gang-incomplete"
	ReasonSLORisk        IneligibilityReason = "slo-risk"
	ReasonOverloaded     IneligibilityReason = "overloaded"
)

// EligibilityReport summarizes the filter pass for one routing decision
// (PAT-1445: receipts explain the eligibility filters applied).
type EligibilityReport struct {
	Region   string                      `json:"region,omitempty"` // residency constraint applied
	Eligible int                         `json:"eligible"`
	Filtered map[IneligibilityReason]int `json:"filtered,omitempty"`
}

// permanentReason reports whether an ineligibility cause is structural
// (no amount of queue waiting fixes it within a TTL): the model, region,
// pool, gang, or servable state does not change by waiting. Overload and
// SLO risk are transient — waiting legitimately helps (late binding).
func permanentReason(r IneligibilityReason) bool {
	switch r {
	case ReasonModelMismatch, ReasonRegionMismatch, ReasonPoolMismatch, ReasonGangIncomplete, ReasonNotServable:
		return true
	}
	return false
}

// Permanent reports whether the filter pass excluded every worker for
// structural reasons only — the bounded-early-rejection signal (WS3: an
// honest retryable reason instead of parking the request to TTL).
func (r *EligibilityReport) Permanent() bool {
	if r.Eligible > 0 {
		return false
	}
	for reason := range r.Filtered {
		if !permanentReason(reason) {
			return false
		}
	}
	return true
}

// DecisionSignals records the chosen worker's measured load at decision
// time (PAT-1445: receipts record the decisive measured signals).
type DecisionSignals struct {
	PrefillActive  int `json:"prefill_active"`
	DecodeKV       int `json:"decode_kv"`
	ActiveRequests int `json:"active_requests"`
}

// ineligible runs the eligibility filter ladder for one worker and
// returns the reason it is excluded, or "" when it may be scored. Order
// is governance first (model, residency, pool), then capability
// (servability, gang readiness), then risk and capacity (SLO violation
// risk, overload). Called with the router lock held; never mutates state.
func (r *CostRouter) ineligible(req RouteRequest, id string, e WorkerEntry, st RouterWorkerState) IneligibilityReason {
	if e.Card.ModelName != req.Model {
		return ReasonModelMismatch
	}
	// Residency: a region-constrained request only sees workers in that
	// region (empty constraint = any). The card's region is signed.
	if req.Region != "" && e.Card.Region != req.Region {
		return ReasonRegionMismatch
	}
	if r.pools != nil && !r.pools.Contains(req.Pool, id) {
		return ReasonPoolMismatch
	}
	if e.Quarantined || e.Lapsed || !e.Card.Servable() {
		return ReasonNotServable
	}
	// Gang readiness: an incomplete parallel group serves nothing
	// (spec §14 row 16).
	if r.gang != nil && r.gang.MemberBlocked(id) {
		return ReasonGangIncomplete
	}
	// SLO gate (S5): reject placements whose predicted TTFT violates the
	// objective with high probability.
	if r.predictor != nil && r.slo != nil {
		cfgID := r.workerCfg[id]
		if cfgID == "" {
			cfgID = id
		}
		target := r.slo.ForRequest(req.Model, req.RequestClass)
		if target.TTFTMs > 0 {
			f := PredictorFeatures{
				InputTokens:          req.InputTokens,
				CachedTokens:         req.CachedTokens,
				ExpectedOutputTokens: req.ExpectedOutputTokens,
				ActivePrefill:        st.PrefillActive,
				ActiveDecodeKV:       st.DecodeKV,
				ActiveRequests:       st.ActiveRequests,
			}
			if risk := r.predictor.PSLOViolation(cfgID, f, float64(target.TTFTMs)); risk > r.maxSLORisk {
				return ReasonSLORisk
			}
		}
	}
	// Overload filter (spec §12.3.1): a saturated worker is ineligible
	// regardless of how cheap its cache would be.
	if st.Load.MaxConcurrent > 0 && st.Load.Active >= st.Load.MaxConcurrent {
		return ReasonOverloaded
	}
	if r.cfg.MaxActiveRequests > 0 && st.ActiveRequests >= r.cfg.MaxActiveRequests {
		return ReasonOverloaded
	}
	return ""
}
