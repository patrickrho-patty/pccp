package scheduler

import "testing"

func TestTopologyInventory(t *testing.T) {
	inv := NewTopologyInventory()
	inv.AddNode("n1", TopologyNode{Zone: "zone-a", Rack: "r1"})
	inv.AddNode("n2", TopologyNode{Zone: "zone-a", Rack: "r1"})
	inv.AddNode("n3", TopologyNode{Zone: "zone-b", Rack: "r9"})
	inv.AddWorker("w1", "n1")
	inv.AddWorker("w2", "n2")
	inv.AddWorker("w3", "n3")

	// Same-rack transfer is cheap (PCIe across nodes); cross-zone is
	// refused (no assumption without fabric evidence).
	if c, ok := inv.TransferCost("w1", "w2"); !ok || c != TransportPCIe {
		t.Fatalf("same-rack cost = %v,%v want pcie", c, ok)
	}
	if c, ok := inv.TransferCost("w1", "w3"); !ok || c == TransportNVLink {
		t.Fatalf("cross-zone cost = %v, want something slower than nvlink", c)
	}
}

func TestTopologySameNodeTransfer(t *testing.T) {
	inv := NewTopologyInventory()
	inv.AddNode("n1", TopologyNode{Zone: "z", Rack: "r"})
	inv.AddWorker("w1", "n1")
	inv.AddWorker("w2", "n1")
	c, ok := inv.TransferCost("w1", "w2")
	if !ok || c != TransportNVLink {
		t.Fatalf("same-node cost = %v,%v want nvlink", c, ok)
	}
}

func TestTopologyUnknownWorker(t *testing.T) {
	inv := NewTopologyInventory()
	if _, ok := inv.TransferCost("w1", "w2"); ok {
		t.Fatal("unknown workers must not have a transfer cost")
	}
}

func TestTransportCostOrdering(t *testing.T) {
	// Faster transports must cost less.
	if TransportNVLink.Cost() >= TransportPCIe.Cost() || TransportPCIe.Cost() >= TransportEthernet.Cost() {
		t.Fatal("transport costs must order nvlink < pcie < ethernet")
	}
}
