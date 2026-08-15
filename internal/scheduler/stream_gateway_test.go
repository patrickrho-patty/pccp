package scheduler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayStreamingSSE(t *testing.T) {
	g, d := newTestGateway()
	d.SetStreamForwarder(&fakeStreamForwarder{deltas: []string{"안", "녕"}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)
	g.Rewriter().SetAlias("m", "model-a")

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_tokens":10,"stream":true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	out := w.Body.String()
	if !strings.Contains(out, "data: ") || !strings.Contains(out, "[DONE]") {
		t.Fatalf("missing SSE shape: %q", out)
	}
	for _, want := range []string{"안", "녕"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing delta %q in %q", want, out)
		}
	}
	var chunk map[string]interface{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "data: ") && !strings.Contains(line, "[DONE]") {
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk)
		}
	}
	if chunk["object"] != "chat.completion.chunk" {
		t.Fatalf("chunk object = %v", chunk["object"])
	}
}
