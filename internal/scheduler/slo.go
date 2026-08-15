package scheduler

import (
	"math"
	"sync"
)

// slo.go implements S5: SLO- and MTP-aware scheduling (spec §12.3.9,
// §14 rows 8–9). TTFT/ITL targets are per model/class/request; the
// router rejects placements whose predicted latency violates the target
// with high probability; MTP acceptance length scales decode capacity.

// SLOTarget is one TTFT/ITL objective pair (ms).
type SLOTarget struct {
	TTFTMs int
	ITLMs  int
}

// SLOResolver maps model + traffic class to SLO targets. Safe for
// concurrent use.
type SLOResolver struct {
	mu      sync.RWMutex
	models  map[string]SLOTarget
	classes map[string]SLOTarget
	def     SLOTarget
}

// NewSLOResolver builds an empty resolver (defaults must be set).
func NewSLOResolver() *SLOResolver {
	return &SLOResolver{
		models:  make(map[string]SLOTarget),
		classes: make(map[string]SLOTarget),
	}
}

// SetModelTarget sets a per-model objective.
func (r *SLOResolver) SetModelTarget(model string, t SLOTarget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models[model] = t
}

// SetClassTarget sets a per-traffic-class objective (agentic requests
// carry tighter budgets, spec §14 row 28).
func (r *SLOResolver) SetClassTarget(class string, t SLOTarget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.classes[class] = t
}

// SetDefault sets the fleet-wide fallback objective.
func (r *SLOResolver) SetDefault(t SLOTarget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.def = t
}

// Target returns the model-scoped objective or the default.
func (r *SLOResolver) Target(model string) (SLOTarget, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if t, ok := r.models[model]; ok {
		return t, true
	}
	return r.def, r.def.TTFTMs > 0 || r.def.ITLMs > 0
}

// ForRequest resolves the objective for a request: class-specific first
// (agentic tighter), then model, then default.
func (r *SLOResolver) ForRequest(model, class string) SLOTarget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if class != "" {
		if t, ok := r.classes[class]; ok {
			return t
		}
	}
	if t, ok := r.models[model]; ok {
		return t
	}
	return r.def
}

// MTPCapacity models the accepted-token length effect on decode capacity
// (spec §12.3.9).
type MTPCapacity struct {
	AcceptedTokensPerVerify float64
	RealTokensPerSecond     float64
}

// NewMTPCapacity builds the capacity model.
func NewMTPCapacity(accepted, realTPS float64) *MTPCapacity {
	if accepted <= 0 {
		accepted = 1.8
	}
	return &MTPCapacity{AcceptedTokensPerVerify: accepted, RealTokensPerSecond: realTPS}
}

// EffectiveTokensPerSecond returns the MTP-amplified decode rate.
func (m *MTPCapacity) EffectiveTokensPerSecond() float64 {
	return m.RealTokensPerSecond * m.AcceptedTokensPerVerify
}

// DecodeGPUsFor returns how many decode GPUs a demand of tokens/s needs
// under MTP-aware capacity (ceil; over-provisioning avoided).
func (m *MTPCapacity) DecodeGPUsFor(tokensPerSecond float64) int {
	eff := m.EffectiveTokensPerSecond()
	if eff <= 0 {
		return 0
	}
	return int(math.Ceil(tokensPerSecond / eff))
}
