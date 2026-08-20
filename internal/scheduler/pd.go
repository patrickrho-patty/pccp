package scheduler

import (
	"sort"
	"sync"
)

// pd.go implements S6 prefill/decode placement (spec §14 row 15,
// §12.3.10): aggregated serving is the default mode; conditional P/D
// engages only when sustained traces show long prefills hurting decode,
// and is refused on SGLang (upstream does not support it).

// PDPhase is the serving phase a request is in.
type PDPhase string

const (
	PDPhasePrefill PDPhase = "prefill"
	PDPhaseDecode  PDPhase = "decode"
)

// PDPlanner places requests on workers by phase and role. Safe for
// concurrent use.
type PDPlanner struct {
	mu      sync.RWMutex
	workers map[string]WorkerEntry
	// prefillShare is a trailing-window EMA of prefill time share per
	// model; conditional disaggregation keys off it.
	prefillShare map[string]float64
}

// NewPDPlanner builds an empty planner.
func NewPDPlanner() *PDPlanner {
	return &PDPlanner{
		workers:      make(map[string]WorkerEntry),
		prefillShare: make(map[string]float64),
	}
}

// Upsert installs or refreshes a worker.
func (p *PDPlanner) Upsert(e WorkerEntry, _ RouterWorkerState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.workers[e.Card.WorkerID] = e
}

// Remove drops a worker.
func (p *PDPlanner) Remove(workerID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.workers, workerID)
}

// Place returns workers eligible for a model + phase. Aggregated workers
// serve both phases; role-declared workers serve only their phase; a
// model on SGLang with split roles gets nothing (conditional P/D
// unsupported upstream, spec §12.3.10).
func (p *PDPlanner) Place(model string, phase PDPhase) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	sglang := false
	roleWorkers := map[string][]string{}
	var aggregated []string
	for id, e := range p.workers {
		if e.Card.ModelName != model || e.Quarantined || e.Lapsed || !e.Card.Servable() {
			continue
		}
		switch e.Card.EffectivePDRole() {
		case PDRoleAggregated:
			aggregated = append(aggregated, id)
		default:
			if e.Card.EngineKind == "sglang" {
				sglang = true
			}
			roleWorkers[e.Card.EffectivePDRole()] = append(roleWorkers[e.Card.EffectivePDRole()], id)
		}
	}

	// SGLang caveat: conditional disaggregation is not supported — split
	// roles on SGLang refuse placement entirely (never misroute).
	if sglang {
		return nil
	}

	var out []string
	switch phase {
	case PDPhasePrefill:
		out = append(out, roleWorkers[PDRolePrefill]...)
		out = append(out, aggregated...)
	case PDPhaseDecode:
		out = append(out, roleWorkers[PDRoleDecode]...)
		out = append(out, aggregated...)
	}
	sort.Strings(out)
	return out
}

// ObservePrefillShare folds one prefill-share sample into the model's
// trailing EMA.
func (p *PDPlanner) ObservePrefillShare(model string, share float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	prev := p.prefillShare[model]
	p.prefillShare[model] = prev*0.8 + share*0.2
}

// ShouldDisaggregate reports whether the model's sustained prefill share
// exceeds the threshold for conditional P/D (spec §12.3.10).
func (p *PDPlanner) ShouldDisaggregate(model string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	const threshold = 0.6
	return p.prefillShare[model] > threshold
}

// PrefillShare returns the model's trailing prefill-share EMA (S10 view).
func (p *PDPlanner) PrefillShare(model string) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.prefillShare[model]
}

// RoleCounts returns how many servable workers carry each role for a
// model (S10 P/D capacity view).
func (p *PDPlanner) RoleCounts(model string) (prefill, decode, aggregated int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, e := range p.workers {
		if e.Card.ModelName != model || e.Quarantined || e.Lapsed || !e.Card.Servable() {
			continue
		}
		switch e.Card.EffectivePDRole() {
		case PDRolePrefill:
			prefill++
		case PDRoleDecode:
			decode++
		default:
			aggregated++
		}
	}
	return prefill, decode, aggregated
}
