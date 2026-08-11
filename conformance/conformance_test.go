package conformance

import (
	"io"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/paper"
)

// Invariant 1: No protected action without authenticated peer.
func TestInvariant1_AuthRequired(t *testing.T) {
	conn := paper.NewConn()
	if conn.CanAcceptApplicationMessages() {
		t.Fatal("INVARIANT VIOLATION: application messages accepted without authentication")
	}
	conn.Transition(paper.StateTransportReady)
	conn.Transition(paper.StateNegotiated)
	if conn.CanAcceptApplicationMessages() {
		t.Fatal("application messages accepted before peer authentication")
	}
	conn.Transition(paper.StatePeerAuthenticated)
	conn.Transition(paper.StateIdentityBound)
	conn.Transition(paper.StateReady)
	if !conn.CanAcceptApplicationMessages() {
		t.Fatal("should accept in READY")
	}
}

// Invariant 2: No protected action outside a valid Capability Lease.
func TestInvariant2_LeaseRequired(t *testing.T) {
	lease := paper.PeerCredential{
		NotBefore: time.Now().Add(-2 * time.Hour).UnixMilli(),
		NotAfter:  time.Now().Add(-1 * time.Hour).UnixMilli(),
	}
	if !lease.IsExpired() {
		t.Fatal("expired credential should be rejected")
	}
	validLease := paper.PeerCredential{
		NotBefore: time.Now().Add(-1 * time.Hour).UnixMilli(),
		NotAfter:  time.Now().Add(1 * time.Hour).UnixMilli(),
	}
	if validLease.IsExpired() {
		t.Fatal("valid credential marked expired")
	}
}

// Invariant 4: No model invocation against an unapproved PMP/Endpoint Lease.
func TestInvariant4_SignedModelPackage(t *testing.T) {
	pub, priv, err := paper.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"model":"patty-kocoder-v1","version":"1.0"}`)
	sig, err := paper.SignWithEd25519(priv, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !paper.VerifyEd25519(pub, manifest, sig) {
		t.Fatal("signed manifest verification failed")
	}
	if paper.VerifyEd25519(pub, []byte(`{"model":"fake"}`), sig) {
		t.Fatal("fake manifest should not verify")
	}
}

// Invariant 9: Every protected exchange terminates with verifiable evidence.
func TestInvariant9_EvidenceReceipt(t *testing.T) {
	openDigest := []byte("exchange_open_hash")
	r0 := paper.EvidenceChainStart(openDigest)
	r1 := paper.EvidenceChainNext(r0, []byte("event_1"))
	r2 := paper.EvidenceChainNext(r1, []byte("event_2"))
	if r2.IsZero() {
		t.Fatal("evidence chain root is zero")
	}
	r1Check := paper.EvidenceChainNext(r0, []byte("event_1"))
	if r1 != r1Check {
		t.Fatal("evidence chain not deterministic")
	}
}

// Invariant 10: Provenance digest changes if content changes.
func TestInvariant10_ProvenanceImmutability(t *testing.T) {
	d1 := paper.ComputeObjectDigest(paper.ObjTypeProvenanceNode, []byte(`{"node":"original"}`))
	d2 := paper.ComputeObjectDigest(paper.ObjTypeProvenanceNode, []byte(`{"node":"modified"}`))
	if d1 == d2 {
		t.Fatal("digest unchanged after modification")
	}
}

func TestConformance_Framing(t *testing.T) {
	tests := []struct {
		name    string
		kind    paper.RecordKind
		msgType uint16
		header  []byte
		payload []byte
		laneID  uint64
		laneSeq uint64
	}{
		{"HELLO", paper.KindMessage, uint16(paper.MsgHello), []byte(`{"v":1}`), []byte("hello"), 0, 0},
		{"DATA", paper.KindData, 0, []byte{}, make([]byte, 100), 1, 5},
		{"RECEIPT", paper.KindReceipt, uint16(paper.MsgEvidenceReceipt), []byte(`{"r":"x"}`), []byte("body"), 2, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &paper.Record{
				Kind: tt.kind, MessageType: tt.msgType,
				Header: tt.header, Payload: tt.payload,
				LaneID: tt.laneID, LaneSequence: tt.laneSeq,
			}
			var buf []byte
			w := &bytesWriter{buf: &buf}
			if err := paper.EncodeRecord(w, rec); err != nil {
				t.Fatal(err)
			}
			decoded, err := paper.DecodeRecord(&bytesReader{buf: buf})
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Kind != rec.Kind || string(decoded.Payload) != string(rec.Payload) {
				t.Fatal("round-trip mismatch")
			}
		})
	}
}

func TestConformance_Authentication(t *testing.T) {
	pub, priv, _ := paper.GenerateKeyPair()
	payload := []byte("auth test")
	sign1, _ := paper.CreateCOSESign1(payload, priv, []byte("kid"))
	if err := paper.VerifyCOSESign1(sign1, pub); err != nil {
		t.Fatal(err)
	}
	sign1.Payload = []byte("different")
	if err := paper.VerifyCOSESign1(sign1, pub); err == nil {
		t.Fatal("tampered payload should fail")
	}
}

func TestConformance_COSESign1RoundTrip(t *testing.T) {
	pub, priv, _ := paper.GenerateKeyPair()
	sign1, _ := paper.CreateCOSESign1([]byte("cose test"), priv, []byte("k"))
	encoded, _ := paper.EncodeCOSESign1(sign1)
	decoded, err := paper.DecodeCOSESign1(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := paper.VerifyCOSESign1(decoded, pub); err != nil {
		t.Fatal(err)
	}
}

type bytesWriter struct{ buf *[]byte }

func (w *bytesWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

type bytesReader struct {
	buf []byte
	pos int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.buf) {
		return 0, io.EOF
	}
	n := copy(p, r.buf[r.pos:])
	r.pos += n
	return n, nil
}
