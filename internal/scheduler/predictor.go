package scheduler

import (
	"math"
	"sync"
)

// predictor.go implements S4: online Bayesian latency prediction with
// predictive variance → P(SLO violation) (spec §13.3, §13.12, §13.13).
// One model per serving config; updates are O(p²) precision-form steps
// on completion events; prediction is a local matrix-vector product —
// zero-hop, no sidecar, no retrain cycles, no predictor-outage mode.

// PredictorVersion is the algorithm/schema identity stamped on routing
// receipts (PAT-1445: receipts record the estimator versions that
// produced the decision).
const PredictorVersion = "bayes-ttft/v1"

// PredictorConfig tunes the Bayesian model.
type PredictorConfig struct {
	NoisePrecision float64 // β: observation precision (1/σ²) prior
	PriorPrecision float64 // λ: weight prior precision
	GlobalPull     float64 // hierarchical prior strength toward the global mean
	MinVariance    float64 // floor on predictive variance (avoid overconfidence)
}

// DefaultPredictorConfig returns reference hyperparameters.
func DefaultPredictorConfig() PredictorConfig {
	return PredictorConfig{
		NoisePrecision: 1.0 / (100.0 * 100.0), // σ ≈ 100ms prior
		PriorPrecision: 1e-4,
		GlobalPull:     0.1,
		MinVariance:    10.0,
	}
}

// PredictorFeatures is the ~15-feature input vector (spec §13.12):
// request shape, live load, and one-hot serving-config attributes.
type PredictorFeatures struct {
	InputTokens          int
	CachedTokens         int
	ExpectedOutputTokens int
	ActivePrefill        int
	ActiveDecodeKV       int
	ActiveRequests       int
	KVUtilization        float64
	MTP                  bool
	GPUSKU               string
	GPUHBMGB             int
	EngineKind           string
}

// Prediction is a distributional forecast: mean latency + variance.
type Prediction struct {
	Mean     float64
	Variance float64
}

const predictorDim = 12

// feature vector layout:
//
//	0 intercept
//	1 log(input_tokens+1)
//	2 log(cached_tokens+1)      (negative TTFT effect)
//	3 log(expected_output+1)
//	4 log(active_prefill+1)
//	5 log(active_decode_kv+1)
//	6 active_requests
//	7 kv_utilization
//	8 mtp flag
//	9 log(gpu_hbm)
//	10 gpu sku A100 one-hot
//	11 gpu sku H100 one-hot
func (f PredictorFeatures) vector() []float64 {
	x := make([]float64, predictorDim)
	x[0] = 1
	x[1] = math.Log(float64(f.InputTokens) + 1)
	x[2] = math.Log(float64(f.CachedTokens) + 1)
	x[3] = math.Log(float64(f.ExpectedOutputTokens) + 1)
	x[4] = math.Log(float64(f.ActivePrefill) + 1)
	x[5] = math.Log(float64(f.ActiveDecodeKV) + 1)
	x[6] = float64(f.ActiveRequests)
	x[7] = f.KVUtilization
	if f.MTP {
		x[8] = 1
	}
	x[9] = math.Log(float64(f.GPUHBMGB) + 1)
	switch f.GPUSKU {
	case "A100":
		x[10] = 1
	case "H100":
		x[11] = 1
	}
	return x
}

// bayesModel is one config's precision-form Bayesian linear model.
// Λ⁻¹ is updated O(p²) per observation (Sherman-Morrison); b accumulates
// β·Σxy; the posterior mean μ = Λ⁻¹b is materialized at prediction time.
// No batch retraining exists.
type bayesModel struct {
	precInv []float64 // p×p inverse precision (posterior covariance / noise)
	b       []float64 // Σ β·x·y
	count   int
}

func newBayesModel(cfg PredictorConfig) *bayesModel {
	m := &bayesModel{
		precInv: make([]float64, predictorDim*predictorDim),
		b:       make([]float64, predictorDim),
	}
	// Prior: Λ⁻¹ = λ⁻¹ I.
	for i := 0; i < predictorDim; i++ {
		m.precInv[i*predictorDim+i] = 1.0 / cfg.PriorPrecision
	}
	return m
}

// update folds one (x, y) observation in O(p²).
func (m *bayesModel) update(cfg PredictorConfig, x []float64, y float64) {
	// Sherman-Morrison: (Λ + βxxᵀ)⁻¹ = Λ⁻¹ − β·Λ⁻¹xxᵀΛ⁻¹ / (1 + β·xᵀΛ⁻¹x)
	u := make([]float64, predictorDim) // u = Λ⁻¹ x
	denom := 1.0
	for i := 0; i < predictorDim; i++ {
		var s float64
		for j := 0; j < predictorDim; j++ {
			s += m.precInv[i*predictorDim+j] * x[j]
		}
		u[i] = s
		denom += cfg.NoisePrecision * x[i] * s
	}
	scale := cfg.NoisePrecision / denom
	for i := 0; i < predictorDim; i++ {
		for j := 0; j < predictorDim; j++ {
			m.precInv[i*predictorDim+j] -= scale * u[i] * u[j]
		}
	}
	// Sufficient statistic: b += β·x·y.
	for i := 0; i < predictorDim; i++ {
		m.b[i] += cfg.NoisePrecision * x[i] * y
	}
	m.count++
}

