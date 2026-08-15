package scheduler

import "testing"

func TestRequestHintsExposesLockedFields(t *testing.T) {
	// Spec §12.3.3: per request, internally expose input_tokens,
	// cached_input_tokens, expected_output_tokens, max_output_tokens,
	// media_tokens, request_class, tenant_priority.
	h := RequestHints{
		InputTokens:          1000,
		CachedInputTokens:    600,
		ExpectedOutputTokens: 250,
		MaxOutputTokens:      1024,
		MediaTokens:          3,
		RequestClass:         "S",
		TenantPriority:       "interactive-paid",
	}
	if h.InputTokens != 1000 || h.CachedInputTokens != 600 || h.ExpectedOutputTokens != 250 {
		t.Fatal("hint fields lost")
	}
}

func TestExpectedOutputEstimate(t *testing.T) {
	est := NewOutputEstimator(DefaultEstimatorConfig())
	// Bounded by max_output_tokens; defaults when no signal yet.
	got := est.Estimate(1000, 0, 512)
	if got <= 0 || got > 512 {
		t.Fatalf("estimate %d out of bounds [1,512]", got)
	}
	// Explicit expected length is taken as-is (bounded).
	got = est.Estimate(1000, 200, 512)
	if got != 200 {
		t.Fatalf("explicit estimate = %d, want 200", got)
	}
	// Estimate capped at max output tokens.
	got = est.Estimate(1000, 900, 512)
	if got != 512 {
		t.Fatalf("capped estimate = %d, want 512", got)
	}
}

func TestRemainingOutputDecaysAsRequestProgresses(t *testing.T) {
	// Spec §12.3.3: remaining expected length decays a request's projected
	// future load as it nears completion.
	est := NewOutputEstimator(DefaultEstimatorConfig())
	r := est.Track(500, 400, 800) // total expected 400
	if r.ProjectedRemaining() != 400 {
		t.Fatalf("initial remaining = %d, want 400", r.ProjectedRemaining())
	}
	r.NoteProduced(150)
	if rem := r.ProjectedRemaining(); rem != 250 {
		t.Fatalf("remaining after 150 produced = %d, want 250", rem)
	}
	r.NoteProduced(250)
	if rem := r.ProjectedRemaining(); rem != 0 {
		t.Fatalf("remaining after completion = %d, want 0", rem)
	}
}

func TestEstimatorLearnsFromCompletions(t *testing.T) {
	est := NewOutputEstimator(DefaultEstimatorConfig())
	// A workload that consistently produces 400 tokens for 500-token inputs
	// must move the default estimate toward 400.
	for i := 0; i < 50; i++ {
		est.ObserveCompletion(500, 400)
	}
	got := est.Estimate(500, 0, 1024)
	if got < 300 || got > 500 {
		t.Fatalf("learned estimate = %d, want near 400", got)
	}
}
