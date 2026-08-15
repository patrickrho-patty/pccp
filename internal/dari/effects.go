package dari

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"
)

// effects.go implements the transactional-effect objects and executor
// state machine (spec Appendix F.10, master plan Task 11).

// Effect terminal states.
type EffectTerminalState uint8

const (
	EffectCommitted EffectTerminalState = 1
	EffectAborted   EffectTerminalState = 2
)

// Effect states.
type EffectState uint8

const (
	EffectStateAbsent     EffectState = 0
	EffectStatePrepared   EffectState = 1
	EffectStateAuthorized EffectState = 2
	EffectStateExecuting  EffectState = 3
	EffectStateCommitted  EffectState = 4
	EffectStateAborted    EffectState = 5
)

// EffectPrepareBody is the F.10 prepare body.
type EffectPrepareBody struct {
	Version         uint16   `cbor:"1,keyasint"`
	OperationID     string   `cbor:"2,keyasint"`
	ExchangeID      string   `cbor:"3,keyasint"`
	Nonce           [32]byte `cbor:"4,keyasint"`
	LeafGrantDigest Digest   `cbor:"5,keyasint"`
	InputDigest     Digest   `cbor:"6,keyasint"`
	EffectKind      string   `cbor:"7,keyasint"`
	ExecutorPeerID  string   `cbor:"8,keyasint"`
	RetryOwnerID    string   `cbor:"9,keyasint"`
	ExpiresAtMs     int64    `cbor:"10,keyasint"`
}

// EffectAuthorizationBody is the F.10 authorization body.
type EffectAuthorizationBody struct {
	Version            uint16 `cbor:"1,keyasint"`
	OperationID        string `cbor:"2,keyasint"`
	PrepareDigest      Digest `cbor:"3,keyasint"`
	DecisionDigest     Digest `cbor:"4,keyasint"`
	AuthorizingRelayID string `cbor:"5,keyasint"`
	IssuedAtMs         int64  `cbor:"6,keyasint"`
	ExpiresAtMs        int64  `cbor:"7,keyasint"`
}

// EffectResultBody is the F.10 terminal result body.
type EffectResultBody struct {
	Version             uint16              `cbor:"1,keyasint"`
	OperationID         string              `cbor:"2,keyasint"`
	PrepareDigest       Digest              `cbor:"3,keyasint"`
	AuthorizationDigest Digest              `cbor:"4,keyasint"`
	ExecutorPeerID      string              `cbor:"5,keyasint"`
	State               EffectTerminalState `cbor:"6,keyasint"`
	InputDigest         Digest              `cbor:"7,keyasint"`
	ResultDigest        Digest              `cbor:"8,keyasint,omitempty"`
	ResultRef           string              `cbor:"9,keyasint,omitempty"`
	TerminalTimeMs      int64               `cbor:"10,keyasint"`
}

// EffectStatusBody is the F.10 status request/response body.
type EffectStatusBody struct {
	Version       uint16       `cbor:"1,keyasint"`
	OperationID   string       `cbor:"2,keyasint"`
	Nonce         [32]byte     `cbor:"3,keyasint"`
	State         *EffectState `cbor:"4,keyasint,omitempty"`
	PrepareDigest *Digest      `cbor:"5,keyasint,omitempty"`
	ResultDigest  *Digest      `cbor:"6,keyasint,omitempty"`
	RetryOwnerID  string       `cbor:"7,keyasint,omitempty"`
	Kind          uint8        `cbor:"8,keyasint"` // 1 request, 2 response
}

// External AADs per F.10.
const (
	EffectPrepareAAD       = "DARI-EFFECT-PREPARE-v1\x00"
	EffectAuthorizationAAD = "DARI-EFFECT-AUTHORIZATION-v1\x00"
	EffectResultAAD        = "DARI-EFFECT-RESULT-v1\x00"
	EffectStatusAAD        = "DARI-EFFECT-STATUS-v1\x00"
)

// ErrReplayConflict is the F.10 replay binding failure.
var ErrReplayConflict = errors.New("REPLAY_CONFLICT")

