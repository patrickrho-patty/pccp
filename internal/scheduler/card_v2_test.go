package scheduler

import (
	"crypto/ed25519"
	"testing"
)

func TestCardV2SigningIncludesDispatchFields(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	c := WorkerCard{
		CardVersion: 2,
		WorkerID:    "w1",
		DariAddr:    "10.0.0.5:9444",
		ActiveSeqs:  3,
		Status:      "active",
		EngineKind:  "vllm",
	}
	if err := c.Sign(priv); err != nil {
		t.Fatal(err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	if err := c.Verify(pub); err != nil {
		t.Fatalf("v2 card verify: %v", err)
	}
	// Tamper with the dispatch address: verification must fail.
	c.DariAddr = "10.0.0.6:9444"
	if err := c.Verify(pub); err == nil {
		t.Fatal("tampered DariAddr must fail verification")
	}
}

func TestCardV1RemainsVerifiable(t *testing.T) {
	// Existing v1 cards (no dispatch fields) must keep verifying — the
	// fleet is forward-compatible (S1 spec §3: extend, never rework).
	_, priv, _ := ed25519.GenerateKey(nil)
	c := WorkerCard{
		CardVersion: 1,
		WorkerID:    "w1",
		Status:      "active",
	}
	if err := c.Sign(priv); err != nil {
		t.Fatal(err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	if err := c.Verify(pub); err != nil {
		t.Fatalf("v1 card verify: %v", err)
	}
}

func TestCardV2MissingDispatchAddr(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	// A v2 card without a dispatch address cannot serve inference — the
	// selector must skip it (fail-closed, no address = no route).
	c := WorkerCard{CardVersion: 2, WorkerID: "w1", Status: "active"}
	if err := c.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if c.Servable() {
		t.Fatal("card without DariAddr must not be servable")
	}
	c.DariAddr = "10.0.0.1:9444"
	if !c.Servable() {
		t.Fatal("card with DariAddr must be servable")
	}
}
