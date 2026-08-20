package scheduler

import "testing"

func TestRouterPicksLowestCost(t *testing.T) {
	r := NewCostRouter(DefaultRouterConfig())
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{PrefillActive: 500, DecodeKV: 200, ActiveRequests: 2})
	r.UpsertWorker(mkWorker("w2", "model-a", 8), RouterWorkerState{PrefillActive: 5000, DecodeKV: 5000, ActiveRequests: 6})

	req := RouteRequest{
		Model:                "model-a",
		InputTokens:          1000,
		CachedTokens:         0,
		ExpectedOutputTokens: 100,
	}
	got, err := r.Route(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w1" {
		t.Fatalf("routed to %s, want w1 (lowest cost)", got.WorkerID)
	}
}

func TestRouterKVOverlapCredits(t *testing.T) {
	// Spec §12.3.1: cached input tokens discount the prefill cost —
	// a busy worker with a warm prefix can beat an idle cold worker.
	r := NewCostRouter(DefaultRouterConfig())
	kv := NewKVIndex()
	kv.Add("w1", KVBlock{Namespace: "tenant-a", Hash: "prefix-h", Tokens: 800})
	r.SetKV(kv)
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{PrefillActive: 300, DecodeKV: 100, ActiveRequests: 1})
	r.UpsertWorker(mkWorker("w2", "model-a", 8), RouterWorkerState{PrefillActive: 0, DecodeKV: 0, ActiveRequests: 0})

	req := RouteRequest{
		Model:                "model-a",
		Namespace:            "tenant-a",
		PrefixHash:           "prefix-h",
		InputTokens:          1000,
		CachedTokens:         800, // served from the KV cache
		ExpectedOutputTokens: 100,
	}
	got, err := r.Route(req)
	if err != nil {
		t.Fatal(err)
	}
	// w1: prefillScale × max(300+200−800,0) + ... ≈ 0 prefill cost;
	// w2: prefillScale × (0+1000) — w1 wins despite being busier.
	if got.WorkerID != "w1" {
		t.Fatalf("routed to %s, want w1 (KV overlap credits)", got.WorkerID)
	}
}

func TestRouterAffinityIsPreferenceNotPin(t *testing.T) {
	r := NewCostRouter(DefaultRouterConfig())
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{PrefillActive: 0, DecodeKV: 0, ActiveRequests: 0})
	// w2 has the session affinity but is overloaded: affinity must break.
	r.UpsertWorker(mkWorker("w2", "model-a", 8), RouterWorkerState{PrefillActive: 9000, DecodeKV: 9000, ActiveRequests: 7})

	req := RouteRequest{
		Model:                "model-a",
		InputTokens:          100,
		ExpectedOutputTokens: 50,
		AffinityWorker:       "w2",
	}
	got, err := r.Route(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID == "w2" {
		t.Fatal("affinity pinned an overloaded worker — must break (§12.3.2)")
	}
	if got.WorkerID != "w1" {
		t.Fatalf("routed to %s, want w1", got.WorkerID)
	}

	// Healthy affinity worker wins ties.
	r2 := NewCostRouter(DefaultRouterConfig())
	r2.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{})
	r2.UpsertWorker(mkWorker("w2", "model-a", 8), RouterWorkerState{})
	got2, err := r2.Route(RouteRequest{
		Model: "model-a", InputTokens: 100, ExpectedOutputTokens: 50,
		AffinityWorker: "w2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got2.WorkerID != "w2" {
		t.Fatalf("healthy affinity ignored: routed to %s", got2.WorkerID)
	}
}

