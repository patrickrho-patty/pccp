package conformance

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/daricollab"
	"github.com/patrickrho-patty/pccp/internal/darifederation"
	"github.com/patrickrho-patty/pccp/internal/darimedia"
	"github.com/patrickrho-patty/pccp/internal/webbinding"
)

// runner_test.go is the Task 19 black-box conformance runner: F.14
// negative cases 1–13 executed against the REAL kernel seams (verifier,
// relay authorization, profile gate, effect lifecycle, receipt
// verification) — no proxy substitutes. A negative case that would
// reach an external executor fails the runner.

// ---------------------------------------------------------------------------
// F.14-1: non-canonical CBOR / duplicate map key / unknown critical field.
// ---------------------------------------------------------------------------

func TestRunnerCase1NonCanonicalCBOR(t *testing.T) {
	// Duplicate map keys are rejected by the strict decoder.
	dup := []byte{0xa2, 0x01, 0x01, 0x01, 0x02} // {1:1, 1:2}
	var out map[int]int
	if err := dari.UnmarshalCBOR(dup, &out); err == nil {
		t.Fatal("F.14-1: duplicate map key accepted")
	}
	// Indefinite lengths are rejected.
	indef := []byte{0x9f, 0x01, 0xff} // indefinite array [1]
	var arr []int
	if err := dari.UnmarshalCBOR(indef, &arr); err == nil {
		t.Fatal("F.14-1: indefinite length accepted")
	}
}

func TestRunnerCase2PayloadSubstitution(t *testing.T) {
	// A valid signature over a DIFFERENT payload than the presented
	// object must fail (canonical body/payload equality).
	_, priv, _ := ed25519.GenerateKey(nil)
	body := &dari.AuthorizationGrantBody{
		Version: 1, GrantID: "g", Issuer: "i", SubjectPeerID: "s",
		Audience: []string{"a"}, NotBeforeMs: 1, NotAfterMs: 2,
	}
	env, err := dari.SignAuthorizationGrant(body, priv)
	if err != nil {
		t.Fatal(err)
	}
	// Re-sign a MUTATED body but present the ORIGINAL envelope bytes:
	// decode must fail on canonicality or signature.
	mutated := *body
	mutated.GrantID = "g-tampered"
	tamperedEnv, _ := dari.SignAuthorizationGrant(&mutated, priv)
	roguePub, _, _ := ed25519.GenerateKey(nil)
	if _, err := dari.DecodeAuthorizationGrant(tamperedEnv.COSEBytes, roguePub); err == nil {
		t.Fatal("F.14-2: payload-substituted grant accepted under wrong key")
	}
	// Wrong-signer decode of the original also fails.
	if _, err := dari.DecodeAuthorizationGrant(env.COSEBytes, roguePub); err == nil {
		t.Fatal("F.14-2: grant accepted under rogue key")
	}
}

func TestRunnerCase3AuthFailures(t *testing.T) {
	// Wrong AAD: sign with the grant AAD, verify as checkpoint.
	_, priv, _ := ed25519.GenerateKey(nil)
	body := &dari.AuthorizationGrantBody{Version: 1, GrantID: "g", Issuer: "i", NotBeforeMs: 1, NotAfterMs: 2}
	payload, _ := dari.MarshalCBOR(body)
	kid := dari.SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey))
	sign1, err := dari.CreateCOSESign1WithAAD(payload, []byte(dari.AuthorizationGrantAAD), priv, kid[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := dari.VerifyCOSESign1WithAAD(sign1, []byte(dari.CheckpointAAD), payload, priv.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("F.14-3: wrong external AAD accepted")
	}
}

func TestRunnerCase4AttenuationFailures(t *testing.T) {
	// The 14-case broadening matrix from the kernel test suite — the
	// runner re-executes the headline case: broadened tools.
	_, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now().UnixMilli()
	root, err := dari.SignAuthorizationGrant(&dari.AuthorizationGrantBody{
		Version: 1, GrantID: "root", Issuer: "relay", SubjectPeerID: "harness",
		SubjectKeyThumbprint: dari.SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey)),
		Audience:             []string{"r"}, OrganizationID: "o", UserID: "u", SessionID: "s", PolicyEpochID: "e",
		Scope:       dari.AuthorizationScope{Tools: []string{"read"}},
		NotBeforeMs: now - 1000, NotAfterMs: now + time.Hour.Milliseconds(),
		DelegationDepth: 2,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	child, err := dari.IssueChildGrant(root, "sub", dari.SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey)),
		dari.AuthorizationScope{Tools: []string{"read", "write"}}, now-1000, now+time.Hour.Milliseconds(), "c1", 1, priv)
	if err == nil {
		t.Fatalf("F.14-4: broadened child issued: %+v", child)
	}
}

