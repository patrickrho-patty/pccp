package scheduler

import (
	"testing"
	"time"
)

func canaryReceipts(candidate string, n, agreeing int, at time.Time) []RoutingReceipt {
	out := make([]RoutingReceipt, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, RoutingReceipt{
			AtUnixMs: at.UnixMilli(),
			Shadow:   &ShadowRecord{CandidateVersion: candidate, Agree: i < agreeing},
		})
	}
	return out
}

func TestCanaryPromotesOnEvidence(t *testing.T) {
	ev := NewEvidenceLog(nil)
	now := time.Now()
	c := NewCanaryController(CanaryConfig{
		Capability:   "stage-planner/v1",
		Candidate:    &stubRouter{version: "cand/v1"},
		ScopePool:    "pool-blue",
		MinSamples:   4,
		MinAgreement: 0.75,
		Window:       time.Hour,
	}, ev)
	c.SetNow(func() time.Time { return now })

	// Below MinSamples: evaluating, never active.
	if got := c.Evaluate(canaryReceipts("cand/v1", 2, 2, now)); got != CanaryEvaluating {
		t.Fatalf("state = %s, want evaluating", got)
	}
	// Enough samples at 100% agreement: promote.
	if got := c.Evaluate(canaryReceipts("cand/v1", 4, 4, now)); got != CanaryActive {
		t.Fatalf("state = %s, want active", got)
	}
	if !c.Active() {
		t.Fatal("candidate should be active")
	}
	found := false
	for _, e := range ev.Events() {
		if e.EventType == EventCanaryActive {
			found = true
		}
	}
	if !found {
		t.Fatal("promotion not audited on the evidence log")
	}
}

func TestCanaryAutoPauseOnRegression(t *testing.T) {
	ev := NewEvidenceLog(nil)
	now := time.Now()
	c := NewCanaryController(CanaryConfig{
		Capability:   "stage-planner/v1",
		Candidate:    &stubRouter{version: "cand/v1"},
		MinSamples:   4,
		MinAgreement: 0.75,
		Window:       time.Hour,
	}, ev)
	c.SetNow(func() time.Time { return now })

	// 1/4 agreement is below threshold with enough samples: auto-pause
	// straight from evaluating (never promoted).
	if got := c.Evaluate(canaryReceipts("cand/v1", 4, 1, now)); got != CanaryPaused {
		t.Fatalf("state = %s, want paused", got)
	}
	// A paused canary ignores further evidence until an operator resets.
	if got := c.Evaluate(canaryReceipts("cand/v1", 10, 10, now)); got != CanaryPaused {
		t.Fatalf("paused canary re-evaluated to %s", got)
	}
	c.Reset("sre-choi")
	if c.State() != CanaryShadow {
		t.Fatalf("after reset state = %s, want shadow", c.State())
	}
	types := map[string]bool{}
	for _, e := range ev.Events() {
		types[e.EventType] = true
	}
	if !types[EventCanaryPaused] || !types[EventCanaryShadow] {
		t.Fatalf("audit events = %v", types)
	}
}

func TestCanaryActiveRegressesToPaused(t *testing.T) {
	now := time.Now()
	c := NewCanaryController(CanaryConfig{
		Capability:   "stage-planner/v1",
		Candidate:    &stubRouter{version: "cand/v1"},
		MinSamples:   4,
		MinAgreement: 0.75,
		Window:       time.Hour,
	}, nil)
	c.SetNow(func() time.Time { return now })

	if got := c.Evaluate(canaryReceipts("cand/v1", 4, 4, now)); got != CanaryActive {
		t.Fatalf("state = %s, want active", got)
	}
	// Regression while active: pause wins over promote.
	if got := c.Evaluate(canaryReceipts("cand/v1", 4, 0, now)); got != CanaryPaused {
		t.Fatalf("state = %s, want paused on regression", got)
	}
	if c.Active() {
		t.Fatal("paused canary must not decide traffic")
	}
}

func TestCanaryIgnoresStaleAndForeignReceipts(t *testing.T) {
	now := time.Now()
	c := NewCanaryController(CanaryConfig{
		Capability:   "stage-planner/v1",
		Candidate:    &stubRouter{version: "cand/v1"},
		MinSamples:   2,
		MinAgreement: 0.5,
		Window:       time.Minute,
	}, nil)
	c.SetNow(func() time.Time { return now })

	// Receipts from a different candidate version and outside the window
	// must not count toward promotion.
	mixed := canaryReceipts("other/v9", 5, 5, now)
	mixed = append(mixed, canaryReceipts("cand/v1", 5, 5, now.Add(-2*time.Hour))...)
	if got := c.Evaluate(mixed); got != CanaryEvaluating {
		t.Fatalf("state = %s, want evaluating (no usable samples)", got)
	}
}
