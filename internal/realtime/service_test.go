package realtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestBroadcast(t *testing.T) {
	svc := New()
	svc.Broadcast("test.event", map[string]string{"msg": "hello"})
	// No clients connected, should not panic
}

func TestNotifySession(t *testing.T) {
	svc := New()
	svc.NotifySessionUpdate("org-1", "ses-1", "active")
	svc.NotifySecurityFinding("org-1", "high", "테스트 보안 발견")
	svc.NotifyChatMessage("org-1", "conv-1", "김개발", "안녕하세요")
	svc.NotifyFleetAction("org-1", "quarantine", "hrn-1")
	// Should not panic with no clients
}

func TestConnectedClients(t *testing.T) {
	svc := New()
	if svc.ConnectedClients() != 0 {
		t.Fatal("expected 0 clients")
	}
}

// --- PAT-1496: SSE contract (auth, named events, org routing, cleanup) ---

func sseTestToken(secret string, orgID string) string {
	claims := jwt.MapClaims{"org_id": orgID}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := t.SignedString([]byte(secret))
	return s
}

func TestSSERejectsInvalidToken(t *testing.T) {
	svc := New()
	h := svc.HandleSSE("secret")
	req := httptest.NewRequest("GET", "/api/realtime/sse?token=bogus", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token → %d, want 401", rec.Code)
	}
}

func TestSSEDeliversNamedSessionEventsToOrg(t *testing.T) {
	svc := New()
	h := svc.HandleSSE("secret")
	tok := sseTestToken("secret", "org-1")

	req := httptest.NewRequest("GET", "/api/realtime/sse?token="+tok, nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()

	// Wait for the handler to register the client.
	deadline := time.Now().Add(2 * time.Second)
	for svc.ConnectedClients() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if svc.ConnectedClients() == 0 {
		t.Fatal("sse client never connected")
	}

	// An org-scoped session update must reach the SSE client as a NAMED
	// event the browser can addEventListener('session.update') for.
	svc.NotifySessionUpdate("org-1", "ses-1", "paused")
	// Let the handler drain its channel before tearing down.
	time.Sleep(50 * time.Millisecond)

	got := rec.Body.String()
	cancel()
	<-done
	if !strings.Contains(got, "event: session.update") {
		t.Fatalf("SSE frame missing 'event: session.update' line:\n%s", got)
	}
	if !strings.Contains(got, `"session_id":"ses-1"`) || !strings.Contains(got, `"status":"paused"`) {
		t.Fatalf("SSE frame missing payload:\n%s", got)
	}
	if !strings.Contains(got, "data: ") {
		t.Fatalf("SSE frame missing data line:\n%s", got)
	}
}

func TestSSEDisconnectCleansClientRegistry(t *testing.T) {
	svc := New()
	h := svc.HandleSSE("secret")
	tok := sseTestToken("secret", "org-2")
	req := httptest.NewRequest("GET", "/api/realtime/sse?token="+tok, nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for svc.ConnectedClients() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if n := svc.ConnectedClients(); n != 0 {
		t.Fatalf("client registry after disconnect = %d, want 0", n)
	}
}
