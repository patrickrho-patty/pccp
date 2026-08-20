package scheduler

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayStampsPackageIdentity(t *testing.T) {
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
	g.SetPackageSource(NewFilePackageSource(map[string]PackageIdentity{
		"model-a": {ModelPackage: "model-a@1.0", TokenizerID: "tok-v3", TemplateID: "chat-v5", PolicyEpoch: "epoch-7"},
	}))

	body := `{"model":"ko-coder","messages":[{"role":"user","content":"안녕"}],"max_tokens":50}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-1")
	setTestTraffic(t, g, req, "tenant-1", "interactive-paid")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}

	var arrived *TraceEvent
	for i, e := range rec.Export().Events {
		if e.Stage == TraceArrived {
			arrived = &rec.Export().Events[i]
		}
	}
	if arrived == nil {
		t.Fatal("no arrived event")
	}
	if arrived.Cache.ModelPackage != "model-a@1.0" || arrived.Cache.TokenizerID != "tok-v3" {
		t.Fatalf("cache identity = %+v", arrived.Cache)
	}
}

func TestGatewayUnknownModelKeepsZeroIdentity(t *testing.T) {
	g, d := newServingGateway(t)
	rec := NewTraceRecorder(16)
	d.SetTraceRecorder(rec)
	g.Rewriter().SetAlias("ko-coder", "model-a")
	g.Rewriter().SetTenantModels("tenant-1", []string{"model-a"})
	g.SetPackageSource(NewFilePackageSource(nil)) // empty source: nothing packaged

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
		if e.Stage == TraceArrived && e.Cache != (CacheIdentity{}) {
			t.Fatalf("unpackaged request carries identity %+v", e.Cache)
		}
	}
}

func TestReplayHonorsTraceIdentity(t *testing.T) {
	dir := NewKVDirectory()
	dir.Add("w1", L1GPU, dirBlock("tenant-a", "h1", 700), testIdentity, true)
	events := []TraceEvent{
		{RequestID: "r1", Stage: TraceArrived, Tenant: "tenant-a", Model: "model-a",
			Cache: testIdentity, PrefixHash: "h1", InputTokens: 1000, ExpectedOutputTokens: 100},
	}
	r := NewCostRouter(DefaultRouterConfig())
	r.SetKVDirectory(dir)
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{})
	s := ReplayDecisions(events, r)
	if s.TotalOverlap != 700 {
		t.Fatalf("replay overlap = %d, want 700 from the traced identity", s.TotalOverlap)
	}
}
