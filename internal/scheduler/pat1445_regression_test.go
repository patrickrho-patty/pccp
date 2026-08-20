package scheduler

import (
	"errors"
	"testing"
	"time"
)

// TestSyncRouterFeedsTopology covers the WS2 wiring: registered workers'
// signed card node/zone populate the oracle's static fallback.
func TestSyncRouterFeedsTopology(t *testing.T) {
	fx := newWorkerFixture(t)
	s := NewScheduler(fx.trust, nil, 30*time.Second, 60*time.Second, testEvidenceKey(t))

	c1 := fx.card
	c1.WorkerID = "w-a"
	c1.NodeID = "node-1"
	c1.Zone = "z1"
	c1.CardVersion = 2
	c1.DariAddr = "10.0.0.1:9444"
	c2 := fx.card
	c2.WorkerID = "w-b"
	c2.NodeID = "node-2"
	c2.Zone = "z1"
	c2.CardVersion = 2
	c2.DariAddr = "10.0.0.2:9444"
	now := time.Now()
	if _, err := s.Registry.Register(c1, fx.subjectPub, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Registry.Register(c2, fx.subjectPub, now); err != nil {
		t.Fatal(err)
	}
	s.SyncRouter()

	// The same card feed must reach the P/D role view (single fan-out point).
	if pre, dec, agg := s.PD.RoleCounts(c1.ModelName); pre+dec+agg != 2 {
		t.Fatalf("planner sees %d/%d/%d workers, want 2 total", pre, dec, agg)
	}

	oracle := NewStaticTopologyOracle(s.Topology)
	// Same node prices NVLink-grade.
	if ms, ok := oracle.TransferCostMs("w-a", "w-a", 1<<20); !ok || ms > 1.0 {
		t.Fatalf("same-node transfer = %v,%v", ms, ok)
	}
	// Cross-node prices the conservative ethernet grade (no rack telemetry).
	ms, ok := oracle.TransferCostMs("w-a", "w-b", 1<<30)
	if !ok {
		t.Fatal("cross-node transfer must be priced from topology")
	}
	if ms < 100 {
		t.Fatalf("cross-node transfer = %vms, want conservative ethernet grade", ms)
	}
}

// TestToolPausedErrorDoesNotPause: an errored completion is not a tool
// pause — the program did not end waiting on a tool.
func TestToolPausedErrorDoesNotPause(t *testing.T) {
	reg := NewProgramRegistry(nil)
	d := NewDispatcher(nil)
	d.SetPrograms(reg)
	d.SetForwarder(&fakeForwarder{err: errors.New("worker exploded")})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	qr := queueRequest("r1", "tenant-1", "interactive-paid", "model-a")
	qr.ProgramID = "p-err"
	qr.ToolPaused = true
	ch, err := d.Submit(qr)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
	if reg.Paused("p-err") {
		t.Fatal("errored tool-paused completion must not pause the program")
	}
}

// TestSweepEvictsFromFleet: lease expiry removes the worker from every
// consumer through the shared fleet — the router cannot route to it.
func TestSweepEvictsFromFleet(t *testing.T) {
	fx := newWorkerFixture(t)
	s := NewScheduler(fx.trust, nil, 30*time.Second, 60*time.Second, testEvidenceKey(t))
	card := fx.card
	card.CardVersion = 2
	card.DariAddr = "10.0.0.1:9444"
	if _, err := s.Registry.Register(card, fx.subjectPub, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.SyncRouter()
	if _, ok := s.Fleet.Get(card.WorkerID); !ok {
		t.Fatal("worker missing from fleet after sync")
	}
	evicted := s.Sweep(time.Now().Add(10 * time.Minute))
	if len(evicted) != 1 {
		t.Fatalf("evicted = %v", evicted)
	}
	if _, ok := s.Fleet.Get(card.WorkerID); ok {
		t.Fatal("evicted worker still in fleet")
	}
	if _, err := s.Router().Route(RouteRequest{Model: card.ModelName, InputTokens: 10, ExpectedOutputTokens: 10}); err == nil {
		t.Fatal("router still routes to the evicted worker")
	}
}

// TestPauseRetainDoesNotInflateHits: a pause-hold refreshes last-use
// without counting as reuse — the hot-prefix signal stays clean.
func TestPauseRetainDoesNotInflateHits(t *testing.T) {
	dir := NewKVDirectory()
	dir.SetNow(func() int64 { return 0 })
	dir.Add("w1", L1GPU, dirBlock("tenant-a", "ph", 1000), testIdentity, true)
	reg := NewProgramRegistry(dir)
	now := time.Now()
	reg.SetNow(func() time.Time { return now })

	reg.Turn("p1", "tenant-a", "ph", testIdentity, "w1", 1)
	if act := reg.ToolPaused("p1"); act != KVActionRetain {
		t.Fatalf("first pause = %s, want retain", act)
	}
	if hot := dir.HotPrefixes(1, 4); len(hot) != 0 {
		t.Fatalf("pause-hold inflated the hot-prefix signal: %+v", hot)
	}
}