// posteriorMean materializes μ = Λ⁻¹ b (O(p²)).
func (m *bayesModel) posteriorMean() []float64 {
	mu := make([]float64, predictorDim)
	for i := 0; i < predictorDim; i++ {
		var s float64
		for j := 0; j < predictorDim; j++ {
			s += m.precInv[i*predictorDim+j] * m.b[j]
		}
		mu[i] = s
	}
	return mu
}

func dot(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// LatencyPredictor holds per-config models plus the hierarchical global
// prior. Safe for concurrent use.
type LatencyPredictor struct {
	mu     sync.RWMutex
	cfg    PredictorConfig
	models map[string]*bayesModel
	global *bayesModel
}

// NewLatencyPredictor builds a predictor.
func NewLatencyPredictor(cfg PredictorConfig) *LatencyPredictor {
	return &LatencyPredictor{
		cfg:    cfg,
		models: make(map[string]*bayesModel),
		global: newBayesModel(cfg),
	}
}

// LatencyPredictorPair forecasts both TTFT and TPOT (spec §14 row 7:
// predicted-latency routing needs both objectives).
type LatencyPredictorPair struct {
	TTFT *LatencyPredictor
	TPOT *LatencyPredictor
}

// NewLatencyPredictorPair builds the TTFT/TPOT twins.
func NewLatencyPredictorPair(cfg PredictorConfig) *LatencyPredictorPair {
	return &LatencyPredictorPair{
		TTFT: NewLatencyPredictor(cfg),
		TPOT: NewLatencyPredictor(cfg),
	}
}

// Observe folds one completion into both twins (ttftMs, tpotMs).
func (p *LatencyPredictorPair) Observe(configID string, f PredictorFeatures, ttftMs, tpotMs float64) {
	p.TTFT.Observe(configID, f, ttftMs)
	p.TPOT.Observe(configID, f, tpotMs)
}

// PSLOViolation returns the TTFT-side violation risk.
func (p *LatencyPredictorPair) PSLOViolation(configID string, f PredictorFeatures, sloMs float64) float64 {
	return p.TTFT.PSLOViolation(configID, f, sloMs)
}

// PITLViolation returns the TPOT-side violation risk.
func (p *LatencyPredictorPair) PITLViolation(configID string, f PredictorFeatures, sloMs float64) float64 {
	return p.TPOT.PSLOViolation(configID, f, sloMs)
}

// Observe folds one completion observation into the config's model and
// the global prior. y is the observed latency (ms).
func (p *LatencyPredictor) Observe(configID string, f PredictorFeatures, y float64) {
	x := f.vector()
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.models[configID]
	if m == nil {
		m = newBayesModel(p.cfg)
		p.models[configID] = m
	}
	m.update(p.cfg, x, y)
	p.global.update(p.cfg, x, y)
}

// PredictTTFT returns the distributional TTFT forecast for a config.
func (p *LatencyPredictor) PredictTTFT(configID string, f PredictorFeatures) Prediction {
	x := f.vector()
	p.mu.RLock()
	defer p.mu.RUnlock()
	m := p.models[configID]
	if m == nil {
		// Unseen config: the global prior's prediction (borrowing
		// strength from similar configs, spec §13.12).
		return p.predictFrom(p.global, nil, x)
	}
	return p.predictFrom(m, p.global, x)
}

func (p *LatencyPredictor) predictFrom(m, global *bayesModel, x []float64) Prediction {
	// Hierarchical mean: shrink the config mean toward the global mean
	// by the config's observation count (rare configs lean global).
	mean := dot(m.posteriorMean(), x)
	if global != nil && m.count < 50 {
		gMean := dot(global.posteriorMean(), x)
		w := float64(50-m.count) / 50 * p.cfg.GlobalPull
		mean = mean*(1-w) + gMean*w
	}
	// Predictive variance: the observation-noise term shrinks with the
	// evidence count (more completions → tighter forecasts), plus the
	// parameter-uncertainty term xᵀΛ⁻¹x.
	xPrecInvX := 0.0
	for i := 0; i < predictorDim; i++ {
		for j := 0; j < predictorDim; j++ {
			xPrecInvX += x[i] * m.precInv[i*predictorDim+j] * x[j]
		}
	}
	variance := (1.0/p.cfg.NoisePrecision)/float64(m.count+1) + xPrecInvX
	if variance < p.cfg.MinVariance {
		variance = p.cfg.MinVariance
	}
	return Prediction{Mean: mean, Variance: variance}
}

// PSLOViolation returns P(TTFT > sloMs) under the predictive normal
// distribution — the risk-aware routing signal (spec §13.3: variance →
// P(SLO violation), not just mean).
func (p *LatencyPredictor) PSLOViolation(configID string, f PredictorFeatures, sloMs float64) float64 {
	pred := p.PredictTTFT(configID, f)
	sd := math.Sqrt(pred.Variance)
	if sd <= 0 {
		if pred.Mean > sloMs {
			return 1
		}
		return 0
	}
	z := (sloMs - pred.Mean) / sd
	return 1 - normalCDF(z)
}

// normalCDF is the standard normal cumulative distribution (Abramowitz-
// Stegun 26.2.17 approximation — pure stdlib math).
func normalCDF(z float64) float64 {
	if z < -8 {
		return 0
	}
	if z > 8 {
		return 1
	}
	t := 1 / (1 + 0.2316419*math.Abs(z))
	d := 0.3989422804014327 * math.Exp(-z*z/2)
	poly := t * (0.319381530 + t*(-0.356563782+t*(1.781477937+t*(-1.821255978+t*1.330274429))))
	phi := 1 - d*poly
	if z < 0 {
		return 1 - phi
	}
	return phi
}
