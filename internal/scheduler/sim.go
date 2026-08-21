package scheduler

import (
	"math/rand"
	"time"
)

// sim.go implements the PAT-1445 simulation harness (criterion 13,
// §simulation and replay): scenario-driven workloads run through the REAL
// cost router, stage planner, KV directory, and program registry — no
// reimplemented engine. Scenarios cover bursty and heavy-tailed arrivals,
// long prompts, repeated prefixes, multi-turn tool-using programs, mixed
// traffic classes, mixed GPU types and regions, network contention,
// worker loss, stale telemetry, and co-located versus disaggregated
// execution.

// SimScenario describes one workload simulation.
type SimScenario struct {
	Name            string
	DurationTicks   int
	ArrivalPattern  string  // "steady" | "burst" | "heavytail"
	RequestsPerTick int     // base arrival rate per tick
	LongPromptShare float64 // fraction of requests with 64K-token prompts
	MultiTurnShare  float64 // fraction of requests that are program turns
	CacheReuseShare float64 // fraction of requests reusing a shared prefix
	MixedClasses    bool    // mix interactive/agentic/batch traffic
	Contention      float64 // 0..1 network contention (scales transfer cost)
	FailureTick     int     // tick at which one worker is lost (0 = never)
	StaleTelemetry  bool    // freeze worker load at tick 0 (stale signals)
	Disaggregate    bool    // engage P/D disaggregation for the model
}

// SimReport summarizes one simulation run.
type SimReport struct {
	Requests      int            `json:"requests"`
	Routed        int            `json:"routed"`
	Rejected      int            `json:"rejected"`
	CacheHits     int            `json:"cache_hits"`
	OverlapTokens int            `json:"overlap_tokens"`
	Disaggregated int            `json:"disaggregated"`
	Colocated     int            `json:"colocated"`
	ByWorker      map[string]int `json:"by_worker"`
	MeanCost      float64        `json:"mean_cost"`
}

// contentionOracle scales transfer costs by network contention.
type contentionOracle struct {
	base  NetworkOracle
	scale float64
}

func (c contentionOracle) TransferCostMs(src, dst string, kvBytes int64) (float64, bool) {
	ms, ok := c.base.TransferCostMs(src, dst, kvBytes)
	return ms * c.scale, ok
}

func (c contentionOracle) Freshness() time.Duration { return c.base.Freshness() }

// simIdentity is the scenario fleet's shared cache identity.
var simIdentity = CacheIdentity{ModelPackage: "model-a@1.0", TokenizerID: "tok", TemplateID: "tpl", PolicyEpoch: "e1"}

// simWorker builds one synthetic worker for the scenario fleet (the
// non-test constructor; mkWorker lives in test files).
func simWorker(id, model string, seqs uint64) WorkerEntry {
	return WorkerEntry{
		Card: WorkerCard{
			CardVersion:       2,
			DariAddr:          id + ":9444",
			WorkerID:          id,
			ModelName:         model,
			MaxConcurrentSeqs: seqs,
			Status:            "ready",
		},
		LeasedUntil: time.Now().Add(time.Hour),
	}
}

// simFleet builds the mixed synthetic fleet: H100 and A100 workers in two
// regions, one prefill + one decode role pair when disaggregating.
func simFleet(disaggregate bool) (*WorkerFleet, *TopologyInventory) {
	fleet := NewWorkerFleet()
	inv := NewTopologyInventory()
	inv.AddNode("n-kr", TopologyNode{Zone: "z1", Rack: "r1"})
	inv.AddNode("n-us", TopologyNode{Zone: "z2", Rack: "r9"})
	add := func(id, gpu, node string, seqs uint64) {
		e := simWorker(id, "model-a", seqs)
		e.Card.GPUSKU = gpu
		e.Card.NodeID = node
		inv.AddWorker(id, node)
		fleet.Upsert(e, RouterWorkerState{Load: WorkerLoad{MaxConcurrent: int(seqs)}})
	}
	add("w-kr-h100", "H100", "n-kr", 16)
	add("w-us-h100", "H100", "n-us", 16)
	add("w-kr-a100", "A100", "n-kr", 8)
	if disaggregate {
		fleet.Mutate("w-kr-h100", func(w *FleetWorker) { w.Entry.Card.PDRole = PDRolePrefill })
		fleet.Mutate("w-us-h100", func(w *FleetWorker) { w.Entry.Card.PDRole = PDRoleDecode })
	}
	return fleet, inv
}

