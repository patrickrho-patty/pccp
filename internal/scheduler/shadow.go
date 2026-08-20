package scheduler

import "fmt"

// shadow.go implements PAT-1445 shadow evaluation: a candidate router
// computes decisions alongside the frozen baseline WITHOUT affecting
// traffic. The routing receipt records the candidate's version, decision,
// and agreement with the baseline; candidate errors and panics never
// change the routed outcome (research evaluation §shadow mode).

// ShadowRecord is the candidate side of one shadowed placement.
type ShadowRecord struct {
	CandidateVersion string  `json:"candidate_version"`
	WorkerID         string  `json:"worker_id,omitempty"`
	Cost             float64 `json:"cost,omitempty"`
	Agree            bool    `json:"agree"`
	Err              string  `json:"err,omitempty"`
}

// SetShadow installs a candidate router evaluated alongside the baseline.
// The candidate MUST be a distinct router instance (never self-shadow) and
// MUST NOT have a receipt store installed — shadow decisions are recorded
// on the baseline's receipt, never emitted as placements of their own.
func (r *CostRouter) SetShadow(c Router) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shadow = c
}

// Shadow exposes the installed candidate (nil when shadow mode is off).
func (r *CostRouter) Shadow() Router {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.shadow
}

// runShadow evaluates the candidate on the same request the baseline just
// routed. A candidate panic is recovered into the record — shadow
// evaluation must never take down production routing.
func (r *CostRouter) runShadow(req RouteRequest, baseline RouteDecision) (rec *ShadowRecord) {
	c := r.shadow
	rec = &ShadowRecord{CandidateVersion: c.Version()}
	defer func() {
		if p := recover(); p != nil {
			rec.WorkerID = ""
			rec.Cost = 0
			rec.Agree = false
			rec.Err = fmt.Sprintf("candidate panic: %v", p)
		}
	}()
	dec, err := c.Route(req)
	if err != nil {
		rec.Err = err.Error()
		return rec
	}
	rec.WorkerID = dec.WorkerID
	rec.Cost = dec.Cost
	rec.Agree = dec.WorkerID == baseline.WorkerID
	return rec
}
