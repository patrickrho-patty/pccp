package scheduler

import "testing"

func TestEngineLifecycleStateMachine(t *testing.T) {
	lc := NewEngineLifecycle()

	// Legal transitions.
	for _, step := range []struct{ from, to, action string }{
		{"", "starting", "start"},
		{"starting", "ready", "readiness_ok"},
		{"ready", "draining", "drain"},
		{"draining", "sleeping", "sleep"},
		{"sleeping", "starting", "wake"},
		{"starting", "ready", "readiness_ok"},
		{"ready", "paused", "pause"},
		{"paused", "ready", "resume"},
		{"ready", "reloading", "reload"},
		{"reloading", "ready", "reload_done"},
		{"ready", "draining", "drain"},
		{"draining", "terminated", "terminate"},
		{"terminated", "starting", "restart"},
	} {
		if err := lc.Transition("w1", step.from, step.to, step.action); err != nil {
			t.Fatalf("%s→%s: %v", step.from, step.to, err)
		}
		if got := lc.State("w1"); got != step.to {
			t.Fatalf("after %s→%s state = %s", step.from, step.to, got)
		}
	}
}

func TestEngineLifecycleIllegalTransition(t *testing.T) {
	lc := NewEngineLifecycle()
	lc.Transition("w1", "", "ready", "start")
	// Terminated is terminal except restart; jumping ready→terminated
	// without draining is refused.
	if err := lc.Transition("w1", "ready", "terminated", "terminate"); err == nil {
		t.Fatal("ready→terminated without drain must be refused")
	}
	// Unknown worker.
	if err := lc.Transition("nope", "", "ready", "start"); err == nil {
		t.Fatal("unknown worker must fail")
	}
}

func TestWarmPoolInventory(t *testing.T) {
	wp := NewWarmPool()
	wp.Add("w1", "model-a")
	wp.Add("w2", "model-a")
	wp.Add("w3", "model-b")

	if n := wp.ReadyCount("model-a"); n != 2 {
		t.Fatalf("ready = %d, want 2", n)
	}
	wp.Remove("w1")
	if n := wp.ReadyCount("model-a"); n != 1 {
		t.Fatalf("after remove ready = %d, want 1", n)
	}
	// Prewarm demand: the pool reports how many more workers a model
	// needs to meet the target.
	if need := wp.Need("model-a", 3); need != 2 {
		t.Fatalf("need = %d, want 2", need)
	}
}

func TestLoRaLifecycle(t *testing.T) {
	ll := NewLoRaLifecycle(4) // capacity: 4 resident adapters
	if err := ll.Load("w1", "lora-x"); err != nil {
		t.Fatal(err)
	}
	if err := ll.Load("w1", "lora-y"); err != nil {
		t.Fatal(err)
	}
	// Popularity tracking: repeated loads raise the score.
	ll.Touch("w1", "lora-x")
	ll.Touch("w1", "lora-x")
	if !ll.Loaded("w1", "lora-x") {
		t.Fatal("lora-x must be loaded")
	}
	// Eviction on capacity: load 4 more → least popular evicted.
	for _, name := range []string{"a", "b", "c", "d"} {
		ll.Touch("w1", name)
		ll.Load("w1", name)
	}
	if ll.Loaded("w1", "lora-y") {
		t.Fatal("lora-y (least popular) must be evicted on capacity pressure")
	}
}

func TestCostOptimizer(t *testing.T) {
	co := NewCostOptimizer()
	// Background work on cheaper hardware: the optimizer picks the
	// cheapest accelerator family that can serve the class.
	co.SetFamilyCost("h100", 4.0)
	co.SetFamilyCost("a100", 1.0)
	got := co.PickFamily("batch", []string{"h100", "a100"})
	if got != "a100" {
		t.Fatalf("batch family = %s, want a100 (cheapest)", got)
	}
	// Interactive never downgrades to cheaper hardware when premium is
	// available (latency is the objective, not cost).
	got = co.PickFamily("interactive-paid", []string{"h100", "a100"})
	if got != "h100" {
		t.Fatalf("interactive family = %s, want h100 (premium)", got)
	}
}
