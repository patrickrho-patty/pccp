package scheduler

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// Evidence event types (DARI scheduler §6): every admission outcome lands on
// the scheduler's evidence log so the fleet's composition is auditable.
const (
	EventWorkerRegister   = "worker.register"
	EventWorkerDeny       = "worker.deny"
	EventWorkerQuarantine = "worker.quarantine"
	EventWorkerEvict      = "worker.evict"
)

// EvidenceEvent is a signed, queryable record of a registry decision. The
// signature binds the canonical JSON body (excluding the signature itself),
// mirroring the CP event-spine pattern.
type EvidenceEvent struct {
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	WorkerID   string    `json:"worker_id"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
	Signature  string    `json:"signature,omitempty"`
}

// EvidenceLog is the scheduler's append-only evidence store. In-memory ring
// in S1; the CP can ingest entries into the durable spine later.
type EvidenceLog struct {
	mu     sync.RWMutex
	key    ed25519.PrivateKey
	events []EvidenceEvent
}

// NewEvidenceLog creates an evidence log signed by the given key.
func NewEvidenceLog(key ed25519.PrivateKey) *EvidenceLog {
	return &EvidenceLog{key: key}
}

func (e *EvidenceEvent) signingBody() ([]byte, error) {
	body := EvidenceEvent{
		EventID:    e.EventID,
		EventType:  e.EventType,
		WorkerID:   e.WorkerID,
		Reason:     e.Reason,
		OccurredAt: e.OccurredAt,
	}
	return json.Marshal(body)
}

// Emit creates, signs, and appends an evidence event.
func (l *EvidenceLog) Emit(eventType, workerID, reason string) EvidenceEvent {
	event := EvidenceEvent{
		EventID:    dari.GenerateID("wkr-evt"),
		EventType:  eventType,
		WorkerID:   workerID,
		Reason:     reason,
		OccurredAt: time.Now().UTC(),
	}
	if l.key != nil {
		if body, err := event.signingBody(); err == nil {
			event.Signature = hex.EncodeToString(ed25519.Sign(l.key, body))
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
	return event
}

// Events returns a copy of the log in insertion order.
func (l *EvidenceLog) Events() []EvidenceEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]EvidenceEvent, len(l.events))
	copy(out, l.events)
	return out
}

// Verify checks the event signature against the evidence public key.
func (e *EvidenceEvent) Verify(pub ed25519.PublicKey) error {
	if e.Signature == "" {
		return fmt.Errorf("scheduler: evidence event has no signature")
	}
	body, err := e.signingBody()
	if err != nil {
		return err
	}
	sig, err := hex.DecodeString(e.Signature)
	if err != nil {
		return fmt.Errorf("scheduler: decode evidence signature: %w", err)
	}
	if !ed25519.Verify(pub, body, sig) {
		return fmt.Errorf("scheduler: evidence signature verification failed")
	}
	return nil
}
