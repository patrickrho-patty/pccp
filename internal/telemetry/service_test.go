package telemetry

import "testing"

func TestCounters(t *testing.T) {
	svc := New(nil)
	svc.IncrementCounter("requests", 1)
	svc.IncrementCounter("requests", 2)
	if svc.GetCounter("requests") != 3 {
		t.Fatalf("expected 3, got %d", svc.GetCounter("requests"))
	}
}

func TestGauges(t *testing.T) {
	svc := New(nil)
	svc.SetGauge("cpu", 0.75)
	if svc.GetGauge("cpu") != 0.75 {
		t.Fatalf("expected 0.75")
	}
}

func TestHistogram(t *testing.T) {
	svc := New(nil)
	for _, v := range []float64{10, 20, 30, 40, 50, 100, 200} {
		svc.RecordHistogram("latency", v)
	}
	stats := svc.GetHistogramStats("latency")
	if stats.Count != 7 {
		t.Fatalf("expected 7, got %d", stats.Count)
	}
	if stats.Min != 10 {
		t.Fatalf("expected min 10, got %.1f", stats.Min)
	}
	if stats.Max != 200 {
		t.Fatalf("expected max 200, got %.1f", stats.Max)
	}
}
