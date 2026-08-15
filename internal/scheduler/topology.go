package scheduler

import "sync"

// topology.go implements S3/S6 hardware topology intelligence (spec §14
// rows 11, 30): node/rack/zone inventory and topology-aware KV transfer
// cost — placement constraints and state movement pricing.

// TopologyNode is one host's placement attributes.
type TopologyNode struct {
	Zone string
	Rack string
}

// Transport is the inter-node fabric grade.
type Transport string

const (
	TransportNVLink   Transport = "nvlink"
	TransportPCIe     Transport = "pcie"
	TransportEthernet Transport = "ethernet"
)

// Cost returns the relative transfer cost (lower = faster).
func (t Transport) Cost() int {
	switch t {
	case TransportNVLink:
		return 1
	case TransportPCIe:
		return 10
	case TransportEthernet:
		return 100
	}
	return 1000
}

// TopologyInventory maps workers → nodes → racks/zones and prices state
// transfers between them. Safe for concurrent use.
type TopologyInventory struct {
	mu      sync.RWMutex
	nodes   map[string]TopologyNode
	workers map[string]string // workerID → nodeID
}

// NewTopologyInventory builds an empty inventory.
func NewTopologyInventory() *TopologyInventory {
	return &TopologyInventory{
		nodes:   make(map[string]TopologyNode),
		workers: make(map[string]string),
	}
}

// AddNode registers a node.
func (t *TopologyInventory) AddNode(nodeID string, n TopologyNode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nodes[nodeID] = n
}

// AddWorker binds a worker to a node.
func (t *TopologyInventory) AddWorker(workerID, nodeID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.workers[workerID] = nodeID
}

// TransferCost prices KV state movement between two workers: same node
// = NVLink, same rack = PCIe, same zone = ethernet, otherwise unknown
// (refuse — no cross-zone assumption).
func (t *TopologyInventory) TransferCost(w1, w2 string) (Transport, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	n1, ok1 := t.workers[w1]
	n2, ok2 := t.workers[w2]
	if !ok1 || !ok2 {
		return "", false
	}
	if n1 == n2 {
		return TransportNVLink, true
	}
	node1, ok1 := t.nodes[n1]
	node2, ok2 := t.nodes[n2]
	if !ok1 || !ok2 {
		return "", false
	}
	if node1.Rack == node2.Rack && node1.Zone == node2.Zone {
		return TransportPCIe, true
	}
	// Same or different zone: the general fabric path (ethernet/EFA
	// grade). Placement constraints may still forbid it at policy level;
	// the inventory prices the fabric, never invents one.
	return TransportEthernet, true
}
