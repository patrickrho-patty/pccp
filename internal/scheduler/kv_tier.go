package scheduler

import (
	"sync"
	"time"
)

// kv_tier.go implements S6 tiered KV fabric (spec §14 row 10): L1–L4
// tiers with promotion/demotion, prefetch scheduling, and retention
// sweeps. Tiers: L1 GPU HBM, L2 CPU RAM, L3 local disk/NVMe, L4 remote.

// KVTier is the storage tier level.
type KVTier int

const (
	L1GPU       KVTier = 1
	L2CPU       KVTier = 2
	L3LocalDisk KVTier = 3
	L4Remote    KVTier = 4
)

// String labels the tier for observability views.
func (t KVTier) String() string {
	switch t {
	case L1GPU:
		return "L1-hbm"
	case L2CPU:
		return "L2-host"
	case L3LocalDisk:
		return "L3-disk"
	case L4Remote:
		return "L4-remote"
	}
	return "unknown"
}

// tieredBlock is one block's residence on one worker.
type tieredBlock struct {
	tier       KVTier
	tokens     int
	lastAccess int64
}

// TieredKV tracks block residence across tiers. Safe for concurrent use.
type TieredKV struct {
	mu       sync.RWMutex
	blocks   map[string]map[string]tieredBlock // worker → (ns\x00hash) → block
	prefetch map[string]int                    // worker → pending count
	now      func() int64
}

// NewTieredKV builds an empty fabric.
func NewTieredKV() *TieredKV {
	return &TieredKV{
		blocks:   make(map[string]map[string]tieredBlock),
		prefetch: make(map[string]int),
		now:      func() int64 { return time.Now().Unix() },
	}
}

// SetNow injects a clock (deterministic tests).
func (f *TieredKV) SetNow(fn func() int64) { f.now = fn }

func blockKey(namespace, hash string) string { return namespace + "\x00" + hash }

// Add records a block's residence. The hottest residence wins: adding a
// colder tier to a block already hot does not downgrade it; adding a
// hotter tier replaces.
func (f *TieredKV) Add(workerID string, tier KVTier, namespace, hash string, tokens int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.blocks[workerID]
	if !ok {
		w = make(map[string]tieredBlock)
		f.blocks[workerID] = w
	}
	key := blockKey(namespace, hash)
	if existing, ok := w[key]; ok && existing.tier < tier {
		existing.lastAccess = f.now()
		w[key] = existing
		return
	}
	w[key] = tieredBlock{tier: tier, tokens: tokens, lastAccess: f.now()}
}

// Resolve returns the block's hottest residence for a worker.
func (f *TieredKV) Resolve(workerID, namespace, hash string) (KVTier, int, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	w, ok := f.blocks[workerID]
	if !ok {
		return 0, 0, false
	}
	b, ok := w[blockKey(namespace, hash)]
	if !ok {
		return 0, 0, false
	}
	return b.tier, b.tokens, true
}

// Demote moves a block to a lower tier, halving resident tokens
// (eviction pressure).
func (f *TieredKV) Demote(workerID string, from, to KVTier, namespace, hash string) {
	if to <= from {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.blocks[workerID]
	if !ok {
		return
	}
	key := blockKey(namespace, hash)
	b, ok := w[key]
	if !ok || b.tier != from {
		return
	}
	b.tier = to
	b.tokens /= 2
	w[key] = b
}

// Promote moves a block to a hotter tier.
func (f *TieredKV) Promote(workerID string, to KVTier, namespace, hash string, tokens int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.blocks[workerID]
	if !ok {
		w = make(map[string]tieredBlock)
		f.blocks[workerID] = w
	}
	key := blockKey(namespace, hash)
	w[key] = tieredBlock{tier: to, tokens: tokens, lastAccess: f.now()}
}

// Touch refreshes the access stamp.
func (f *TieredKV) Touch(workerID, namespace, hash string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.blocks[workerID]
	if !ok {
		return
	}
	key := blockKey(namespace, hash)
	if b, ok := w[key]; ok {
		b.lastAccess = f.now()
		w[key] = b
	}
}

// Sweep evicts blocks idle longer than retentionSeconds.
func (f *TieredKV) Sweep(retentionSeconds int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	for workerID, w := range f.blocks {
		for key, b := range w {
			if now-b.lastAccess > retentionSeconds {
				delete(w, key)
			}
		}
		if len(w) == 0 {
			delete(f.blocks, workerID)
		}
	}
}

// Prefetch schedules a cold block for promotion.
func (f *TieredKV) Prefetch(workerID, namespace, hash string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prefetch[workerID]++
}

// PrefetchPending returns the worker's scheduled prefetches.
func (f *TieredKV) PrefetchPending(workerID string) int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.prefetch[workerID]
}

// EvictWorker drops a worker's entire tiered residence.
func (f *TieredKV) EvictWorker(workerID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.blocks, workerID)
	delete(f.prefetch, workerID)
}
