package scheduler

import (
	"sort"
	"sync"
)

// kvindex.go implements the S3 exact fleet-wide KV-cache map (spec §14
// row 5, §13.4 tenant namespaces, §13.11 restart-safe indexing). The
// index is event-driven: workers publish KV blocks over DARI; the
// scheduler maintains the global map keyed by (namespace, block_hash,
// media_hash). Replayed journal entries are deduped per (worker, seq).

// KVBlock is one resident cache block on a worker.
type KVBlock struct {
	Namespace string `json:"namespace"`
	Hash      string `json:"hash"`
	Tokens    int    `json:"tokens"`
	MediaHash string `json:"media_hash,omitempty"`
}

// kvKey is the composite index key.
type kvKey struct {
	namespace string
	hash      string
	media     string
}

// KVIndex is the in-memory fleet cache map. Safe for concurrent use.
type KVIndex struct {
	mu     sync.RWMutex
	blocks map[kvKey]map[string]int // key → workerID → tokens
	water  map[string]uint64        // worker → highest applied journal seq
}

// NewKVIndex builds an empty index.
func NewKVIndex() *KVIndex {
	return &KVIndex{
		blocks: make(map[kvKey]map[string]int),
		water:  make(map[string]uint64),
	}
}

// Add indexes one block for a worker.
func (k *KVIndex) Add(workerID string, b KVBlock) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.addLocked(workerID, b)
}

func (k *KVIndex) addLocked(workerID string, b KVBlock) {
	key := kvKey{namespace: b.Namespace, hash: b.Hash, media: b.MediaHash}
	ws, ok := k.blocks[key]
	if !ok {
		ws = make(map[string]int)
		k.blocks[key] = ws
	}
	ws[workerID] = b.Tokens
}

// WorkersWith returns every worker holding the block (namespace-scoped;
// spec §13.4 — the same hash in another tenant's namespace never leaks).
// Any media variant of the block matches: the text prefix lives with or
// without encoder state.
func (k *KVIndex) WorkersWith(namespace, hash string) []string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	seen := map[string]bool{}
	for key, ws := range k.blocks {
		if key.namespace != namespace || key.hash != hash {
			continue
		}
		for w := range ws {
			seen[w] = true
		}
	}
	out := make([]string, 0, len(seen))
	for w := range seen {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// WorkersWithMedia returns workers holding the block with the given media
// hash. Media joins the cache key (spec §12.3.6): repeated-image
// conversations route back to the worker with warm encoder state.
func (k *KVIndex) WorkersWithMedia(namespace, hash, media string) []string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	ws, ok := k.blocks[kvKey{namespace: namespace, hash: hash, media: media}]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(ws))
	for w := range ws {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// OverlapTokens returns the cached prefix size on one worker (the best
// media variant of the block).
func (k *KVIndex) OverlapTokens(workerID, namespace, hash string) int {
	k.mu.RLock()
	defer k.mu.RUnlock()
	best := 0
	for key, ws := range k.blocks {
		if key.namespace != namespace || key.hash != hash {
			continue
		}
		if t := ws[workerID]; t > best {
			best = t
		}
	}
	return best
}

// EvictWorker removes all blocks owned by a worker (eviction/restart).
func (k *KVIndex) EvictWorker(workerID string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for key, ws := range k.blocks {
		if _, ok := ws[workerID]; ok {
			delete(ws, workerID)
			if len(ws) == 0 {
				delete(k.blocks, key)
			}
		}
	}
	delete(k.water, workerID)
}

// ApplyJournal applies one worker's journal batch with (worker, seq)
// dedup (spec §13.11: engine restarts replay the journal; the index
// dedups). Reports whether the batch was applied.
func (k *KVIndex) ApplyJournal(workerID string, seq uint64, blocks []KVBlock) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if prev, ok := k.water[workerID]; ok && seq <= prev {
		return false
	}
	k.water[workerID] = seq
	for _, b := range blocks {
		k.addLocked(workerID, b)
	}
	return true
}

// Watermark returns the highest applied journal seq for a worker.
func (k *KVIndex) Watermark(workerID string) uint64 {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.water[workerID]
}

// TotalCachedTokens returns the fleet-wide cached token volume (S10
// observability, autoscaling input §12.2).
func (k *KVIndex) TotalCachedTokens() int64 {
	k.mu.RLock()
	defer k.mu.RUnlock()
	var total int64
	for _, ws := range k.blocks {
		for _, t := range ws {
			total += int64(t)
		}
	}
	return total
}
