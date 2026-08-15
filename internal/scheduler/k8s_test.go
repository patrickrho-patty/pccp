package scheduler

import "testing"

func TestCRDTypesRegistered(t *testing.T) {
	// Spec §14 row 32: CRDs (ModelDeployment, ServingVariant,
	// RoutingPolicy, KVCachePolicy, InferenceSLO, ScalingPolicy,
	// MediaPolicy).
	reg := NewCRDRegistry()
	for _, name := range []string{
		"ModelDeployment", "ServingVariant", "RoutingPolicy",
		"KVCachePolicy", "InferenceSLO", "ScalingPolicy", "MediaPolicy",
	} {
		if _, ok := reg.types[name]; !ok {
			t.Fatalf("CRD %s not registered", name)
		}
	}
}

func TestPodToCardMapping(t *testing.T) {
	// The adapter maps pods → capability cards (the card schema was
	// shaped so a K8s adapter can do exactly this, spec §2.3).
	pod := PodInfo{
		Name:      "qwen-27b-fp8-0",
		NodeName:  "gpu-node-1",
		NodeIP:    "10.0.0.11",
		Zone:      "zone-a",
		ModelName: "Qwen3.6-27B-FP8",
		Engine:    "vllm",
		Port:      9444,
		TP:        1, DP: 1, EP: 1,
		GPUCount: 1, GPUSKU: "H100",
	}
	card := PodToCard(pod)
	if card.WorkerID != pod.Name || card.ModelName != pod.ModelName {
		t.Fatalf("card = %+v", card)
	}
	if card.DariAddr != "10.0.0.11:9444" {
		t.Fatalf("dari addr = %s, want node IP + PIA port", card.DariAddr)
	}
	if !card.Servable() {
		t.Fatal("mapped card must be servable")
	}
}

func TestInferencePoolConformance(t *testing.T) {
	// GAIE v1.5: the pool aggregates endpoint slices; the adapter maps
	// pool state into the registry.
	pool := InferencePool{
		Name: "default-pool",
		Endpoints: []PoolEndpoint{
			{Name: "qwen-27b-fp8-0", Address: "10.0.0.11:9444", Model: "Qwen3.6-27B-FP8", Ready: true},
			{Name: "qwen-27b-fp8-1", Address: "10.0.0.12:9444", Model: "Qwen3.6-27B-FP8", Ready: false},
		},
	}
	cards := PoolToCards(pool)
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(cards))
	}
	if cards[0].Status != "active" || cards[1].Status != "not_ready" {
		t.Fatalf("statuses = %s/%s", cards[0].Status, cards[1].Status)
	}
}

func TestHPAScalingMetricExport(t *testing.T) {
	// KEDA scales on queue depth (true demand, llm-d flow-control
	// metric); the adapter exposes it as a Prometheus-style metric.
	exporter := NewMetricExporter()
	exporter.Set("llm_d_queue_tokens", 12345.0)
	if got := exporter.Get("llm_d_queue_tokens"); got != 12345.0 {
		t.Fatalf("metric = %v", got)
	}
}

func TestGAIEVersionPin(t *testing.T) {
	// The adapter targets GAIE v1.5.0 (GA) per the locked reference
	// status (spec §12.4).
	if GAIEVersion != "v1.5.0" {
		t.Fatalf("GAIE version = %s, want v1.5.0", GAIEVersion)
	}
}
