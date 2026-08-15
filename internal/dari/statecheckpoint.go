package dari

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
)

// statecheckpoint.go implements the Signed State Checkpoint, its
// freshness/rollback ledger, and state-content resolution (spec
// Appendix F.7, master plan Task 9).

// StateClass is the F.7 state-class.
type StateClass uint8

const (
	StateClassRevocation     StateClass = 1
	StateClassIssuerKeys     StateClass = 2
	StateClassPolicyEpochs   StateClass = 3
	StateClassModelManifests StateClass = 4
	StateClassEndpoints      StateClass = 5
	StateClassExtension      StateClass = 255
)

// FreshnessClass is INTEGRITY / LOW_RISK_READONLY.
type FreshnessClass uint8

const (
	FreshnessIntegrity       FreshnessClass = 1
	FreshnessLowRiskReadonly FreshnessClass = 2
)

// StateContentKind is SNAPSHOT / DELTA.
type StateContentKind uint8

const (
	StateContentSnapshot StateContentKind = 1
	StateContentDelta    StateContentKind = 2
)

// StateContentRef is the F.7 state-content-ref.
type StateContentRef struct {
	ContentType   string           `cbor:"1,keyasint"`
	Kind          StateContentKind `cbor:"2,keyasint"`
	ContentDigest Digest           `cbor:"3,keyasint"`
	LocationURI   string           `cbor:"4,keyasint,omitempty"`
	Inline        []byte           `cbor:"5,keyasint,omitempty"`
}

// SignedStateCheckpointBody is the F.7 body (labels 1-13).
type SignedStateCheckpointBody struct {
	Version            uint16          `cbor:"1,keyasint"`
	CheckpointID       string          `cbor:"2,keyasint"`
	Issuer             string          `cbor:"3,keyasint"`
	TrustDomain        string          `cbor:"4,keyasint"`
	StateClass         StateClass      `cbor:"5,keyasint"`
	Sequence           uint64          `cbor:"6,keyasint"`
	IssuedAtMs         int64           `cbor:"7,keyasint"`
	ExpiresAtMs        int64           `cbor:"8,keyasint"`
	MaxStalenessMs     uint64          `cbor:"9,keyasint"`
	Content            StateContentRef `cbor:"10,keyasint"`
	PreviousCheckpoint *Digest         `cbor:"11,keyasint,omitempty"`
	Audience           []string        `cbor:"12,keyasint,omitempty"`
	Freshness          FreshnessClass  `cbor:"13,keyasint"`
}

// CheckpointAAD is the F.7 external AAD.
const CheckpointAAD = "DARI-SIGNED-STATE-CHECKPOINT-v1\x00"

// StateContentDigest computes the F.7 content digest:
// SHA-256("DARI-STATE-CONTENT-v1\0" || deterministic_cbor([type, kind, bytes])).
func StateContentDigest(contentType string, kind StateContentKind, content []byte) Digest {
	inner, _ := MarshalCBOR([]interface{}{contentType, uint8(kind), content})
	return KernelObjectDigestRaw("DARI-STATE-CONTENT-v1\x00", inner)
}

// KernelObjectDigestRaw is the shared domain-prefixed digest helper.
func KernelObjectDigestRaw(domain string, body []byte) Digest {
	var d Digest
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write(body)
	copy(d[:], h.Sum(nil))
	return d
}

// CheckpointEnvelope is a signed checkpoint with derived digests.
type CheckpointEnvelope struct {
	Body         *SignedStateCheckpointBody
	COSE         *COSESign1
	COSEBytes    []byte
	SignedDigest Digest // signed_object_digest(0x0305, cose)
}

// SignStateCheckpoint signs the canonical checkpoint body.
func SignStateCheckpoint(b *SignedStateCheckpointBody, priv ed25519.PrivateKey) (*CheckpointEnvelope, error) {
	if b == nil {
		return nil, errors.New("dari: nil checkpoint body")
	}
	body, err := MarshalCBOR(b)
	if err != nil {
		return nil, err
	}
	kid := SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey))
	sign1, err := CreateCOSESign1WithAAD(body, []byte(CheckpointAAD), priv, kid[:])
	if err != nil {
		return nil, err
	}
	coseBytes, err := MarshalCBOR(sign1)
	if err != nil {
		return nil, err
	}
	return &CheckpointEnvelope{
		Body: b, COSE: sign1, COSEBytes: coseBytes,
		SignedDigest: KernelSignedObjectDigest(ObjTypeSignedStateCheckpoint, coseBytes),
	}, nil
}

// DecodeStateCheckpoint verifies signature + canonical body.
func DecodeStateCheckpoint(coseBytes []byte, signer ed25519.PublicKey) (*CheckpointEnvelope, error) {
	var sign1 COSESign1
	if err := UnmarshalCBOR(coseBytes, &sign1); err != nil {
		return nil, fmt.Errorf("dari: decode checkpoint COSE: %w", err)
	}
	var body SignedStateCheckpointBody
	if err := UnmarshalCBOR(sign1.Payload, &body); err != nil {
		return nil, fmt.Errorf("dari: decode checkpoint body: %w", err)
	}
	reencoded, err := MarshalCBOR(&body)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(reencoded, sign1.Payload) {
		return nil, errors.New("dari: checkpoint body is not the canonical payload")
	}
	if err := VerifyCOSESign1WithAAD(&sign1, []byte(CheckpointAAD), reencoded, signer); err != nil {
		return nil, fmt.Errorf("dari: checkpoint signature: %w", err)
	}
	return &CheckpointEnvelope{
		Body: &body, COSE: &sign1, COSEBytes: coseBytes,
		SignedDigest: KernelSignedObjectDigest(ObjTypeSignedStateCheckpoint, coseBytes),
	}, nil
}

