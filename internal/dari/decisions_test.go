package dari

import (
	"crypto/ed25519"
	"reflect"
	"testing"
	"time"
)

// decisions_test.go implements the Task 9 conformance matrix: decision
// invariants, deny-overrides aggregation, obligation lifecycle, and
// checkpoint freshness/rollback.

func decisionKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func baseDecision(outcome DecisionOutcome) *AuthorizationDecisionBody {
	return &AuthorizationDecisionBody{
		Version: 1, DecisionID: "dec-1", ExchangeID: "exch-1",
		EvaluatorPeerID: "relay-1", Outcome: outcome,
		IssuedAtMs:  time.Now().Add(-time.Minute).UnixMilli(),
		ExpiresAtMs: time.Now().Add(time.Hour).UnixMilli(),
	}
}

func TestDecisionInvariants(t *testing.T) {
	now := time.Now().UnixMilli()
	// DENY with obligations is invalid.
	d := baseDecision(DecisionDeny)
	d.Obligations = []Obligation{{ObligationID: "o1", Kind: "redact", State: ObligationPending, ResponsiblePeer: "relay-1"}}
	if err := ValidateDecision(d, now); err == nil {
		t.Fatal("DENY with obligations accepted")
	}
	// ALLOW with obligations is invalid.
	d = baseDecision(DecisionAllow)
	d.Obligations = []Obligation{{ObligationID: "o1", State: ObligationPending, ResponsiblePeer: "r"}}
	if err := ValidateDecision(d, now); err == nil {
		t.Fatal("ALLOW with obligations accepted")
	}
	// ALLOW_WITH_OBLIGATIONS with none is invalid.
	if err := ValidateDecision(baseDecision(DecisionAllowWithObligations), now); err == nil {
		t.Fatal("ALLOW_WITH_OBLIGATIONS without obligations accepted")
	}
	// Valid AWO: one PENDING obligation.
	d = baseDecision(DecisionAllowWithObligations)
	d.Obligations = []Obligation{{ObligationID: "o1", Kind: "transform", State: ObligationPending, ResponsiblePeer: "harness-1"}}
	if err := ValidateDecision(d, now); err != nil {
		t.Fatalf("valid AWO rejected: %v", err)
	}
	// Satisfied without bound evidence is invalid at issuance.
	d.Obligations[0].State = ObligationSatisfied
	if err := ValidateDecision(d, now); err == nil {
		t.Fatal("satisfied obligation without evidence accepted")
	}
	d.Obligations[0].EvidenceDigest = Digest{1}
	if err := ValidateDecision(d, now); err != nil {
		t.Fatalf("satisfied obligation with evidence rejected: %v", err)
	}
	// expires <= issued invalid.
	d2 := baseDecision(DecisionAllow)
	d2.ExpiresAtMs = d2.IssuedAtMs
	if err := ValidateDecision(d2, now); err == nil {
		t.Fatal("non-increasing validity window accepted")
	}
}

