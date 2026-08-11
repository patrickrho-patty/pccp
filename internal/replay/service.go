package replay

import (
	"fmt"
	"sync"
	"time"
)

// IdempotencyClass declares the replay behavior of a side-effecting operation (PAPER §52).
type IdempotencyClass string

const (
	ClassSafeReplay        IdempotencyClass = "SAFE_REPLAY"
	ClassSameKeyOnly       IdempotencyClass = "SAME_KEY_ONLY"
	ClassQueryBeforeRetry  IdempotencyClass = "QUERY_BEFORE_RETRY"
	ClassNeverAutoRetry    IdempotencyClass = "NEVER_AUTORETRY"
)

// Protection implements replay protection (PAPER §51) and idempotency tracking (PAPER §52).
type Protection struct {
	mu sync.Mutex
	// replayWindow tracks seen idempotency keys within the active window
	replayWindow map[string]*IdempotencyEntry
	// maxAge is how long to keep entries
	maxAge time.Duration
}

// IdempotencyEntry tracks a previously-seen operation.
type IdempotencyEntry struct {
	Key        string
	Class      IdempotencyClass
	Result     interface{}
	SeenAt     time.Time
	ExchangeID string
	SessionID  string
}

// New creates a new replay protection service.
func New(maxAge time.Duration) *Protection {
	if maxAge == 0 {
		maxAge = 10 * time.Minute
	}
	p := &Protection{
		replayWindow: make(map[string]*IdempotencyEntry),
		maxAge:       maxAge,
	}
	go p.cleanupLoop()
	return p
}

// Check checks whether an operation with the given idempotency key can proceed.
// Returns (seen, entry, error) — if seen is true, the previous result should be returned.
func (p *Protection) Check(key, sessionID, exchangeID string, class IdempotencyClass) (bool, *IdempotencyEntry, error) {
	if key == "" {
		return false, nil, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	entry, exists := p.replayWindow[key]
	if !exists {
		// New operation, record it
		p.replayWindow[key] = &IdempotencyEntry{
			Key:        key,
			Class:      class,
			SeenAt:     time.Now(),
			ExchangeID: exchangeID,
			SessionID:  sessionID,
		}
		return false, nil, nil
	}

	// Operation was seen before — handle based on class
	switch class {
	case ClassSafeReplay:
		// Safe to replay, let it proceed
		return false, entry, nil
	case ClassSameKeyOnly:
		// Return the previous result
		return true, entry, nil
	case ClassQueryBeforeRetry:
		// Mark as needing query — caller must check state
		return true, entry, fmt.Errorf("replay: query before retry required for key %s", key)
	case ClassNeverAutoRetry:
		return true, entry, fmt.Errorf("replay: never auto-retry operation with key %s", key)
	default:
		return true, entry, nil
	}
}

// Record stores the result of a completed operation for future replays.
func (p *Protection) Record(key string, result interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.replayWindow[key]; ok {
		entry.Result = result
	}
}

// Clear removes an entry from the replay window.
func (p *Protection) Clear(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.replayWindow, key)
}

// Size returns the number of entries in the replay window.
func (p *Protection) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.replayWindow)
}

// cleanupLoop periodically removes expired entries.
func (p *Protection) cleanupLoop() {
	ticker := time.NewTicker(p.maxAge / 2)
	defer ticker.Stop()
	for range ticker.C {
		p.cleanup()
	}
}

func (p *Protection) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for key, entry := range p.replayWindow {
		if now.Sub(entry.SeenAt) > p.maxAge {
			delete(p.replayWindow, key)
		}
	}
}

// OperationClass maps operation types to their idempotency classes (PAPER §52 table).
func OperationClass(operationType string) IdempotencyClass {
	switch operationType {
	case "presence.update":
		return ClassSafeReplay
	case "context.read":
		return ClassSameKeyOnly
	case "model.request":
		return ClassSameKeyOnly
	case "shell.command":
		return ClassQueryBeforeRetry
	case "file.finalize":
		return ClassSameKeyOnly
	case "broadcast.send":
		return ClassSameKeyOnly
	case "model.recall":
		return ClassSameKeyOnly
	case "runtime.destructive":
		return ClassNeverAutoRetry
	case "tool.propose":
		return ClassSafeReplay
	case "commit.bind":
		return ClassSameKeyOnly
	case "session.open":
		return ClassSameKeyOnly
	case "session.close":
		return ClassSameKeyOnly
	case "lease.issue":
		return ClassSameKeyOnly
	case "lease.revoke":
		return ClassSameKeyOnly
	case "evidence.receipt":
		return ClassSafeReplay
	default:
		return ClassSameKeyOnly
	}
}
