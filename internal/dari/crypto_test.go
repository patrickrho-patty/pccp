package dari

import (
	"bytes"
	"testing"
	"time"
)

func TestComputeObjectDigest(t *testing.T) {
	objType := ObjTypePeerCredential
	data := []byte(`{"test":"data"}`)
	d := ComputeObjectDigest(objType, data)

	if d.IsZero() {
		t.Fatal("expected non-zero digest")
	}
	if len(d) != 32 {
		t.Fatalf("expected 32-byte digest, got %d", len(d))
	}

	d2 := ComputeObjectDigest(objType, data)
	if d != d2 {
		t.Fatal("digest is not deterministic")
	}

	d3 := ComputeObjectDigest(ObjTypeCapabilityLease, data)
	if d == d3 {
		t.Fatal("different object types should produce different digests")
	}
}

func TestEvidenceChain(t *testing.T) {
	open := []byte("exchange_open")
	r0 := EvidenceChainStart(open)
	if r0.IsZero() {
		t.Fatal("expected non-zero R0")
	}

	r1 := EvidenceChainNext(r0, []byte("event_1"))
	if r1.IsZero() {
		t.Fatal("expected non-zero R1")
	}
	if r0 == r1 {
		t.Fatal("R0 and R1 should differ")
	}
}

func TestSignAndVerify(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	message := []byte("test message for signing")
	sig, err := SignWithEd25519(priv, message)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if !VerifyEd25519(pub, message, sig) {
		t.Fatal("signature verification failed")
	}
	if VerifyEd25519(pub, []byte("wrong"), sig) {
		t.Fatal("verification should fail for wrong message")
	}
}

func TestPeerCredentialValidity(t *testing.T) {
	issuer, err := NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatalf("create issuer: %v", err)
	}

	pub, _, _ := GenerateKeyPair()
	cred, err := issuer.Issue(IssueRequest{
		SubjectPeerID:          "hrn_test",
		Organization:           "org_test",
		Profile:                ProfileHarness,
		PublicKey:              pub,
		Validity:               3600e9,
		RevocationAuthority:    "test-ca",
		AllowedProtocolVersions: []uint8{1},
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	if !cred.IsValidAt(time.Now()) {
		t.Fatal("credential should be valid")
	}
	if cred.Issuer != "test-ca" {
		t.Fatalf("expected issuer test-ca, got %s", cred.Issuer)
	}
}

func TestGenerateID(t *testing.T) {
	id1 := GenerateID("test")
	id2 := GenerateID("test")
	if id1 == id2 {
		t.Fatal("IDs should be unique")
	}
}

func TestRecordFraming(t *testing.T) {
	rec := &Record{
		Kind:         KindMessage,
		Flags:        FlagFinal,
		MessageType:  uint16(MsgHello),
		Header:       []byte(`{"key":"value"}`),
		Payload:      []byte("payload"),
		LaneID:       42,
		LaneSequence: 1,
	}

	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := DecodeRecord(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.Kind != rec.Kind {
		t.Fatalf("kind mismatch")
	}
	if decoded.MessageType != rec.MessageType {
		t.Fatalf("message type mismatch")
	}
	if string(decoded.Payload) != string(rec.Payload) {
		t.Fatalf("payload mismatch")
	}
	if decoded.LaneID != rec.LaneID {
		t.Fatalf("lane ID mismatch")
	}
}
