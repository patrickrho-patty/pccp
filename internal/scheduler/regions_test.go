package scheduler

import "testing"

func TestSelectRegion(t *testing.T) {
	reg := NewRegionRegistry()

	// Empty registry: wanted region passes through (single-region no-op).
	if got, ok := reg.SelectRegion("kr"); !ok || got != "kr" {
		t.Fatalf("empty registry = %q,%v", got, ok)
	}
	// Unconstrained request: unchanged.
	if got, ok := reg.SelectRegion(""); !ok || got != "" {
		t.Fatalf("unconstrained = %q,%v", got, ok)
	}

	reg.SetHealth("kr", true)
	reg.SetHealth("jp", false)
	reg.SetHealth("us", true)
	reg.SetFailover("jp", []string{"kr", "us"})

	// Healthy wanted region serves directly.
	if got, ok := reg.SelectRegion("kr"); !ok || got != "kr" {
		t.Fatalf("healthy = %q,%v", got, ok)
	}
	// Unhealthy: first preauthorized healthy region wins.
	if got, ok := reg.SelectRegion("jp"); !ok || got != "kr" {
		t.Fatalf("failover = %q,%v want kr", got, ok)
	}

	// An unauthorized failover target is never chosen.
	reg2 := NewRegionRegistry()
	reg2.SetHealth("jp", false)
	reg2.SetHealth("us", true)
	// No failover configured for jp: refusing is the only honest answer.
	if got, ok := reg2.SelectRegion("jp"); ok || got != "" {
		t.Fatalf("unauthorized failover = %q,%v — must refuse", got, ok)
	}

	// All candidates unhealthy: clear availability failure.
	reg3 := NewRegionRegistry()
	reg3.SetHealth("jp", false)
	reg3.SetHealth("us", false)
	reg3.SetFailover("jp", []string{"us"})
	if got, ok := reg3.SelectRegion("jp"); ok || got != "" {
		t.Fatalf("all-unhealthy = %q,%v — must refuse", got, ok)
	}
}

func TestRouterGlobalStageFailover(t *testing.T) {
	reg := NewRegionRegistry()
	reg.SetHealth("kr", false)
	reg.SetHealth("us", true)
	reg.SetFailover("kr", []string{"us"})

	r := NewCostRouter(DefaultRouterConfig())
	rs := NewReceiptStore(8)
	r.SetReceipts(rs)
	r.SetRegions(reg)
	r.UpsertWorker(mkRegionWorker("w-kr", "model-a", "kr"), RouterWorkerState{})
	r.UpsertWorker(mkRegionWorker("w-us", "model-a", "us"), RouterWorkerState{})

	// kr is unhealthy: the request fails over to the preauthorized us
	// region and the receipt records the hierarchical path.
	got, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10, Region: "kr"})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w-us" {
		t.Fatalf("routed to %s, want w-us via failover", got.WorkerID)
	}
	rec := rs.Recent()[0]
	if rec.Path == nil || rec.Path.Region != "us" || rec.Path.Worker != "w-us" {
		t.Fatalf("receipt path = %+v, want us/w-us", rec.Path)
	}

	// Without preauthorization the same request refuses (permanent).
	reg2 := NewRegionRegistry()
	reg2.SetHealth("kr", false)
	r2 := NewCostRouter(DefaultRouterConfig())
	r2.SetRegions(reg2)
	r2.UpsertWorker(mkRegionWorker("w-us", "model-a", "us"), RouterWorkerState{})
	_, err = r2.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10, Region: "kr"})
	rerr, ok := err.(*RouteError)
	if !ok || !rerr.Permanent {
		t.Fatalf("err = %v, want permanent RouteError", err)
	}
}
