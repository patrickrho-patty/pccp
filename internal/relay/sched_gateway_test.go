package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestDefaultForwarderRoutesThroughSchedulerGateway verifies the S2 hop:
// when PCCP_SCHED_GATEWAY_ADDR is set, the governed inference path posts to
// the scheduler's unified gateway (which owns admission/queue/dispatch) —
// never directly to a PIA and never bypassed when the scheduler errors.
func TestDefaultForwarderRoutesThroughSchedulerGateway(t *testing.T) {
	sched := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if tenant := r.Header.Get("X-Tenant-ID"); tenant != "org-1" {
			t.Errorf("X-Tenant-ID = %q, want org-1", tenant)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"cmp-1","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`))
	}))
	defer sched.Close()
	t.Setenv("PCCP_SCHED_GATEWAY_ADDR", sched.URL)

	s := &Service{httpClient: &http.Client{Timeout: 10 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := s.defaultForwarder(ctx, InferenceRequest{
		ExchangeID:     "exch-1",
		OrganizationID: "org-1",
		Model:          "some-model",
		Messages:       []map[string]string{{"role": "user", "content": "hi"}},
		MaxTokens:      10,
	}, "lease-1")
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatalf("no response: %+v", resp)
	}
	if resp.Choices[0]["message"].(map[string]interface{})["content"] != "ok" {
		t.Fatalf("content = %+v", resp.Choices[0])
	}
}

// TestDefaultForwarderSchedulerDownFailsClosed: a dead scheduler gateway
// must produce an error — the relay never falls through to a direct PIA
// hop or a mock (spec §13.15 fail-closed).
func TestDefaultForwarderSchedulerDownFailsClosed(t *testing.T) {
	sched := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"saturated"}`))
	}))
	defer sched.Close()
	t.Setenv("PCCP_SCHED_GATEWAY_ADDR", sched.URL)

	s := &Service{httpClient: &http.Client{Timeout: 10 * time.Second}}
	_, err := s.defaultForwarder(context.Background(), InferenceRequest{
		ExchangeID:     "exch-2",
		OrganizationID: "org-1",
		Model:          "m",
		Messages:       []map[string]string{{"role": "user", "content": "hi"}},
		MaxTokens:      10,
	}, "lease-1")
	if err == nil {
		t.Fatal("scheduler 503 must fail the exchange, not fall through")
	}
	if !strings.Contains(err.Error(), "scheduler gateway error") {
		t.Fatalf("error = %v, want the scheduler gateway failure surfaced", err)
	}
}

// TestNoSchedulerEnvKeepsLegacyPath: without the env var the pre-S2 path
// remains available (DARI/HTTP to PIA) — deployment backwards-compat.
func TestNoSchedulerEnvKeepsLegacyPath(t *testing.T) {
	os.Unsetenv("PCCP_SCHED_GATEWAY_ADDR")
	if got := schedGatewayBase(); got != "" {
		t.Fatalf("schedGatewayBase = %q with env unset", got)
	}
}