// Simulate runs the scenario through the real components with a
// deterministic arrival process.
func Simulate(sc SimScenario) SimReport {
	rng := rand.New(rand.NewSource(42))
	fleet, inv := simFleet(sc.Disaggregate)

	dir := NewKVDirectory()
	router := NewCostRouter(DefaultRouterConfig())
	router.SetFleet(fleet)
	router.SetKVDirectory(dir)

	var oracle NetworkOracle = NewStaticTopologyOracle(inv)
	if sc.Contention > 0 {
		oracle = contentionOracle{base: oracle, scale: 1 + sc.Contention*9}
	}
	pd := NewPDPlanner(fleet)
	if sc.Disaggregate {
		for i := 0; i < 10; i++ {
			pd.ObservePrefillShare("model-a", 0.9)
		}
	}
	planner := NewStagePlanner(pd, oracle, nil)
	registry := NewProgramRegistry(dir)

	// Network contention manifests as transfer-queue depth (the measured
	// signal the planner backpressures against), not just slower links.
	if sc.Contention > 0 {
		queues := NewStageQueues()
		depth := int(sc.Contention * 8)
		for i := 0; i < depth; i++ {
			queues.Enter(StageTransfer, "contention")
		}
		planner.SetQueues(queues)
	}

	rep := SimReport{ByWorker: make(map[string]int)}
	var costSum float64

	// Shared prefix pool drives the cache-reuse traffic.
	const prefixPool = 8
	classes := []string{"interactive"}
	if sc.MixedClasses {
		classes = []string{"interactive", "agentic", "batch"}
	}

	arrivals := func(tick int) int {
		switch sc.ArrivalPattern {
		case "burst":
			if tick == 0 {
				return sc.RequestsPerTick * sc.DurationTicks
			}
			return 0
		case "heavytail":
			if rng.Float64() < 0.15 {
				return sc.RequestsPerTick * 8
			}
			return sc.RequestsPerTick / 2
		default:
			return sc.RequestsPerTick
		}
	}

	for tick := 0; tick < sc.DurationTicks; tick++ {
		if sc.FailureTick > 0 && tick == sc.FailureTick {
			// Worker loss mid-run: the fleet, router, and directory all
			// drop it (the state plane's eviction path).
			fleet.Remove("w-kr-h100")
			dir.EvictWorker("w-kr-h100")
		}
		for i := 0; i < arrivals(tick); i++ {
			rep.Requests++
			inputTokens := 2048
			if rng.Float64() < sc.LongPromptShare {
				inputTokens = 64 * 1024
			}
			req := RouteRequest{
				Model:                "model-a",
				Namespace:            "tenant-a",
				Region:               "",
				Cache:                simIdentity,
				InputTokens:          inputTokens,
				ExpectedOutputTokens: inputTokens / 8,
				RequestClass:         classes[rng.Intn(len(classes))],
			}
			reuse := rng.Float64() < sc.CacheReuseShare
			if reuse {
				req.PrefixHash = simPrefix(rng.Intn(prefixPool))
				req.CachedTokens = inputTokens / 2
			}
			programID := ""
			if rng.Float64() < sc.MultiTurnShare {
				programID = simPrefix(rng.Intn(4))
				registry.Turn(programID, "tenant-a", req.PrefixHash, simIdentity, "", 1, 0)
			}
			dec, err := router.Route(req)
			if err != nil {
				rep.Rejected++
				continue
			}
			rep.Routed++
			rep.ByWorker[dec.WorkerID]++
			costSum += dec.Cost
			if dec.OverlapTokens > 0 {
				rep.CacheHits++
				rep.OverlapTokens += dec.OverlapTokens
			} else if req.PrefixHash != "" {
				// Cold miss: the worker becomes the prefix's residence for
				// later reuse (the state plane learning).
				dir.Add(dec.WorkerID, L1GPU, KVBlock{
					Namespace: req.Namespace, Hash: req.PrefixHash, Tokens: req.InputTokens / 2,
				}, simIdentity, true)
			}
			if plan := planner.Plan(req.Model, dec.WorkerID, req.InputTokens); plan.Mode == StageDisaggregated {
				rep.Disaggregated++
			} else {
				rep.Colocated++
			}
			// Tool pauses drive pause-aware KV decisions for programs.
			if programID != "" && rng.Float64() < 0.3 {
				registry.ToolPaused(programID)
			}
			// Live load feedback unless telemetry is stale.
			if !sc.StaleTelemetry {
				fleet.Mutate(dec.WorkerID, func(w *FleetWorker) {
					w.State.ActiveRequests++
					w.State.DecodeKV += req.ExpectedOutputTokens
					w.State.Load.Active++
				})
			}
		}
		// Load decays between ticks (requests complete).
		if !sc.StaleTelemetry {
			for _, w := range fleet.List() {
				fleet.Mutate(w.Entry.Card.WorkerID, func(fw *FleetWorker) {
					fw.State.ActiveRequests /= 2
					fw.State.DecodeKV /= 2
					fw.State.Load.Active /= 2
				})
			}
		}
	}
	if rep.Routed > 0 {
		rep.MeanCost = costSum / float64(rep.Routed)
	}
	return rep
}

// simPrefix renders one of the scenario's bounded shared prefixes.
func simPrefix(n int) string {
	const letters = "abcdefgh"
	return "sim-" + string(letters[n%len(letters)])
}
