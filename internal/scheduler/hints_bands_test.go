package scheduler

import (
	"testing"
	"time"
)

func TestEstimateBandsTrackAccuracy(t *testing.T) {
	e := NewOutputEstimator(DefaultEstimatorConfig())
	// Accurate, consistent completions: the band narrows.
	for i := 0; i < 50; i++ {
		e.ObserveCompletion(1000, 250)
	}
	if e.Uncertain() {
		t.Fatal("accurate history must not be uncertain")
	}
	est, low, high := e.EstimateWithBand(1000, 0, 4096)
	// Near-zero uncertainty: the band collapses onto the point estimate.
	if est != 250 || high-low > 2 || low < 1 {
		t.Fatalf("band = %d [%d,%d], want tight around 250", est, low, high)
	}

	// Wildly varying outputs: the band widens and the estimator reports
	// uncertainty.
	e2 := NewOutputEstimator(DefaultEstimatorConfig())
	for i := 0; i < 60; i++ {
		if i%2 == 0 {
			e2.ObserveCompletion(1000, 100)
		} else {
			e2.ObserveCompletion(1000, 900)
		}
	}
	if !e2.Uncertain() {
		t.Fatal("volatile history must report uncertainty")
	}
	est2, _, high2 := e2.EstimateWithBand(1000, 0, 4096)
	if high2 <= est2 {
		t.Fatalf("uncertain band must reserve above the point estimate: %d high %d", est2, high2)
	}

	// Explicit hints bypass learning but still produce a band shape.
	est3, _, _ := e2.EstimateWithBand(1000, 300, 4096)
	if est3 != 300 {
		t.Fatalf("hint est = %d, want 300", est3)
	}
}

func TestProgramTaskBudgetTracking(t *testing.T) {
	r := NewProgramRegistry(nil)
	now := time.Now()
	r.SetNow(func() time.Time { return now })

	// First turn sets the budget clock.
	r.Turn("p1", "tenant-a", "", CacheIdentity{}, "", 1, 100*time.Millisecond)
	if r.OverBudget() != 0 {
		t.Fatal("over-budget before the budget elapsed")
	}
	now = now.Add(200 * time.Millisecond)
	r.Turn("p1", "tenant-a", "", CacheIdentity{}, "", 2, 100*time.Millisecond)
	if r.OverBudget() != 1 {
		t.Fatalf("over-budget = %d, want 1", r.OverBudget())
	}

	// A program without a budget never counts.
	r.Turn("p2", "tenant-a", "", CacheIdentity{}, "", 1, 0)
	now = now.Add(time.Hour)
	r.Turn("p2", "tenant-a", "", CacheIdentity{}, "", 2, 0)
	if r.OverBudget() != 1 {
		t.Fatalf("over-budget = %d, want still 1 (budgetless programs never count)", r.OverBudget())
	}
}