func TestRouterMediaHashPreference(t *testing.T) {
	// Media-hash KV routing (§12.3.6): a repeated-image conversation must
	// prefer the worker holding the warm encoder state.
	r := NewCostRouter(DefaultRouterConfig())
	kv := NewKVIndex()
	kv.Add("w1", KVBlock{Namespace: "tenant-a", Hash: "ctx", Tokens: 50, MediaHash: "m1"})
	r.SetKV(kv)
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{})
	r.UpsertWorker(mkWorker("w2", "model-a", 8), RouterWorkerState{})

	got, err := r.Route(RouteRequest{
		Model: "model-a", Namespace: "tenant-a", PrefixHash: "ctx",
		InputTokens: 100, ExpectedOutputTokens: 50, MediaHash: "m1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w1" {
		t.Fatalf("media-hash routing = %s, want w1 (warm encoder state)", got.WorkerID)
	}
}

func TestRouterNoEligibleWorker(t *testing.T) {
	r := NewCostRouter(DefaultRouterConfig())
	r.UpsertWorker(mkWorker("w1", "model-b", 8), RouterWorkerState{})
	_, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10})
	if err == nil {
		t.Fatal("route to an unserved model must fail")
	}
}

func TestRouterOverloadFilter(t *testing.T) {
	// Spec §12.3.1: overload filters — saturated workers are ineligible
	// regardless of cost.
	r := NewCostRouter(DefaultRouterConfig())
	hot := mkWorker("w1", "model-a", 8)
	r.UpsertWorker(hot, RouterWorkerState{PrefillActive: 99999, DecodeKV: 99999, ActiveRequests: 8, Load: WorkerLoad{MaxConcurrent: 8, Active: 8}})
	r.UpsertWorker(mkWorker("w2", "model-a", 8), RouterWorkerState{PrefillActive: 100, DecodeKV: 50, ActiveRequests: 1})

	got, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 100, ExpectedOutputTokens: 50})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w2" {
		t.Fatalf("routed to %s, want w2 (w1 saturated)", got.WorkerID)
	}
}

func TestRouterGangIncompleteWorkerExcluded(t *testing.T) {
	r := NewCostRouter(DefaultRouterConfig())
	gr := NewGangRegistry()
	// w1 is one of two TP ranks but w2 is missing: w1 must be ineligible.
	w1 := gangWorker("w1", "model-a", 2, 1, 1)
	w3 := gangWorker("w3", "model-a", 1, 1, 1)
	gr.Upsert(w1)
	gr.Upsert(w3)
	r.SetGang(gr)
	r.UpsertWorker(w1, RouterWorkerState{})
	r.UpsertWorker(w3, RouterWorkerState{})

	got, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 100, ExpectedOutputTokens: 50})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w3" {
		t.Fatalf("routed to %s, want w3 (w1's gang incomplete)", got.WorkerID)
	}
}

func TestRouterLoRAAffinity(t *testing.T) {
	// Spec §14 row 18: LoRA affinity — a request for an adapter prefers
	// the worker with it resident.
	r := NewCostRouter(DefaultRouterConfig())
	ll := NewLoRaLifecycle(4)
	ll.Load("w1", "lora-x")
	r.SetLoRA(ll)
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{})
	r.UpsertWorker(mkWorker("w2", "model-a", 8), RouterWorkerState{})

	got, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 100, ExpectedOutputTokens: 50, LoRAAdapter: "lora-x"})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w1" {
		t.Fatalf("LoRA affinity ignored: routed to %s", got.WorkerID)
	}
}

func TestRouterModelPoolScoping(t *testing.T) {
	// Spec §14 row 17: model pools — pool-scoped requests only see
	// workers in that pool.
	r := NewCostRouter(DefaultRouterConfig())
	pm := NewModelPoolManager()
	pm.Add("pool-blue", "w1")
	pm.Add("pool-green", "w2")
	r.SetPools(pm)
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{})
	r.UpsertWorker(mkWorker("w2", "model-a", 8), RouterWorkerState{})

	got, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 100, ExpectedOutputTokens: 50, Pool: "pool-blue"})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w1" {
		t.Fatalf("pool scoping failed: routed to %s", got.WorkerID)
	}
}
