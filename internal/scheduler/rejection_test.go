package scheduler

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRouteErrorClassification(t *testing.T) {
	// No worker serves the model: permanent.
	r := NewCostRouter(DefaultRouterConfig())
	r.UpsertWorker(mkWorker("w1", "model-b", 8), RouterWorkerState{})
	_, err := r.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10})
	rerr, ok := err.(*RouteError)
	if !ok {
		t.Fatalf("error type %T, want *RouteError", err)
	}
	if !rerr.Permanent {
		t.Fatal("unserved model must be permanent")
	}
	if rerr.Eligibility == nil || rerr.Eligibility.Filtered[ReasonModelMismatch] != 1 {
		t.Fatalf("eligibility = %+v", rerr.Eligibility)
	}

	// The only worker is saturated: transient (waiting may help).
	r2 := NewCostRouter(DefaultRouterConfig())
	r2.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{
		Load: WorkerLoad{MaxConcurrent: 4, Active: 4},
	})
	_, err = r2.Route(RouteRequest{Model: "model-a", InputTokens: 10, ExpectedOutputTokens: 10})
	rerr, ok = err.(*RouteError)
	if !ok {
		t.Fatalf("error type %T", err)
	}
	if rerr.Permanent {
		t.Fatal("saturated worker must be transient, not permanent")
	}
}

func TestDispatcherRejectsPermanentlyUnroutable(t *testing.T) {
	rec := NewTraceRecorder(16)
	d := NewDispatcher(nil)
	d.SetTraceRecorder(rec)
	router := NewCostRouter(DefaultRouterConfig())
	router.UpsertWorker(mkWorker("w1", "model-b", 8), RouterWorkerState{})
	d.SetRouter(router)
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-b", 8), 1)
	d.SetSelector(sel)

	// The request's model is served nowhere: the waiter completes NOW
	// with an honest retryable reason, not after a 60s TTL.
	ch, err := d.Submit(queueRequest("r1", "tenant-1", "interactive-paid", "model-a"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	bound := d.Assign("w1")
	if bound != nil {
		t.Fatalf("permanently unroutable request bound to %+v", bound)
	}
	select {
	case res := <-ch:
		if res.Err == "" || !res.Retryable {
			t.Fatalf("result = %+v, want honest retryable rejection", res)
		}
		if !strings.Contains(res.Err, "permanent") {
			t.Fatalf("reason %q missing permanence class", res.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rejection did not complete promptly")
	}
	if time.Since(start) > time.Second {
		t.Fatal("rejection took too long — must be early, not TTL-bounded")
	}
	// Nothing requeued: the queue stays empty.
	if d.Queue().Pending() != 0 {
		t.Fatalf("permanently rejected request was requeued: pending %d", d.Queue().Pending())
	}
	// The rejection is traced.
	found := false
	for _, e := range rec.Export().Events {
		if e.Stage == TraceRejected && e.RequestID == "r1" {
			found = true
		}
	}
	if !found {
		t.Fatal("no rejected trace event")
	}
}

func TestGatewaySurfacesRetryableRejection(t *testing.T) {
	d := NewDispatcher(nil)
	router := NewCostRouter(DefaultRouterConfig())
	router.UpsertWorker(mkWorker("w1", "model-b", 8), RouterWorkerState{})
	d.SetRouter(router)
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-b", 8), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	g := NewGateway(d, nil)
	g.Rewriter().SetAlias("ghost-model", "model-a")
	g.Rewriter().SetTenantModels("tenant-1", []string{"model-a"})

	body := `{"model":"ghost-model","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-1")
	setTestTraffic(t, g, req, "tenant-1", "interactive-paid")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != 503 {
		t.Fatalf("status = %d, want 503 retryable rejection; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no eligible worker") {
		t.Fatalf("body %q missing the honest reason", w.Body.String())
	}
}

func TestTransientMissKeepsLateBinding(t *testing.T) {
	d := NewDispatcher(nil)
	router := NewCostRouter(DefaultRouterConfig())
	router.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{
		Load: WorkerLoad{MaxConcurrent: 4, Active: 4},
	})
	d.SetRouter(router)
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 8), 1)
	sel.SetLoad("w1", 4, 0) // saturated
	d.SetSelector(sel)

	if _, err := d.Submit(queueRequest("r1", "tenant-1", "interactive-paid", "model-a")); err != nil {
		t.Fatal(err)
	}
	if bound := d.Assign("w1"); bound != nil {
		t.Fatal("saturated transient state must not bind")
	}
	// Transient miss: requeued, waiting for capacity (late binding).
	if d.Queue().Pending() != 1 {
		t.Fatalf("transient request must stay queued: pending %d", d.Queue().Pending())
	}
}
