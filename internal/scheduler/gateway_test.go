package scheduler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestGateway() (*Gateway, *Dispatcher) {
	d := NewDispatcher(nil)
	g := NewGateway(d, nil)
	return g, d
}

func TestGatewayChatCompletionsEnqueues(t *testing.T) {
	g, d := newTestGateway()
	g.Rewriter().SetAlias("ko-coder", "patty-kocoder-v1")
	body := `{"model":"ko-coder","messages":[{"role":"user","content":"안녕하세요"}],"max_tokens":100}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-1")
	req.Header.Set("X-Traffic-Class", "interactive-paid")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (late binding)", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	id, _ := resp["id"].(string)
	if id == "" {
		t.Fatal("no request id in response")
	}
	if d.Queue().Pending() != 1 {
		t.Fatalf("queue depth = %d, want 1", d.Queue().Pending())
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
	g, d := newTestGateway()
	g.Rewriter().SetAlias("ko-coder", "patty-kocoder-v1")
	body := `{"model":"ko-coder","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d", w.Code)
	}
	// The queued payload carries the RESOLVED catalog ID, not the alias.
	out, ok := d.Queue().Next()
	if !ok || out.Request == nil {
		t.Fatal("nothing queued")
	}
	if out.Request.Payload != "patty-kocoder-v1" {
		t.Fatalf("queued model = %v, want resolved catalog ID", out.Request.Payload)
	}
}

func TestGatewayCorrelationIDDeterminism(t *testing.T) {
	g, _ := newTestGateway()
	g.Rewriter().SetSplit("ab", map[string]int{"a": 50, "b": 50})
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
	g, d := newTestGateway()
	g.Rewriter().SetAlias("ko-coder", "patty-kocoder-v1")
	body := `{"model":"ko-coder","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("anthropic-version", "2023-06-01")
	w := httptest.NewRecorder()
	g.HandleAnthropicMessages(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if d.Queue().Pending() != 1 {
		t.Fatal("anthropic-format request not enqueued")
	}
}

func TestGatewayCancellationPropagates(t *testing.T) {
	g, d := newTestGateway()
	g.Rewriter().SetAlias("m", "model-a")
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if d.Queue().Pending() != 1 {
		t.Fatal("request not queued")
	}
	// The caller disconnects: the request must leave the queue (no zombie
	// work; spec §14 row 1 cancellation).
	id := w.Body.String()
	var resp struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(id), &resp)
	g.Cancel(resp.ID)
	if d.Queue().Pending() != 0 {
		t.Fatal("cancelled request still queued")
	}
}
