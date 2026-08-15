package scheduler

import (
	"math"
	"testing"
)

func sampleFeatures() PredictorFeatures {
	return PredictorFeatures{
		InputTokens:          512,
		CachedTokens:         0,
		ExpectedOutputTokens: 200,
		ActivePrefill:        1000,
		ActiveDecodeKV:       500,
		ActiveRequests:       3,
		KVUtilization:        0.4,
		MTP:                  false,
		GPUSKU:               "H100",
		GPUHBMGB:             80,
		EngineKind:           "vllm",
	}
}

func TestPredictorLearnsLinearLatency(t *testing.T) {
	p := NewLatencyPredictor(DefaultPredictorConfig())
	// Latency ≈ 0.05 × input_tokens + 50. Teach it, then check the
	// prediction converges toward the true function.
	for i := 0; i < 200; i++ {
		in := 100 + i*10
		f := sampleFeatures()
		f.InputTokens = in
		p.Observe("cfg-1", f, 0.05*float64(in)+50)
	}
	probe := sampleFeatures()
	probe.InputTokens = 1000
	pred := p.PredictTTFT("cfg-1", probe)
	if math.Abs(pred.Mean-100) > 30 {
		t.Fatalf("learned mean = %.1f, want ≈100", pred.Mean)
	}
}

func TestPredictorVarianceShrinksWithData(t *testing.T) {
	p := NewLatencyPredictor(DefaultPredictorConfig())
	first := p.PredictTTFT("cfg-x", sampleFeatures())
	for i := 0; i < 500; i++ {
		p.Observe("cfg-x", sampleFeatures(), 300)
	}
	after := p.PredictTTFT("cfg-x", sampleFeatures())
	if after.Variance >= first.Variance {
		t.Fatalf("variance must shrink with observations: %.1f → %.1f", first.Variance, after.Variance)
	}
}

func TestPredictorPSLOViolation(t *testing.T) {
	p := NewLatencyPredictor(DefaultPredictorConfig())
	// A fast config: mean 100, small variance → tiny violation risk for
	// SLO 500.
	for i := 0; i < 300; i++ {
		p.Observe("fast", sampleFeatures(), 100)
	}
	risk := p.PSLOViolation("fast", sampleFeatures(), 500)
	if risk > 0.01 {
		t.Fatalf("fast config SLO risk = %.4f, want <0.01", risk)
	}
	// A slow config: mean 4000 → near-certain violation.
	for i := 0; i < 300; i++ {
		p.Observe("slow", sampleFeatures(), 4000)
	}
	risk = p.PSLOViolation("slow", sampleFeatures(), 500)
	if risk < 0.99 {
		t.Fatalf("slow config SLO risk = %.4f, want ≈1", risk)
	}
}

func TestPredictorPerConfigModels(t *testing.T) {
	// Spec §13.12: one model per serving config — heterogeneous fleets
	// are first-class.
	p := NewLatencyPredictor(DefaultPredictorConfig())
	for i := 0; i < 200; i++ {
		p.Observe("h100-cfg", sampleFeatures(), 100)
		p.Observe("a100-cfg", sampleFeatures(), 900)
	}
	h := p.PredictTTFT("h100-cfg", sampleFeatures())
	a := p.PredictTTFT("a100-cfg", sampleFeatures())
	if h.Mean >= a.Mean {
		t.Fatalf("h100 mean %.1f must be below a100 mean %.1f", h.Mean, a.Mean)
	}
}

func TestPredictorBorrowsFromGlobalPrior(t *testing.T) {
	// Rare configs borrow strength from similar ones (hierarchical
	// prior, spec §13.12): a brand-new config's prediction starts near
	// the global mean, not at a wild prior.
	p := NewLatencyPredictor(DefaultPredictorConfig())
	for i := 0; i < 200; i++ {
		p.Observe("common-a", sampleFeatures(), 300)
		p.Observe("common-b", sampleFeatures(), 320)
	}
	rare := p.PredictTTFT("never-seen", sampleFeatures())
	if rare.Mean < 200 || rare.Mean > 400 {
		t.Fatalf("rare config mean %.1f should sit near the global prior (~310)", rare.Mean)
	}
	// A config with 2 observations is pulled toward the global mean,
	// not dominated by noise.
	p.Observe("rare-2", sampleFeatures(), 5000)
	p.Observe("rare-2", sampleFeatures(), 5000)
	shrunk := p.PredictTTFT("rare-2", sampleFeatures())
	if shrunk.Mean >= 5000 {
		t.Fatalf("hierarchical shrinkage missing: mean %.1f", shrunk.Mean)
	}
}

func TestPredictorUpdatesAreZeroHop(t *testing.T) {
	// Spec §13.13: prediction is a local function call — the predictor
	// lives in the scheduler process, no network, no retrain cycles.
	p := NewLatencyPredictor(DefaultPredictorConfig())
	start := timeNowUnixMs()
	for i := 0; i < 100; i++ {
		p.PredictTTFT("cfg", sampleFeatures())
	}
	elapsed := timeNowUnixMs() - start
	if elapsed > 500 {
		t.Fatalf("100 local predictions took %dms — far too slow for zero-hop", elapsed)
	}
}

func TestPredictorPairTTFTAndTPOT(t *testing.T) {
	p := NewLatencyPredictorPair(DefaultPredictorConfig())
	// Same config: TTFT slow, TPOT fast.
	for i := 0; i < 200; i++ {
		p.Observe("cfg", sampleFeatures(), 800, 30)
	}
	if p.PSLOViolation("cfg", sampleFeatures(), 500) < 0.9 {
		t.Fatal("slow TTFT must violate the 500ms SLO")
	}
	if p.PITLViolation("cfg", sampleFeatures(), 100) > 0.1 {
		t.Fatal("fast TPOT must satisfy the 100ms ITL SLO")
	}
}
