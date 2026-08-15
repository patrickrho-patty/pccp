package scheduler

import "testing"

func TestSLOResolverTargets(t *testing.T) {
	r := NewSLOResolver()
	r.SetModelTarget("model-a", SLOTarget{TTFTMs: 500, ITLMs: 40})
	r.SetDefault(SLOTarget{TTFTMs: 2000, ITLMs: 200})

	got, ok := r.Target("model-a")
	if !ok || got.TTFTMs != 500 {
		t.Fatalf("model target = %+v", got)
	}
	def, ok := r.Target("model-b")
	if !ok || def.TTFTMs != 2000 {
		t.Fatalf("default target = %+v", def)
	}
}

func TestSLOAwareRouteRejectsViolatingWorker(t *testing.T) {
	// A worker whose predicted TTFT violates the SLO with high
	// probability must lose to a slower-but-compliant one (risk-aware
	// routing, spec §13.3).
	p := NewLatencyPredictor(DefaultPredictorConfig())
	for i := 0; i < 200; i++ {
		p.Observe("slow-cfg", sampleFeatures(), 3000)
		p.Observe("fast-cfg", sampleFeatures(), 200)
	}

	r := NewCostRouter(DefaultRouterConfig())
	r.SetPredictor(p)
	r.SetSLOResolver(func() *SLOResolver {
		sl := NewSLOResolver()
		sl.SetDefault(SLOTarget{TTFTMs: 500, ITLMs: 50})
		return sl
	}())
	r.UpsertWorker(mkWorker("w-slow", "model-a", 8), RouterWorkerState{})
	r.UpsertWorker(mkWorker("w-fast", "model-a", 8), RouterWorkerState{})
	r.SetConfigForWorker("w-slow", "slow-cfg")
	r.SetConfigForWorker("w-fast", "fast-cfg")

	got, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 512, ExpectedOutputTokens: 200})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w-fast" {
		t.Fatalf("routed to %s, want w-fast (SLO-compliant)", got.WorkerID)
	}
}

func TestSLOObjectiveAgentPriority(t *testing.T) {
	// Agentic requests carry a tighter TTFT objective; the resolver
	// returns the request-specific target when set.
	sl := NewSLOResolver()
	sl.SetDefault(SLOTarget{TTFTMs: 2000, ITLMs: 200})
	sl.SetClassTarget("agentic", SLOTarget{TTFTMs: 800, ITLMs: 60})
	got := sl.ForRequest("any-model", "agentic")
	if got.TTFTMs != 800 {
		t.Fatalf("agentic TTFT = %d, want 800", got.TTFTMs)
	}
}

func TestMTPAcceptanceCapacityModel(t *testing.T) {
	// Spec §12.3.9: planner decode estimates must use MTP accepted-token
	// length (~1.8 accepted per verification) — otherwise decode-GPU
	// counts are badly overestimated.
	cap := NewMTPCapacity(1.8, 30.0) // 1.8 accepted tokens, 30 tok/s real
	got := cap.EffectiveTokensPerSecond()
	// 30 × 1.8 = 54 effective tokens/s.
	if got < 50 || got > 58 {
		t.Fatalf("effective tok/s = %.1f, want ≈54", got)
	}
	// Decode GPU count for 10k tok/s demand: ceil(10000/54) = 186.
	if n := cap.DecodeGPUsFor(10000); n != 186 {
		t.Fatalf("decode GPUs = %d, want 186", n)
	}
}
