package scheduler

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func TestTrafficEnvelopeRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	env := NewTrafficEnvelope("req-1", "tenant-a", "interactive-paid", 30*time.Second)
	if err := env.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := env.Verify(pub); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestTrafficEnvelopeTamperRejected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	env := NewTrafficEnvelope("req-1", "tenant-a", "interactive-paid", 30*time.Second)
	if err := env.Sign(priv); err != nil {
		t.Fatal(err)
	}
	// Tamper with the claimed class after signing: verification must fail.
	env.Class = "interactive-paid"[:len("interactive-paid")-4] + "GOLD"
	if err := env.Verify(pub); err == nil {
		t.Fatal("tampered class must fail verification")
	}
}

func TestTrafficEnvelopeExpiry(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	env := NewTrafficEnvelope("req-1", "tenant-a", "batch", -time.Second)
	if err := env.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := env.Verify(pub); err == nil {
		t.Fatal("expired envelope must fail verification")
	}
}

func TestClassResolverFailClosed(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	r := NewClassResolver(pub)
	// No envelope: the caller gets the lowest class, never an elevated one.
	if got := r.Resolve(nil); got != "batch" {
		t.Fatalf("no-envelope class = %s, want batch (fail-closed)", got)
	}
	// A valid signed envelope elevates.
	env := NewTrafficEnvelope("r1", "tenant-a", "interactive-paid", time.Minute)
	if err := env.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if got := r.Resolve(env); got != "interactive-paid" {
		t.Fatalf("signed class = %s, want interactive-paid", got)
	}
	// A forged envelope (wrong key) also lands at batch.
	forged := NewTrafficEnvelope("r1", "tenant-a", "interactive-paid", time.Minute)
	otherPub, otherPriv, _ := ed25519.GenerateKey(nil)
	_ = otherPub
	if err := forged.Sign(otherPriv); err != nil {
		t.Fatal(err)
	}
	if got := r.Resolve(forged); got != "batch" {
		t.Fatalf("forged-envelope class = %s, want batch", got)
	}
}