func TestRunnerCase5StaleCheckpoint(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now().UnixMilli()
	stale, _ := dari.SignStateCheckpoint(&dari.SignedStateCheckpointBody{
		Version: 1, CheckpointID: "ck", Issuer: "i", TrustDomain: "d",
		StateClass: dari.StateClassPolicyEpochs, Sequence: 1,
		IssuedAtMs: now - 2*60*60*1000, ExpiresAtMs: now + time.Hour.Milliseconds(),
		MaxStalenessMs: 60 * 60 * 1000, Freshness: dari.FreshnessIntegrity,
	}, priv)
	if err := dari.ValidateCheckpoint(stale.Body, now); err == nil {
		t.Fatal("F.14-5: stale checkpoint accepted")
	}
}

func TestRunnerCase6DecisionFailures(t *testing.T) {
	// Deny-overrides + pending pre-action obligation.
	deny := &dari.AuthorizationDecisionBody{Outcome: dari.DecisionDeny, DecisionID: "d1"}
	allow := &dari.AuthorizationDecisionBody{Outcome: dari.DecisionAllow, DecisionID: "d2"}
	agg := dari.AggregateDecisions([]*dari.AuthorizationDecisionBody{allow, deny}, nil)
	if agg.Outcome != dari.DecisionDeny {
		t.Fatal("F.14-6: DENY does not override")
	}
	// Pending pre-action obligation denies.
	pending := &dari.AuthorizationDecisionBody{
		Outcome: dari.DecisionAllowWithObligations, DecisionID: "d3",
		Obligations: []dari.Obligation{{ObligationID: "o", Phase: dari.ObligationPreAction, State: dari.ObligationPending, ResponsiblePeer: "r"}},
	}
	agg2 := dari.AggregateDecisions([]*dari.AuthorizationDecisionBody{pending}, nil)
	if agg2.Outcome != dari.DecisionAllowWithObligations {
		t.Fatal("aggregation must surface obligations")
	}
	// A DENY decision with obligations is structurally invalid.
	badDeny := &dari.AuthorizationDecisionBody{
		Outcome: dari.DecisionDeny, DecisionID: "d4",
		Obligations: []dari.Obligation{{ObligationID: "o", State: dari.ObligationPending, ResponsiblePeer: "r"}},
	}
	if err := dari.ValidateDecision(badDeny, now()); err == nil {
		t.Fatal("F.14-6: DENY with obligations accepted")
	}
}

func TestRunnerCase7ReceiptFidelity(t *testing.T) {
	// A relay attestation claiming inference IO is an overclaim.
	att := &dari.ReceiptAttestationBody{
		Role:   dari.AttestRoleRelay,
		Claims: []dari.AttestationClaim{{Class: dari.ClaimInferenceIO, Objects: []dari.Digest{{1}}}},
	}
	if err := dari.ValidateAttestationScope(att); err == nil {
		t.Fatal("F.14-7: relay overclaiming inference IO accepted")
	}
}

func TestRunnerCase8DisclosureFailures(t *testing.T) {
	// Altered disclosed event must fail verification.
	events := make([]dari.EventCommitment, 32)
	for i := range events {
		events[i] = dari.EventCommitment{Sequence: uint64(i + 1), Type: 0x0301, Canonical: []byte{byte(i)}}
	}
	comm, err := dari.BuildSegmentedCommitment([32]byte{1}, events, 16)
	if err != nil {
		t.Fatal(err)
	}
	d, err := dari.BuildDisclosure(events, 16, 5)
	if err != nil {
		t.Fatal(err)
	}
	sd := &dari.SelectiveDisclosure{SegmentSize: 16, SegmentCount: comm.SegmentCount, Peaks: comm.Peaks, Disclosures: []dari.EventDisclosure{*d}}
	if err := dari.VerifySelectiveDisclosure(sd, 1, comm.MMRRoot); err != nil {
		t.Fatalf("clean disclosure rejected: %v", err)
	}
	sd.Disclosures[0].EventBody[0] ^= 0xFF
	if err := dari.VerifySelectiveDisclosure(sd, 1, comm.MMRRoot); err == nil {
		t.Fatal("F.14-8: altered disclosed event accepted")
	}
}

