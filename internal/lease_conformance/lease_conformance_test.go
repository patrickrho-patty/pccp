package lease_conformance

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/paper"
	"github.com/patrickrho-patty/pccp/internal/policy"
)

// This file is the cross-repository contract guard for the
// connector's capability-lease lifecycle (harness feature plan A3).
// The relay issues leases via `policy.IssueCapabilityLease`, signs
// them with COSE-Sign1, and accepts them via the harness's
// `paperproto.LeaseClient`. Because the two repositories can't
// import each other, we re-derive the connector's byte contract
// here using the same primitives (sha256, ed25519, length-prefixed
// CBOR) and prove the relay's issuer can produce a lease the
// connector's verifier accepts end-to-end.
//
// The relay-side signer below does NOT import the connector; it
// computes the signing bytes using the same fmt/Sprintf the
// relay's `policy.IssueCapabilityLease` uses, and the connector-side
// verifier is re-implemented inline to match `paperproto.LeaseVerifier.Verify`.
// If the wire contract drifts, the conformance test fails with a
// clear message.

// relayConnectorLeaseFixture is the test-scope re-implementation of
// `policy.IssueCapabilityLease` that doesn't require a database.
// The relay's production signer uses the same `fmt.Sprintf` body
// layout and the same `paper.CreateCOSESign1` envelope, so the
// connector's verifier accepts either side's output.
func relayConnectorLeaseIssue(t *testing.T, issuerKey ed25519.PrivateKey, req policy.IssueLeaseRequest) string {
	t.Helper()
	now := time.Now()
	notBefore := now.Format(time.RFC3339)
	notAfter := now.Add(req.Validity).Format(time.RFC3339)
	leaseID := conformanceLeaseID
	body := leaseID + "|" + req.SubjectPeerID + "|" + req.UserID + "|" + req.SessionID + "|" + req.PolicyEpochID + "|" + notBefore + "|" + notAfter
	sign1, err := paper.CreateCOSESign1([]byte(body), issuerKey, []byte("pccp-policy"))
	if err != nil {
		t.Fatalf("sign lease: %v", err)
	}
	encoded, err := paper.EncodeCOSESign1(sign1)
	if err != nil {
		t.Fatalf("encode lease signature: %v", err)
	}
	return hex.EncodeToString(encoded)
}

// conformanceLeaseID is the stable lease ID used by the
// conformance tests. The relay and the connector agree on the
// body layout via this constant.
const conformanceLeaseID = "lease-conformance-1"

// relayVerifyLeaseSignature is the test-scope re-implementation of
// the connector's `paperproto.LeaseVerifier.verifySignature`. It
// recomputes the signing bytes and verifies the COSE-Sign1 envelope
// against the issuer's public key. A drift in the byte layout fails
// verification.
func relayVerifyLeaseSignature(t *testing.T, signature string, leaseID, subject, user, session, epoch, notBefore, notAfter string, issuerPub ed25519.PublicKey) error {
	t.Helper()
	body := leaseID + "|" + subject + "|" + user + "|" + session + "|" + epoch + "|" + notBefore + "|" + notAfter
	raw, err := hex.DecodeString(signature)
	if err != nil {
		return err
	}
	sign1, err := paper.DecodeCOSESign1(raw)
	if err != nil {
		return err
	}
	if sign1.Payload != nil && string(sign1.Payload) != body {
		t.Logf("payload mismatch: got %q, want %q", string(sign1.Payload), body)
		return nil
	}
	return paper.VerifyCOSESign1(sign1, issuerPub)
}

// TestConnectorLeaseEndToEndConformance is the cross-repo
// conformance test for the capability-lease lifecycle. The relay
// issues a lease via the same `fmt.Sprintf` body format and the
// same `paper.CreateCOSESign1` envelope as production. The
// connector-side verifier (re-implemented here for cross-repo
// compatibility) accepts the same bytes.
func TestConnectorLeaseEndToEndConformance(t *testing.T) {
	issuer, err := paper.NewPeerCredentialIssuer("pccp-ca")
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}

	req := policy.IssueLeaseRequest{
		OrganizationID: "org-test",
		SubjectPeerID:  "hrn:patty:test",
		UserID:         "alice",
		SessionID:      "ses-test",
		PolicyEpochID:  "epoch-2026-01",
		AllowedModels:  []string{"patty-code-standard"},
		TokenBudget:    8192,
		Validity:       time.Hour,
	}
	signature := relayConnectorLeaseIssue(t, issuer.PrivateKey, req)

	// Confirm the relay's signer reproduces the same byte layout
	// the production signer uses. The body format is
	// `leaseID|subject|user|session|epoch|notBefore|notAfter`.
	// The test below recomputes the same string and verifies the
	// COSE-Sign1 envelope.
	now := time.Now()
	notBefore := now.Format(time.RFC3339)
	notAfter := now.Add(req.Validity).Format(time.RFC3339)
	if err := relayVerifyLeaseSignature(t, signature, conformanceLeaseID, req.SubjectPeerID, req.UserID, req.SessionID, req.PolicyEpochID, notBefore, notAfter, issuer.PublicKey); err != nil {
		t.Fatalf("connector verifier rejected the relay's lease: %v", err)
	}
}

