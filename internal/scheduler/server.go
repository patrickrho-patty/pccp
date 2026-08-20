package scheduler

import (
	"crypto/ed25519"
	"time"
)

// Scheduler is the composition root of the pccp-scheduler process: registry,
// admission ladder, evidence log, and the S2 serving stack (queue +
// dispatcher). The DARI listener feeds it; the HTTP API reads it (S1.5);
// the router (S3) will consume the registry.
type Scheduler struct {
	Registry  *Registry
	Admission *Admission
	Evidence  *EvidenceLog
	Serving   *Serving
	KV        *KVIndex
	KVDir     *KVDirectory
	Trace     *TraceRecorder
	PD        *PDController
	Programs  *ProgramRegistry
	Topology  *TopologyInventory
	Fleet     *WorkerFleet
}

// NewScheduler assembles the full S1–S12 scheduler with the given trust
// material, policy source, lease parameters, and evidence signing key.
// The production composition: registry + admission + evidence + serving
// stack (gateway/dispatcher/queue) + KV index + cost router + gang
// registry + SLO table + latency predictor + autoscaler + batch gateway.
func NewScheduler(trust Trust, policy PolicySource, ttl, grace time.Duration, evidenceKey ed25519.PrivateKey) *Scheduler {
	svc := &Scheduler{
		Registry:  NewRegistry(ttl, grace),
		Admission: NewAdmission(trust, NewRevocationStore(), policy),
		Evidence:  NewEvidenceLog(evidenceKey),
		Serving:   NewServing(),
		KV:        NewKVIndex(),
		KVDir:     NewKVDirectory(),
		Trace:     NewTraceRecorder(4096),
		Topology:  NewTopologyInventory(),
		Fleet:     NewWorkerFleet(),
	}
	svc.PD = NewPDController(NewPDPlanner(svc.Fleet),
		NewLatencyPredictorPair(DefaultPredictorConfig()))
	svc.Programs = NewProgramRegistry(svc.KVDir)
	svc.wireServingStack()
	return svc
}

// wireServingStack composes the S2–S12 serving path: the dispatcher's
// router consumes the KV index, gang registry, SLO table, and predictor;
// receipts land in a bounded store; the batch gateway and autoscaler are
// built for the binary to expose.
func (s *Scheduler) wireServingStack() {
	router := NewCostRouter(DefaultRouterConfig())
	router.SetFleet(s.Fleet)
	router.SetKV(s.KV)
	router.SetKVDirectory(s.KVDir)
	receipts := NewReceiptStore(1024)
	receipts.SetSigningKey(s.Evidence.key)
	router.SetReceipts(receipts)
	router.SetGang(NewGangRegistry())
	router.SetSLOResolver(NewSLOResolver())
	router.SetPredictor(NewLatencyPredictor(DefaultPredictorConfig()))
	s.Serving.Dispatcher.SetRouter(router)
	// PAT-1445 governed trace capture: versioned, content-free decisions
	// for replay/shadow evaluation.
	s.Trace.SetVersion("router", CostRouterVersion)
	s.Trace.SetVersion("predictor", PredictorVersion)
	s.Trace.SetVersion("output_estimator", OutputEstimatorVersion)
	s.Serving.Dispatcher.SetTraceRecorder(s.Trace)
	// WS2 stage planning: every binding records its execution path as
	// trace evidence; execution stays co-located until the PIA stage
	// protocol lands (PAT-1445 rollout: decide in shadow first).
	s.Serving.Dispatcher.SetStagePlanner(
		NewStagePlanner(s.PD.planner, NewStaticTopologyOracle(s.Topology), s.PD))
	s.Serving.Dispatcher.SetPrograms(s.Programs)
}

// SyncRouter refreshes the shared worker fleet from the live registry —
// the single fan-out point: cost router, selector, P/D planner, gang
// registry, and the network oracle's topology all read from it (the
// listener owns the card feed; the fleet owns worker state).
func (s *Scheduler) SyncRouter() {
	router := s.Serving.Dispatcher.router
	if router == nil {
		return
	}
	gang := router.gang
	for _, e := range s.Registry.List() {
		s.Fleet.Upsert(e, RouterWorkerState{
			ActiveRequests: int(e.Card.ActiveSeqs),
			Load:           WorkerLoad{MaxConcurrent: int(e.Card.MaxConcurrentSeqs), Active: int(e.Card.ActiveSeqs)},
		})
		if gang != nil {
			gang.Upsert(e)
		}
		// Feed the network oracle's static fallback from signed cards.
		// Rack is pinned to the node ID: with no rack telemetry, distinct
		// nodes never price as PCIe — cross-node transfers stay at the
		// conservative ethernet grade (WS2 conservative fallback).
		if e.Card.NodeID != "" {
			s.Topology.AddNode(e.Card.NodeID, TopologyNode{Zone: e.Card.Zone, Rack: e.Card.NodeID})
			s.Topology.AddWorker(e.Card.WorkerID, e.Card.NodeID)
		}
	}
}

// Router exposes the composed cost router (admin/telemetry wiring).
func (s *Scheduler) Router() *CostRouter {
	return s.Serving.Dispatcher.router
}

// Admit runs the admission ladder for a registration/heartbeat request.
func (s *Scheduler) Admit(req AdmissionRequest) AdmissionResult {
	return s.Admission.Admit(req)
}

// UpdateRevocations applies a revocation-feed refresh (serials and peer IDs)
// from the Control Plane.
func (s *Scheduler) UpdateRevocations(serials, peerIDs []string) {
	s.Admission.revoked.Replace(serials, peerIDs)
}

// Sweep evicts expired workers and emits evidence for each eviction.
// Removal propagates through the shared fleet (router, selector, P/D
// planner all drop it) and the cache planes drop its state (PAT-1445 B1:
// eviction is one event with total locality).
func (s *Scheduler) Sweep(now time.Time) []string {
	evicted := s.Registry.Sweep(now)
	for _, id := range evicted {
		s.Fleet.Remove(id)
		s.KV.EvictWorker(id)
		s.KVDir.EvictWorker(id)
		s.Evidence.Emit(EventWorkerEvict, id, "lease expired")
	}
	return evicted
}
