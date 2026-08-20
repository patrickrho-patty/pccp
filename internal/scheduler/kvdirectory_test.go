package scheduler

import (
	"testing"
	"time"
)

var testIdentity = CacheIdentity{
	ModelPackage: "model-a@1.0",
	TokenizerID:  "tok-v3",
	TemplateID:   "chat-v5",
	PolicyEpoch:  "epoch-7",
}

func dirBlock(ns, hash string, tokens int) KVBlock {
	return KVBlock{Namespace: ns, Hash: hash, Tokens: tokens}
}

func TestKeyPrefixHashTenantKeyed(t *testing.T) {
	prefix := []byte("identical system prefix bytes")
	a := KeyPrefixHash([]byte("tenant-a-key"), prefix)
	b := KeyPrefixHash([]byte("tenant-b-key"), prefix)
	if a == b {
		t.Fatal("same prefix under different tenant keys produced the same hash — cross-tenant oracle")
	}
	if a != KeyPrefixHash([]byte("tenant-a-key"), prefix) {
		t.Fatal("keyed hash is not stable for the same tenant key")
	}
	if a == NewContextBlock(prefix).Hash() {
		t.Fatal("keyed hash must differ from the legacy plain hash")
	}
}

func TestDirectoryOverlapIdentityExact(t *testing.T) {
	d := NewKVDirectory()
	d.Add("w1", L1GPU, dirBlock("tenant-a", "h1", 800), testIdentity, true)

	// Exact identity match earns reuse.
	if got := d.OverlapTokens("w1", "tenant-a", "h1", testIdentity); got != 800 {
		t.Fatalf("overlap = %d, want 800", got)
	}
	// Any identity dimension mismatch: no reuse.
	wrong := testIdentity
	wrong.TokenizerID = "tok-v4"
	if got := d.OverlapTokens("w1", "tenant-a", "h1", wrong); got != 0 {
		t.Fatalf("tokenizer mismatch overlap = %d, want 0", got)
	}
	// Zero identity never matches: reuse without identity is refused.
	if got := d.OverlapTokens("w1", "tenant-a", "h1", CacheIdentity{}); got != 0 {
		t.Fatalf("identity-less overlap = %d, want 0", got)
	}
	// Other tenants never see the extent.
	if got := d.OverlapTokens("w1", "tenant-b", "h1", testIdentity); got != 0 {
		t.Fatalf("cross-tenant overlap = %d, want 0", got)
	}
	// Other workers hold nothing.
	if got := d.OverlapTokens("w2", "tenant-a", "h1", testIdentity); got != 0 {
		t.Fatalf("absent worker overlap = %d, want 0", got)
	}
}

func TestDirectoryCrossTenantAdversarial(t *testing.T) {
	// Attack: tenant B replays tenant A's exact prefix bytes hoping to hit
	// A's cache. Tenant-keyed hashes make the directory identities
	// distinct; even a hash-collision guess stays namespace-isolated.
	d := NewKVDirectory()
	prefix := []byte("shared-prefix-bytes")
	hashA := KeyPrefixHash([]byte("key-a"), prefix)
	hashB := KeyPrefixHash([]byte("key-b"), prefix)
	d.Add("w1", L1GPU, dirBlock("tenant-a", hashA, 4096), testIdentity, true)

	if got := d.OverlapTokens("w1", "tenant-b", hashB, testIdentity); got != 0 {
		t.Fatalf("tenant B gained %d tokens of tenant A's cache", got)
	}
	// B presenting A's namespace and hash outright is still isolated by
	// the namespace key (directory never answers across namespaces).
	if got := d.OverlapTokens("w1", "tenant-a", hashB, testIdentity); got != 0 {
		t.Fatalf("namespace confusion yielded %d tokens", got)
	}
}

