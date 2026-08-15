package dari

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"
)

// effects_evidence_test.go implements the Task 10/11 conformance
// matrix: effect state machine (idempotency, replay conflict,
// terminal freeze, status shapes), segmented MMR commitment +
// selective disclosure verification, and attestation scope rules.

func effectKeys(t *testing.T) (ed25519.PrivateKey, ed25519.PrivateKey) {
	t.Helper()
	_, executor, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, relay, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return executor, relay
}

func TestEffectStateMachine(t *testing.T) {
	executorPriv, relayPriv := effectKeys(t)
	ex := NewEffectExecutor("executor-1", executorPriv)

	nonce := NewOperationNonce()
	prepare, err := SignEffectPrepare(&EffectPrepareBody{
		Version: 1, OperationID: "op-1", ExchangeID: "exch-1", Nonce: nonce,
		LeafGrantDigest: Digest{1}, InputDigest: Digest{2}, EffectKind: "file.write",
		ExecutorPeerID: "executor-1", RetryOwnerID: "harness-1",
	}, executorPriv)
	if err != nil {
		t.Fatal(err)
	}
	// Wrong executor refused.
	other := NewEffectExecutor("executor-2", executorPriv)
	if err := other.AckPrepare(prepare); err == nil {
		t.Fatal("prepare for another executor accepted")
	}
	// PREPARED.
	if err := ex.AckPrepare(prepare); err != nil {
		t.Fatal(err)
	}
	// Idempotent re-prepare with the identical envelope.
	if err := ex.AckPrepare(prepare); err != nil {
		t.Fatalf("idempotent replay rejected: %v", err)
	}
	// Same operation ID, different binding → REPLAY_CONFLICT.
	nonce2 := nonce
	nonce2[0] ^= 1
	rebound, err := SignEffectPrepare(&EffectPrepareBody{
		Version: 1, OperationID: "op-1", ExchangeID: "exch-1", Nonce: nonce2,
		LeafGrantDigest: Digest{1}, InputDigest: Digest{2}, EffectKind: "file.write",
		ExecutorPeerID: "executor-1", RetryOwnerID: "harness-1",
	}, executorPriv)
	if err != nil {
		t.Fatal(err)
	}
	if err := ex.AckPrepare(rebound); !errors.Is(err, ErrReplayConflict) {
		t.Fatalf("expected REPLAY_CONFLICT, got %v", err)
	}

	// AUTHORIZED with the bound prepare digest.
	auth, err := SignEffectAuthorization(&EffectAuthorizationBody{
		Version: 1, OperationID: "op-1", PrepareDigest: prepare.SignedDigest,
		DecisionDigest: Digest{9}, AuthorizingRelayID: "relay-1",
	}, relayPriv)
	if err != nil {
		t.Fatal(err)
	}
	if err := ex.AckAuthorize("op-1", auth); err != nil {
		t.Fatal(err)
	}
	// Authorization binding to a different prepare refused.
	if err := ex.AckAuthorize("op-1", &EffectAuthorizationEnvelope{Body: &EffectAuthorizationBody{OperationID: "op-1", PrepareDigest: Digest{7}}}); err == nil {
		t.Fatal("unbound authorization accepted")
	}

	// EXECUTING then COMMIT.
	if err := ex.Execute("op-1"); err != nil {
		t.Fatal(err)
	}
	result, err := ex.Finish("op-1", EffectCommitted, Digest{5}, prepare.SignedDigest, auth.SignedDigest)
	if err != nil {
		t.Fatal(err)
	}
	// Verify the terminal result round-trips.
	decoded, err := DecodeEffectResult(result.COSEBytes, executorPriv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Body.State != EffectCommitted {
		t.Fatal("result not COMMITTED")
	}
	// Terminal freeze: Finish returns the SAME durable result.
	again, err := ex.Finish("op-1", EffectAborted, Digest{6}, prepare.SignedDigest, auth.SignedDigest)
	if err != nil {
		t.Fatal(err)
	}
	if again != result {
		t.Fatal("terminal state transitioned")
	}
	// Illegal: execute after terminal.
	if err := ex.Execute("op-1"); err == nil {
		t.Fatal("terminal operation re-executed")
	}

	// Retry-owner reconciliation boundary.
	if err := ex.Reconcile("op-1", "harness-2"); err == nil {
		t.Fatal("non-retry-owner reconciled")
	}
	if err := ex.Reconcile("op-1", "harness-1"); err != nil {
		t.Fatal(err)
	}
}

func TestEffectStatusShapes(t *testing.T) {
	// Request must omit labels 4-7.
	req := &EffectStatusBody{Version: 1, OperationID: "op", Nonce: [32]byte{}, Kind: 1}
	if err := ValidateEffectStatusShape(req); err != nil {
		t.Fatal(err)
	}
	st := EffectStateExecuting
	req.State = &st
	if err := ValidateEffectStatusShape(req); err == nil {
		t.Fatal("request with state accepted")
	}
	req.State = nil
	// Response must include state + prepare digest.
	resp := &EffectStatusBody{Version: 1, OperationID: "op", Nonce: [32]byte{}, Kind: 2}
	if err := ValidateEffectStatusShape(resp); err == nil {
		t.Fatal("response without state accepted")
	}
	d := Digest{1}
	resp.State, resp.PrepareDigest = &st, &d
	if err := ValidateEffectStatusShape(resp); err != nil {
		t.Fatal(err)
	}
}

func committedEvents(n int, startSeq uint64) []EventCommitment {
	events := make([]EventCommitment, 0, n)
	for i := 0; i < n; i++ {
		body := []byte{byte(startSeq + uint64(i)), byte(i)}
		events = append(events, EventCommitment{Sequence: startSeq + uint64(i), Type: ObjectType(0x0301 + i%3), Canonical: body})
	}
	return events
}

func TestSegmentedCommitmentAndDisclosure(t *testing.T) {
	exchange := Digest{3}
	// Multiple segments: 2500 events, 1024-size segments → 3 segments.
	events := committedEvents(2500, 1)
	comm, err := BuildSegmentedCommitment(exchange, events, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if comm.SegmentCount != 3 || comm.EventCount != 2500 {
		t.Fatalf("geometry: %+v", comm)
	}
	if comm.LastSequence-comm.FirstSequence+1 != comm.EventCount {
		t.Fatal("sequence arithmetic violated")
	}
	// Peak list strictly descending.
	for i := 1; i < len(comm.Peaks); i++ {
		if comm.Peaks[i-1].Height <= comm.Peaks[i].Height {
			t.Fatal("peaks not strictly descending")
		}
	}
	// Deterministic: same events → same roots.
	comm2, _ := BuildSegmentedCommitment(exchange, events, 1024)
	if comm2.MMRRoot != comm.MMRRoot || comm2.LinearRoot != comm.LinearRoot {
		t.Fatal("commitment not deterministic")
	}

	// Disclose one event from the LAST (partial) segment and one from
	// the first; both must verify against the root.
	sd := &SelectiveDisclosure{
		Version: 1, ReceiptDigest: Digest{9}, SegmentSize: 1024, SegmentCount: comm.SegmentCount,
		Peaks: comm.Peaks,
	}
	// One disclosure per DISTINCT peak (segments 0, 1, 2): each
	// disclosure replaces exactly one matching peak.
	for _, seq := range []uint64{1, 1025, 2500} {
		d, err := BuildDisclosure(events, 1024, seq)
		if err != nil {
			t.Fatalf("disclosure %d: %v", seq, err)
		}
		sd.Disclosures = append(sd.Disclosures, *d)
	}
	if err := VerifySelectiveDisclosure(sd, 1, comm.MMRRoot); err != nil {
		t.Fatalf("valid disclosures rejected: %v", err)
	}

	// Tampered event body → mismatch.
	sd.Disclosures[0].EventBody = append([]byte(nil), sd.Disclosures[0].EventBody...)
	sd.Disclosures[0].EventBody[0] ^= 0xFF
	if err := VerifySelectiveDisclosure(sd, 1, comm.MMRRoot); err == nil {
		t.Fatal("tampered event verified")
	}

	// Duplicated sequence → failure.
	good, _ := BuildDisclosure(events, 1024, 1)
	sd.Disclosures = []EventDisclosure{*good, *good}
	if err := VerifySelectiveDisclosure(sd, 1, comm.MMRRoot); err == nil {
		t.Fatal("duplicated sequence verified")
	}

	// Single-segment edge case: 1 event.
	one, err := BuildSegmentedCommitment(exchange, committedEvents(1, 7), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if one.SegmentCount != 1 || len(one.Peaks) != 1 {
		t.Fatal("single-event geometry wrong")
	}
	d1, _ := BuildDisclosure(committedEvents(1, 7), 1024, 7)
	sd1 := &SelectiveDisclosure{SegmentSize: 1024, SegmentCount: 1, Peaks: one.Peaks, Disclosures: []EventDisclosure{*d1}}
	if err := VerifySelectiveDisclosure(sd1, 7, one.MMRRoot); err != nil {
		t.Fatalf("single-event disclosure rejected: %v", err)
	}

	// Non-contiguous events rejected.
	bad := committedEvents(3, 1)
	bad[2].Sequence = 5
	if _, err := BuildSegmentedCommitment(exchange, bad, 1024); err == nil {
		t.Fatal("non-contiguous events accepted")
	}
}

func TestAttestationScope(t *testing.T) {
	_, relayPriv := effectKeys(t)
	body := &ReceiptAttestationBody{
		Version: 1, ReceiptBodyDigest: Digest{1}, SignerCredentialDigest: Digest{2},
		Role:   AttestRoleRelay,
		Claims: []AttestationClaim{{Class: ClaimDecisionState, Objects: []Digest{{1}}}},
	}
	if err := ValidateAttestationScope(body); err != nil {
		t.Fatal(err)
	}
	cose, digest, err := SignReceiptAttestation(body, relayPriv)
	if err != nil || len(cose) == 0 || digest == (Digest{}) {
		t.Fatalf("sign attestation: %v", err)
	}
	// Relay attesting inference IO → scope violation.
	body.Claims = []AttestationClaim{{Class: ClaimInferenceIO, Objects: []Digest{{1}}}}
	if err := ValidateAttestationScope(body); !errors.Is(err, ErrAttestationScope) {
		t.Fatalf("expected scope violation, got %v", err)
	}
	// Inference peer with a decision claim → violation.
	body.Role = AttestRoleInference
	body.Claims = []AttestationClaim{{Class: ClaimDecisionState, Objects: []Digest{{1}}}}
	if err := ValidateAttestationScope(body); !errors.Is(err, ErrAttestationScope) {
		t.Fatal("inference peer attested a decision claim")
	}
	// Effect executor with an inference claim → violation.
	body.Role = AttestRoleEffect
	body.Claims = []AttestationClaim{{Class: ClaimInferenceIO, Objects: []Digest{{1}}}}
	if err := ValidateAttestationScope(body); !errors.Is(err, ErrAttestationScope) {
		t.Fatal("effect executor attested an inference claim")
	}
	// Duplicate objects in one claim rejected.
	body.Role = AttestRoleRelay
	body.Claims = []AttestationClaim{{Class: ClaimEvidenceRoot, Objects: []Digest{{1}, {1}}}}
	if err := ValidateAttestationScope(body); err == nil {
		t.Fatal("duplicate claim objects accepted")
	}
	_ = bytes.Equal
}
