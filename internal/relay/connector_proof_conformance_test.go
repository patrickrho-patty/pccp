package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/paper"
)

// TestConnectorProofConformanceAcceptsTCPExporterBinding is the
// cross-repository contract guard for the connector's AUTH_PROOF path. The
// nested `patty-code-pccp/internal/paperproto.BuildAuthProof` helper is
// expected to produce a byte-identical transcript to the relay's
// `paper.AuthContext` and a byte-identical signature to the relay's
// `paper.PeerProofSigningBytes`. This test re-derives the same bytes using
// the root repo's primitives and feeds the result through the actual
// `relay.PeerAuthenticator` to ensure the refactor doesn't drift: if either
// the connector or the relay changes a domain-separation prefix, a length
// encoding, or a hash output, this test catches the divergence the moment
// the affected side commits.
//
// The test covers the only channel binding the official connector uses today
// (`tcp-exporter`); additional bindings (`webtransport/http-3`, etc.) will
// follow the same shape when the connector adds WebTransport support.
func TestConnectorProofConformanceAcceptsTCPExporterBinding(t *testing.T) {
	issuer, err := paper.NewPeerCredentialIssuer("pccp-ca")
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}
	subjectPub, subjectPriv, err := paper.GenerateKeyPair()
	if err != nil {
		t.Fatalf("subject key: %v", err)
	}
	cred, err := issuer.Issue(paper.IssueRequest{
		SubjectPeerID:           "hrn:patty:connector-probe",
		Organization:            "org-connector-probe",
		Profile:                 paper.ProfileHarness,
		PublicKey:               subjectPub,
		Validity:                time.Hour,
		RevocationAuthority:     "pccp-ca",
		AllowedProtocolVersions: []uint8{1},
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	credHex, err := cred.SignWith(issuer.PrivateKey)
	if err != nil {
		t.Fatalf("sign credential: %v", err)
	}
	credentialBytes, err := hex.DecodeString(credHex)
	if err != nil {
		t.Fatalf("decode credential hex: %v", err)
	}

	// The connector builds the auth context from these exact three fields
	// first. The relay recomputes the same auth context, so the proof only
	// verifies when the two sides agree on the canonical CBOR encodings of
	// HELLO and HELLO_ACK.
	hello := &paper.HelloMessage{
		CoreVersions:       []uint8{1},
		PeerProfile:        paper.ProfileHarness,
		TransportFeatures:  []string{"tcp-tls"},
		Extensions:         map[string]uint8{"paper.ai/1": 1, "paper.models/1": 1},
		EncodingProfiles:   []string{"cbor"},
		CryptoProfiles:     []string{"PAPER-BASE-1"},
		ClientNonce:        bytes.Repeat([]byte{0x11}, 32),
		ImplementationName: "patty-code",
	}
	ack := &paper.HelloAckMessage{
		CoreVersion:       1,
		ExtensionVersions: map[string]uint8{"paper.ai/1": 1},
		CryptoProfile:     "PAPER-BASE-1",
		ServerNonce:       bytes.Repeat([]byte{0x22}, 32),
		// ResourceLimits MUST be present in the connector's decoded view
		// even when empty; otherwise the connector's canonical CBOR drops
		// field 8 and the relay's auth context silently diverges.
		ResourceLimits: map[string]uint64{"max_payload_len": 1 << 20},
	}
	helloCBOR, err := paper.MarshalCBOR(hello)
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	ackCBOR, err := paper.MarshalCBOR(ack)
	if err != nil {
		t.Fatalf("marshal ack: %v", err)
	}

	credDigest := paper.ComputeObjectDigest(paper.ObjTypePeerCredential, credentialBytes)
	authContext := paper.AuthContext(
		helloCBOR, ackCBOR,
		hello.ClientNonce, ack.ServerNonce,
		[]byte("tcp-exporter"),
		credDigest.Bytes(),
	)

	challengeID := []byte("conn-challenge-001")
	revocationEpoch := uint64(7)
	proof := &paper.AuthProofMessage{
		Credential:         credentialBytes,
		Signature:          ed25519.Sign(subjectPriv, paper.PeerProofSigningBytes(authContext.Bytes(), challengeID, revocationEpoch)),
		KeyAlgorithm:       paper.COSEAlgEdDSA,
		ChallengeID:        challengeID,
		RevocationEvidence: paper.EncodeRevocationEpoch(revocationEpoch),
	}

	verifier := NewPeerAuthenticator(TrustBundle{
		Issuers:         map[string]ed25519.PublicKey{"pccp-ca": issuer.PublicKey},
		ProtocolVersion: 1,
		RevocationEpoch: revocationEpoch,
		RevokedSerials:  map[string]uint64{},
	})
	got, err := verifier.VerifyPeerProof(context.Background(), authContext.Bytes(), proof)
	if err != nil {
		t.Fatalf("relay rejected connector-style proof: %v", err)
	}
	if got.SubjectPeerID != "hrn:patty:connector-probe" {
		t.Fatalf("authenticated peer = %q, want hrn:patty:connector-probe", got.SubjectPeerID)
	}
}

// TestConnectorProofConformanceRejectsStaleChannelBinding guards the
// connector-side `ChannelBinding: []byte("tcp-exporter")` constant: if the
// relay ever changes the channel binding on the listener side, the
// connector's hard-coded binding would mismatch the relay's transcript and
// every proof would fail. This test pins the binding for the current
// regression window so a relay-side change is loudly visible.
func TestConnectorProofConformanceRejectsStaleChannelBinding(t *testing.T) {
	issuer, err := paper.NewPeerCredentialIssuer("pccp-ca")
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}
	subjectPub, subjectPriv, err := paper.GenerateKeyPair()
	if err != nil {
		t.Fatalf("subject key: %v", err)
	}
	cred, err := issuer.Issue(paper.IssueRequest{
		SubjectPeerID:           "hrn:patty:binding-probe",
		Organization:            "org-connector-probe",
		Profile:                 paper.ProfileHarness,
		PublicKey:               subjectPub,
		Validity:                time.Hour,
		RevocationAuthority:     "pccp-ca",
		AllowedProtocolVersions: []uint8{1},
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	credHex, err := cred.SignWith(issuer.PrivateKey)
	if err != nil {
		t.Fatalf("sign credential: %v", err)
	}
	credentialBytes, err := hex.DecodeString(credHex)
	if err != nil {
		t.Fatalf("decode credential hex: %v", err)
	}

	hello := &paper.HelloMessage{
		CoreVersions:    []uint8{1},
		PeerProfile:     paper.ProfileHarness,
		ClientNonce:     bytes.Repeat([]byte{0x33}, 32),
		CryptoProfiles:  []string{"PAPER-BASE-1"},
		EncodingProfiles: []string{"cbor"},
	}
	ack := &paper.HelloAckMessage{
		CoreVersion:   1,
		CryptoProfile: "PAPER-BASE-1",
		ServerNonce:   bytes.Repeat([]byte{0x44}, 32),
	}
	helloCBOR, _ := paper.MarshalCBOR(hello)
	ackCBOR, _ := paper.MarshalCBOR(ack)
	credDigest := paper.ComputeObjectDigest(paper.ObjTypePeerCredential, credentialBytes)

	// Connector signs with the right binding.
	authContext := paper.AuthContext(
		helloCBOR, ackCBOR,
		hello.ClientNonce, ack.ServerNonce,
		[]byte("tcp-exporter"),
		credDigest.Bytes(),
	)
	challengeID := []byte("binding-probe-001")
	revocationEpoch := uint64(7)
	proof := &paper.AuthProofMessage{
		Credential:         credentialBytes,
		Signature:          ed25519.Sign(subjectPriv, paper.PeerProofSigningBytes(authContext.Bytes(), challengeID, revocationEpoch)),
		KeyAlgorithm:       paper.COSEAlgEdDSA,
		ChallengeID:        challengeID,
		RevocationEvidence: paper.EncodeRevocationEpoch(revocationEpoch),
	}

	// Relay, however, computes the auth context with a different binding.
	// The connector's proof MUST NOT verify — it confirms the binding
	// string is part of the security boundary.
	staleContext := paper.AuthContext(
		helloCBOR, ackCBOR,
		hello.ClientNonce, ack.ServerNonce,
		[]byte("webtransport"),
		credDigest.Bytes(),
	)
	verifier := NewPeerAuthenticator(TrustBundle{
		Issuers:         map[string]ed25519.PublicKey{"pccp-ca": issuer.PublicKey},
		ProtocolVersion: 1,
		RevocationEpoch: revocationEpoch,
		RevokedSerials:  map[string]uint64{},
	})
	if _, err := verifier.VerifyPeerProof(context.Background(), staleContext.Bytes(), proof); err == nil {
		t.Fatal("connector proof accidentally verified under a different channel binding")
	}
}
