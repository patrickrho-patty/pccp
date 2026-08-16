package scheduler

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestGatewayTenantEnvelopeMismatchRejected (spec 13.14): a signed
// envelope whose tenant conflicts with the X-Tenant-ID header must be
// rejected — the envelope is authoritative, headers are not.
func TestGatewayTenantEnvelopeMismatchRejected(t *testing.T) {
	g, _ := newServingGateway(t)
	g.Rewriter().SetAlias("ko-coder", "model-a")
	g.Rewriter().SetTenantModels("tenant-1", []string{"model-a"})
	g.Rewriter().SetTenantModels("tenant-2", []string{"model-a"})

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	g.SetTrafficIssuer(pub)
	env := NewTrafficEnvelope("req-mismatch", "tenant-1", "interactive-normal", time.Minute)
	if err := env.Sign(priv); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(env)

	body := `{"model":"ko-coder","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-2") // conflicts with the envelope
	req.Header.Set("X-Traffic-Envelope", string(raw))
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch must be 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestGatewayEnvelopeTenantAuthoritative: header+envelope agreeing uses
// the envelope tenant for the allow-list check and dispatches normally.
func TestGatewayEnvelopeTenantAuthoritative(t *testing.T) {
	g, _ := newServingGateway(t)
	g.Rewriter().SetAlias("ko-coder", "model-a")
	g.Rewriter().SetTenantModels("tenant-1", []string{"model-a"})

	body := `{"model":"ko-coder","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-1")
	setTestTraffic(t, g, req, "tenant-1", "interactive-normal")
	w := httptest.NewRecorder()
	g.HandleChatCompletions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("envelope tenant with access must pass, got %d body=%s", w.Code, w.Body.String())
	}
}
