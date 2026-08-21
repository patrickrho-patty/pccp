package scheduler

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func viewTestScheduler() *Scheduler {
	return &Scheduler{
		Registry: NewRegistry(time.Minute, time.Minute),
		KVDir:    NewKVDirectory(),
		PD:       NewPDController(NewPDPlanner(), nil),
		Programs: NewProgramRegistry(nil),
	}
}

func TestKVDirView(t *testing.T) {
	svc := viewTestScheduler()
	svc.KVDir.Add("w1", L1GPU, dirBlock("tenant-a", "h1", 100), testIdentity, true)
	svc.KVDir.Add("w2", L2CPU, dirBlock("tenant-a", "h2", 100), testIdentity, false)
	for i := 0; i < 4; i++ {
		svc.KVDir.Hit("tenant-a", "h1", testIdentity)
	}
	o := &Observability{svc: svc}
	sum, ok := o.KVDirView().(DirectorySummary)
	if !ok {
		t.Fatal("KVDirView did not return a DirectorySummary")
	}
	if sum.Extents != 2 || sum.LocationsVerified != 1 || sum.LocationsUnverified != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if sum.ByTier["L1-hbm"] != 1 || sum.ByTier["L2-host"] != 1 {
		t.Fatalf("tiers = %+v", sum.ByTier)
	}
	if len(sum.HotPrefixes) != 1 || sum.HotPrefixes[0].Hash != "h1" {
		t.Fatalf("hot = %+v", sum.HotPrefixes)
	}
}

func TestPDView(t *testing.T) {
	svc := viewTestScheduler()
	pre := mkWorker("w-pre", "model-a", 8)
	pre.Card.PDRole = PDRolePrefill
	dec := mkWorker("w-dec", "model-a", 8)
	dec.Card.PDRole = PDRoleDecode
	svc.PD.planner.Upsert(pre, RouterWorkerState{})
	svc.PD.planner.Upsert(dec, RouterWorkerState{})
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Registry.Register(pre.Card, pub, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Registry.Register(dec.Card, pub, time.Now()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		svc.PD.Evaluate("model-a", 0.9)
	}

	o := &Observability{svc: svc, registry: svc.Registry}
	views, ok := o.PDView().([]PDModelView)
	if !ok || len(views) != 1 {
		t.Fatalf("PDView = %+v", o.PDView())
	}
	v := views[0]
	if v.Model != "model-a" || v.Prefill != 1 || v.Decode != 1 || v.Aggregated != 0 {
		t.Fatalf("view = %+v", v)
	}
	if !v.Engaged || v.PrefillShare <= 0.6 {
		t.Fatalf("engagement = %+v", v)
	}
}

func TestProgramsAndShadowViews(t *testing.T) {
	svc := viewTestScheduler()
	svc.Programs.Turn("p1", "tenant-a", "", CacheIdentity{}, "", 1, 0)
	svc.Programs.ToolPaused("p1")

	rs := NewReceiptStore(16)
	rs.Add(RoutingReceipt{
		Decision:      RouteDecision{WorkerID: "w1"},
		PolicyVersion: CostRouterVersion,
		Shadow:        &ShadowRecord{CandidateVersion: "cand/v1", WorkerID: "w1", Agree: true},
		Eligibility:   &EligibilityReport{Eligible: 1, Filtered: map[IneligibilityReason]int{ReasonOverloaded: 2}},
	})
	rs.Add(RoutingReceipt{
		Decision:      RouteDecision{WorkerID: "w2"},
		PolicyVersion: CostRouterVersion,
		Shadow:        &ShadowRecord{CandidateVersion: "cand/v1", WorkerID: "w9", Agree: false},
	})

	o := &Observability{svc: svc}
	o.SetReceipts(rs)

	pv, ok := o.ProgramsView().(map[string]interface{})
	if !ok || pv["programs"] != 1 || pv["tool_paused"] != 1 {
		t.Fatalf("programs view = %+v", pv)
	}

	sv, ok := o.ShadowView().(map[string]interface{})
	if !ok {
		t.Fatal("shadow view type")
	}
	if sv["receipts"] != 2 || sv["shadowed"] != 2 || sv["agree"] != 1 {
		t.Fatalf("shadow view = %+v", sv)
	}
	if sv["agreement_rate"] != 0.5 {
		t.Fatalf("agreement rate = %v", sv["agreement_rate"])
	}
	filtered := sv["filtered"].(map[string]int)
	if filtered["overloaded"] != 2 {
		t.Fatalf("filtered = %+v", filtered)
	}
	versions := sv["policy_versions"].(map[string]int)
	if versions[CostRouterVersion] != 2 {
		t.Fatalf("versions = %+v", versions)
	}
}
