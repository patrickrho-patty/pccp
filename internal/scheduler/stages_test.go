package scheduler

import (
	"testing"
	"time"
)

func TestStaticTopologyOracle(t *testing.T) {
	inv := NewTopologyInventory()
	inv.AddNode("n1", TopologyNode{Zone: "z1", Rack: "r1"})
	inv.AddNode("n2", TopologyNode{Zone: "z1", Rack: "r2"})
	inv.AddWorker("w1", "n1")
	inv.AddWorker("w1b", "n1")
	inv.AddWorker("w2", "n2")

	o := NewStaticTopologyOracle(inv)
	const gb = int64(1) << 30

	// Same node: NVLink grade.
	ms, ok := o.TransferCostMs("w1", "w1b", gb)
	if !ok || ms != 0.05+3.0 {
		t.Fatalf("nvlink transfer = %v,%v", ms, ok)
	}
	// Cross rack: ethernet grade.
	ms, ok = o.TransferCostMs("w1", "w2", gb)
	if !ok || ms != 1.0+120.0 {
		t.Fatalf("ethernet transfer = %v,%v", ms, ok)
	}
	// Unknown worker: no estimate — caller falls back conservatively.
	if _, ok = o.TransferCostMs("w1", "ghost", gb); ok {
		t.Fatal("unknown worker must yield no estimate")
	}
	if o.Freshness() != 0 {
		t.Fatal("static oracle freshness must be zero")
	}
	// Nil inventory: no estimate, never a crash.
	if _, ok = NewStaticTopologyOracle(nil).TransferCostMs("a", "b", gb); ok {
		t.Fatal("nil inventory must yield no estimate")
	}
}

func TestStagePlannerFallbacks(t *testing.T) {
	inv := NewTopologyInventory()
	inv.AddNode("n1", TopologyNode{Zone: "z1", Rack: "r1"})
	inv.AddWorker("w-pre", "n1")
	inv.AddWorker("w-dec", "n1")
	oracle := NewStaticTopologyOracle(inv)

	pd := NewPDPlanner()
	pre := mkWorker("w-pre", "model-a", 8)
	pre.Card.PDRole = PDRolePrefill
	dec := mkWorker("w-dec", "model-a", 8)
	dec.Card.PDRole = PDRoleDecode
	pd.Upsert(pre, RouterWorkerState{})
	pd.Upsert(dec, RouterWorkerState{})

	planner := NewStagePlanner(pd, oracle, nil)

	// Not engaged: co-located regardless of roles.
	plan := planner.Plan("model-a", "w-dec", 4096)
	if plan.Mode != StageColocated || plan.PrefillWorker != "w-dec" {
		t.Fatalf("unengaged plan = %+v", plan)
	}

	// Engage sustained prefill pressure (the EMA needs sustained
	// samples, not a blip, to cross the threshold).
	for i := 0; i < 10; i++ {
		pd.ObservePrefillShare("model-a", 0.9)
	}
	if !pd.ShouldDisaggregate("model-a") {
		t.Fatal("planner should disaggregate on sustained 0.9 share")
	}
	plan = planner.Plan("model-a", "w-dec", 4096)
	if plan.Mode != StageDisaggregated || plan.PrefillWorker != "w-pre" || plan.DecodeWorker != "w-dec" {
		t.Fatalf("disaggregated plan = %+v", plan)
	}
	if plan.TransferMs <= 0 || plan.KVBytes <= 0 {
		t.Fatalf("plan missing transfer pricing: %+v", plan)
	}

	// No prefill-capable peer: co-located fallback.
	pd2 := NewPDPlanner()
	for i := 0; i < 10; i++ {
		pd2.ObservePrefillShare("model-a", 0.9)
	}
	pd2.Upsert(dec, RouterWorkerState{})
	plan = NewStagePlanner(pd2, oracle, nil).Plan("model-a", "w-dec", 4096)
	if plan.Mode != StageColocated {
		t.Fatalf("no prefill peer must fall back co-located: %+v", plan)
	}

	// Unpriceable transfer: co-located fallback.
	plan = planner.Plan("model-a", "w-ghost", 4096)
	if plan.Mode != StageColocated {
		t.Fatalf("unpriced transfer must fall back co-located: %+v", plan)
	}
}

