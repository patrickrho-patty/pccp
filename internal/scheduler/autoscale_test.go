package scheduler

import (
	"testing"
	"time"
)

func TestForecastWarmFloor(t *testing.T) {
	a := NewAutoscaler(DefaultAutoscaleConfig())
	// Historical pattern: 08:00 UTC weekday = heavy traffic.
	hist := []ForecastSample{
		{At: time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC), ActiveTokens: 50000},
		{At: time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC), ActiveTokens: 52000},
		{At: time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC), ActiveTokens: 48000},
	}
	a.TrainForecast(hist)
	// At the matching time-of-day, the warm floor must anticipate the
	// historical load — not react to it.
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	floor := a.WarmFloor(now)
	if floor < 40000 {
		t.Fatalf("warm floor = %d, want ≥40000 (forecast from history)", floor)
	}
}

func TestBurstFastLoopScalesUp(t *testing.T) {
	a := NewAutoscaler(DefaultAutoscaleConfig())
	a.SetFleet(FleetSignals{QueuedTokens: 0, KVUtilization: 0.3, ActivePrefillTok: 100, P95TTFTMs: 300, P95ITLMs: 30, AvailableReplicas: 5})

	// Queue pressure spikes: the fast loop must demand more capacity.
	a.SetFleet(FleetSignals{QueuedTokens: 800000, KVUtilization: 0.92, ActivePrefillTok: 15000, P95TTFTMs: 1500, P95ITLMs: 150, AvailableReplicas: 1})
	if a.FastLoopDemand() <= 0 {
		t.Fatal("burst pressure must produce positive scale-up demand")
	}
}

func TestWarmSpareAlwaysMaintained(t *testing.T) {
	// Spec §12.3.8: always maintain warm spare — GPU cold starts are not
	// Lambda cold starts.
	a := NewAutoscaler(DefaultAutoscaleConfig())
	if a.Config().WarmSpareReplicas <= 0 {
		t.Fatal("warm spare must be ≥1")
	}
	target := a.TargetReplicas(time.Now(), FleetSignals{QueuedTokens: 100, KVUtilization: 0.4, AvailableReplicas: 3})
	if target < 4 {
		t.Fatalf("target = %d, want ≥ available+spare", target)
	}
}

func TestScaleToZero(t *testing.T) {
	// Spec §14 row 39: scale-to-zero with idle timeout.
	a := NewAutoscaler(DefaultAutoscaleConfig())
	idle := time.Now().Add(-2 * time.Hour)
	target := a.TargetReplicas(time.Now(), FleetSignals{QueuedTokens: 0, AvailableReplicas: 3})
	_ = target
	if !a.ShouldScaleToZero(idle, time.Now()) {
		t.Fatal("prolonged idle must scale to zero")
	}
	if a.ShouldScaleToZero(time.Now().Add(-time.Minute), time.Now()) {
		t.Fatal("recent activity must not scale to zero")
	}
}

func TestForecastPreWarmDirective(t *testing.T) {
	// Spec §13.7: the long loop issues signed lifecycle directives to
	// pre-warm standby workers BEFORE the predicted burst.
	a := NewAutoscaler(DefaultAutoscaleConfig())
	hist := []ForecastSample{
		{At: time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC), ActiveTokens: 80000},
		{At: time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC), ActiveTokens: 85000},
	}
	a.TrainForecast(hist)
	// 30 minutes before the predicted burst: the directive must fire.
	now := time.Date(2026, 8, 12, 8, 30, 0, 0, time.UTC)
	dir := a.PrewarmDirective(now)
	if dir == nil || dir.Action != "prewarm" {
		t.Fatalf("directive = %+v, want prewarm before the burst", dir)
	}
	if dir.ETAUnix <= now.Unix() {
		t.Fatalf("prewarm must target the future burst, got %d", dir.ETAUnix)
	}
}
