package scheduler

import "time"

// netoracle.go implements the PAT-1445 WS2 network cost oracle seam: a
// bounded, failure-tolerant interface for pricing KV transfers between
// workers. Implementations must never block inference — when no estimate
// exists the answer is ok=false and the caller falls back conservatively
// (co-located execution). The static topology oracle is the conservative
// fallback implementation; live measurement oracles plug in behind the
// same interface later (WS2 §network cost oracle and flow scheduling).

// NetworkOracle prices KV transfers between workers.
type NetworkOracle interface {
	// TransferCostMs estimates one KV transfer's duration in
	// milliseconds for kvBytes between two workers; ok=false when no
	// estimate exists (unknown path, unmeasured link).
	TransferCostMs(srcWorker, dstWorker string, kvBytes int64) (ms float64, ok bool)
	// Freshness reports the age of the newest underlying measurement;
	// the static oracle reports 0 (topology facts do not go stale).
	Freshness() time.Duration
}

// transportLinkMs are conservative per-transport transfer estimates:
// base link latency plus effective throughput for KV movement. They are
// deliberately pessimistic — the static fallback must prefer co-located
// execution whenever a live oracle would quote a cheaper transfer.
var transportLinkMs = map[Transport]struct{ base, perGB float64 }{
	TransportNVLink:   {0.05, 3.0},
	TransportPCIe:     {0.2, 25.0},
	TransportEthernet: {1.0, 120.0},
}

// StaticTopologyOracle is the conservative NetworkOracle: transfer cost
// derived from the static topology inventory alone (WS2: stale or
// unavailable telemetry falls back conservatively).
type StaticTopologyOracle struct {
	inv *TopologyInventory
}

// NewStaticTopologyOracle builds the static fallback over an inventory.
func NewStaticTopologyOracle(inv *TopologyInventory) *StaticTopologyOracle {
	return &StaticTopologyOracle{inv: inv}
}

// TransferCostMs prices the transfer from the fabric grade: same-node
// NVLink, same-rack PCIe, else Ethernet; unknown worker pairs report no
// estimate (the caller falls back to co-located execution).
func (o *StaticTopologyOracle) TransferCostMs(srcWorker, dstWorker string, kvBytes int64) (float64, bool) {
	if o.inv == nil {
		return 0, false
	}
	t, ok := o.inv.TransferCost(srcWorker, dstWorker)
	if !ok {
		return 0, false
	}
	link, ok := transportLinkMs[t]
	if !ok {
		return 0, false
	}
	gb := float64(kvBytes) / float64(int64(1)<<30)
	return link.base + link.perGB*gb, true
}

// Freshness is always zero: static topology facts do not age.
func (o *StaticTopologyOracle) Freshness() time.Duration { return 0 }