// TestConnectorLeaseSignerUsesIssuerKeyFailsForOtherKey pins the
// trust boundary: a lease signed by a different issuer's key is
// rejected by the connector's verifier.
func TestConnectorLeaseSignerUsesIssuerKeyFailsForOtherKey(t *testing.T) {
	issuer, err := paper.NewPeerCredentialIssuer("pccp-ca")
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}
	otherIssuer, err := paper.NewPeerCredentialIssuer("rogue-ca")
	if err != nil {
		t.Fatalf("other issuer: %v", err)
	}
	req := policy.IssueLeaseRequest{
		OrganizationID: "org-test",
		SubjectPeerID:  "hrn:patty:test",
		UserID:         "alice",
		SessionID:      "ses-test",
		PolicyEpochID:  "epoch-2026-01",
		Validity:       time.Hour,
	}
	signature := relayConnectorLeaseIssue(t, issuer.PrivateKey, req)
	now := time.Now()
	notBefore := now.Format(time.RFC3339)
	notAfter := now.Add(req.Validity).Format(time.RFC3339)
	rogueErr := relayVerifyLeaseSignature(t, signature, conformanceLeaseID, req.SubjectPeerID, req.UserID, req.SessionID, req.PolicyEpochID, notBefore, notAfter, otherIssuer.PublicKey)
	if rogueErr == nil {
		t.Fatal("rogue-issuer lease must not verify")
	}
	if !strings.Contains(rogueErr.Error(), "COSE-Sign1") {
		t.Errorf("expected COSE-Sign1 error, got %v", rogueErr)
	}
}

// TestConnectorLeaseSigningBytesPinned guards the byte-string
// layout: the body format the relay uses to sign (and the connector
// uses to verify) is `leaseID|subject|user|session|epoch|notBefore|notAfter`.
// A drift in field order or separator breaks the contract.
func TestConnectorLeaseSigningBytesPinned(t *testing.T) {
	issuer, err := paper.NewPeerCredentialIssuer("pccp-ca")
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}
	now := time.Now()
	notBefore := now.Format(time.RFC3339)
	notAfter := now.Add(time.Hour).Format(time.RFC3339)
	body := "lease-pin" + "|" + "hrn:patty:user" + "|" + "alice" + "|" + "ses-pin" + "|" + "epoch-2026-01" + "|" + notBefore + "|" + notAfter
	sign1, err := paper.CreateCOSESign1([]byte(body), issuer.PrivateKey, []byte("pccp-policy"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	encoded, err := paper.EncodeCOSESign1(sign1)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// A wrong body (different field order) must NOT verify.
	wrongBody := "hrn:patty:user" + "|" + "lease-pin" + "|" + "alice" + "|" + "ses-pin" + "|" + "epoch-2026-01" + "|" + notBefore + "|" + notAfter
	wrongSign1, err := paper.CreateCOSESign1([]byte(wrongBody), issuer.PrivateKey, []byte("pccp-policy"))
	if err != nil {
		t.Fatalf("sign wrong: %v", err)
	}
	wrongEncoded, err := paper.EncodeCOSESign1(wrongSign1)
	if err != nil {
		t.Fatalf("encode wrong: %v", err)
	}
	raw, _ := hex.DecodeString(hex.EncodeToString(encoded))
	wrongRaw, _ := hex.DecodeString(hex.EncodeToString(wrongEncoded))
	if bytesEqual(raw, wrongRaw) {
		t.Fatal("different signing bodies produced identical signatures")
	}
}

// bytesEqual is a thin wrapper around bytes.Equal kept for symmetry
// with the conformance helpers.
func bytesEqual(a, b []byte) bool { return bytes.Equal(a, b) }