func TestDecisionSignVerifyRoundTrip(t *testing.T) {
	priv := decisionKey(t)
	d := baseDecision(DecisionAllowWithObligations)
	d.Obligations = []Obligation{{ObligationID: "o1", Kind: "transform", State: ObligationPending, ResponsiblePeer: "harness-1"}}
	env, err := SignAuthorizationDecision(d, priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAuthorizationDecision(env.COSEBytes, priv.Public().(ed25519.PublicKey)); err != nil {
		t.Fatal(err)
	}
	_, rogue, _ := ed25519.GenerateKey(nil)
	if _, err := DecodeAuthorizationDecision(env.COSEBytes, rogue.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("rogue decision signer accepted")
	}
}

func TestAggregateDecisionsDenyOverrides(t *testing.T) {
	valid := func(*AuthorizationDecisionBody) bool { return true }
	ob := func(id string) Obligation {
		return Obligation{ObligationID: id, Kind: "k", State: ObligationPending, ResponsiblePeer: "r"}
	}
	// Empty → DENY.
	if got := AggregateDecisions(nil, valid); got.Outcome != DecisionDeny {
		t.Fatal("no decisions must aggregate to DENY")
	}
	// Invalid required decision → DENY.
	allow := baseDecision(DecisionAllow)
	got := AggregateDecisions([]*AuthorizationDecisionBody{allow}, func(*AuthorizationDecisionBody) bool { return false })
	if got.Outcome != DecisionDeny {
		t.Fatal("invalid required decision must aggregate to DENY")
	}
	// Single ALLOW → ALLOW.
	got = AggregateDecisions([]*AuthorizationDecisionBody{allow}, valid)
	if got.Outcome != DecisionAllow {
		t.Fatalf("single ALLOW aggregated to %d", got.Outcome)
	}
	// DENY overrides ALLOW and AWO.
	a := baseDecision(DecisionAllow)
	a.DecisionID = "d-a"
	w := baseDecision(DecisionAllowWithObligations)
	w.DecisionID = "d-w"
	w.Obligations = []Obligation{ob("x")}
	dn := baseDecision(DecisionDeny)
	dn.DecisionID = "d-n"
	got = AggregateDecisions([]*AuthorizationDecisionBody{a, w, dn}, valid)
	if got.Outcome != DecisionDeny {
		t.Fatal("DENY must override")
	}
	// Two AWOs union obligations.
	w2 := baseDecision(DecisionAllowWithObligations)
	w2.DecisionID = "d-w2"
	w2.Obligations = []Obligation{ob("y")}
	got = AggregateDecisions([]*AuthorizationDecisionBody{w, w2}, valid)
	if got.Outcome != DecisionAllowWithObligations || len(got.Obligations) != 2 {
		t.Fatalf("union failed: %+v", got)
	}
	// Same obligation ID with different encodings → DECISION_CONFLICT → DENY.
	w3 := baseDecision(DecisionAllowWithObligations)
	w3.DecisionID = "d-w3"
	w3.Obligations = []Obligation{ob("x")}
	w3.Obligations[0].Kind = "different"
	got = AggregateDecisions([]*AuthorizationDecisionBody{w, w3}, valid)
	if got.Outcome != DecisionDeny {
		t.Fatal("conflicting obligation encodings must aggregate to DENY")
	}
}

func TestObligationLifecycle(t *testing.T) {
	d := baseDecision(DecisionAllowWithObligations)
	d.Obligations = []Obligation{
		{ObligationID: "pre", Kind: "approval", Phase: ObligationPreAction, State: ObligationPending, ResponsiblePeer: "harness-1"},
		{ObligationID: "post", Kind: "report", Phase: ObligationPostAction, State: ObligationPending, ResponsiblePeer: "relay-1"},
	}
	digest := Digest{9}

	ledger := NewObligationLedger()
	ledger.Register(d, digest)

	if ledger.PreActionSatisfied(d, digest) {
		t.Fatal("pending PRE_ACTION obligation must block the action")
	}

	upd := func(id string, state ObligationState, actor string, seq uint64) *ObligationUpdate {
		return &ObligationUpdate{
			Version: 1, DecisionDigest: digest, ObligationID: id, NewState: state,
			Actor: actor, EvidenceDigest: Digest{1}, UpdateSequence: seq, AtMs: time.Now().UnixMilli(),
		}
	}
	// Wrong actor.
	if err := ledger.Apply(upd("pre", ObligationSatisfied, "relay-1", 1)); err == nil {
		t.Fatal("non-responsible actor accepted")
	}
	// Happy path.
	if err := ledger.Apply(upd("pre", ObligationSatisfied, "harness-1", 1)); err != nil {
		t.Fatal(err)
	}
	if !ledger.PreActionSatisfied(d, digest) {
		t.Fatal("satisfied PRE_ACTION obligation must unblock")
	}
	// Sequence must strictly increase.
	if err := ledger.Apply(upd("pre", ObligationFailed, "harness-1", 1)); err == nil {
		t.Fatal("repeated sequence accepted")
	}
	// Terminal state frozen.
	if err := ledger.Apply(upd("pre", ObligationFailed, "harness-1", 2)); err == nil {
		t.Fatal("terminal SATISFIED transitioned")
	}
	// Unknown obligation.
	if err := ledger.Apply(upd("nope", ObligationSatisfied, "harness-1", 1)); err == nil {
		t.Fatal("unknown obligation accepted")
	}
	// Expired deadline flips PENDING to FAILED.
	ledger.Register(&AuthorizationDecisionBody{
		Outcome:     DecisionAllowWithObligations,
		Obligations: []Obligation{{ObligationID: "timed", State: ObligationPending, ResponsiblePeer: "r", DeadlineMs: 100}},
	}, Digest{7})
	key := obligationKey(Digest{7}, "timed")
	if expired := ledger.ExpirePending(200, map[string]int64{key: 100}); len(expired) != 1 {
		t.Fatal("expired deadline must fail the obligation")
	}
	if s, _ := ledger.State(Digest{7}, "timed"); s != ObligationFailed {
		t.Fatal("expired obligation not FAILED")
	}
}

func TestCheckpointLifecycle(t *testing.T) {
	priv := decisionKey(t)
	pub := priv.Public().(ed25519.PublicKey)
	content := []byte(`{"allowed":["m1"]}`)
	ck := func(seq uint64, prev *Digest) *SignedStateCheckpointBody {
		return &SignedStateCheckpointBody{
			Version: 1, CheckpointID: "ck-1", Issuer: "pccp-policy", TrustDomain: "org-1",
			StateClass: StateClassPolicyEpochs, Sequence: seq,
			IssuedAtMs:     time.Now().Add(-time.Second).UnixMilli(),
			ExpiresAtMs:    time.Now().Add(time.Hour).UnixMilli(),
			MaxStalenessMs: uint64(time.Hour.Milliseconds()),
			Content: StateContentRef{
				ContentType:   "policy-epochs",
				Kind:          StateContentSnapshot,
				ContentDigest: StateContentDigest("policy-epochs", StateContentSnapshot, content),
				Inline:        content,
			},
			PreviousCheckpoint: prev,
			Freshness:          FreshnessIntegrity,
		}
	}
	sign := func(b *SignedStateCheckpointBody) *CheckpointEnvelope {
		env, err := SignStateCheckpoint(b, priv)
		if err != nil {
			t.Fatal(err)
		}
		return env
	}
	now := time.Now().UnixMilli()

	// Inline content must hash to the declared digest.
	bad := ck(0, nil)
	bad.Content.Inline = []byte("tampered")
	if err := ValidateCheckpoint(bad, now); err == nil {
		t.Fatal("inline content/digest mismatch accepted")
	}

	// Valid first checkpoint.
	first := sign(ck(0, nil))
	if err := ValidateCheckpoint(first.Body, now); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStateCheckpoint(first.COSEBytes, pub); err != nil {
		t.Fatal(err)
	}

	ledger := NewCheckpointLedger()
	if err := ledger.Accept(first); err != nil {
		t.Fatal(err)
	}
	// Idempotent replay: the SAME envelope (identical bytes) is
	// accepted without state change.
	if err := ledger.Accept(first); err != nil {
		t.Fatalf("idempotent replay of the identical envelope rejected: %v", err)
	}
	// Equal sequence with MUTATED + re-signed bytes → fork.
	forkedZero := ck(0, nil)
	forkedZero.CheckpointID = "ck-fork"
	if err := ledger.Accept(sign(forkedZero)); err == nil {
		t.Fatal("forked sequence-0 body accepted")
	}
	// Higher sequence without predecessor → rollback.
	next := sign(ck(1, nil))
	if err := ledger.Accept(next); err == nil {
		t.Fatal("missing predecessor accepted")
	}
	// Correct chain accepted.
	next = sign(ck(1, &first.SignedDigest))
	if err := ledger.Accept(next); err != nil {
		t.Fatal(err)
	}
	// Idempotent replay of the same envelope.
	if err := ledger.Accept(next); err != nil {
		t.Fatal(err)
	}
	// Fork: same sequence, MUTATED + re-signed bytes.
	forkBody := ck(1, &first.SignedDigest)
	forkBody.CheckpointID = "ck-fork-2"
	if err := ledger.Accept(sign(forkBody)); err == nil {
		t.Fatal("forked sequence accepted")
	}
	// Rollback to a lower sequence.
	if err := ledger.Accept(first); err == nil {
		t.Fatal("rollback accepted")
	}
	// First checkpoint not at sequence 0.
	l2 := NewCheckpointLedger()
	if err := l2.Accept(sign(ck(5, nil))); err == nil {
		t.Fatal("first checkpoint with sequence > 0 accepted")
	}
	// Provisioned baseline accepts the designated digest at sequence N.
	l3 := NewCheckpointLedger()
	base := sign(ck(7, nil))
	l3.ProvisionBaseline("pccp-policy", "org-1", StateClassPolicyEpochs, base.SignedDigest, 7)
	if err := l3.Accept(base); err != nil {
		t.Fatal(err)
	}
	// Staleness exceeded.
	stale := ck(2, &next.SignedDigest)
	stale.IssuedAtMs = time.Now().Add(-2 * time.Hour).UnixMilli()
	stale.ExpiresAtMs = time.Now().Add(time.Hour).UnixMilli()
	if err := ValidateCheckpoint(stale, now); err == nil {
		t.Fatal("stale checkpoint accepted")
	}
	// Audience must be sorted without duplicates.
	aud := ck(3, &next.SignedDigest)
	aud.Audience = []string{"b", "a"}
	if err := ValidateCheckpoint(aud, now); err == nil {
		t.Fatal("unsorted audience accepted")
	}
	_ = reflect.DeepEqual
}