func TestStagePlannerTransferBudget(t *testing.T) {
	inv := NewTopologyInventory()
	inv.AddNode("n1", TopologyNode{Zone: "z1", Rack: "r1"})
	inv.AddNode("n2", TopologyNode{Zone: "z2", Rack: "r9"})
	inv.AddWorker("w-pre", "n1")
	inv.AddWorker("w-dec", "n2") // ethernet path: slow for huge transfers

	pd := NewPDPlanner()
	pre := mkWorker("w-pre", "model-a", 8)
	pre.Card.PDRole = PDRolePrefill
	dec := mkWorker("w-dec", "model-a", 8)
	dec.Card.PDRole = PDRoleDecode
	pd.Upsert(pre, RouterWorkerState{})
	pd.Upsert(dec, RouterWorkerState{})
	for i := 0; i < 10; i++ {
		pd.ObservePrefillShare("model-a", 0.9)
	}

	planner := NewStagePlanner(pd, NewStaticTopologyOracle(inv), nil)
	planner.SetTransferBudget(5) // tight TTFT budget for the guard test
	// A 1M-token prompt prices ~16ms over ethernet: beyond the 5ms budget,
	// the plan must fall back co-located instead of missing TTFT.
	plan := planner.Plan("model-a", "w-dec", 1<<20)
	if plan.Mode != StageColocated {
		t.Fatalf("over-budget transfer planned disaggregated: %+v", plan)
	}
}

func TestPDControllerHysteresisAndCooldown(t *testing.T) {
	c := NewPDController(NewPDPlanner(), nil)
	now := time.Now()
	c.SetNow(func() time.Time { return now })

	// Engage above the threshold.
	if !c.Evaluate("model-a", 0.7) {
		t.Fatal("should engage above 0.65")
	}
	// Inside the hysteresis band: stays engaged.
	if !c.Evaluate("model-a", 0.6) {
		t.Fatal("hysteresis: 0.6 must not release (band 0.55–0.65)")
	}
	// Below the band but inside cooldown: holds engaged.
	if !c.Evaluate("model-a", 0.4) {
		t.Fatal("cooldown must hold the current state")
	}
	// Past cooldown, below the band: releases.
	now = now.Add(3 * time.Minute)
	if c.Evaluate("model-a", 0.4) {
		t.Fatal("should release below 0.55 after cooldown")
	}
	// Flapping inside cooldown: no re-engage.
	if c.Evaluate("model-a", 0.9) {
		t.Fatal("cooldown must prevent immediate re-engagement")
	}
	if c.MinColocated() < 1 {
		t.Fatal("co-located capacity floor must be at least one")
	}
}

func TestPDControllerDeflectionGuard(t *testing.T) {
	// No predictor: never deflect (conservative).
	c := NewPDController(NewPDPlanner(), nil)
	if c.MayDeflect("cfg", PredictorFeatures{}, 150, 0.5) {
		t.Fatal("deflection without predictor evidence must be refused")
	}

	pair := NewLatencyPredictorPair(DefaultPredictorConfig())
	c = NewPDController(NewPDPlanner(), pair)
	// Untrained pair: huge prior variance → high risk → refuse.
	if c.MayDeflect("cfg", PredictorFeatures{}, 150, 0.5) {
		t.Fatal("deflection with uncalibrated predictions must be refused")
	}
	// Train the TPOT twin tight and well under the SLO.
	f := PredictorFeatures{InputTokens: 100, ExpectedOutputTokens: 10}
	for i := 0; i < 60; i++ {
		pair.Observe("cfg", f, 100, 20)
	}
	if !c.MayDeflect("cfg", f, 150, 0.5) {
		t.Fatal("calibrated low TBT risk should permit bounded deflection")
	}
	// A zero SLO budget always refuses.
	if c.MayDeflect("cfg", f, 0, 0.5) {
		t.Fatal("zero TBT budget must refuse deflection")
	}
}
