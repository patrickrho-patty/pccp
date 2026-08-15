package scheduler

import (
	"sync"
)

// k8s.go implements S12: the Kubernetes-native adapter (spec §14 rows
// 32–33). The non-K8s core remains authoritative; this adapter maps K8s
// state (pods, InferencePool endpoint slices) into the same signed
// capability cards and exposes the queue-depth metric KEDA/HPA scale on.
// GAIE v1.5.0 (GA) is the locked conformance target (spec §12.4).

// GAIEVersion is the Gateway API Inference Extension conformance target.
const GAIEVersion = "v1.5.0"

// CRDRegistry lists the custom resource definitions the adapter speaks.
type CRDRegistry struct {
	types map[string]CRDKind
}

// CRDKind is one custom resource's apiVersion/kind pair.
type CRDKind struct {
	APIVersion string
	Kind       string
}

// NewCRDRegistry registers the seven CRDs (spec §14 row 32).
func NewCRDRegistry() *CRDRegistry {
	return &CRDRegistry{types: map[string]CRDKind{
		"ModelDeployment": {APIVersion: "inference.patty.io/v1", Kind: "ModelDeployment"},
		"ServingVariant":  {APIVersion: "inference.patty.io/v1", Kind: "ServingVariant"},
		"RoutingPolicy":   {APIVersion: "inference.patty.io/v1", Kind: "RoutingPolicy"},
		"KVCachePolicy":   {APIVersion: "inference.patty.io/v1", Kind: "KVCachePolicy"},
		"InferenceSLO":    {APIVersion: "inference.patty.io/v1", Kind: "InferenceSLO"},
		"ScalingPolicy":   {APIVersion: "inference.patty.io/v1", Kind: "ScalingPolicy"},
		"MediaPolicy":     {APIVersion: "inference.patty.io/v1", Kind: "MediaPolicy"},
	}}
}

// PodInfo is the adapter's view of one serving pod.
type PodInfo struct {
	Name       string
	NodeName   string
	NodeIP     string
	Zone       string
	ModelName  string
	Engine     string
	Port       int
	TP, DP, EP uint32
	GPUCount   uint32
	GPUSKU     string
}

// PodToCard maps a pod into the fleet's capability card (the card
// schema was shaped for exactly this mapping, spec §2.3).
func PodToCard(pod PodInfo) WorkerCard {
	return WorkerCard{
		CardVersion: 2,
		WorkerID:    pod.Name,
		NodeID:      pod.NodeName,
		IP:          pod.NodeIP,
		Zone:        pod.Zone,
		EngineKind:  pod.Engine,
		ModelName:   pod.ModelName,
		TP:          pod.TP,
		DP:          pod.DP,
		EP:          pod.EP,
		GPUCount:    pod.GPUCount,
		GPUSKU:      pod.GPUSKU,
		DariAddr:    pod.NodeIP + ":" + itoa(pod.Port),
		Status:      "active",
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// InferencePool is the GAIE pool view (endpoint slices).
type InferencePool struct {
	Name      string
	Endpoints []PoolEndpoint
}

// PoolEndpoint is one pool member.
type PoolEndpoint struct {
	Name    string
	Address string
	Model   string
	Ready   bool
}

// PoolToCards maps an InferencePool's endpoint slices into capability
// cards (GAIE v1.5 conformance surface).
func PoolToCards(pool InferencePool) []WorkerCard {
	cards := make([]WorkerCard, 0, len(pool.Endpoints))
	for _, ep := range pool.Endpoints {
		status := "active"
		if !ep.Ready {
			status = "not_ready"
		}
		host, port := splitHostPort(ep.Address)
		cards = append(cards, WorkerCard{
			CardVersion: 2,
			WorkerID:    ep.Name,
			ModelName:   ep.Model,
			DariAddr:    ep.Address,
			IP:          host,
			Status:      status,
		})
		_ = port
	}
	return cards
}

// splitHostPort splits "host:port" (tolerant of missing port).
func splitHostPort(addr string) (string, string) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:]
		}
	}
	return addr, ""
}

// MetricExporter is the Prometheus-style metric surface KEDA/HPA scale
// on: queue depth is the true-demand signal (llm-d flow control).
type MetricExporter struct {
	mu      sync.RWMutex
	metrics map[string]float64
}

// NewMetricExporter builds the exporter.
func NewMetricExporter() *MetricExporter {
	return &MetricExporter{metrics: make(map[string]float64)}
}

// Set records a metric value.
func (m *MetricExporter) Set(name string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics[name] = value
}

// Get reads a metric value.
func (m *MetricExporter) Get(name string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metrics[name]
}
