package scheduler

import "testing"

func TestKVLookupDirectoryPreferredWithIdentity(t *testing.T) {
	legacy := NewKVIndex()
	dir := NewKVDirectory()
	legacy.Add("w1", KVBlock{Namespace: "t", Hash: "h", Tokens: 100})
	dir.Add("w1", L1GPU, KVBlock{Namespace: "t", Hash: "h", Tokens: 800}, testIdentity, true)
	lookup := NewKVLookup(legacy, dir)

	// Identity present: the directory answers (800, not the legacy 100).
	if got := lookup.OverlapTokens("w1", "t", "h", testIdentity); got != 800 {
		t.Fatalf("overlap = %d, want directory 800", got)
	}
	// No identity: legacy fallback (100).
	if got := lookup.OverlapTokens("w1", "t", "h", CacheIdentity{}); got != 100 {
		t.Fatalf("overlap = %d, want legacy 100", got)
	}
	// Media: same policy.
	legacy.Add("w2", KVBlock{Namespace: "t", Hash: "h", Tokens: 50, MediaHash: "m"})
	if ws := lookup.WorkersWithMedia("t", "h", "m", CacheIdentity{}); len(ws) != 1 || ws[0] != "w2" {
		t.Fatalf("legacy media = %v", ws)
	}
	dir.Add("w3", L1GPU, KVBlock{Namespace: "t", Hash: "h", Tokens: 50, MediaHash: "m"}, testIdentity, true)
	if ws := lookup.WorkersWithMedia("t", "h", "m", testIdentity); len(ws) != 1 || ws[0] != "w3" {
		t.Fatalf("directory media = %v", ws)
	}
}

func TestKVLookupDegradedAdapters(t *testing.T) {
	// Legacy-only and directory-only deployments both answer safely.
	legacyOnly := NewKVLookup(NewKVIndex(), nil)
	legacyOnly.legacy.Add("w1", KVBlock{Namespace: "t", Hash: "h", Tokens: 10})
	if got := legacyOnly.OverlapTokens("w1", "t", "h", CacheIdentity{}); got != 10 {
		t.Fatalf("legacy-only overlap = %d", got)
	}
	if got := legacyOnly.OverlapTokens("w1", "t", "h", testIdentity); got != 10 {
		t.Fatalf("legacy-only must answer identity queries from the index: %d", got)
	}
	dirOnly := NewKVLookup(nil, NewKVDirectory())
	if got := dirOnly.OverlapTokens("w1", "t", "h", testIdentity); got != 0 {
		t.Fatalf("directory-only without identity evidence = %d, want 0", got)
	}
	empty := NewKVLookup(nil, nil)
	if got := empty.OverlapTokens("w1", "t", "h", CacheIdentity{}); got != 0 {
		t.Fatalf("empty lookup = %d, want 0", got)
	}
}