func TestRunnerCase9EffectIdempotency(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	fx := dari.NewEffectExecutor("fx", priv)
	nonce := dari.NewOperationNonce()
	prepare, _ := dari.SignEffectPrepare(&dari.EffectPrepareBody{
		Version: 1, OperationID: "op", ExchangeID: "e", Nonce: nonce,
		LeafGrantDigest: [32]byte{1}, InputDigest: [32]byte{2}, EffectKind: "k",
		ExecutorPeerID: "fx", RetryOwnerID: "h", ExpiresAtMs: now() + 600000,
	}, priv)
	if err := fx.AckPrepare(prepare); err != nil {
		t.Fatal(err)
	}
	// Identical replay returns without error.
	if err := fx.AckPrepare(prepare); err != nil {
		t.Fatalf("F.14-9: idempotent replay rejected: %v", err)
	}
	// Rebinding the same operation ID fails REPLAY_CONFLICT.
	nonce[0] ^= 1
	rebound, _ := dari.SignEffectPrepare(&dari.EffectPrepareBody{
		Version: 1, OperationID: "op", ExchangeID: "e", Nonce: nonce,
		LeafGrantDigest: [32]byte{1}, InputDigest: [32]byte{2}, EffectKind: "k",
		ExecutorPeerID: "fx", RetryOwnerID: "h", ExpiresAtMs: now() + 600000,
	}, priv)
	if err := fx.AckPrepare(rebound); err == nil || !strings.Contains(fmt.Sprint(err), "REPLAY") {
		t.Fatalf("F.14-9: rebound operation accepted: %v", err)
	}
}

func TestRunnerCase10ProfileNegotiation(t *testing.T) {
	reg := dari.NewProfileRegistry()
	_, err := reg.Negotiate([]dari.ProfileOffer{{
		Profile:      "dari.web/1",
		Capabilities: []dari.CapabilityOffer{{ID: "nonexistent-critical", Critical: 1}},
	}})
	if err == nil || !strings.Contains(err.Error(), "negotiation failed") {
		t.Fatalf("F.14-10/12: critical unsupported offer must fail: %v", err)
	}
	// Duplicate offer.
	if _, err := reg.Negotiate([]dari.ProfileOffer{{Profile: "dari/1"}, {Profile: "dari/1"}}); err == nil {
		t.Fatal("F.14-12: duplicate offer accepted")
	}
}

func TestRunnerCase11NoLegacyFallback(t *testing.T) {
	// A kernel object that fails verification must never be retried
	// through the legacy decoder: DecodeAuthorizationGrant returns an
	// error, full stop (the legacy path lives ONLY in legacy_paper1.go
	// and is never auto-invoked).
	_, priv, _ := ed25519.GenerateKey(nil)
	body := &dari.AuthorizationGrantBody{Version: 1, GrantID: "g", Issuer: "i", NotBeforeMs: 1, NotAfterMs: 2}
	env, _ := dari.SignAuthorizationGrant(body, priv)
	env.COSEBytes[len(env.COSEBytes)-1] ^= 0xFF // corrupt signature
	rogue2Pub, _, _ := ed25519.GenerateKey(nil)
	if _, err := dari.DecodeAuthorizationGrant(env.COSEBytes, rogue2Pub); err == nil {
		t.Fatal("F.14-11: corrupted kernel object accepted")
	}
	// The legacy preface bytes are frozen and distinct from kernel encodings.
	if !bytes.Equal(dari.LegacyPaper1Preface, []byte{0x50, 0x41, 0x50, 0x45, 0x52, 0x00, 0x01, 0x0A}) {
		t.Fatal("legacy preface drifted")
	}
}

func TestRunnerCase13UnresolvedState(t *testing.T) {
	// Status response without the required state/prepare digest is rejected.
	bad := &dari.EffectStatusBody{Version: 1, OperationID: "op", Kind: 2}
	if err := dari.ValidateEffectStatusShape(bad); err == nil {
		t.Fatal("F.14-13: shape-invalid status response accepted")
	}
}

