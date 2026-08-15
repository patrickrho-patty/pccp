package dari

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"sort"
)

// decisions.go implements the Authorization Decision, obligation
// lifecycle, and deterministic aggregation (spec Appendix F.6, master
// plan Task 9).

// DecisionOutcome is the F.6 decision-outcome.
type DecisionOutcome uint8

const (
	DecisionAllow                DecisionOutcome = 1
	DecisionDeny                 DecisionOutcome = 2
	DecisionAllowWithObligations DecisionOutcome = 3
)

// ObligationPhase is PRE_ACTION / POST_ACTION.
type ObligationPhase uint8

const (
	ObligationPreAction  ObligationPhase = 1
	ObligationPostAction ObligationPhase = 2
)

// ObligationState is PENDING / SATISFIED / FAILED.
type ObligationState uint8

const (
	ObligationPending   ObligationState = 1
	ObligationSatisfied ObligationState = 2
	ObligationFailed    ObligationState = 3
)

// Obligation is the F.6 obligation object.
type Obligation struct {
	ObligationID    string          `cbor:"1,keyasint"`
	Kind            string          `cbor:"2,keyasint"`
	ParameterDigest Digest          `cbor:"3,keyasint"`
	Phase           ObligationPhase `cbor:"4,keyasint"`
	State           ObligationState `cbor:"5,keyasint"`
	ResponsiblePeer string          `cbor:"6,keyasint"`
	DeadlineMs      int64           `cbor:"7,keyasint,omitempty"`
	EvidenceDigest  Digest          `cbor:"8,keyasint,omitempty"`
}

// AuthorizationDecisionBody is the F.6 decision body (labels 1-14).
type AuthorizationDecisionBody struct {
	Version                uint16          `cbor:"1,keyasint"`
	DecisionID             string          `cbor:"2,keyasint"`
	ExchangeID             string          `cbor:"3,keyasint"`
	GovernedExchangeDigest Digest          `cbor:"4,keyasint"`
	ActionDigest           Digest          `cbor:"5,keyasint"`
	LeafGrantDigest        Digest          `cbor:"6,keyasint"`
	PolicyCheckpointDigest Digest          `cbor:"7,keyasint"`
	EvaluatorPeerID        string          `cbor:"8,keyasint"`
	Outcome                DecisionOutcome `cbor:"9,keyasint"`
	Obligations            []Obligation    `cbor:"10,keyasint"`
	ReasonCodes            []string        `cbor:"11,keyasint"`
	IssuedAtMs             int64           `cbor:"12,keyasint"`
	ExpiresAtMs            int64           `cbor:"13,keyasint"`
	SupportingEvidence     []Digest        `cbor:"14,keyasint,omitempty"`
}

// DecisionAAD is the F.6 external AAD.
const DecisionAAD = "DARI-AUTHORIZATION-DECISION-v1\x00"

// DecisionEnvelope is a signed decision with derived digests.
type DecisionEnvelope struct {
	Body         *AuthorizationDecisionBody
	COSE         *COSESign1
	COSEBytes    []byte
	SignedDigest Digest // signed_object_digest(0x0304, cose)
}

// EncodeDecisionBody returns the canonical encoding with sorted
// reason codes and obligations.
func EncodeDecisionBody(b *AuthorizationDecisionBody) ([]byte, error) {
	if b == nil {
		return nil, errors.New("dari: nil decision body")
	}
	b.ReasonCodes = sortedCopy(b.ReasonCodes)
	sort.SliceStable(b.Obligations, func(i, j int) bool {
		return b.Obligations[i].ObligationID < b.Obligations[j].ObligationID
	})
	return MarshalCBOR(b)
}

// SignAuthorizationDecision signs the canonical body.
func SignAuthorizationDecision(b *AuthorizationDecisionBody, priv ed25519.PrivateKey) (*DecisionEnvelope, error) {
	body, err := EncodeDecisionBody(b)
	if err != nil {
		return nil, err
	}
	kid := SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey))
	sign1, err := CreateCOSESign1WithAAD(body, []byte(DecisionAAD), priv, kid[:])
	if err != nil {
		return nil, err
	}
	coseBytes, err := MarshalCBOR(sign1)
	if err != nil {
		return nil, err
	}
	return &DecisionEnvelope{
		Body: b, COSE: sign1, COSEBytes: coseBytes,
		SignedDigest: KernelSignedObjectDigest(ObjTypeAuthorizationDecision, coseBytes),
	}, nil
}

