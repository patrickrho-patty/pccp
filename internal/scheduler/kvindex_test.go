package scheduler

import (
	"testing"
)

func TestKVIndexAddAndLookup(t *testing.T) {
	idx := NewKVIndex()
	idx.Add("w1", KVBlock{Namespace: "tenant-a", Hash: "h1", Tokens: 100, MediaHash: ""})
	idx.Add("w1", KVBlock{Namespace: "tenant-a", Hash: "h2", Tokens: 200, MediaHash: "img-1"})
	idx.Add("w2", KVBlock{Namespace: "tenant-a", Hash: "h1", Tokens: 100, MediaHash: ""})

	// h1 lives on both workers; h2 only on w1.
	workers := idx.WorkersWith("tenant-a", "h1")
	if len(workers) != 2 {
		t.Fatalf("h1 workers = %v, want w1+w2", workers)
	}
	workers = idx.WorkersWith("tenant-a", "h2")
	if len(workers) != 1 || workers[0] != "w1" {
		t.Fatalf("h2 workers = %v, want [w1]", workers)
	}
}

func TestKVIndexNamespaceIsolation(t *testing.T) {
	idx := NewKVIndex()
	idx.Add("w1", KVBlock{Namespace: "tenant-a", Hash: "h1", Tokens: 100})
	// Same hash in another tenant's namespace must not leak (spec §13.4
	// tenant-scoped namespaces).
	if got := idx.WorkersWith("tenant-b", "h1"); len(got) != 0 {
		t.Fatalf("cross-tenant hash leak: %v", got)
	}
}

func TestKVIndexOverlapTokens(t *testing.T) {
	idx := NewKVIndex()
	// w1 has a 300-token prefix; w2 has 50 tokens of it.
	idx.Add("w1", KVBlock{Namespace: "tenant-a", Hash: "h1", Tokens: 300})
	idx.Add("w2", KVBlock{Namespace: "tenant-a", Hash: "h1", Tokens: 50})

	got := idx.OverlapTokens("w1", "tenant-a", "h1")
	if got != 300 {
		t.Fatalf("w1 overlap = %d, want 300", got)
	}
	got = idx.OverlapTokens("w2", "tenant-a", "h1")
	if got != 50 {
		t.Fatalf("w2 overlap = %d, want 50", got)
	}
	got = idx.OverlapTokens("w3", "tenant-a", "h1")
	if got != 0 {
		t.Fatalf("unknown worker overlap = %d, want 0", got)
	}
}

func TestKVIndexMediaHashRouting(t *testing.T) {
	// Spec §12.3.6: media hash joins the cache key — a repeated-image
	// conversation must find the worker holding that media state.
	idx := NewKVIndex()
	idx.Add("w1", KVBlock{Namespace: "tenant-a", Hash: "ctx", Tokens: 50, MediaHash: "m1"})
	idx.Add("w2", KVBlock{Namespace: "tenant-a", Hash: "ctx", Tokens: 50, MediaHash: "m2"})

	got := idx.WorkersWithMedia("tenant-a", "ctx", "m1")
	if len(got) != 1 || got[0] != "w1" {
		t.Fatalf("media-hash workers = %v, want [w1]", got)
	}
}

func TestKVIndexEvictionAndWatermark(t *testing.T) {
	idx := NewKVIndex()
	idx.Add("w1", KVBlock{Namespace: "tenant-a", Hash: "h1", Tokens: 10})
	idx.Add("w1", KVBlock{Namespace: "tenant-a", Hash: "h2", Tokens: 20})
	idx.EvictWorker("w1")
	if got := idx.WorkersWith("tenant-a", "h1"); len(got) != 0 {
		t.Fatalf("evicted worker still indexed: %v", got)
	}
}

func TestKVIndexDedupByWorkerSeq(t *testing.T) {
	// Spec §13.11: the index dedups by (worker, seq) — a replayed
	// journal entry must not double-count or corrupt state.
	idx := NewKVIndex()
	if !idx.ApplyJournal("w1", 1, []KVBlock{{Namespace: "tenant-a", Hash: "h1", Tokens: 10}}) {
		t.Fatal("first journal entry must apply")
	}
	if idx.ApplyJournal("w1", 1, []KVBlock{{Namespace: "tenant-a", Hash: "h1", Tokens: 10}}) {
		t.Fatal("replayed journal entry must be deduped")
	}
	// Out-of-order seq: newer only.
	if !idx.ApplyJournal("w1", 2, []KVBlock{{Namespace: "tenant-a", Hash: "h2", Tokens: 5}}) {
		t.Fatal("seq 2 must apply")
	}
	if idx.ApplyJournal("w1", 1, []KVBlock{}) {
		t.Fatal("stale seq must be ignored")
	}
	if idx.OverlapTokens("w1", "tenant-a", "h1") != 10 {
		t.Fatalf("h1 tokens corrupted by dedup: %d", idx.OverlapTokens("w1", "tenant-a", "h1"))
	}
}
