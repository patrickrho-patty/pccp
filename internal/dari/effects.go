package dari

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
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

// EffectPrepareEnvelope carries the signed prepare.
type EffectPrepareEnvelope struct {
	Body         *EffectPrepareBody
	COSE         *COSESign1
	COSEBytes    []byte
	SignedDigest Digest // 0x0610
}

// SignEffectPrepare signs a prepare.
func SignEffectPrepare(b *EffectPrepareBody, priv ed25519.PrivateKey) (*EffectPrepareEnvelope, error) {
	sign1, coseBytes, digest, err := SignKernelObject(b, EffectPrepareAAD, priv, ObjTypeEffectPrepare)
	if err != nil {
		return nil, err
	}
	return &EffectPrepareEnvelope{Body: b, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
}

// DecodeEffectPrepare verifies + decodes a prepare under the signer.
func DecodeEffectPrepare(coseBytes []byte, signer ed25519.PublicKey) (*EffectPrepareEnvelope, error) {
	body, sign1, digest, err := DecodeKernelObject(
		coseBytes, EffectPrepareAAD,
		func(b *EffectPrepareBody) ([]byte, error) { return MarshalCBOR(b) }, signer)
	if err != nil {
		return nil, err
	}
	return &EffectPrepareEnvelope{Body: body, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
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
	sign1, coseBytes, digest, err := SignKernelObject(b, EffectAuthorizationAAD, priv, ObjTypeEffectAuthorize)
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
	sign1, coseBytes, digest, err := SignKernelObject(b, EffectResultAAD, priv, ObjTypeEffectResult)
	if err != nil {
		return nil, err
	}
	return &EffectResultEnvelope{Body: b, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
}

// DecodeEffectResult verifies + decodes a terminal result.
func DecodeEffectResult(coseBytes []byte, signer ed25519.PublicKey) (*EffectResultEnvelope, error) {
	body, sign1, digest, err := DecodeKernelObject(
		coseBytes, EffectResultAAD,
		func(b *EffectResultBody) ([]byte, error) { return MarshalCBOR(b) }, signer)
	if err != nil {
		return nil, err
	}
	return &EffectResultEnvelope{Body: body, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
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
	authDigest    Digest
	executor      string
	retryOwner    string
	result        *EffectResultEnvelope
}

// EffectExecutor is the F.10 executor: persists prepare bindings
// before acknowledging PREPARED, the authorization digest before
// AUTHORIZED, and the terminal result before COMMIT/ABORT. The
// operation ID is the idempotency key.
// EffectStore persists effect records so EFFECT_STATUS survives
// executor restarts (F.10 durability). Nil store = memory-only (tests).
type EffectStore interface {
	SaveEffect(opID string, rec *EffectRecordRow) error
	LoadEffect(opID string) (*EffectRecordRow, error)
}

// EffectRecordRow is the persistable effect record (effectRecord
// exported for storage).
type EffectRecordRow struct {
	State         EffectState
	Nonce         [32]byte
	PrepareDigest Digest
	GrantDigest   Digest
	InputDigest   Digest
	AuthDigest    Digest
	Executor      string
	RetryOwner    string
	ResultCOSE    []byte // nil until COMMIT
}

type EffectExecutor struct {
	mu          sync.Mutex
	ops         map[string]*effectRecord
	executorID  string
	executorKey ed25519.PrivateKey
	store       EffectStore
}

// NewEffectExecutor builds an executor bound to a peer identity.
func NewEffectExecutor(executorID string, executorKey ed25519.PrivateKey) *EffectExecutor {
	return &EffectExecutor{ops: map[string]*effectRecord{}, executorID: executorID, executorKey: executorKey}
}

// NewDurableEffectExecutor builds an executor with a persistence store:
// every state transition is written through, and lookups fall back to
// the store for operations predating this process (F.10: a restart
// answers EFFECT_STATUS from history instead of ABSENT).
func NewDurableEffectExecutor(executorID string, executorKey ed25519.PrivateKey, store EffectStore) *EffectExecutor {
	e := NewEffectExecutor(executorID, executorKey)
	e.store = store
	return e
}

// persistLocked writes one record through (caller holds mu).
func (e *EffectExecutor) persistLocked(opID string, rec *effectRecord) {
	if e.store == nil || rec == nil {
		return
	}
	row := &EffectRecordRow{
		State: rec.state, Nonce: rec.nonce,
		PrepareDigest: rec.prepareDigest, GrantDigest: rec.grantDigest,
		InputDigest: rec.inputDigest, AuthDigest: rec.authDigest,
		Executor: rec.executor, RetryOwner: rec.retryOwner,
	}
	if rec.result != nil {
		row.ResultCOSE = rec.result.COSEBytes
	}
	if err := e.store.SaveEffect(opID, row); err != nil {
		// Memory remains authoritative for this process, but after a
		// restart the store is the ONLY source — a failed persist is
		// lost F.10 state and must be visible to the operator.
		log.Printf("dari: effect %s persist FAILED (state lost on restart): %v", opID, err)
	}
}

// loadFromStore materializes a pre-restart record (caller holds mu).
func (e *EffectExecutor) loadFromStore(opID string) *effectRecord {
	if e.store == nil {
		return nil
	}
	row, err := e.store.LoadEffect(opID)
	if err != nil || row == nil {
		return nil
	}
	rec := &effectRecord{
		state: row.State, nonce: row.Nonce,
		prepareDigest: row.PrepareDigest, grantDigest: row.GrantDigest,
		inputDigest: row.InputDigest, authDigest: row.AuthDigest,
		executor: row.Executor, retryOwner: row.RetryOwner,
	}
	e.ops[opID] = rec
	return rec
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
		if prior := e.loadFromStore(p.Body.OperationID); prior != nil {
			// Cross-restart replay: fall THROUGH to the binding check —
			// a retried prepare with a different binding must conflict,
			// exactly like the in-memory path.
			e.ops[p.Body.OperationID] = prior
			rec = prior
			exists = true
		} else {
			rec = &effectRecord{
				state: EffectStatePrepared, nonce: p.Body.Nonce,
				prepareDigest: p.SignedDigest, grantDigest: p.Body.LeafGrantDigest,
				inputDigest: p.Body.InputDigest, executor: p.Body.ExecutorPeerID,
				retryOwner: p.Body.RetryOwnerID,
			}
			e.ops[p.Body.OperationID] = rec
			e.persistLocked(p.Body.OperationID, rec)
			return nil
		}
	}
	// Idempotency: identical binding replays; anything else conflicts.
	if rec.prepareDigest == p.SignedDigest && rec.nonce == p.Body.Nonce &&
		rec.inputDigest == p.Body.InputDigest && rec.retryOwner == p.Body.RetryOwnerID {
		return nil
	}
	return fmt.Errorf("%w: operation %s rebound", ErrReplayConflict, p.Body.OperationID)
}

// AckAuthorize is the AUTHORIZED transition: the authorization must
// bind the recorded prepare; its signed-object digest is persisted
// BEFORE the state acknowledges AUTHORIZED.
func (e *EffectExecutor) AckAuthorize(opID string, auth *EffectAuthorizationEnvelope) error {
	if auth == nil || auth.Body == nil {
		return errors.New("dari: nil authorization")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.ops[opID]
	if !ok {
		rec = e.loadFromStore(opID)
		ok = rec != nil
	}
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
	rec.authDigest = auth.SignedDigest
	rec.state = EffectStateAuthorized
	e.persistLocked(opID, rec)
	return nil
}

// Execute is the atomic AUTHORIZED→EXECUTING compare-and-set.
func (e *EffectExecutor) Execute(opID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.ops[opID]
	if !ok {
		rec = e.loadFromStore(opID)
		ok = rec != nil
	}
	if !ok {
		return errors.New("dari: unknown operation")
	}
	if rec.state != EffectStateAuthorized {
		return fmt.Errorf("dari: illegal EXECUTING transition from %d", rec.state)
	}
	rec.state = EffectStateExecuting
	e.persistLocked(opID, rec)
	return nil
}

// Finish builds, signs, persists, and returns the terminal result
// (COMMIT or ABORT). Terminal states never transition again.
func (e *EffectExecutor) Finish(opID string, terminal EffectTerminalState, resultDigest Digest, prepareDigest Digest, decisionDigest Digest) (*EffectResultEnvelope, error) {
	e.mu.Lock()
	rec, ok := e.ops[opID]
	if !ok {
		rec = e.loadFromStore(opID)
		ok = rec != nil
	}
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
		AuthorizationDigest: rec.authDigest,
		ExecutorPeerID:      e.executorID,
		State:               terminal,
		InputDigest:         rec.inputDigest,
		ResultDigest:        resultDigest,
		TerminalTimeMs:      time.Now().UnixMilli(),
	}
	env, err := SignEffectResult(body, e.executorKey)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	// Atomic: persist terminal state + result before returning. The
	// state is RE-CHECKED after re-acquiring the lock — a concurrent
	// Finish that won the race returns ITS durable result.
	if rec.state == EffectStateCommitted || rec.state == EffectStateAborted {
		return rec.result, nil
	}
	if rec.state != EffectStateExecuting {
		return nil, fmt.Errorf("dari: illegal terminal transition from %d", rec.state)
	}
	// The terminal result MUST bind the authorization accepted at
	// AUTHORIZED — a caller-supplied digest cannot substitute it.
	if body.AuthorizationDigest != rec.authDigest {
		return nil, errors.New("dari: terminal result does not bind the recorded authorization")
	}
	if terminal == EffectCommitted {
		rec.state = EffectStateCommitted
	} else {
		rec.state = EffectStateAborted
	}
	rec.result = env
	e.persistLocked(opID, rec)
	_ = prepareDigest
	return env, nil
}

// Status returns the durable current state for an operation.
func (e *EffectExecutor) Status(opID string) (EffectState, *EffectResultEnvelope, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.ops[opID]
	if !ok {
		rec = e.loadFromStore(opID)
		ok = rec != nil
	}
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
		rec = e.loadFromStore(opID)
		ok = rec != nil
	}
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

// EffectStatusEnvelope carries a signed status response.
type EffectStatusEnvelope struct {
	Body         *EffectStatusBody
	COSE         *COSESign1
	COSEBytes    []byte
	SignedDigest Digest
}

// SignEffectStatus signs a status response (executor-signed, F.10).
func SignEffectStatus(b *EffectStatusBody, priv ed25519.PrivateKey) (*EffectStatusEnvelope, error) {
	if err := ValidateEffectStatusShape(b); err != nil {
		return nil, err
	}
	sign1, coseBytes, digest, err := SignKernelObject(b, EffectStatusAAD, priv, ObjTypeEffectStatusResp)
	if err != nil {
		return nil, err
	}
	return &EffectStatusEnvelope{Body: b, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
}

// DecodeEffectStatus verifies + decodes a signed status response.
func DecodeEffectStatus(coseBytes []byte, signer ed25519.PublicKey) (*EffectStatusEnvelope, error) {
	body, sign1, digest, err := DecodeKernelObject(
		coseBytes, EffectStatusAAD,
		func(b *EffectStatusBody) ([]byte, error) { return MarshalCBOR(b) }, signer)
	if err != nil {
		return nil, err
	}
	if err := ValidateEffectStatusShape(body); err != nil {
		return nil, err
	}
	return &EffectStatusEnvelope{Body: body, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
}

// DecodeEffectAuthorization verifies + decodes an effect authorization
// (completes the kernel sign/decode pairs).
func DecodeEffectAuthorization(coseBytes []byte, signer ed25519.PublicKey) (*EffectAuthorizationEnvelope, error) {
	body, sign1, digest, err := DecodeKernelObject(
		coseBytes, EffectAuthorizationAAD,
		func(b *EffectAuthorizationBody) ([]byte, error) { return MarshalCBOR(b) }, signer)
	if err != nil {
		return nil, err
	}
	return &EffectAuthorizationEnvelope{Body: body, COSE: sign1, COSEBytes: coseBytes, SignedDigest: digest}, nil
}
