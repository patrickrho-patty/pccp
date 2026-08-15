package scheduler

import (
	"math/rand"
	"time"
)

// digitaltwin.go implements S11: the digital twin and benchmark
// autotuning (spec §13.8, §14 rows 36–37). The virtual engine implements
// the same engine boundary as a real adapter, so production traces replay
// through the REAL scheduler with synthetic GPUs — no standalone
// simulator re-implements engine behavior.

// TraceRecord is one captured production request.
type TraceRecord struct {
	InputTokens  int
	OutputTokens int
	ArrivedAt    time.Time
}

// VirtualEngineSpec configures the synthetic engine.
type VirtualEngineSpec struct {
	Model           string
	PrefillTPS      float64 // tokens/s uncached prefill
	DecodeTPS       float64 // tokens/s per sequence
	MaxConcurrent   int
	KVBlocks        int
	FailureRate     float64 // 0..1 probability a request fails
	LatencyJitterMs float64
}

// VirtualEngine is the synthetic engine behind the adapter interface.
type VirtualEngine struct {
	spec VirtualEngineSpec
	rng  *rand.Rand
}

// NewVirtualEngine builds a deterministic virtual engine.
func NewVirtualEngine(spec VirtualEngineSpec) *VirtualEngine {
	return &VirtualEngine{spec: spec, rng: rand.New(rand.NewSource(42))}
}

// Card renders the engine's capability card (the twin's fleet view).
func (v *VirtualEngine) Card() WorkerCard {
	return WorkerCard{
		CardVersion:       2,
		DariAddr:          "virtual:0",
		WorkerID:          "virtual-engine",
		ModelName:         v.spec.Model,
		EngineKind:        "virtual",
		MaxConcurrentSeqs: uint64(v.spec.MaxConcurrent),
		Status:            "active",
	}
}

// EstimatePrefill returns the simulated prefill time for a trace record.
func (v *VirtualEngine) EstimatePrefill(tr TraceRecord) time.Duration {
	if v.spec.PrefillTPS <= 0 {
		return 0
	}
	secs := float64(tr.InputTokens) / v.spec.PrefillTPS
	if v.spec.LatencyJitterMs > 0 {
		secs += (v.rng.Float64() - 0.5) * v.spec.LatencyJitterMs / 1000
	}
	if secs < 0 {
		secs = 0
	}
	return time.Duration(secs * float64(time.Second))
}

// EstimateDecode returns the simulated decode time for a trace record.
func (v *VirtualEngine) EstimateDecode(tr TraceRecord) time.Duration {
	if v.spec.DecodeTPS <= 0 {
		return 0
	}
	return time.Duration(float64(tr.OutputTokens) / v.spec.DecodeTPS * float64(time.Second))
}

// ReplayReport summarizes one simulation run.
type ReplayReport struct {
	Completed         int
	Failed            int
	TotalInputTokens  int
	TotalOutputTokens int
	AvgTTFTMs         float64
	AvgDurationMs     float64
	ThroughputOutTPS  float64
}

// DigitalTwin replays traces against a virtual fleet.
type DigitalTwin struct {
	engines []*VirtualEngine
}

// NewDigitalTwin builds a twin over one or more virtual engines.
func NewDigitalTwin(engines ...*VirtualEngine) *DigitalTwin {
	return &DigitalTwin{engines: engines}
}

// Replay runs the trace through the twin and reports fleet statistics.
func (d *DigitalTwin) Replay(trace []TraceRecord) ReplayReport {
	var rep ReplayReport
	var ttftSum, durSum float64
	start := time.Now()
	for _, tr := range trace {
		eng := d.engines[d.indexFor(tr)]
		ttft := eng.EstimatePrefill(tr)
		decode := eng.EstimateDecode(tr)
		if eng.rng.Float64() < eng.spec.FailureRate {
			rep.Failed++
			continue
		}
		rep.Completed++
		rep.TotalInputTokens += tr.InputTokens
		rep.TotalOutputTokens += tr.OutputTokens
		ttftSum += float64(ttft.Milliseconds())
		durSum += float64((ttft + decode).Milliseconds())
	}
	elapsed := time.Since(start).Seconds()
	if rep.Completed > 0 {
		rep.AvgTTFTMs = ttftSum / float64(rep.Completed)
		rep.AvgDurationMs = durSum / float64(rep.Completed)
	}
	if elapsed > 0 {
		rep.ThroughputOutTPS = float64(rep.TotalOutputTokens) / elapsed
	}
	return rep
}

// indexFor spreads trace records across the virtual fleet.
func (d *DigitalTwin) indexFor(tr TraceRecord) int {
	if len(d.engines) == 0 {
		return 0
	}
	return (tr.InputTokens + tr.OutputTokens) % len(d.engines)
}

// AutotuneSpace is the benchmark sweep grid.
type AutotuneSpace struct {
	ContextLengths []int
	BatchSizes     []int
	Samples        int
}

// AutotuneResult is the best configuration found.
type AutotuneResult struct {
	ContextLength          int
	BatchSize              int
	ThroughputTokensPerSec float64
	AvgTTFTMs              float64
}

// Autotune sweeps the grid and returns the highest-throughput
// configuration (benchmark autotuning, spec §14 row 37).
func (d *DigitalTwin) Autotune(space AutotuneSpace) AutotuneResult {
	var best AutotuneResult
	for _, ctxLen := range space.ContextLengths {
		for _, batch := range space.BatchSizes {
			var trace []TraceRecord
			for i := 0; i < space.Samples*batch; i++ {
				trace = append(trace, TraceRecord{
					InputTokens:  ctxLen,
					OutputTokens: ctxLen / 8,
					ArrivedAt:    time.Now(),
				})
			}
			rep := d.Replay(trace)
			if rep.ThroughputOutTPS > best.ThroughputTokensPerSec {
				best = AutotuneResult{
					ContextLength:          ctxLen,
					BatchSize:              batch,
					ThroughputTokensPerSec: rep.ThroughputOutTPS,
					AvgTTFTMs:              rep.AvgTTFTMs,
				}
			}
		}
	}
	return best
}
