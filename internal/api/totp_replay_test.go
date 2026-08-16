package api

import (
	"testing"
	"time"
)

// TestTOTPReplayRejected: a code accepted once must be refused on
// immediate reuse within its validity window.
func TestTOTPReplayRejected(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	code, err := totpCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !verifyTOTPAcct("replay-user@example.com", secret, code) {
		t.Fatal("first use must accept")
	}
	if verifyTOTPAcct("replay-user@example.com", secret, code) {
		t.Fatal("replay within window must be rejected")
	}
	// A different account may still use the same code (its own first use).
	if !verifyTOTPAcct("other-user@example.com", secret, code) {
		t.Fatal("independent account must not be blocked")
	}
}