// ValidateCheckpoint enforces the F.7 body invariants: expires >
// issued; nonzero max-staleness; inline content hashes to the declared
// digest; freshness window at nowMs; audience sorted and duplicate-free.
func ValidateCheckpoint(b *SignedStateCheckpointBody, nowMs int64) error {
	if b.ExpiresAtMs <= b.IssuedAtMs {
		return errors.New("dari: checkpoint expires_at must exceed issued_at")
	}
	if b.MaxStalenessMs == 0 {
		return errors.New("dari: checkpoint maximum staleness must be nonzero")
	}
	if len(b.Content.Inline) > 0 {
		want := StateContentDigest(b.Content.ContentType, b.Content.Kind, b.Content.Inline)
		if want != b.Content.ContentDigest {
			return errors.New("dari: inline state content does not hash to the declared digest")
		}
	}
	// Audience sorted, no duplicates.
	for i := 1; i < len(b.Audience); i++ {
		if b.Audience[i-1] >= b.Audience[i] {
			return errors.New("dari: checkpoint audience must be sorted without duplicates")
		}
	}
	if nowMs != 0 {
		if nowMs < b.IssuedAtMs || nowMs >= b.ExpiresAtMs {
			return errors.New("dari: checkpoint outside its validity window")
		}
		if uint64(nowMs-b.IssuedAtMs) > b.MaxStalenessMs {
			return errors.New("dari: checkpoint exceeds maximum staleness")
		}
	}
	return nil
}

// CheckpointLedger is the durable high-water mark per (issuer, trust
// domain, state class) stream with atomic accept semantics.
type CheckpointLedger struct {
	mu        sync.Mutex
	highWater map[string]struct {
		sequence uint64
		digest   Digest
	}
	baseline map[string]bool // explicitly provisioned trust baselines
}

// NewCheckpointLedger builds an empty ledger.
func NewCheckpointLedger() *CheckpointLedger {
	return &CheckpointLedger{
		highWater: map[string]struct {
			sequence uint64
			digest   Digest
		}{},
		baseline: map[string]bool{},
	}
}

func streamKey(issuer, trustDomain string, class StateClass) string {
	return issuer + "|" + trustDomain + "|" + fmt.Sprint(uint8(class))
}

// ProvisionBaseline marks a digest as an explicitly provisioned trust
// baseline (the first acceptable checkpoint).
func (l *CheckpointLedger) ProvisionBaseline(issuer, trustDomain string, class StateClass, digest Digest, sequence uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.highWater[streamKey(issuer, trustDomain, class)] = struct {
		sequence uint64
		digest   Digest
	}{sequence, digest}
	l.baseline[streamKey(issuer, trustDomain, class)] = true
}

var ErrStateRollback = errors.New("STATE_ROLLBACK")

// Accept applies the F.7 sequence/predecessor rules atomically:
// first accepted checkpoint is a provisioned baseline or sequence 0
// with no predecessor; lower sequence = rollback; equal sequence is
// an idempotent replay only with an identical digest; a higher
// sequence MUST chain to the current high-water digest.
func (l *CheckpointLedger) Accept(env *CheckpointEnvelope) error {
	if env == nil || env.Body == nil {
		return errors.New("dari: nil checkpoint")
	}
	key := streamKey(env.Body.Issuer, env.Body.TrustDomain, env.Body.StateClass)
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, exists := l.highWater[key]
	if !exists {
		// First checkpoint: sequence 0 and no predecessor, unless
		// explicitly provisioned (handled by ProvisionBaseline).
		if env.Body.Sequence != 0 || env.Body.PreviousCheckpoint != nil {
			return fmt.Errorf("%w: first checkpoint must be sequence 0 without a predecessor", ErrStateRollback)
		}
		l.highWater[key] = struct {
			sequence uint64
			digest   Digest
		}{env.Body.Sequence, env.SignedDigest}
		return nil
	}
	switch {
	case env.Body.Sequence < cur.sequence:
		return fmt.Errorf("%w: sequence %d below high-water %d", ErrStateRollback, env.Body.Sequence, cur.sequence)
	case env.Body.Sequence == cur.sequence:
		if env.SignedDigest == cur.digest {
			return nil // idempotent replay
		}
		return fmt.Errorf("%w: sequence %d forked", ErrStateRollback, env.Body.Sequence)
	default:
		if env.Body.PreviousCheckpoint == nil || *env.Body.PreviousCheckpoint != cur.digest {
			return fmt.Errorf("%w: missing or wrong predecessor", ErrStateRollback)
		}
		l.highWater[key] = struct {
			sequence uint64
			digest   Digest
		}{env.Body.Sequence, env.SignedDigest}
		return nil
	}
}

// HighWater returns the current high-water mark for a stream.
func (l *CheckpointLedger) HighWater(issuer, trustDomain string, class StateClass) (uint64, Digest, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	hw, ok := l.highWater[streamKey(issuer, trustDomain, class)]
	return hw.sequence, hw.digest, ok
}
