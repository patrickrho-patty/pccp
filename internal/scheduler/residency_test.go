package scheduler

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// staticResidency is a test ResidencyPolicy with a fixed tenant→region map.
type staticResidency map[string]string

func (s staticResidency) ResidencyRegion(tenantID string) (string, bool) {
	r, ok := s[tenantID]
	return r, ok
}

func TestGatewayAppliesResidencyConstraint(t *testing.T) {
	d := NewDispatcher(nil)
	rec := NewTraceRecorder(16)
	d.SetTraceRecorder(rec)
	d.SetForwarder(&fakeForwarder{result: InferenceResult{Text: "ok", Usage: map[string]int{"prompt_tokens": 10, "completion_tokens": 5}}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	g := NewGateway(d, nil)
	g.Rewriter().SetAlias("ko-coder", "model-a")
	g.Rewriter().SetTenantModels("tenant-1", []string{"model-a"})
	g.SetResidencyPolicy(staticResidency{"tenant-1": "kr"})

	body := `{"model":"ko-coder","messages":[{"role":"user","content":"안녕"}],"max_tokens":50}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-1")
	setTestTraffic(t, g, req, "tenant-1", "interactive-paid")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}

	// The arrived trace event carries the policy-plane constraint.
	var arrived *TraceEvent
	for i, e := range rec.Export().Events {
		if e.Stage == TraceArrived {
			arrived = &rec.Export().Events[i]
		}
	}
	if arrived == nil || arrived.Region != "kr" || arrived.Tenant != "tenant-1" {
		t.Fatalf("arrived event = %+v, want region kr for tenant-1", arrived)
	}
}

func TestGatewayNoPolicyLeavesRegionEmpty(t *testing.T) {
	g, d := newServingGateway(t)
	rec := NewTraceRecorder(16)
	d.SetTraceRecorder(rec)
	g.Rewriter().SetAlias("ko-coder", "model-a")
	g.Rewriter().SetTenantModels("tenant-1", []string{"model-a"})

	body := `{"model":"ko-coder","messages":[{"role":"user","content":"안녕"}],"max_tokens":50}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-1")
	setTestTraffic(t, g, req, "tenant-1", "interactive-paid")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	for _, e := range rec.Export().Events {
		if e.Stage == TraceArrived && e.Region != "" {
			t.Fatalf("unconstrained request carries region %q", e.Region)
		}
	}
}

func TestDispatcherPassesRegionToRouter(t *testing.T) {
	d := NewDispatcher(nil)
	router := NewCostRouter(DefaultRouterConfig())
	router.UpsertWorker(mkRegionWorker("w-kr", "model-a", "kr"), RouterWorkerState{})
	router.UpsertWorker(mkRegionWorker("w-us", "model-a", "us"), RouterWorkerState{})
	d.SetRouter(router)
	sel := NewWorkerSelector()
	sel.Upsert(mkRegionWorker("w-kr", "model-a", "kr"), 1)
	sel.Upsert(mkRegionWorker("w-us", "model-a", "us"), 1)
	d.SetSelector(sel)

	qr := queueRequest("r1", "tenant-1", "interactive-paid", "model-a")
	qr.Region = "us"
	if err := d.Queue().Enqueue(qr); err != nil {
		t.Fatal(err)
	}
	// The freed us worker binds; the kr worker must never be chosen.
	bound := d.Assign("w-us")
	if bound == nil || bound.WorkerID != "w-us" {
		t.Fatalf("bound = %+v, want w-us under region=us", bound)
	}
}

func TestReplayPreservesRegionConstraint(t *testing.T) {
	events := []TraceEvent{
		{RequestID: "r1", Stage: TraceArrived, Tenant: "t1", Model: "model-a", Region: "kr", InputTokens: 10, ExpectedOutputTokens: 10},
	}
	r := NewCostRouter(DefaultRouterConfig())
	r.UpsertWorker(mkRegionWorker("w-kr", "model-a", "kr"), RouterWorkerState{})
	r.UpsertWorker(mkRegionWorker("w-us", "model-a", "us"), RouterWorkerState{})

	s := ReplayDecisions(events, r)
	if s.Decisions != 1 || s.ByWorker["w-kr"] != 1 {
		t.Fatalf("replay = %+v, want the constrained decision on w-kr", s)
	}
}