func TestDirectoryUnverifiedAndExpiredFallback(t *testing.T) {
	d := NewKVDirectory()
	d.SetNow(func() int64 { return 1000 })
	d.Add("w1", L1GPU, dirBlock("tenant-a", "h1", 800), testIdentity, false)

	// Unverified residence: recompute fallback (zero credit).
	if got := d.OverlapTokens("w1", "tenant-a", "h1", testIdentity); got != 0 {
		t.Fatalf("unverified overlap = %d, want 0", got)
	}
	// Source verification flips it usable.
	if !d.VerifyLocation("w1", "tenant-a", "h1", testIdentity) {
		t.Fatal("verify failed")
	}
	if got := d.OverlapTokens("w1", "tenant-a", "h1", testIdentity); got != 800 {
		t.Fatalf("verified overlap = %d, want 800", got)
	}

	// TTL expiry removes credit deterministically.
	d.SetTTL("tenant-a", "h1", testIdentity, 100*time.Second)
	if got := d.OverlapTokens("w1", "tenant-a", "h1", testIdentity); got != 800 {
		t.Fatalf("in-TTL overlap = %d, want 800", got)
	}
	d.SetNow(func() int64 { return 1200 })
	if got := d.OverlapTokens("w1", "tenant-a", "h1", testIdentity); got != 0 {
		t.Fatalf("expired overlap = %d, want 0", got)
	}
}

func TestDirectoryTierLifecycle(t *testing.T) {
	d := NewKVDirectory()
	d.Add("w1", L1GPU, dirBlock("tenant-a", "h1", 800), testIdentity, true)

	d.Demote("w1", "tenant-a", "h1", testIdentity, L3LocalDisk)
	locs := d.Locations("tenant-a", "h1", testIdentity)
	if len(locs) != 1 || locs[0].Tier != L3LocalDisk {
		t.Fatalf("after demote locations = %+v", locs)
	}
	// Demote never upgrades; promote never downgrades.
	d.Demote("w1", "tenant-a", "h1", testIdentity, L1GPU)
	if locs := d.Locations("tenant-a", "h1", testIdentity); locs[0].Tier != L3LocalDisk {
		t.Fatalf("demote moved to hotter tier: %+v", locs)
	}
	d.Promote("w1", "tenant-a", "h1", testIdentity, L1GPU)
	if locs := d.Locations("tenant-a", "h1", testIdentity); locs[0].Tier != L1GPU {
		t.Fatalf("promote did not move to hotter tier: %+v", locs)
	}
}

func TestDirectoryJournalDedupAndEviction(t *testing.T) {
	d := NewKVDirectory()
	blocks := []KVBlock{dirBlock("tenant-a", "h1", 100), dirBlock("tenant-a", "h2", 200)}
	if !d.ApplyJournal("w1", 5, blocks, testIdentity) {
		t.Fatal("first journal not applied")
	}
	if d.ApplyJournal("w1", 5, blocks, testIdentity) || d.ApplyJournal("w1", 4, blocks, testIdentity) {
		t.Fatal("replayed journal applied twice")
	}
	if d.Watermark("w1") != 5 {
		t.Fatalf("watermark = %d, want 5", d.Watermark("w1"))
	}
	d.EvictWorker("w1")
	if got := d.OverlapTokens("w1", "tenant-a", "h1", testIdentity); got != 0 {
		t.Fatalf("evicted worker overlap = %d", got)
	}
	if d.Watermark("w1") != 0 {
		t.Fatal("eviction must reset the journal watermark")
	}
}

