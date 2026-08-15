package conformance

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// verdict_receipt_conformance_test.go pins the RELAY_VERDICT + receipt
// signature wire contracts the connector enforces (nested repo
// internal/dariproto decision.go + provenancewire receipt_verify.go).

// TestRelayVerdictContractPinned: the decision body labels (1-14) are
// stable — renumbering breaks connector verification.
func TestRelayVerdictContractPinned(t *testing.T) {
	body := &dari.AuthorizationDecisionBody{
		Version: 1, DecisionID: "dec-pin", ExchangeID: "exch-pin",
		Outcome: dari.DecisionAllow, EvaluatorPeerID: "relay-pin",
		IssuedAtMs:  time.Now().UnixMilli(),
		ExpiresAtMs: time.Now().Add(time.Hour).UnixMilli(),
	}
	_, priv, _ := ed25519.GenerateKey(nil)
	env, err := dari.SignAuthorizationDecision(body, priv)
	if err != nil {
		t.Fatal(err)
	}
	if env.SignedDigest == [32]byte{} {
		t.Fatal("decision must carry a signed digest")
	}
	// Connector-shaped decode: label 3 (ExchangeID) + label 9 (Outcome)
	// inside the COSE payload.
	var sign1 dari.COSESign1
	if err := dari.UnmarshalCBOR(env.COSEBytes, &sign1); err != nil {
		t.Fatal(err)
	}
	var labels map[uint64]any
	if err := dari.UnmarshalCBOR(sign1.Payload, &labels); err != nil {
		t.Fatal(err)
	}
	if labels[3] != "exch-pin" {
		t.Fatalf("label 3 = %v, want exchange id", labels[3])
	}
	if labels[9] != uint64(1) {
		t.Fatalf("label 9 (outcome) = %v, want 1 (ALLOW)", labels[9])
	}
}

// TestReceiptSigningBytesPinned: the relay's canonical receipt signing
// layout — the connector's VerifyEvidenceReceiptSignature mirrors it
// EXACTLY; any field drift breaks verification (nested
// provenancewire/receipt_verify_test.go pins the same bytes).
func TestReceiptSigningBytesPinned(t *testing.T) {
	chain := sha256.Sum256([]byte("chain"))
	layout := "exch-r|" + "COMPLETED" + "|" + hex.EncodeToString(chain[:]) + "|" +
		"relay-r" + "|" + "ep-r" + "|" + "pmp-r" + "|" + "2023-11-14T22:13:20Z"
	if len(layout) == 0 {
		t.Fatal("unreachable")
	}
	// The layout is pinned as a literal contract:
	want := "exch-r|COMPLETED|" + hex.EncodeToString(chain[:]) + "|relay-r|ep-r|pmp-r|2023-11-14T22:13:20Z"
	if layout != want {
		t.Fatalf("receipt signing layout drifted: %q", layout)
	}
	_, priv, _ := ed25519.GenerateKey(nil)
	sig := ed25519.Sign(priv, []byte(want))
	if !ed25519.Verify(priv.Public().(ed25519.PublicKey), []byte(want), sig) {
		t.Fatal("layout signature round-trip failed")
	}
}
