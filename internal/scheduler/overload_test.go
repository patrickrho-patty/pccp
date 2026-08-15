package scheduler

import (
	"testing"
	"time"
)

func healthySignals() FleetSignals {
	return FleetSignals{
		QueuedTokens:      100,
		P95TTFTMs:         400,
		P95ITLMs:          40,
		KVUtilization:     0.5,
		ActivePrefillTok:  1000,
		ActiveDecodeKV:    500,
		AvailableReplicas: 3,
	}
}

func TestOverloadAdmitsWhenHealthy(t *testing.T) {
	p := DefaultOverloadPolicy()
	if v := p.Evaluate(healthySignals()); v != VerdictAdmit {
		t.Fatalf("healthy fleet verdict = %v, want admit", v)
	}
}

func TestOverloadShedsBatchWhenSaturated(t *testing.T) {
	p := DefaultOverloadPolicy()
	s := healthySignals()
	s.KVUtilization = 0.98
	s.QueuedTokens = 2_000_000

	// Sheddable classes (batch/background) are rejected immediately —
	// retryable 429.
	if v := p.EvaluateFor(s, "batch"); v != VerdictShed {
		t.Fatalf("saturated batch verdict = %v, want shed", v)
	}
	if v := p.EvaluateFor(s, "background-agent"); v != VerdictShed {
		t.Fatalf("saturated background verdict = %v, want shed", v)
	}
	// Interactive classes are held, not shed: they get the wait budget.
	if v := p.EvaluateFor(s, "interactive-paid"); v != VerdictWait {
		t.Fatalf("saturated interactive verdict = %v, want wait", v)
	}
}

func TestOverloadEverySignalTriggers(t *testing.T) {
	p := DefaultOverloadPolicy()
	cases := []struct {
		name   string
		mutate func(*FleetSignals)
	}{
		{"queued tokens", func(s *FleetSignals) { s.QueuedTokens = 5_000_000 }},
		{"p95 ttft", func(s *FleetSignals) { s.P95TTFTMs = 5000 }},
		{"p95 itl", func(s *FleetSignals) { s.P95ITLMs = 800 }},
		{"kv utilization", func(s *FleetSignals) { s.KVUtilization = 0.99 }},
		{"active prefill", func(s *FleetSignals) { s.ActivePrefillTok = 50_000 }},
		{"active decode kv", func(s *FleetSignals) { s.ActiveDecodeKV = 40_000 }},
		{"available replicas", func(s *FleetSignals) { s.AvailableReplicas = 0 }},
	}
	for _, c := range cases {
		s := healthySignals()
		c.mutate(&s)
		if v := p.Evaluate(s); v == VerdictAdmit {
			t.Errorf("%s: saturated signal did not block admission", c.name)
		}
	}
}

func TestOverloadWaitBudgetBounded(t *testing.T) {
	// Wait budget is the edge-admission hold ceiling (spec §12.3.7: short
	// bounded wait, then reject/retry). Assert it is configured and sane:
	// strictly positive and short (seconds, not minutes).
	p := DefaultOverloadPolicy()
	if p.WaitBudget <= 0 {
		t.Fatal("wait budget must be positive")
	}
	if p.WaitBudget > 10*time.Second {
		t.Fatalf("wait budget %v too long for an overload gate", p.WaitBudget)
	}
}

func TestWorkerLocalQueueAdmission(t *testing.T) {
	// Layer 2: worker-local queues stay small to keep continuous batching
	// saturated without parking long tails on the engine (spec §12.3.7).
	w := WorkerLoad{MaxConcurrent: 8, LocalQueued: 0, Active: 7}
	if !w.CanAccept() {
		t.Fatal("worker with free slot should accept")
	}
	w.Active = 8
	if w.CanAccept() {
		t.Fatal("worker at max concurrency should not accept")
	}
	if w.LocalQueued >= 2 {
		t.Fatal("worker-local queue must be a small buffer, not a parking lot")
	}
	w.LocalQueued = 2
	if w.CanAccept() {
		t.Fatal("worker-local buffer full should not accept")
	}
}