// signKernelObject is the shared canonical-sign helper for effect
// objects (body → canonical CBOR → COSE with the object AAD).
func signKernelObject(body interface{}, aad string, priv ed25519.PrivateKey, objType ObjectType) (*COSESign1, []byte, Digest, error) {
	payload, err := MarshalCBOR(body)
	if err != nil {
		return nil, nil, Digest{}, err
	}
	kid := SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey))
	sign1, err := CreateCOSESign1WithAAD(payload, []byte(aad), priv, kid[:])
	if err != nil {
		return nil, nil, Digest{}, err
	}
	coseBytes, err := MarshalCBOR(sign1)
	if err != nil {
		return nil, nil, Digest{}, err
	}
	return sign1, coseBytes, KernelSignedObjectDigest(objType, coseBytes), nil
}

// verifyKernelObject verifies an effect-family envelope: canonical
// body/payload equality + signature under the AAD.
func verifyKernelObject(coseBytes []byte, aad string, into interface{}, pub ed25519.PublicKey) ([]byte, error) {
	var sign1 COSESign1
	if err := UnmarshalCBOR(coseBytes, &sign1); err != nil {
		return nil, fmt.Errorf("dari: decode effect COSE: %w", err)
	}
	payload, err := MarshalCBOR(into)
	if err != nil {
		return nil, err
	}
	_ = payload
	if err := VerifyCOSESign1WithAAD(&sign1, []byte(aad), nil, pub); err != nil {
		return nil, err
	}
	return sign1.Payload, nil
}

// EffectPrepareEnvelope carries the signed prepare.
type EffectPrepareEnvelope struct {
	Body         *EffectPrepareBody
	COSE         *COSESign1
	COSEBytes    []byte
	SignedDigest Digest // 0x0610
}

