package scheduler

import "strings"

// migration.go implements the WS1 placement lifecycle's execution half
// (PAT-1445 criterion 5): the migration coordinator executes hot-prefix
// replication when the directory flags a hotspot — never overloading a
// worker for a hit (issue §edge cases: cache hotspot mitigation). New
// residences are placed UNVERIFIED: the target confirms residency before
// the router grants overlap credit (stale/false-location fallback stays
// recompute). Transfers are priced by the network oracle; an
// unpriceable or over-budget path skips the candidate conservatively.

// MigrationCoordinator executes hot-prefix replication. Safe for
// concurrent use.
type MigrationCoordinator struct {
	dir           *KVDirectory
	oracle        NetworkOracle
	fleet         *WorkerFleet
	minHits       int64
	maxReplicas   int
	maxTransferMs float64
}

// NewMigrationCoordinator builds the coordinator over the directory,
// oracle, and fleet.
func NewMigrationCoordinator(dir *KVDirectory, oracle NetworkOracle, fleet *WorkerFleet) *MigrationCoordinator {
	return &MigrationCoordinator{
		dir:           dir,
		oracle:        oracle,
		fleet:         fleet,
		minHits:       3,
		maxReplicas:   2,
		maxTransferMs: 500,
	}
}

// MigrationResult summarizes one replication pass.
type MigrationResult struct {
	Candidates int `json:"candidates"`
	Placed     int `json:"placed"`
	Skipped    int `json:"skipped"`
}

// ReplicateOnce evaluates hot-prefix candidates and places at most one
// new residence per candidate, on the cheapest healthy target with a
// priceable, in-budget transfer. Targets must have free capacity —
// replication never overloads a worker for a hit.
func (m *MigrationCoordinator) ReplicateOnce() MigrationResult {
	var res MigrationResult
	if m.dir == nil || m.oracle == nil || m.fleet == nil {
		return res
	}
	candidates := m.dir.HotPrefixes(m.minHits, m.maxReplicas)
	res.Candidates = len(candidates)
	for _, cand := range candidates {
		if m.replicate(cand) {
			res.Placed++
		} else {
			res.Skipped++
		}
	}
	return res
}

// replicate places one new residence for a candidate. Returns false when
// no suitable target exists (skipped conservatively).
func (m *MigrationCoordinator) replicate(cand HotPrefix) bool {
	sources := m.dir.Locations(cand.Namespace, cand.Hash, cand.Identity)
	if len(sources) == 0 {
		return false
	}
	src := sources[0] // Locations returns hottest tier first
	holders := make(map[string]bool, len(sources))
	for _, loc := range sources {
		holders[loc.WorkerID] = true
	}

	bestCost := -1.0
	bestTarget := ""
	for _, w := range m.fleet.List() {
		id := w.Entry.Card.WorkerID
		if holders[id] {
			continue
		}
		// Capacity is sacred: never overload for a hit.
		if !w.State.Load.CanAccept() {
			continue
		}
		// The replica must be able to serve the extent's model package
		// (card model is the package prefix, e.g. "model-a" in
		// "model-a@1.0"); an empty package id imposes no constraint.
		if cand.Identity.ModelPackage != "" &&
			!strings.HasPrefix(cand.Identity.ModelPackage, w.Entry.Card.ModelName) {
			continue
		}
		if w.Entry.Quarantined || w.Entry.Lapsed || !w.Entry.Card.Servable() {
			continue
		}
		cost, ok := m.oracle.TransferCostMs(src.WorkerID, id, int64(cand.Tokens))
		if !ok || cost > m.maxTransferMs {
			continue
		}
		if bestTarget == "" || cost < bestCost {
			bestTarget = id
			bestCost = cost
		}
	}
	if bestTarget == "" {
		return false
	}
	m.dir.Add(bestTarget, src.Tier, KVBlock{
		Namespace: cand.Namespace,
		Hash:      cand.Hash,
		Tokens:    cand.Tokens,
	}, cand.Identity, false) // unverified until the target confirms
	return true
}
