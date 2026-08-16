package dari

import (
	"errors"
	"testing"
)

// F.10 durability pin: a store-backed executor answers EFFECT_STATUS
// from history after "restart" (a fresh executor over the same store).
type memEffectStore struct{ m map[string]*EffectRecordRow }

func (s *memEffectStore) SaveEffect(opID string, rec *EffectRecordRow) error {
	s.m[opID] = rec
	return nil
}
func (s *memEffectStore) LoadEffect(opID string) (*EffectRecordRow, error) {
	r, ok := s.m[opID]
	if !ok {
		return nil, errors.New("not found")
	}
	return r, nil
}

func TestEffectExecutorDurabilityAcrossRestart(t *testing.T) {
	store := &memEffectStore{m: map[string]*EffectRecordRow{}}
	executorPriv, _ := effectKeys(t)
	e1 := NewDurableEffectExecutor("executor-1", executorPriv, store)

	prepare, err := SignEffectPrepare(&EffectPrepareBody{
		Version: 1, OperationID: "op-dur", ExchangeID: "exch-1", Nonce: NewOperationNonce(),
		LeafGrantDigest: Digest{1}, InputDigest: Digest{2}, EffectKind: "file.write",
		ExecutorPeerID: "executor-1", RetryOwnerID: "harness-1",
	}, executorPriv)
	if err != nil {
		t.Fatal(err)
	}
	if err := e1.AckPrepare(prepare); err != nil {
		t.Fatal(err)
	}
	// "Restart": fresh executor, same store.
	e2 := NewDurableEffectExecutor("executor-1", executorPriv, store)
	state, _, ok := e2.Status("op-dur")
	if !ok || state == EffectStateAbsent {
		t.Fatalf("post-restart status = %v ok=%v — durability broken", state, ok)
	}
	if state != EffectStatePrepared {
		t.Fatalf("post-restart state = %d, want PREPARED", state)
	}
}

// C3 (code review): a cross-restart retried PREPARE with a DIFFERENT
// binding must REPLAY_CONFLICT — the store-loaded path used to accept
// anything silently.
func TestEffectStoreReboundPrepareConflictsAcrossRestart(t *testing.T) {
	store := &memEffectStore{m: map[string]*EffectRecordRow{}}
	executorPriv, _ := effectKeys(t)
	e1 := NewDurableEffectExecutor("executor-1", executorPriv, store)

	nonce1 := NewOperationNonce()
	prep1, err := SignEffectPrepare(&EffectPrepareBody{
		Version: 1, OperationID: "op-c3", ExchangeID: "exch", Nonce: nonce1,
		LeafGrantDigest: Digest{1}, InputDigest: Digest{2}, EffectKind: "file.write",
		ExecutorPeerID: "executor-1", RetryOwnerID: "harness-1",
	}, executorPriv)
	if err != nil {
		t.Fatal(err)
	}
	if err := e1.AckPrepare(prep1); err != nil {
		t.Fatal(err)
	}

	// Restart + a rebound prepare for the SAME operation.
	e2 := NewDurableEffectExecutor("executor-1", executorPriv, store)
	nonce2 := nonce1
	nonce2[0] ^= 1
	rebound, err := SignEffectPrepare(&EffectPrepareBody{
		Version: 1, OperationID: "op-c3", ExchangeID: "exch", Nonce: nonce2,
		LeafGrantDigest: Digest{1}, InputDigest: Digest{2}, EffectKind: "file.write",
		ExecutorPeerID: "executor-1", RetryOwnerID: "harness-1",
	}, executorPriv)
	if err != nil {
		t.Fatal(err)
	}
	if err := e2.AckPrepare(rebound); !errors.Is(err, ErrReplayConflict) {
		t.Fatalf("cross-restart rebound prepare must conflict, got %v", err)
	}
	// Identical retry still idempotent.
	if err := e2.AckPrepare(prep1); err != nil {
		t.Fatalf("identical cross-restart retry must be idempotent: %v", err)
	}
}