// DecodeAuthorizationGrant-style verifier for decisions.
func DecodeAuthorizationDecision(coseBytes []byte, signer ed25519.PublicKey) (*DecisionEnvelope, error) {
	var sign1 COSESign1
	if err := UnmarshalCBOR(coseBytes, &sign1); err != nil {
		return nil, fmt.Errorf("dari: decode decision COSE: %w", err)
	}
	var body AuthorizationDecisionBody
	if err := UnmarshalCBOR(sign1.Payload, &body); err != nil {
		return nil, fmt.Errorf("dari: decode decision body: %w", err)
	}
	reencoded, err := EncodeDecisionBody(&body)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(reencoded, sign1.Payload) {
		return nil, errors.New("dari: decision body is not the canonical payload")
	}
	if err := VerifyCOSESign1WithAAD(&sign1, []byte(DecisionAAD), reencoded, signer); err != nil {
		return nil, fmt.Errorf("dari: decision signature: %w", err)
	}
	return &DecisionEnvelope{
		Body: &body, COSE: &sign1, COSEBytes: coseBytes,
		SignedDigest: KernelSignedObjectDigest(ObjTypeAuthorizationDecision, coseBytes),
	}, nil
}

// ValidateDecision enforces the F.6 body invariants: expires > issued;
// DENY and ALLOW carry no obligations; ALLOW_WITH_OBLIGATIONS carries
// at least one PENDING obligation (a pre-satisfied one MUST carry its
// bound evidence digest); time window at nowMs.
func ValidateDecision(b *AuthorizationDecisionBody, nowMs int64) error {
	if b.ExpiresAtMs <= b.IssuedAtMs {
		return errors.New("dari: decision expires_at must exceed issued_at")
	}
	switch b.Outcome {
	case DecisionAllow, DecisionDeny:
		if len(b.Obligations) != 0 {
			return errors.New("dari: ALLOW/DENY decision must carry no obligations")
		}
	case DecisionAllowWithObligations:
		if len(b.Obligations) == 0 {
			return errors.New("dari: ALLOW_WITH_OBLIGATIONS must carry at least one obligation")
		}
		for _, o := range b.Obligations {
			if o.State == ObligationPending {
				continue
			}
			// A non-PENDING freshly-issued obligation must prove a
			// satisfaction bound into the decision.
			if o.State == ObligationSatisfied && o.EvidenceDigest == (Digest{}) {
				return errors.New("dari: satisfied obligation lacks bound evidence digest")
			}
			if o.State == ObligationFailed {
				return errors.New("dari: freshly issued obligation may not be FAILED")
			}
		}
	default:
		return errors.New("dari: unknown decision outcome")
	}
	if nowMs != 0 && (nowMs < b.IssuedAtMs || nowMs >= b.ExpiresAtMs) {
		return errors.New("dari: decision outside its validity window")
	}
	return nil
}

// AggregatedDecision is the deterministic aggregate of required
// decisions (F.6 aggregation algorithm).
type AggregatedDecision struct {
	Outcome     DecisionOutcome
	Obligations []Obligation
}

// AggregateDecisions applies the F.6 rules: missing/invalid/stale/
// expired required decision → DENY; any valid DENY overrides; union
// obligations by ID (different encodings for one ID = DECISION_CONFLICT
// → DENY); non-empty union → ALLOW_WITH_OBLIGATIONS.
func AggregateDecisions(decisions []*AuthorizationDecisionBody, valid func(*AuthorizationDecisionBody) bool) AggregatedDecision {
	if len(decisions) == 0 {
		return AggregatedDecision{Outcome: DecisionDeny}
	}
	union := map[string]Obligation{}
	sawAllow, sawDeny := false, false
	for _, d := range decisions {
		if d == nil || (valid != nil && !valid(d)) {
			return AggregatedDecision{Outcome: DecisionDeny}
		}
		if d.Outcome == DecisionDeny {
			sawDeny = true
			continue
		}
		sawAllow = true
		for _, o := range d.Obligations {
			prev, exists := union[o.ObligationID]
			if exists && prev != o {
				// Two encodings for one ID: DECISION_CONFLICT → DENY.
				return AggregatedDecision{Outcome: DecisionDeny}
			}
			union[o.ObligationID] = o
		}
	}
	_ = sawAllow
	if sawDeny {
		return AggregatedDecision{Outcome: DecisionDeny}
	}
	if len(union) == 0 {
		return AggregatedDecision{Outcome: DecisionAllow}
	}
	out := make([]Obligation, 0, len(union))
	for _, o := range union {
		out = append(out, o)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ObligationID < out[j].ObligationID })
	return AggregatedDecision{Outcome: DecisionAllowWithObligations, Obligations: out}
}

// ---------------------------------------------------------------------------
// Obligation state machine (append-only, keyed by decision digest +
// obligation ID).
// ---------------------------------------------------------------------------

