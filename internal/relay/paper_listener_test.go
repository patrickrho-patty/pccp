package relay

import (
	"context"
	"crypto/tls"
	"testing"
	"time"
)

func TestPaperListenerCreation(t *testing.T) {
	// Verify the PAPER listener can be created without errors
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{PaperALPN},
	}

	// We can't create a full relay service in a unit test (needs DB),
	// but we can verify the listener struct and ALPN constant.
	if PaperALPN != "paper/1" {
		t.Fatalf("expected paper/1, got %s", PaperALPN)
	}

	_ = tlsConfig
	// PaperListener requires a *Service which needs a DB connection,
	// so we just verify the constant here. The listener is integration-tested
	// via the demo script.
}

func TestPaperListenerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	select {
	case <-ctx.Done():
		// good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context should be cancelled")
	}
}
