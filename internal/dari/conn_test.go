package dari

import (
	"testing"
)

func TestConnStateTransitions(t *testing.T) {
	conn := NewConn()
	if conn.State() != StateNew {
		t.Fatalf("expected NEW, got %s", conn.State())
	}

	// Valid transitions
	steps := []ConnectionState{
		StateTransportReady,
		StateNegotiated,
		StatePeerAuthenticated,
		StateIdentityBound,
		StateReady,
		StateDraining,
		StateClosed,
	}

	for _, next := range steps {
		if err := conn.Transition(next); err != nil {
			t.Fatalf("transition to %s: %v", next, err)
		}
	}
}

func TestConnInvalidTransition(t *testing.T) {
	conn := NewConn()

	// Can't jump from NEW to READY
	if err := conn.Transition(StateReady); err == nil {
		t.Fatal("should reject invalid transition NEW → READY")
	}

	// Go through valid path to READY
	conn.Transition(StateTransportReady)
	conn.Transition(StateNegotiated)
	conn.Transition(StatePeerAuthenticated)
	conn.Transition(StateIdentityBound)
	conn.Transition(StateReady)

	// Can't go back to NEGOTIATED from READY
	if err := conn.Transition(StateNegotiated); err == nil {
		t.Fatal("should reject backward transition READY → NEGOTIATED")
	}
}

func TestConnCanAcceptMessages(t *testing.T) {
	conn := NewConn()
	if conn.CanAcceptApplicationMessages() {
		t.Fatal("should not accept messages in NEW state")
	}

	conn.Transition(StateTransportReady)
	conn.Transition(StateNegotiated)
	conn.Transition(StatePeerAuthenticated)
	conn.Transition(StateIdentityBound)
	conn.Transition(StateReady)

	if !conn.CanAcceptApplicationMessages() {
		t.Fatal("should accept messages in READY state")
	}
}

func TestConnSetIdentity(t *testing.T) {
	conn := NewConn()
	conn.SetPeerIdentity("peer-123", "org-456")

	if conn.PeerID() != "peer-123" {
		t.Fatalf("expected peer-123, got %s", conn.PeerID())
	}
	if conn.OrgID() != "org-456" {
		t.Fatalf("expected org-456, got %s", conn.OrgID())
	}

	conn.SetSession("ses-1", "lease-1", "epoch-1")
	if conn.SessionID() != "ses-1" {
		t.Fatalf("expected ses-1, got %s", conn.SessionID())
	}
}

func TestHelloMessageCBOR(t *testing.T) {
	hello := HelloMessage{
		CoreVersions:      []uint8{1},
		PeerProfile:       ProfileHarness,
		TransportFeatures: []string{"quic", "tcp"},
		Extensions:        map[string]uint8{"dari.ai/1": 1, "dari.tools/1": 1},
		CryptoProfiles:    []string{"DARI-BASE-1"},
		ClientNonce:       []byte("0123456789abcdef0123456789abcdef"),
		ImplementationName: "patty-harness",
		ImplementationVersion: "1.0.0",
	}

	data, err := MarshalCBOR(hello)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded HelloMessage
	if err := UnmarshalCBOR(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.PeerProfile != ProfileHarness {
		t.Fatal("peer profile mismatch")
	}
	if len(decoded.CoreVersions) != 1 || decoded.CoreVersions[0] != 1 {
		t.Fatal("core version mismatch")
	}
	if len(decoded.Extensions) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(decoded.Extensions))
	}
}

func TestGovernanceEnvelopeCBOR(t *testing.T) {
	env := GovernanceEnvelope{
		LeaseID:    "lease_123",
		PolicyEpoch: "epoch_456",
		Organization: "org_789",
		UserID:     "user_abc",
		HarnessID:  "hrn_def",
		SessionID:  "ses_ghi",
		Classification: "confidential",
		Purpose:    "code_review",
		RequestedCapabilities: []string{"read", "write"},
		ProtectionProfile: uint8(ProtectionP0),
	}

	data, err := MarshalCBOR(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded GovernanceEnvelope
	if err := UnmarshalCBOR(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.LeaseID != "lease_123" {
		t.Fatal("lease ID mismatch")
	}
	if decoded.Classification != "confidential" {
		t.Fatal("classification mismatch")
	}
}
