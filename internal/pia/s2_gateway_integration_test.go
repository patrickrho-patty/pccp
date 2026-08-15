package pia

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

// TestS2GatewayDispatchEndToEnd exercises the S2 data path with all real
// components: worker agent registers with the scheduler (card v2 carrying
// the PIA's DARI dispatch address), a gateway request enters the global
// queue, late-binding assigns it to the worker, the DARI forwarder performs
// AI_OPEN/AI_COMPLETE against the PIA's DARI listener, and the completion
// returns through the gateway. No mocks on the wire path.
func TestS2GatewayDispatchEndToEnd(t *testing.T) {
	// 1. Fake engine serving the OpenAI-compatible surface the PIA
	//    proxies to. The PIA↔scheduler wire path is real DARI; the
	//    PIA↔engine hop is real HTTP against the fake engine.
	engine := fakeChatEngine(t)
	cfg, svc := integrationFixture(t)

	// 2. A real PIA DARI listener bound to an ephemeral port — this is
	//    the worker's dispatch address (card v2 DariAddr). The dev
	//    direct-proxy flag skips CP lease issuance (no CP in this test).
	t.Setenv("PCCP_PIA_DIRECT", "1")
	piaService, err := New(nil, Config{PeerID: "pia-s2", ServingType: "vllm", ServingURL: engine.URL})
	if err != nil {
		t.Fatal(err)
	}
	piaListener := NewDARIListener(piaService)
	// Reserve an ephemeral port, then hand it to the listener's accept
	// loop (the scheduler's DARI forwarder dials it as the worker's
	// dispatch address).
	probe, err := dari.ListenTCP("127.0.0.1:0", piaListener.TLSConfig())
	if err != nil {
		t.Fatal(err)
	}
	piaAddr := probe.Addr().String()
	probe.Close()
	go piaListener.ListenTCP(context.Background(), piaAddr)

	// 3. Card v2 dispatch address: the worker agent reads it from env
	//    (production wiring). Point it at the real PIA DARI listener.
	t.Setenv("PCCP_PIA_DARI_ADDR", piaAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent, err := NewWorkerAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	go agent.Run(ctx)
	go svc.Serving.Start(ctx)

	// 4. Wait for the registry + selector to hold the servable worker.
	waitFor(t, 5*time.Second, func() bool {
		entry, ok := svc.Registry.Get("wkr-integration-001")
		return ok && entry.Card.Servable() && entry.Card.DariAddr == piaAddr
	})

	// 5. Gateway request: alias resolves, queues, dispatches, completes.
	svc.Serving.Gateway.Rewriter().SetAlias("ko-coder", "Qwen3.6-27B-FP8")
	svc.Serving.Gateway.Rewriter().SetTenantModels("tenant-a", []string{"Qwen3.6-27B-FP8"})
	body := `{"model":"ko-coder","messages":[{"role":"user","content":"안녕하세요"}],"max_tokens":20}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	svc.Serving.Gateway.HandleChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("gateway status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Model != "Qwen3.6-27B-FP8" {
		t.Fatalf("model = %q, want the resolved catalog ID", resp.Model)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		t.Fatalf("no completion content: %s", w.Body.String())
	}
}

// TestS2GatewayNoWorkerFailsClosed: without any servable worker, a gateway
// request must park in the queue and expire (503) — never bypass to a
// phantom endpoint (spec §13.15).
func TestS2GatewayNoWorkerFailsClosed(t *testing.T) {
	trust := scheduler.Trust{Issuers: map[string]ed25519.PublicKey{}, Now: time.Now}
	_, evidenceKey, _ := ed25519.GenerateKey(rand.Reader)
	svc := scheduler.NewScheduler(trust, nil, 2*time.Second, 4*time.Second, evidenceKey)

	svc.Serving.Gateway.SetQueueTTL(200 * time.Millisecond)
	svc.Serving.Gateway.Rewriter().SetAlias("any-model", "nowhere-model")

	// A signed interactive envelope so the request is held (not shed),
	// then expires — proving the no-worker path fails closed, never
	// bypasses to a phantom endpoint (spec §13.15).
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	svc.Serving.Gateway.SetTrafficIssuer(pub)
	env := scheduler.NewTrafficEnvelope("r1", "t1", "interactive-paid", time.Minute)
	if err := env.Sign(priv); err != nil {
		t.Fatal(err)
	}
	envJSON, _ := json.Marshal(env)

	svc.Serving.Gateway.Rewriter().SetTenantModels("t1", []string{"nowhere-model"})
	body := `{"model":"any-model","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-Traffic-Envelope", string(envJSON))
	w := httptest.NewRecorder()
	svc.Serving.Gateway.HandleChatCompletions(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (no worker, TTL expiry — fail closed)", w.Code)
	}
}