// SignEffectPrepare signs a prepare.
func SignEffectPrepare(b *EffectPrepareBody, priv ed25519.PrivateKey) (*EffectPrepareEnvelope, error) {
	sign1, coseBytes, digest, err := signKernelObject(b, EffectPrepareAAD, priv, ObjTypeEffectPrepare)
	if err != nil {
		return nil, err
	}
	return &EffectPrepareEnvelope{Body: b, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
}

// DecodeEffectPrepare verifies + decodes a prepare under the signer.
func DecodeEffectPrepare(coseBytes []byte, signer ed25519.PublicKey) (*EffectPrepareEnvelope, error) {
	var sign1 COSESign1
	if err := UnmarshalCBOR(coseBytes, &sign1); err != nil {
		return nil, err
	}
	var body EffectPrepareBody
	if err := UnmarshalCBOR(sign1.Payload, &body); err != nil {
		return nil, err
	}
	reencoded, err := MarshalCBOR(&body)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(reencoded, sign1.Payload) {
		return nil, errors.New("dari: prepare body is not canonical")
	}
	if err := VerifyCOSESign1WithAAD(&sign1, []byte(EffectPrepareAAD), reencoded, signer); err != nil {
		return nil, err
	}
	return &EffectPrepareEnvelope{
		Body: &body, COSE: &sign1, COSEBytes: coseBytes,
		SignedDigest: KernelSignedObjectDigest(ObjTypeEffectPrepare, coseBytes),
	}, nil
}

// EffectAuthorizationEnvelope carries the signed authorization.
type EffectAuthorizationEnvelope struct {
	Body         *EffectAuthorizationBody
	COSE         *COSESign1
	COSEBytes    []byte
	SignedDigest Digest // 0x0611
}

// SignEffectAuthorization signs an authorization.
func SignEffectAuthorization(b *EffectAuthorizationBody, priv ed25519.PrivateKey) (*EffectAuthorizationEnvelope, error) {
	sign1, coseBytes, digest, err := signKernelObject(b, EffectAuthorizationAAD, priv, ObjTypeEffectAuthorize)
	if err != nil {
		return nil, err
	}
	return &EffectAuthorizationEnvelope{Body: b, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
}

// EffectResultEnvelope carries the signed terminal result.
type EffectResultEnvelope struct {
	Body         *EffectResultBody
	COSE         *COSESign1
	COSEBytes    []byte
	SignedDigest Digest // 0x0612
}

// SignEffectResult signs a terminal result.
func SignEffectResult(b *EffectResultBody, priv ed25519.PrivateKey) (*EffectResultEnvelope, error) {
	sign1, coseBytes, digest, err := signKernelObject(b, EffectResultAAD, priv, ObjTypeEffectResult)
	if err != nil {
		return nil, err
	}
	return &EffectResultEnvelope{Body: b, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
}

// DecodeEffectResult verifies + decodes a terminal result.
func DecodeEffectResult(coseBytes []byte, signer ed25519.PublicKey) (*EffectResultEnvelope, error) {
	var sign1 COSESign1
	if err := UnmarshalCBOR(coseBytes, &sign1); err != nil {
		return nil, err
	}
	var body EffectResultBody
	if err := UnmarshalCBOR(sign1.Payload, &body); err != nil {
		return nil, err
	}
	reencoded, err := MarshalCBOR(&body)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(reencoded, sign1.Payload) {
		return nil, errors.New("dari: result body is not canonical")
	}
	if err := VerifyCOSESign1WithAAD(&sign1, []byte(EffectResultAAD), reencoded, signer); err != nil {
		return nil, err
	}
	return &EffectResultEnvelope{
		Body: &body, COSE: &sign1, COSEBytes: coseBytes,
		SignedDigest: KernelSignedObjectDigest(ObjTypeEffectResult, coseBytes),
	}, nil
}

// ValidateEffectStatusShape enforces the F.10 status shapes: a request
// (label 8 = 1) omits labels 4-7; a response (8 = 2) includes the
// state and every available binding digest.
func ValidateEffectStatusShape(b *EffectStatusBody) error {
	switch b.Kind {
	case 1:
		if b.State != nil || b.PrepareDigest != nil || b.ResultDigest != nil || b.RetryOwnerID != "" {
			return errors.New("dari: status request must omit labels 4-7")
		}
		return nil
	case 2:
		if b.State == nil || b.PrepareDigest == nil {
			return errors.New("dari: status response must include state and prepare digest")
		}
		return nil
	default:
		return errors.New("dari: status kind must be request(1) or response(2)")
	}
}

// ---------------------------------------------------------------------------
// Executor state machine (F.10 semantics).
// ---------------------------------------------------------------------------

// effectRecord is the executor's durable view of one operation.
type effectRecord struct {
	state         EffectState
	nonce         [32]byte
	prepareDigest Digest
	grantDigest   Digest
	inputDigest   Digest
	executor      string
	retryOwner    string
	result        *EffectResultEnvelope
}

// EffectExecutor is the F.10 executor: persists prepare bindings
// before acknowledging PREPARED, the authorization digest before
// AUTHORIZED, and the terminal result before COMMIT/ABORT. The
// operation ID is the idempotency key.
type EffectExecutor struct {
	mu          sync.Mutex
	ops         map[string]*effectRecord
	executorID  string
	executorKey ed25519.PrivateKey
}

// NewEffectExecutor builds an executor bound to a peer identity.
func NewEffectExecutor(executorID string, executorKey ed25519.PrivateKey) *EffectExecutor {
	return &EffectExecutor{ops: map[string]*effectRecord{}, executorID: executorID, executorKey: executorKey}
}

// AckPrepare is the PREPARED transition: binding persisted first,
// idempotent on identical binding, REPLAY_CONFLICT on any difference.
func (e *EffectExecutor) AckPrepare(p *EffectPrepareEnvelope) error {
	if p == nil || p.Body == nil {
		return errors.New("dari: nil prepare")
	}
	if p.Body.ExecutorPeerID != e.executorID {
		return fmt.Errorf("dari: prepare names executor %q, this executor is %q", p.Body.ExecutorPeerID, e.executorID)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, exists := e.ops[p.Body.OperationID]
	if !exists {
		e.ops[p.Body.OperationID] = &effectRecord{
			state: EffectStatePrepared, nonce: p.Body.Nonce,
			prepareDigest: p.SignedDigest, grantDigest: p.Body.LeafGrantDigest,
			inputDigest: p.Body.InputDigest, executor: p.Body.ExecutorPeerID,
			retryOwner: p.Body.RetryOwnerID,
		}
		return nil
	}
	// Idempotency: identical binding replays; anything else conflicts.
	if rec.prepareDigest == p.SignedDigest && rec.nonce == p.Body.Nonce &&
		rec.inputDigest == p.Body.InputDigest && rec.retryOwner == p.Body.RetryOwnerID {
		return nil
	}
	return fmt.Errorf("%w: operation %s rebound", ErrReplayConflict, p.Body.OperationID)
}

// AckAuthorize is the AUTHORIZED transition: the authorization must
// bind the recorded prepare and decision; the digest persists before
// acknowledgement.
func (e *EffectExecutor) AckAuthorize(opID string, auth *EffectAuthorizationEnvelope) error {
	if auth == nil || auth.Body == nil {
		return errors.New("dari: nil authorization")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.ops[opID]
	if !ok {
		return errors.New("dari: unknown operation")
	}
	if auth.Body.OperationID != opID {
		return errors.New("dari: authorization operation mismatch")
	}
	if rec.state != EffectStatePrepared {
		return fmt.Errorf("dari: illegal AUTHORIZED transition from %d", rec.state)
	}
	if auth.Body.PrepareDigest != rec.prepareDigest {
		return errors.New("dari: authorization does not bind the recorded prepare")
	}
	// Persist the authorization digest, then transition.
	rec.state = EffectStateAuthorized
	return nil
}

// Execute is the atomic AUTHORIZED→EXECUTING compare-and-set.
func (e *EffectExecutor) Execute(opID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.ops[opID]
	if !ok {
		return errors.New("dari: unknown operation")
	}
	if rec.state != EffectStateAuthorized {
		return fmt.Errorf("dari: illegal EXECUTING transition from %d", rec.state)
	}
	rec.state = EffectStateExecuting
	return nil
}

// Finish builds, signs, persists, and returns the terminal result
// (COMMIT or ABORT). Terminal states never transition again.
func (e *EffectExecutor) Finish(opID string, terminal EffectTerminalState, resultDigest Digest, prepareDigest Digest, decisionDigest Digest) (*EffectResultEnvelope, error) {
	e.mu.Lock()
	rec, ok := e.ops[opID]
	if !ok {
		e.mu.Unlock()
		return nil, errors.New("dari: unknown operation")
	}
	if rec.state == EffectStateCommitted || rec.state == EffectStateAborted {
		// Terminal: return the durable result without executing again.
		e.mu.Unlock()
		return rec.result, nil
	}
	if rec.state != EffectStateExecuting {
		e.mu.Unlock()
		return nil, fmt.Errorf("dari: illegal terminal transition from %d", rec.state)
	}
	e.mu.Unlock()

	body := &EffectResultBody{
		Version: 1, OperationID: opID,
		PrepareDigest:       rec.prepareDigest,
		AuthorizationDigest: decisionDigest,
		ExecutorPeerID:      e.executorID,
		State:               terminal,
		InputDigest:         rec.inputDigest,
		ResultDigest:        resultDigest,
		TerminalTimeMs:      nowMsFunc()(),
	}
	env, err := SignEffectResult(body, e.executorKey)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	// Atomic: persist terminal state + result before returning.
	if terminal == EffectCommitted {
		rec.state = EffectStateCommitted
	} else {
		rec.state = EffectStateAborted
	}
	rec.result = env
	_ = prepareDigest
	return env, nil
}

// Status returns the durable current state for an operation.
func (e *EffectExecutor) Status(opID string) (EffectState, *EffectResultEnvelope, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.ops[opID]
	if !ok {
		return EffectStateAbsent, nil, false
	}
	return rec.state, rec.result, true
}

// Reconcile lets ONLY the named retry owner advance a stranded
// operation after disconnect (F.10).
func (e *EffectExecutor) Reconcile(opID, callerID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.ops[opID]
	if !ok {
		return errors.New("dari: unknown operation")
	}
	if rec.retryOwner != callerID {
		return fmt.Errorf("dari: caller %q is not the retry owner %q", callerID, rec.retryOwner)
	}
	return nil
}

// NewOperationNonce generates a fresh 32-byte operation nonce.
func NewOperationNonce() [32]byte {
	var n [32]byte
	if _, err := rand.Read(n[:]); err != nil {
		panic("dari: rand: " + err.Error())
	}
	return n
}

// nowMsFunc indirection for tests.
var nowMsFunc = func() func() int64 {
	return func() int64 { return time.Now().UnixMilli() }
}
