package scheduler

import "sync"

// hints.go implements spec §12.3.3: per-request hints exposed internally so
// every downstream stage (admission debit, overload signals, routing in S3,
// scaling in S7) consumes the same expected-load model. The remaining
// expected length decays as a request nears completion, so its projected
// future load shrinks accordingly.

// RequestHints is the expected-load model for one request. Fields are
// produced at ingress (tokenizer estimate) and refined in flight.
type RequestHints struct {
	InputTokens          int    `json:"input_tokens"`
	CachedInputTokens    int    `json:"cached_input_tokens"`
	ExpectedOutputTokens int    `json:"expected_output_tokens"`
	MaxOutputTokens      int    `json:"max_output_tokens"`
	MediaTokens          int    `json:"media_tokens"`
	RequestClass         string `json:"request_class"`
	TenantPriority       string `json:"tenant_priority"`
}

// OutputEstimatorVersion is the algorithm identity stamped on exported
// traces (PAT-1445: versioned traces record estimator identities).
const OutputEstimatorVersion = "ema-ratio/v1"

// EstimatorConfig tunes the online output-length estimator.
type EstimatorConfig struct {
	// DefaultRatio is the initial expected-output-per-input-token ratio
	// used before any completion observation arrives.
	DefaultRatio float64
	// LearningRate blends each observed ratio into the running estimate
	// (exponential moving average).
	LearningRate float64
	// MaxOutputTokens caps any estimate.
	MaxOutputTokens int
}

// DefaultEstimatorConfig returns sane starting parameters.
func DefaultEstimatorConfig() EstimatorConfig {
	return EstimatorConfig{
		DefaultRatio:    0.25,
		LearningRate:    0.1,
		MaxOutputTokens: 4096,
	}
}

// OutputEstimator predicts expected output tokens per input size, learned
// online from completions (spec §12.3.3 + llm-d output-length hints). A
// per-request explicit hint wins; otherwise the learned ratio applies.
type OutputEstimator struct {
	mu    sync.RWMutex
	cfg   EstimatorConfig
	ratio float64 // tokens out per token in, EMA
}

// NewOutputEstimator builds an estimator seeded with the default ratio.
func NewOutputEstimator(cfg EstimatorConfig) *OutputEstimator {
	if cfg.DefaultRatio <= 0 {
		cfg.DefaultRatio = 0.25
	}
	if cfg.LearningRate <= 0 || cfg.LearningRate > 1 {
		cfg.LearningRate = 0.1
	}
	return &OutputEstimator{cfg: cfg, ratio: cfg.DefaultRatio}
}

// Estimate returns the expected output length for an input of the given
// size. An explicit hint (hint > 0) is honored; otherwise the learned
// ratio applies. Results are always bounded by maxOutput.
func (e *OutputEstimator) Estimate(inputTokens, hint, maxOutput int) int {
	if maxOutput <= 0 {
		maxOutput = e.cfg.MaxOutputTokens
	}
	est := hint
	if est <= 0 {
		e.mu.RLock()
		r := e.ratio
		e.mu.RUnlock()
		est = int(float64(inputTokens) * r)
		if est <= 0 {
			est = 1
		}
	}
	if est > maxOutput {
		return maxOutput
	}
	return est
}

// ObserveCompletion folds one (input, output) observation into the EMA.
func (e *OutputEstimator) ObserveCompletion(inputTokens, outputTokens int) {
	if inputTokens <= 0 {
		return
	}
	ratio := float64(outputTokens) / float64(inputTokens)
	e.mu.Lock()
	e.ratio = e.ratio*(1-e.cfg.LearningRate) + ratio*e.cfg.LearningRate
	e.mu.Unlock()
}

// TrackedRequest is a live projection for one in-flight request.
type TrackedRequest struct {
	ExpectedTotal int
	Produced      int
}

// Track starts tracking a request with the given expected total.
func (e *OutputEstimator) Track(inputTokens, hint, maxOutput int) *TrackedRequest {
	return &TrackedRequest{ExpectedTotal: e.Estimate(inputTokens, hint, maxOutput)}
}

// NoteProduced advances the produced counter.
func (r *TrackedRequest) NoteProduced(n int) { r.Produced += n }

// ProjectedRemaining returns the remaining expected output — decaying as
// production nears completion (spec §12.3.3).
func (r *TrackedRequest) ProjectedRemaining() int {
	rem := r.ExpectedTotal - r.Produced
	if rem < 0 {
		return 0
	}
	return rem
}
