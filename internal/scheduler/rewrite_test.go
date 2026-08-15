package scheduler

import (
	"math/rand"
	"testing"
)

func TestRewriteAliasResolves(t *testing.T) {
	rw := NewModelRewriter(nil)
	rw.SetAlias("ko-coder", "patty-kocoder-v1")
	got, err := rw.Resolve("ko-coder", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "patty-kocoder-v1" {
		t.Fatalf("resolve(ko-coder) = %s", got)
	}
}

func TestRewriteUnknownModelRejected(t *testing.T) {
	rw := NewModelRewriter(nil)
	if _, err := rw.Resolve("definitely-not-a-model", 0); err == nil {
		t.Fatal("unknown model must be rejected (raw/fake model ID gate §10A.11)")
	}
}

func TestRewriteTrafficSplitDeterministic(t *testing.T) {
	rw := NewModelRewriter(nil)
	rw.SetSplit("alias-model", map[string]int{"m-v1": 70, "m-v2": 30})

	// Deterministic by correlation ID — same ID, same target, always.
	first, err := rw.Resolve("alias-model", 424242)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := rw.Resolve("alias-model", 424242)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Fatalf("same correlation ID resolved differently: %s vs %s", got, first)
		}
	}

	// Distribution converges toward the configured split.
	var v1, v2 int
	for i := int64(0); i < 2000; i++ {
		got, err := rw.Resolve("alias-model", i)
		if err != nil {
			t.Fatal(err)
		}
		if got == "m-v1" {
			v1++
		} else if got == "m-v2" {
			v2++
		}
	}
	if v1 < 1200 || v1 > 1600 {
		t.Fatalf("v1 share %d/2000 far from 70%% split", v1)
	}
	if v2 < 400 || v2 > 800 {
		t.Fatalf("v2 share %d/2000 far from 30%% split", v2)
	}
}

func TestRewriteFallbackChain(t *testing.T) {
	rw := NewModelRewriter(nil)
	// canary model A with fallback to stable B; if A is withdrawn, B serves.
	rw.SetRule("canary-model", RewriteRule{
		Targets:      []string{"canary-v2", "stable-v1"},
		Unavailable:  map[string]bool{"canary-v2": true},
		Strategy:     StrategyFallback,
	})
	got, err := rw.Resolve("canary-model", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "stable-v1" {
		t.Fatalf("fallback = %s, want stable-v1", got)
	}
}

func TestRewriteRemapMigration(t *testing.T) {
	rw := NewModelRewriter(nil)
	// Version migration: every v1 request silently lands on v2.
	rw.SetRemap("patty-kocoder-v1", "patty-kocoder-v2")
	got, err := rw.Resolve("patty-kocoder-v1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got != "patty-kocoder-v2" {
		t.Fatalf("remap = %s, want patty-kocoder-v2", got)
	}
}

func TestRewriteAbSplitEven(t *testing.T) {
	rw := NewModelRewriter(nil)
	rw.SetSplit("ab-model", map[string]int{"a": 50, "b": 50})
	rng := rand.New(rand.NewSource(1))
	var a, b int
	for i := 0; i < 1000; i++ {
		got, err := rw.Resolve("ab-model", rng.Int63())
		if err != nil {
			t.Fatal(err)
		}
		if got == "a" {
			a++
		} else if got == "b" {
			b++
		}
	}
	if a < 400 || a > 600 {
		t.Fatalf("A share %d/1000 far from 50%%", a)
	}
	if a+b != 1000 {
		t.Fatalf("a+b = %d, want 1000 (no other targets)", a+b)
	}
}
