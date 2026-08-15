package scheduler

import (
	"testing"
)

func TestContextBlockIdentity(t *testing.T) {
	// Spec §14 row 28: context-block identity — an agentic session's
	// message prefix hashes to a stable block ID for KV routing.
	b1 := NewContextBlock([]byte(`[{"role":"system","content":"be helpful"},{"role":"user","content":"hi"}]`))
	b2 := NewContextBlock([]byte(`[{"role":"system","content":"be helpful"},{"role":"user","content":"hi"}]`))
	b3 := NewContextBlock([]byte(`[{"role":"system","content":"be helpful"},{"role":"user","content":"different"}]`))
	if b1.Hash() != b2.Hash() {
		t.Fatal("identical prefixes must produce identical block hashes")
	}
	if b1.Hash() == b3.Hash() {
		t.Fatal("different prefixes must not collide")
	}
	if len(b1.Hash()) < 16 {
		t.Fatal("hash too short for a cache key")
	}
}

func TestCacheRetentionHints(t *testing.T) {
	// Spec §14 row 28: cache-retention hints — agentic sessions mark
	// their context blocks for retention (branching will revisit them).
	h := CacheRetentionHint{Pin: true, TTLSeconds: 600}
	if !h.Pinned() {
		t.Fatal("pinned hint must report pinned")
	}
	h2 := CacheRetentionHint{}
	if h2.Pinned() {
		t.Fatal("default hint must not pin")
	}
}
