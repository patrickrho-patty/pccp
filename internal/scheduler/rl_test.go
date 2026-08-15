package scheduler

import "testing"

func TestRLPoolSeparation(t *testing.T) {
	// Spec §14 row 29: post-training/RL serving — rollouts run in a
	// separate pool; serving traffic never lands on an RL pool.
	pm := NewRLPoolManager()
	pm.MarkRL("w-rl-1")
	pm.MarkRL("w-rl-2")
	pm.MarkServing("w-serve-1")

	if !pm.IsRL("w-rl-1") {
		t.Fatal("rl worker not marked")
	}
	if pm.IsRL("w-serve-1") {
		t.Fatal("serving worker must not be RL")
	}
	// A rollout request is placed only on RL workers.
	if got := pm.PlaceRollout([]string{"w-rl-1", "w-serve-1"}); len(got) != 1 || got[0] != "w-rl-1" {
		t.Fatalf("rollout placement = %v", got)
	}
}

func TestRLWeightUpdateDirective(t *testing.T) {
	// In-place weight updates: the scheduler issues a signed directive;
	// RL workers execute it out-of-band from serving traffic.
	pm := NewRLPoolManager()
	d := pm.WeightUpdateDirective("w-rl-1", "checkpoint-42")
	if d.Action != "weight_update" || d.WorkerID != "w-rl-1" || d.Reason != "checkpoint-42" {
		t.Fatalf("directive = %+v", d)
	}
}

func TestRLRolloutIsolationByDefault(t *testing.T) {
	// Unmarked workers are serving-only: no RL traffic by default
	// (fail-closed).
	pm := NewRLPoolManager()
	if pm.IsRL("any-worker") {
		t.Fatal("unmarked worker must not accept RL traffic")
	}
}