func TestDirectorySweepAndInvalidation(t *testing.T) {
	d := NewKVDirectory()
	d.SetNow(func() int64 { return 1000 })
	d.Add("w1", L1GPU, dirBlock("tenant-a", "hot", 100), testIdentity, true)
	d.Add("w1", L1GPU, dirBlock("tenant-a", "cold", 100), testIdentity, true)
	d.SetNow(func() int64 { return 2000 })
	d.Hit("tenant-a", "hot", testIdentity) // refresh hot only
	d.Sweep(500)
	if got := d.OverlapTokens("w1", "tenant-a", "hot", testIdentity); got != 100 {
		t.Fatalf("swept hot extent, overlap = %d", got)
	}
	if got := d.OverlapTokens("w1", "tenant-a", "cold", testIdentity); got != 0 {
		t.Fatalf("idle extent survived sweep, overlap = %d", got)
	}

	// Identity invalidation: a tokenizer upgrade drops exactly its
	// extents — "hot" (survived the sweep) and "h1" share tok-v3.
	v4 := testIdentity
	v4.TokenizerID = "tok-v4"
	d.Add("w1", L1GPU, dirBlock("tenant-a", "h1", 100), testIdentity, true)
	d.Add("w1", L1GPU, dirBlock("tenant-a", "h2", 100), v4, true)
	n := d.InvalidateIf(func(id CacheIdentity) bool { return id.TokenizerID == "tok-v3" })
	if n != 2 {
		t.Fatalf("invalidated %d extents, want 2 (hot + h1)", n)
	}
	if got := d.OverlapTokens("w1", "tenant-a", "hot", testIdentity); got != 0 {
		t.Fatal("stale-identity extent survived invalidation")
	}
	if got := d.OverlapTokens("w1", "tenant-a", "h1", testIdentity); got != 0 {
		t.Fatal("stale-identity extent survived invalidation")
	}
	if got := d.OverlapTokens("w1", "tenant-a", "h2", v4); got != 100 {
		t.Fatal("unrelated identity wrongly invalidated")
	}
}

func TestDirectoryHotPrefixCandidacy(t *testing.T) {
	d := NewKVDirectory()
	d.Add("w1", L1GPU, dirBlock("tenant-a", "viral", 1024), testIdentity, true)
	for i := 0; i < 5; i++ {
		d.Hit("tenant-a", "viral", testIdentity)
	}
	d.Add("w2", L1GPU, dirBlock("tenant-a", "quiet", 512), testIdentity, true)

	hot := d.HotPrefixes(3, 2)
	if len(hot) != 1 || hot[0].Hash != "viral" {
		t.Fatalf("hot prefixes = %+v", hot)
	}
	if hot[0].Hits != 5 || hot[0].Replicas != 1 || hot[0].Tokens != 1024 {
		t.Fatalf("hot prefix = %+v", hot[0])
	}
	// Enough replicas: no longer a candidate.
	d.Add("w3", L2CPU, dirBlock("tenant-a", "viral", 1024), testIdentity, true)
	d.Add("w4", L2CPU, dirBlock("tenant-a", "viral", 1024), testIdentity, true)
	if hot := d.HotPrefixes(3, 2); len(hot) != 0 {
		t.Fatalf("sufficiently replicated prefix still candidate: %+v", hot)
	}
}

func TestRouterDirectoryPreferredWithIdentity(t *testing.T) {
	r := NewCostRouter(DefaultRouterConfig())
	d := NewKVDirectory()
	r.SetKVDirectory(d)
	// w1 is busy but warm; w2 is idle but cold. Identity-carrying requests
	// get the overlap credit toward w1.
	d.Add("w1", L1GPU, dirBlock("tenant-a", "prefix-h", 800), testIdentity, true)
	r.UpsertWorker(mkWorker("w1", "model-a", 8), RouterWorkerState{PrefillActive: 300, DecodeKV: 100, ActiveRequests: 1})
	r.UpsertWorker(mkWorker("w2", "model-a", 8), RouterWorkerState{})

	req := RouteRequest{
		Model: "model-a", Namespace: "tenant-a", PrefixHash: "prefix-h",
		InputTokens: 1000, ExpectedOutputTokens: 100, Cache: testIdentity,
	}
	got, err := r.Route(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w1" || got.OverlapTokens != 800 {
		t.Fatalf("routed = %+v, want w1 with 800 overlap", got)
	}

	// The same request WITHOUT cache identity uses the legacy path: the
	// directory is never consulted, so the cold idle worker wins.
	req.Cache = CacheIdentity{}
	got, err = r.Route(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w2" {
		t.Fatalf("identity-less request consulted the directory: routed = %+v", got)
	}

	// Identity mismatch: no credit, conservative recompute wins.
	wrong := testIdentity
	wrong.ModelPackage = "model-a@2.0"
	req.Cache = wrong
	got, err = r.Route(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "w2" {
		t.Fatalf("mismatched identity earned credit: routed = %+v", got)
	}
}
