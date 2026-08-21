package scheduler

import (
	"testing"
	"time"
)

func TestStageQueuesDepthAndWait(t *testing.T) {
	now := time.UnixMilli(0)
	q := NewStageQueues()
	q.SetNow(func() time.Time { return now })

	q.Enter(StageLookup, "r1")
	q.Enter(StageLookup, "r2")
	if q.Depth(StageLookup) != 2 {
		t.Fatalf("depth = %d, want 2", q.Depth(StageLookup))
	}
	now = now.Add(120 * time.Millisecond)
	q.Leave(StageLookup, "r1")
	if q.Depth(StageLookup) != 1 {
		t.Fatalf("depth = %d, want 1", q.Depth(StageLookup))
	}
	now = now.Add(240 * time.Millisecond)
	q.Leave(StageLookup, "r2")

	snap := q.Snapshot()
	stat := snap[StageLookup]
	if stat.Completed != 2 || stat.AvgWaitMs != 240 || stat.MaxWaitMs != 360 {
		t.Fatalf("lookup stat = %+v", stat)
	}
	// All four stages are always present.
	for _, s := range []StageID{StageLookup, StagePrefill, StageTransfer, StageDecode} {
		if _, ok := snap[s]; !ok {
			t.Fatalf("snapshot missing stage %s", s)
		}
	}
	// Leaving a stage never entered is a safe no-op (no negative depth).
	q.Leave(StagePrefill, "ghost")
	if q.Depth(StagePrefill) != 0 {
		t.Fatalf("depth = %d, want 0", q.Depth(StagePrefill))
	}
}

func TestStagePlannerBackpressureRestoresCoLocated(t *testing.T) {
	inv := NewTopologyInventory()
	inv.AddNode("n1", TopologyNode{Zone: "z1", Rack: "r1"})
	inv.AddWorker("w-pre", "n1")
	inv.AddWorker("w-dec", "n1")

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
	// Engaged with a clear path: disaggregates.
	if plan := planner.Plan("model-a", "w-dec", 100); plan.Mode != StageDisaggregated {
		t.Fatalf("plan = %+v, want disaggregated", plan)
	}
	// A deep measured transfer queue restores co-located execution.
	queues := NewStageQueues()
	for i := 0; i < 4; i++ {
		queues.Enter(StageTransfer, "x")
	}
	planner.SetQueues(queues)
	if plan := planner.Plan("model-a", "w-dec", 100); plan.Mode != StageColocated {
		t.Fatalf("backpressured plan = %+v, want co-located", plan)
	}
	// Draining the queue re-enables disaggregation.
	for i := 0; i < 4; i++ {
		queues.Leave(StageTransfer, "x")
	}
	if plan := planner.Plan("model-a", "w-dec", 100); plan.Mode != StageDisaggregated {
		t.Fatalf("drained plan = %+v, want disaggregated", plan)
	}
}

func TestDispatcherStageQueueWiring(t *testing.T) {
	fw := &fakeStageForwarder{result: InferenceResult{Text: "staged", Usage: map[string]int{"prompt_tokens": 10, "completion_tokens": 3}}}
	d, _ := stagedDispatcher(t, fw)
	queues := NewStageQueues()
	d.SetStageQueues(queues)

	ch, err := d.Submit(queueRequest("r1", "tenant-1", "interactive-paid", "model-a"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	snap := queues.Snapshot()
	if snap[StageLookup].Completed != 1 {
		t.Fatalf("lookup completed = %d, want 1", snap[StageLookup].Completed)
	}
	if snap[StagePrefill].Completed != 1 || snap[StageTransfer].Completed != 1 || snap[StageDecode].Completed != 1 {
		t.Fatalf("staged snapshot = %+v", snap)
	}
	// Everything drained.
	for s, stat := range snap {
		if stat.Depth != 0 {
			t.Fatalf("stage %s depth = %d after completion", s, stat.Depth)
		}
	}
}
