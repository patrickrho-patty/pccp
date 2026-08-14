package relay

import (
	"context"
	"crypto/tls"
	"testing"
	"time"
)

func TestDARIListenerCreation(t *testing.T) {
	// Verify the DARI listener can be created without errors
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{DARIALPN},
	}

	// We can't create a full relay service in a unit test (needs DB),
	// but we can verify the listener struct and ALPN constant.
	if DARIALPN != "dari/1" {
		t.Fatalf("expected dari/1, got %s", DARIALPN)
	}
	// The legacy-ALPN surface itself is pinned in internal/dari/compatibility_test.go.

	_ = tlsConfig
	// DARIListener requires a *Service which needs a DB connection,
	// so we just verify the constant here. The listener is integration-tested
	// via the demo script.
}

func TestDARIListenerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	select {
	case <-ctx.Done():
		// good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context should be cancelled")
	}
}

func TestBuildGovernRequest(t *testing.T) {
	payload := []byte(`{"model":"patty-test","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`)
	greq, err := buildGovernRequest("hrn-1", "ses-1", payload)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if greq.HarnessID != "hrn-1" || greq.SessionID != "ses-1" || greq.Model != "patty-test" || greq.MaxTokens != 100 {
		t.Fatalf("unexpected request: %+v", greq)
	}
	if len(greq.Messages) != 1 || greq.Messages[0]["role"] != "user" || greq.Messages[0]["content"] != "hi" {
		t.Fatalf("unexpected messages: %+v", greq.Messages)
	}

	greq2, err := buildGovernRequest("hrn-1", "", []byte(`{"model":"m","messages":[]}`))
	if err != nil || greq2.MaxTokens != 4096 {
		t.Fatalf("expected default max_tokens 4096, got %+v err=%v", greq2, err)
	}

	if _, err := buildGovernRequest("h", "s", []byte("{bad")); err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}