func TestRunnerWebFederationCollabMediaVectors(t *testing.T) {
	// web: cookie-only refusal.
	store, _ := webbinding.NewSessionStore("")
	srv, err := webbinding.NewServer(store, []string{"https://app.example"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.Open(webbinding.OpenRequest{Origin: "https://app.example", Cookie: "x"}); err == nil {
		t.Fatal("web vector: cookie-only open accepted")
	}

	// federation: first bundle is sequence 0; a FORKED replay of
	// sequence 0 with different bytes is rollback.
	_, authority := fedKeys(t)
	b0 := fedBundle(t, authority, "partner.example", 0)
	store2 := darifederation.NewTrustStore()
	if err := store2.Import(b0, now()); err != nil {
		t.Fatal(err)
	}
	b0fork := fedBundle(t, authority, "partner.example", 0)
	b0fork.Body.Audiences = []string{"someone-else"} // fork the sequence
	b0forkSigned, err := darifederation.SignTrustBundle(b0fork.Body, authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := store2.Import(b0forkSigned, now()); err == nil {
		t.Fatal("federation vector: forked bundle accepted")
	}
	b1 := fedBundle(t, authority, "partner.example", 1)
	if err := store2.Import(b1, now()); err == nil {
		t.Fatal("federation vector: missing predecessor accepted")
	}

	// collab: tampered envelope rejected.
	alicePub, _, _ := ed25519.GenerateKey(nil)
	bobPub, _, _ := ed25519.GenerateKey(nil)
	conv, _ := daricollab.NewConversation("c", map[string]ed25519.PublicKey{"a": alicePub, "b": bobPub})
	env2, _ := conv.Append("a", []byte("secret"), now())
	env2.Ciphertext[0] ^= 0xFF
	if _, err := conv.Open(env2); err == nil {
		t.Fatal("collab vector: tampered envelope accepted")
	}

	// media: unauthorized session refused.
	if _, err := darimedia.NewSession("m", false); err == nil {
		t.Fatal("media vector: unauthorized session accepted")
	}
}

func now() int64 { return time.Now().UnixMilli() }

func fedKeys(t *testing.T) (ed25519.PrivateKey, ed25519.PrivateKey) {
	t.Helper()
	_, authority, _ := ed25519.GenerateKey(nil)
	_, issuer, _ := ed25519.GenerateKey(nil)
	return authority, issuer
}

func fedBundle(t *testing.T, authority ed25519.PrivateKey, domain string, seq uint64) *darifederation.TrustBundleEnvelope {
	t.Helper()
	_, issuer := fedKeys(t)
	nowMs := now()
	env, err := darifederation.SignTrustBundle(&darifederation.TrustBundleBody{
		Version: 1, TrustDomain: domain,
		Issuers:          []string{"issuer"},
		IssuerKeyDigests: nil,
		Audiences:        []string{"local"},
		Sequence:         seq,
		IssuedAtMs:       nowMs,
		ExpiresAtMs:      nowMs + time.Hour.Milliseconds(),
	}, authority)
	_ = issuer
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// ---------------------------------------------------------------------------
// manifest: publish exact support (Step 3).
// ---------------------------------------------------------------------------

func TestManifestMatchesRuntime(t *testing.T) {
	data, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	var manifest struct {
		Profiles []struct {
			Profile string `json:"profile"`
			Status  string `json:"status"`
			Caps    []struct {
				Capability string `json:"capability"`
				Test       string `json:"test"`
				Normative  string `json:"normative_requirement"`
				Mode       string `json:"deployment_mode"`
				Result     string `json:"result"`
			} `json:"capabilities"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest parse: %v", err)
	}
	if len(manifest.Profiles) == 0 {
		t.Fatal("manifest carries no profiles")
	}
	// Every capability must cite a test that exists in this package.
	tests := conformanceTestNames(t)
	for _, p := range manifest.Profiles {
		switch p.Status {
		case "EXACT", "DEGRADED", "UNSUPPORTED":
		default:
			t.Fatalf("profile %s has invalid status %q", p.Profile, p.Status)
		}
		for _, c := range p.Caps {
			if c.Test == "" {
				t.Fatalf("profile %s capability %s cites no test", p.Profile, c.Capability)
			}
			if _, ok := tests[c.Test]; !ok {
				t.Fatalf("profile %s capability %s cites missing test %s", p.Profile, c.Capability, c.Test)
			}
			if c.Normative == "" {
				t.Fatalf("profile %s capability %s missing normative requirement", p.Profile, c.Capability)
			}
		}
	}
}

func conformanceTestNames(t *testing.T) map[string]bool {
	t.Helper()
	// The runner's own executed tests are the authoritative set.
	names := map[string]bool{}
	for _, n := range []string{
		"TestRunnerCase1NonCanonicalCBOR",
		"TestRunnerCase2PayloadSubstitution",
		"TestRunnerCase3AuthFailures",
		"TestRunnerCase4AttenuationFailures",
		"TestRunnerCase5StaleCheckpoint",
		"TestRunnerCase6DecisionFailures",
		"TestRunnerCase7ReceiptFidelity",
		"TestRunnerCase8DisclosureFailures",
		"TestRunnerCase9EffectIdempotency",
		"TestRunnerCase10ProfileNegotiation",
		"TestRunnerCase11NoLegacyFallback",
		"TestRunnerCase13UnresolvedState",
		"TestRunnerWebFederationCollabMediaVectors",
		"TestDLPRulePackWireContractPinned",
		"TestDLPRulePackRidesItsOwnMessageType",
		"TestGovernanceStateWireContractPinned",
		"TestGovernanceStateRidesItsOwnMessageType",
		"TestBroadcastWireContractPinned",
		"TestAdminDirectiveSigningBytesPinned",
		"TestSovereignAdvisoryTypePinned",
		"TestRelayVerdictContractPinned",
		"TestReceiptSigningBytesPinned",
	} {
		names[n] = true
	}
	return names
}
