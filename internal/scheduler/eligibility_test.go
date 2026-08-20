package scheduler

import "testing"

// mkRegionWorker builds a servable worker carrying a signed region.
func mkRegionWorker(id, model, region string) WorkerEntry {
	e := mkWorker(id, model, 8)
	e.Card.Region = region
	return e
}

func TestEligibilityReportCountsReasons(t *testing.T) {
	r := NewCostRouter(DefaultRouterConfig())
	rs := NewReceiptStore(8)
	r.SetReceipts(rs)
	// w1: eligible. w2: wrong model. w3: saturated.
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{})
	r.UpsertWorker(mkWorker("w2", "model-b", 8), RouterWorkerState{})
	r.UpsertWorker(mkWorker("w3", "model-a", 8), RouterWorkerState{
		Load: WorkerLoad{MaxConcurrent: 4, Active: 4},
	})

	if _, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10}); err != nil {
		t.Fatal(err)
	}
	rec := rs.Recent()[0]
	if rec.Eligibility == nil {
		t.Fatal("receipt carries no eligibility report")
	}
	if rec.Eligibility.Eligible != 1 {
		t.Fatalf("eligible = %d, want 1", rec.Eligibility.Eligible)
	}
	if rec.Eligibility.Filtered[ReasonModelMismatch] != 1 {
		t.Fatalf("model-mismatch = %d, want 1", rec.Eligibility.Filtered[ReasonModelMismatch])
	}
	if rec.Eligibility.Filtered[ReasonOverloaded] != 1 {
		t.Fatalf("overloaded = %d, want 1", rec.Eligibility.Filtered[ReasonOverloaded])
	}
}

func TestEligibilityRegionConstraint(t *testing.T) {
	r := NewCostRouter(DefaultRouterConfig())
	rs := NewReceiptStore(8)
	r.SetReceipts(rs)
	r.UpsertWorker(mkRegionWorker("w-kr", "model-a", "kr"), RouterWorkerState{})
	r.UpsertWorker(mkRegionWorker("w-us", "model-a", "us"), RouterWorkerState{})

	// A residency-constrained request only sees the matching region.
	got, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10, Region: "kr"})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w-kr" {
		t.Fatalf("routed to %s, want w-kr under region=kr", got.WorkerID)
	}
	rec := rs.Recent()[0]
	if rec.Eligibility.Region != "kr" {
		t.Fatalf("receipt region = %q, want kr", rec.Eligibility.Region)
	}
	if rec.Eligibility.Filtered[ReasonRegionMismatch] != 1 {
		t.Fatalf("region-mismatch = %d, want 1", rec.Eligibility.Filtered[ReasonRegionMismatch])
	}

	// An unconstrained request sees both regions (no region filtering).
	got, err = r.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w-kr" && got.WorkerID != "w-us" {
		t.Fatalf("routed to %s", got.WorkerID)
	}
	rec = rs.Recent()[1]
	if rec.Eligibility.Region != "" || rec.Eligibility.Eligible != 2 {
		t.Fatalf("unconstrained eligibility = %+v", rec.Eligibility)
	}

	// A region with no workers fails closed: no silent cross-region routing.
	if _, err = r.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10, Region: "eu"}); err == nil {
		t.Fatal("region with no workers must fail, not silently reroute")
	}
}

func TestEligibilitySignalsRecordChosenWorker(t *testing.T) {
	r := NewCostRouter(DefaultRouterConfig())
	rs := NewReceiptStore(8)
	r.SetReceipts(rs)
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{PrefillActive: 300, DecodeKV: 120, ActiveRequests: 2})

	if _, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10}); err != nil {
		t.Fatal(err)
	}
	rec := rs.Recent()[0]
	if rec.Signals == nil {
		t.Fatal("receipt carries no decision signals")
	}
	if rec.Signals.PrefillActive != 300 || rec.Signals.DecodeKV != 120 || rec.Signals.ActiveRequests != 2 {
		t.Fatalf("signals = %+v, want w1's measured load", rec.Signals)
	}
}