// ObligationUpdate is the F.6 obligation-update body (labels 1-8).
type ObligationUpdate struct {
	Version        uint16          `cbor:"1,keyasint"`
	DecisionDigest Digest          `cbor:"2,keyasint"`
	ObligationID   string          `cbor:"3,keyasint"`
	NewState       ObligationState `cbor:"4,keyasint"`
	Actor          string          `cbor:"5,keyasint"`
	EvidenceDigest Digest          `cbor:"6,keyasint"`
	UpdateSequence uint64          `cbor:"7,keyasint"`
	AtMs           int64           `cbor:"8,keyasint"`
}

// ObligationUpdateAAD is the F.6 update AAD.
const ObligationUpdateAAD = "DARI-OBLIGATION-UPDATE-v1\x00"

// ObligationLedger is the append-only obligation state machine.
type ObligationLedger struct {
	// state[(decisionDigest, obligationID)] → current state.
	state map[string]ObligationState
	// lastSeq[(decisionDigest, obligationID)] → highest update seq.
	lastSeq map[string]uint64
	// decisions by digest for responsible-peer checks.
	responsible map[string]map[string]string
}

// NewObligationLedger builds an empty ledger.
func NewObligationLedger() *ObligationLedger {
	return &ObligationLedger{
		state:       map[string]ObligationState{},
		lastSeq:     map[string]uint64{},
		responsible: map[string]map[string]string{},
	}
}

func obligationKey(digest Digest, obligationID string) string {
	return string(digest[:]) + "|" + obligationID
}

// Register admits a decision's obligations into the ledger.
func (l *ObligationLedger) Register(decision *AuthorizationDecisionBody, decisionDigest Digest) {
	if decision == nil {
		return
	}
	if l.responsible[string(decisionDigest[:])] == nil {
		l.responsible[string(decisionDigest[:])] = map[string]string{}
	}
	for _, o := range decision.Obligations {
		key := obligationKey(decisionDigest, o.ObligationID)
		if _, exists := l.state[key]; !exists {
			l.state[key] = o.State
		}
		l.responsible[string(decisionDigest[:])][o.ObligationID] = o.ResponsiblePeer
	}
}

// Apply validates and applies a signed obligation-update per F.6:
// strictly increasing sequence per key; only PENDING→SATISFIED and
// PENDING→FAILED; terminal states frozen; actor must equal the
// responsible peer; evidence digest must be present.
func (l *ObligationLedger) Apply(u *ObligationUpdate) error {
	if u == nil {
		return errors.New("dari: nil obligation update")
	}
	key := obligationKey(u.DecisionDigest, u.ObligationID)
	current, exists := l.state[key]
	if !exists {
		return errors.New("dari: unknown obligation")
	}
	if u.Actor == "" || u.EvidenceDigest == (Digest{}) {
		return errors.New("dari: update requires actor and evidence digest")
	}
	if resp, ok := l.responsible[string(u.DecisionDigest[:])][u.ObligationID]; ok && resp != u.Actor {
		return fmt.Errorf("dari: actor %q is not the responsible peer %q", u.Actor, resp)
	}
	if u.UpdateSequence <= l.lastSeq[key] {
		return fmt.Errorf("dari: update sequence %d not greater than %d", u.UpdateSequence, l.lastSeq[key])
	}
	switch {
	case current == ObligationPending && (u.NewState == ObligationSatisfied || u.NewState == ObligationFailed):
		l.state[key] = u.NewState
		l.lastSeq[key] = u.UpdateSequence
		return nil
	default:
		return fmt.Errorf("dari: illegal transition %d -> %d", current, u.NewState)
	}
}

// State returns the current state of an obligation.
func (l *ObligationLedger) State(decisionDigest Digest, obligationID string) (ObligationState, bool) {
	s, ok := l.state[obligationKey(decisionDigest, obligationID)]
	return s, ok
}

// ExpirePending fails every pending obligation whose deadline passed —
// F.6: an expired deadline changes PENDING to FAILED. Returns the IDs
// transitioned.
func (l *ObligationLedger) ExpirePending(nowMs int64, deadlines map[string]int64) []string {
	var expired []string
	for key, state := range l.state {
		if state != ObligationPending {
			continue
		}
		if dl, ok := deadlines[key]; ok && nowMs >= dl {
			l.state[key] = ObligationFailed
			expired = append(expired, key)
		}
	}
	sort.Strings(expired)
	return expired
}

// PreActionSatisfied reports whether every PRE_ACTION obligation of a
// decision is SATISFIED (required before a protected action starts).
func (l *ObligationLedger) PreActionSatisfied(decision *AuthorizationDecisionBody, decisionDigest Digest) bool {
	for _, o := range decision.Obligations {
		if o.Phase != ObligationPreAction {
			continue
		}
		if s, ok := l.State(decisionDigest, o.ObligationID); !ok || s != ObligationSatisfied {
			return false
		}
	}
	return true
}
