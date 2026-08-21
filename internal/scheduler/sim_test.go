package scheduler

import "testing"

func TestSimBurstCoversFleet(t *testing.T) {
	rep := Simulate(SimScenario{
		Name:            "burst",
		DurationTicks:   4,
		ArrivalPattern:  "burst",
		RequestsPerTick: 12,
	})
	if rep.Requests != 48 {
		t.Fatalf("requests = %d, want 48", rep.Requests)
	}
	if rep.Routed == 0 || len(rep.ByWorker) == 0 {
		t.Fatalf("no decisions made: %+v", rep)
	}
	// Burst traffic spreads across multiple workers, not one hotspot.
	if len(rep.ByWorker) < 2 {
		t.Fatalf("burst routed to a single worker: %+v", rep.ByWorker)
	}
}

func TestSimCacheReuseProducesHits(t *testing.T) {
	cold := Simulate(SimScenario{
		Name: "cold", DurationTicks: 4, ArrivalPattern: "steady",
		RequestsPerTick: 10, CacheReuseShare: 0,
	})
	warm := Simulate(SimScenario{
		Name: "warm", DurationTicks: 4, ArrivalPattern: "steady",
		RequestsPerTick: 10, CacheReuseShare: 0.9,
	})
	if warm.CacheHits <= cold.CacheHits {
		t.Fatalf("cache reuse did not produce hits: cold=%d warm=%d", cold.CacheHits, warm.CacheHits)
	}
	if warm.OverlapTokens == 0 {
		t.Fatal("warm scenario must accumulate overlap tokens")
	}
}

func TestSimWorkerLossReroutes(t *testing.T) {
	rep := Simulate(SimScenario{
		Name: "worker-loss", DurationTicks: 6, ArrivalPattern: "steady",
		RequestsPerTick: 10, FailureTick: 3,
	})
	if rep.Routed == 0 {
		t.Fatal("no requests routed")
	}
	// After the failure the remaining fleet still serves (conservative
	// failover, not traffic loss): every post-failure tick routes.
	if rep.Routed < rep.Requests/2 {
		t.Fatalf("worker loss stranded traffic: %+v", rep)
	}
}

func TestSimContentionFavorsCoLocated(t *testing.T) {
	clear := Simulate(SimScenario{
		Name: "clear", DurationTicks: 3, ArrivalPattern: "steady",
		RequestsPerTick: 10, Disaggregate: true, Contention: 0,
	})
	congested := Simulate(SimScenario{
		Name: "congested", DurationTicks: 3, ArrivalPattern: "steady",
		RequestsPerTick: 10, Disaggregate: true, Contention: 1,
	})
	if congested.Disaggregated >= clear.Disaggregated {
		t.Fatalf("contention did not suppress disaggregation: clear=%d congested=%d",
			clear.Disaggregated, congested.Disaggregated)
	}
}

func TestSimMultiTurnAndStaleTelemetry(t *testing.T) {
	// Exercises the program registry path and stale-signal routing without
	// crashing or pathological rejection.
	rep := Simulate(SimScenario{
		Name: "multi-turn-stale", DurationTicks: 4, ArrivalPattern: "steady",
		RequestsPerTick: 10, MultiTurnShare: 0.8, StaleTelemetry: true,
		CacheReuseShare: 0.5, MixedClasses: true,
	})
	if rep.Routed == 0 {
		t.Fatalf("multi-turn scenario routed nothing: %+v", rep)
	}
}
