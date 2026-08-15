package scheduler

import "testing"

func TestTierOrder(t *testing.T) {
	if L1GPU >= L2CPU || L2CPU >= L3LocalDisk || L3LocalDisk >= L4Remote {
		t.Fatal("KV tiers must order L1 < L2 < L3 < L4 (locality)")
	}
}

func TestTieredKVAddAndResolve(t *testing.T) {
	f := NewTieredKV()
	f.Add("w1", L1GPU, "tenant-a", "h1", 100)
	f.Add("w1", L2CPU, "tenant-a", "h1", 50)

	// The hottest tier wins for a lookup.
	tier, tokens, ok := f.Resolve("w1", "tenant-a", "h1")
	if !ok || tier != L1GPU || tokens != 100 {
		t.Fatalf("resolve = %v,%d,%v — want L1,100", tier, tokens, ok)
	}
}

func TestTieredKVDemotion(t *testing.T) {
	// Demotion moves a block down a tier and halves the resident tokens
	// (eviction pressure model).
	f := NewTieredKV()
	f.Add("w1", L1GPU, "tenant-a", "h1", 100)
	f.Demote("w1", L1GPU, L2CPU, "tenant-a", "h1")
	tier, tokens, ok := f.Resolve("w1", "tenant-a", "h1")
	if !ok || tier != L2CPU || tokens != 50 {
		t.Fatalf("after demotion = %v,%d — want L2,50", tier, tokens)
	}
}

func TestTieredKVPromotion(t *testing.T) {
	f := NewTieredKV()
	f.Add("w1", L2CPU, "tenant-a", "h1", 40)
	f.Promote("w1", L1GPU, "tenant-a", "h1", 90)
	tier, tokens, ok := f.Resolve("w1", "tenant-a", "h1")
	if !ok || tier != L1GPU || tokens != 90 {
		t.Fatalf("after promotion = %v,%d — want L1,90", tier, tokens)
	}
}

func TestTieredKVRetentionSweep(t *testing.T) {
	// Retention: blocks unused beyond the retention window drop out.
	f := NewTieredKV()
	f.SetNow(func() int64 { return 1000 })
	f.Add("w1", L3LocalDisk, "tenant-a", "old", 30)
	f.Touch("w1", "tenant-a", "old")
	f.SetNow(func() int64 { return 1000 + 3600 }) // 1h later
	f.Sweep(1800)                                 // 30m retention
	if _, _, ok := f.Resolve("w1", "tenant-a", "old"); ok {
		t.Fatal("stale block must be swept")
	}
}

func TestTieredKVPrefetch(t *testing.T) {
	// Prefetch: a block requested from a cold tier is scheduled for
	// promotion (prefetch count tracked).
	f := NewTieredKV()
	f.Add("w1", L3LocalDisk, "tenant-a", "h1", 20)
	f.Prefetch("w1", "tenant-a", "h1")
	if f.PrefetchPending("w1") != 1 {
		t.Fatalf("prefetch pending = %d, want 1", f.PrefetchPending("w1"))
	}
}
