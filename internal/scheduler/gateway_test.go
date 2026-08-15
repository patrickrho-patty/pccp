package scheduler

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestGateway() (*Gateway, *Dispatcher) {
	d := NewDispatcher(nil)
	g := NewGateway(d, nil)
	return g, d
}

// setTestTraffic attaches a signed traffic envelope for the given class
// and grants the tenant access to the resolved model (production tenants
// get their allow-list from policy config).
func setTestTraffic(t *testing.T, g *Gateway, req *http.Request, tenant, class string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	g.SetTrafficIssuer(pub)
	env := NewTrafficEnvelope(req.Header.Get("X-Correlation-ID"), tenant, class, time.Minute)
	if err := env.Sign(priv); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(env)
	req.Header.Set("X-Traffic-Envelope", string(raw))
}

// newServingGateway wires a dispatcher with one worker, a fake forwarder,
// and the running dispatch loop — the production serving shape.
func newServingGateway(t *testing.T) (*Gateway, *Dispatcher) {
	t.Helper()
	g, d := newTestGateway()
	d.SetForwarder(&fakeForwarder{result: InferenceResult{Text: "응답", Finish: "stop", Usage: map[string]int{"prompt_tokens": 10, "completion_tokens": 5}}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)
	return g, d
}

func TestGatewayChatCompletionsCompletes(t *testing.T) {
	g, _ := newServingGateway(t)
	g.Rewriter().SetAlias("ko-coder", "model-a")
	g.Rewriter().SetTenantModels("tenant-1", []string{"model-a"})
	body := `{"model":"ko-coder","messages":[{"role":"user","content":"안녕하세요"}],"max_tokens":100}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-1")
	setTestTraffic(t, g, req, "tenant-1", "interactive-paid")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "응답" {
		t.Fatalf("completion = %+v, want the forwarder's result", resp)
	}
}

func TestGatewayRejectsUnknownModel(t *testing.T) {
	g, _ := newTestGateway()
	g.Rewriter().SetAlias("ko-coder", "patty-kocoder-v1")
	body := `{"model":"not-a-real-model","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown model", w.Code)
	}
}

func TestGatewayModelRewriteApplied(t *testing.T) {
	// The forwarder records the model it was asked to serve — after
	// rewriting, the worker receives the RESOLVED catalog ID, not the alias.
	fw := &fakeForwarder{result: InferenceResult{Text: "ok", Finish: "stop"}}
	g, d := newTestGateway()
	d.SetForwarder(fw)
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "patty-kocoder-v1", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)
	g.Rewriter().SetAlias("ko-coder", "patty-kocoder-v1")
	g.Rewriter().SetTenantModels("t1", []string{"patty-kocoder-v1"})

	body := `{"model":"ko-coder","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	// The response echoes the resolved model.
	if !strings.Contains(w.Body.String(), "patty-kocoder-v1") {
		t.Fatalf("response missing resolved model: %s", w.Body.String())
	}
}

func TestGatewayCorrelationIDDeterminism(t *testing.T) {
	// Both split targets are served; the response model field exposes the
	// split decision. Same correlation ID must pick the same target.
	g, d := newTestGateway()
	d.SetForwarder(&fakeForwarder{result: InferenceResult{Text: "ok", Finish: "stop"}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "a", 4), 1)
	sel.Upsert(mkWorker("w2", "b", 4), 2)
	d.SetSelector(sel)
	startLoop(t, d)
	g.Rewriter().SetSplit("ab", map[string]int{"a": 50, "b": 50})
	g.Rewriter().SetTenantModels("t1", []string{"a", "b"})
	mk := func(corr string) string {
		body := `{"model":"ab","messages":[{"role":"user","content":"x"}],"max_tokens":5}`
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("X-Tenant-ID", "t1")
		req.Header.Set("X-Correlation-ID", corr)
		w := httptest.NewRecorder()
		g.HandleChatCompletions(w, req)
		var resp struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Model
	}
	// Same correlation ID → same split decision (idempotent retries).
	if mk("corr-1") != mk("corr-1") {
		t.Fatal("same correlation ID produced different split decisions")
	}
}

func TestGatewayMediaSSRFRejected(t *testing.T) {
	g, _ := newTestGateway()
	g.Rewriter().SetAlias("m1", "model-a")
	body := `{"model":"m1","messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"http://169.254.169.254/latest/meta-data"}}]}],"max_tokens":10}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for SSRF media URL", w.Code)
	}
}

func TestGatewayMediaLimitRejected(t *testing.T) {
	g, _ := newTestGateway()
	g.Rewriter().SetAlias("m1", "model-a")
	// 20 images, limit 8 — rejected before enqueue.
	var parts []map[string]interface{}
	for i := 0; i < 20; i++ {
		parts = append(parts, map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "https://cdn.example.com/i.png"}})
	}
	content := append([]map[string]interface{}{{"type": "text", "text": "many images"}}, parts...)
	body, _ := json.Marshal(map[string]interface{}{
		"model": "m1", "messages": []map[string]interface{}{{"role": "user", "content": content}}, "max_tokens": 10,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for media-limit breach", w.Code)
	}
}

func TestGatewayModelDiscovery(t *testing.T) {
	g, d := newTestGateway()
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 8), 1)
	sel.Upsert(mkWorker("w2", "model-b", 8), 2)
	d.SetSelector(sel)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	g.HandleModelDiscovery(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "model-a") || !strings.Contains(body, "model-b") {
		t.Fatalf("discovery missing models: %s", body)
	}
}

func TestGatewayAnthropicFormatAccepted(t *testing.T) {
	g, _ := newServingGateway(t)
	g.Rewriter().SetAlias("ko-coder", "model-a")
	g.Rewriter().SetTenantModels("t1", []string{"model-a"})
	body := `{"model":"ko-coder","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("anthropic-version", "2023-06-01")
	w := httptest.NewRecorder()
	g.HandleAnthropicMessages(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestGatewayCancellationPropagates(t *testing.T) {
	g, d := newTestGateway()
	// No worker at all: the request parks in the queue; the client's
	// disconnect must pull it out (no zombie work; spec §14 row 1).
	g.Rewriter().SetAlias("m", "model-a")
	g.Rewriter().SetTenantModels("t1", []string{"model-a"})
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	setTestTraffic(t, g, req, "t1", "interactive-paid")
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel() // the caller disconnects mid-queue
	}()
	g.HandleChatCompletions(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 after client disconnect", w.Code)
	}
	if d.Queue().Pending() != 0 {
		t.Fatal("disconnected request still queued")
	}
}
