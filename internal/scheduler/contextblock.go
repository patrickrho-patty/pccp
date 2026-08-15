package scheduler

import (
	"crypto/sha256"
	"encoding/hex"
)

// contextblock.go implements the agentic-workload pieces of spec §14
// row 28: context-block identity (stable cache keys for long, branching
// sessions) and cache-retention hints (branch revisits).

// ContextBlock is the identity of one conversation prefix.
type ContextBlock struct {
	hash string
}

// NewContextBlock derives the stable block ID from the canonical prefix
// bytes. The same prefix always yields the same hash; the hash is the
// KV index key (namespace, block_hash).
func NewContextBlock(prefix []byte) ContextBlock {
	sum := sha256.Sum256(prefix)
	return ContextBlock{hash: hex.EncodeToString(sum[:])}
}

// Hash returns the block's cache key.
func (b ContextBlock) Hash() string { return b.hash }

// CacheRetentionHint is a request's advisory for the KV fabric.
type CacheRetentionHint struct {
	Pin        bool `json:"pin"`
	TTLSeconds int  `json:"ttl_seconds"`
}

// Pinned reports whether the hint requests retention.
func (h CacheRetentionHint) Pinned() bool { return h.Pin }
