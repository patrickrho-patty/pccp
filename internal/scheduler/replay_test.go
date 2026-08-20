package scheduler

import (
	"errors"
	"testing"
)

func replayTrace() []TraceEvent {
	return []TraceEvent{
		{RequestID: "r1", Stage: TraceArrived, Tenant: "t1", Model: "model-a", Class: "interactive", InputTokens: 100, ExpectedOutputTokens: 50},
		{RequestID: "r2", Stage: TraceArrived, Tenant: "t1", Model: "model-a", Class: "interactive", InputTokens: 200, ExpectedOutputTokens: 50},
		// Non-arrival stages must not produce replayed decisions.
		{RequestID: "r1", Stage: TraceBound, Tenant: "t1", Model: "model-a", WorkerID: "w1"},
		{RequestID: "r1", Stage: TraceCompleted, Tenant: "t1", Model: "model-a", WorkerID: "w1", OutputTokens: 50},
	}
}

func TestReplayDecisions(t *testing.T) {
	r := &stubRouter{version: "candidate/v1", decision: RouteDecision{WorkerID: "w1", Cost: 10, OverlapTokens: 32}}
	s := ReplayDecisions(replayTrace(), r)
	if s.RouterVersion != "candidate/v1" {
		t.Fatalf("router version = %q", s.RouterVersion)
	}
	if s.Decisions != 2 || r.calls != 2 {
		t.Fatalf("decisions = %d (calls %d), want 2 — only arrived events replay", s.Decisions, r.calls)
	}
	if s.ByWorker["w1"] != 2 || s.TotalOverlap != 64 || s.MeanCost != 10 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestReplayDecisionsCountsErrors(t *testing.T) {
	r := &stubRouter{version: "candidate/v2", err: errors.New("no eligible worker")}
	s := ReplayDecisions(replayTrace(), r)
	if s.Errors != 2 || s.Decisions != 0 {
		t.Fatalf("summary = %+v, want 2 errors 0 decisions", s)
	}
}

func TestCompareRouters(t *testing.T) {
	base := &stubRouter{version: "cost-router/v1", decision: RouteDecision{WorkerID: "w1", Cost: 10}}

	agreeing := &stubRouter{version: "candidate/v1", decision: RouteDecision{WorkerID: "w1", Cost: 1}}
	rep := CompareRouters(replayTrace(), base, agreeing)
	if rep.Compared != 2 || rep.Agree != 2 || rep.AgreementRate != 1.0 {
		t.Fatalf("agreeing comparison = %+v", rep)
	}
	if rep.BaselineVersion != "cost-router/v1" || rep.CandidateVersion != "candidate/v1" {
		t.Fatalf("versions = %+v", rep)
	}

	diverging := &stubRouter{version: "candidate/v2", decision: RouteDecision{WorkerID: "w2", Cost: 1}}
	rep = CompareRouters(replayTrace(), base, diverging)
	if rep.Compared != 2 || rep.Agree != 0 || rep.AgreementRate != 0 {
		t.Fatalf("diverging comparison = %+v", rep)
	}

	failing := &stubRouter{version: "candidate/v3", err: errors.New("no route")}
	rep = CompareRouters(replayTrace(), base, failing)
	if rep.Compared != 2 || rep.CandidateErrors != 2 || rep.Agree != 0 {
		t.Fatalf("failing comparison = %+v", rep)
	}
}
