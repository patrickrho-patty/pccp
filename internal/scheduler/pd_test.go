package scheduler

import "testing"

func pdWorker(id, model, engine, role string) WorkerEntry {
	e := mkWorker(id, model, 8)
	e.Card.EngineKind = engine
	e.Card.PDRole = role
	return e
}

func TestPDDefaultIsAggregated(t *testing.T) {
	// Spec §12.3.10: aggregated serving first — every worker defaults to
	// aggregated unless its card declares a P/D role.
	e := mkWorker("w1", "model-a", 8)
	if got := e.Card.EffectivePDRole(); got != PDRoleAggregated {
		t.Fatalf("default role = %s, want aggregated", got)
	}
}

func TestPDAggregatedServesBothPhases(t *testing.T) {
	p := NewPDPlanner()
	p.Upsert(pdWorker("w1", "model-a", "vllm", PDRoleAggregated), RouterWorkerState{})
	got := p.Place("model-a", PDPhaseDecode)
	if len(got) != 1 || got[0] != "w1" {
		t.Fatalf("aggregated placement = %v, want [w1]", got)
	}
	got = p.Place("model-a", PDPhasePrefill)
	if len(got) != 1 || got[0] != "w1" {
		t.Fatalf("aggregated prefill placement = %v", got)
	}
}

func TestPDSeparateRoles(t *testing.T) {
	p := NewPDPlanner()
	p.Upsert(pdWorker("pf1", "model-a", "vllm", PDRolePrefill), RouterWorkerState{})
	p.Upsert(pdWorker("dc1", "model-a", "vllm", PDRoleDecode), RouterWorkerState{})

	if got := p.Place("model-a", PDPhasePrefill); len(got) != 1 || got[0] != "pf1" {
		t.Fatalf("prefill placement = %v, want [pf1]", got)
	}
	if got := p.Place("model-a", PDPhaseDecode); len(got) != 1 || got[0] != "dc1" {
		t.Fatalf("decode placement = %v, want [dc1]", got)
	}
}

func TestPDSGLangConditionalUnsupported(t *testing.T) {
	// Spec §12.3.10: conditional disaggregation is NOT supported with
	// SGLang — the planner must refuse, not silently misroute.
	p := NewPDPlanner()
	p.Upsert(pdWorker("s1", "model-a", "sglang", PDRolePrefill), RouterWorkerState{})
	p.Upsert(pdWorker("s2", "model-a", "sglang", PDRoleDecode), RouterWorkerState{})
	if got := p.Place("model-a", PDPhasePrefill); len(got) != 0 {
		t.Fatalf("SGLang conditional P/D must refuse placement, got %v", got)
	}
}

func TestPDConditionalSplitDecision(t *testing.T) {
	// Conditional P/D: engage only when traces show long prefills
	// hurting decode (spec §12.3.10).
	p := NewPDPlanner()
	p.Upsert(pdWorker("a1", "model-a", "vllm", PDRoleAggregated), RouterWorkerState{})
	p.ObservePrefillShare("model-a", 0.2)
	if p.ShouldDisaggregate("model-a") {
		t.Fatal("short prefills must not trigger disaggregation")
	}
	for i := 0; i < 10; i++ {
		p.ObservePrefillShare("model-a", 0.8)
	}
	if !p.ShouldDisaggregate("model-a") {
		t.Fatal("sustained long prefills must trigger conditional P/D")
	}
}
