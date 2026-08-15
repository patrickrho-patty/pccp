package scheduler

import (
	"testing"
	"time"
)

func TestVirtualEngineReplaysTrace(t *testing.T) {
	// Spec §13.8: the virtual engine implements the same interface as a
	// real adapter — replaying production traces through the REAL
	// scheduler with synthetic GPUs.
	ve := NewVirtualEngine(VirtualEngineSpec{
		Model:           "model-a",
		PrefillTPS:      1000,
		DecodeTPS:       50,
		MaxConcurrent:   8,
		KVBlocks:        4096,
		FailureRate:     0,
		LatencyJitterMs: 0,
	})
	tr := TraceRecord{
		InputTokens:  1000,
		OutputTokens: 200,
		ArrivedAt:    time.Now(),
	}
	card := ve.Card()
	if card.ModelName != "model-a" {
		t.Fatalf("card model = %s", card.ModelName)
	}
	// Prefill estimate: 1000 tokens / 1000 tps = 1s.
	if prefill := ve.EstimatePrefill(tr); prefill < 900*time.Millisecond || prefill > 1100*time.Millisecond {
		t.Fatalf("prefill estimate = %v", prefill)
	}
}

func TestDigitalTwinSimulation(t *testing.T) {
	// A trace replayed against the twin produces throughput + latency
	// stats without touching a real GPU.
	twin := NewDigitalTwin(NewVirtualEngine(VirtualEngineSpec{
		Model: "model-a", PrefillTPS: 2000, DecodeTPS: 100,
		MaxConcurrent: 16, KVBlocks: 8192,
	}))
	trace := []TraceRecord{
		{InputTokens: 500, OutputTokens: 100, ArrivedAt: time.Now()},
		{InputTokens: 500, OutputTokens: 100, ArrivedAt: time.Now().Add(time.Millisecond)},
		{InputTokens: 8000, OutputTokens: 50, ArrivedAt: time.Now().Add(2 * time.Millisecond)},
	}
	report := twin.Replay(trace)
	if report.Completed != 3 {
		t.Fatalf("completed = %d, want 3", report.Completed)
	}
	if report.TotalOutputTokens != 250 {
		t.Fatalf("output tokens = %d, want 250", report.TotalOutputTokens)
	}
	if report.AvgTTFTMs <= 0 {
		t.Fatal("avg TTFT must be positive")
	}
}

func TestDigitalTwinAutotune(t *testing.T) {
	// Benchmark autotuning: sweep context lengths × batch sizes and pick
	// the best throughput configuration.
	twin := NewDigitalTwin(NewVirtualEngine(VirtualEngineSpec{
		Model: "model-a", PrefillTPS: 2000, DecodeTPS: 100,
		MaxConcurrent: 32, KVBlocks: 16384,
	}))
	best := twin.Autotune(AutotuneSpace{
		ContextLengths: []int{1024, 4096, 8192},
		BatchSizes:     []int{1, 4, 8},
		Samples:        2,
	})
	if best.ContextLength <= 0 || best.BatchSize <= 0 {
		t.Fatalf("autotune result = %+v", best)
	}
	if best.ThroughputTokensPerSec <= 0 {
		t.Fatalf("throughput = %.1f, want positive", best.ThroughputTokensPerSec)
	}
}
