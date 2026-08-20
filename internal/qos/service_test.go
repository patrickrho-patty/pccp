package qos

import (
	"math"
	"sort"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func qosDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/qos.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{&models.QoSRequestEvent{}, &models.QoSRollup{}, &models.QoSForecast{}} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// Percentile MUST be order-independent (sorted distribution), unlike the old
// insertion-ordered telemetry path: p50 of [1..100] is ~50.5 regardless of
// insertion order.
func TestPercentileIsSortedNotInsertionOrder(t *testing.T) {
	asc := make([]float64, 100)
	desc := make([]float64, 100)
	for i := 0; i < 100; i++ {
		asc[i] = float64(i + 1)
		desc[i] = float64(100 - i)
	}
	// Insertion-order (buggy) would give different p50 for desc.
	if Percentile(asc, 0.5) != 50.5 || Percentile(desc, 0.5) != 50.5 {
		t.Fatalf("percentile order-dependent: asc=%v desc=%v", Percentile(asc, 0.5), Percentile(desc, 0.5))
	}
	if Percentile(asc, 0.99) < 98 || Percentile(asc, 0.01) > 2 {
		t.Fatalf("edge percentiles wrong: %v %v", Percentile(asc, 0.01), Percentile(asc, 0.99))
	}
}

// Lifecycle traces count once: ingress→queued→left→completed is one terminal
// with a valid queue wait; a retried trace is separate.
func TestLifecycleCountsOnceWithWait(t *testing.T) {
	db := qosDB(t)
	svc := New(db)
	base := time.Now().UTC().Add(-time.Minute)
	// Trace A: complete lifecycle.
	svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "ingress", TrafficClass: "interactive-normal", Model: "m1", OccurredAt: base})
	svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "queued", TrafficClass: "interactive-normal", Model: "m1", OccurredAt: base, TraceKey: "tA", StratumKey: Stratum("public", "m1", "", "", "interactive-normal", "")})
	svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "left", TrafficClass: "interactive-normal", Model: "m1", OccurredAt: base.Add(200 * time.Millisecond), TraceKey: "tA", StratumKey: Stratum("public", "m1", "", "", "interactive-normal", "")})
	svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "completed", TrafficClass: "interactive-normal", Model: "m1", OccurredAt: base.Add(900 * time.Millisecond), TraceKey: "tA", StratumKey: Stratum("public", "m1", "", "", "interactive-normal", "")})
	// Trace B: retried (separate trace, separate terminal).
	svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "ingress", TrafficClass: "interactive-normal", Model: "m1", OccurredAt: base.Add(time.Second)})
	svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "queued", TrafficClass: "interactive-normal", Model: "m1", OccurredAt: base.Add(time.Second), TraceKey: "tB", StratumKey: Stratum("public", "m1", "", "", "interactive-normal", "")})
	svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "left", TrafficClass: "interactive-normal", Model: "m1", OccurredAt: base.Add(1500 * time.Millisecond), TraceKey: "tB", StratumKey: Stratum("public", "m1", "", "", "interactive-normal", "")})
	svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "completed", TrafficClass: "interactive-normal", Model: "m1", OccurredAt: base.Add(2 * time.Second), TraceKey: "tB", StratumKey: Stratum("public", "m1", "", "", "interactive-normal", "")})

	stats, err := svc.QueueWaitStats("public", base.Add(-time.Minute), "m1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Completions != 2 {
		t.Fatalf("expected 2 terminal completions (no double count): %d", stats.Completions)
	}
	if stats.Arrivals != 2 {
		t.Fatalf("expected 2 arrivals: %d", stats.Arrivals)
	}
	if stats.WaitSampleN != 2 {
		t.Fatalf("expected 2 wait samples: %d", stats.WaitSampleN)
	}
}

// Insufficient samples → forecast withheld (health=insufficient), never a
// fabricated zero-time claim.
func TestForecastWithheldWhenUndersampled(t *testing.T) {
	svc := New(qosDB(t))
	f, err := svc.Forecast("public", "m1", "", "", "interactive-normal", 10)
	if err != nil {
		t.Fatal(err)
	}
	if f.Health != "insufficient" {
		t.Fatalf("undersampled forecast must be withheld: %+v", f)
	}
}

// EWMA forecast over a known distribution produces a sane p50 within range and
// records calibration metrics.
func TestForecastEstimateAndCalibration(t *testing.T) {
	db := qosDB(t)
	svc := New(db)
	base := time.Now().UTC().Add(-30 * time.Minute)
	stratum := Stratum("public", "m2", "", "", "batch", "")
	// 40 completed requests with waits from 100ms..400ms.
	for i := 0; i < 40; i++ {
		wait := time.Duration(100+i*8) * time.Millisecond
		queuedAt := base.Add(time.Duration(i) * time.Second)
		svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "queued", TrafficClass: "batch", Model: "m2", OccurredAt: queuedAt, TraceKey: "f" + itoa(i), StratumKey: stratum})
		svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "left", TrafficClass: "batch", Model: "m2", OccurredAt: queuedAt.Add(wait), TraceKey: "f" + itoa(i), StratumKey: stratum})
		svc.RecordLifecycle(models.QoSRequestEvent{Deployment: "public", Lifecycle: "completed", TrafficClass: "batch", Model: "m2", OccurredAt: queuedAt.Add(wait + 50*time.Millisecond), TraceKey: "f" + itoa(i), StratumKey: stratum})
	}
	f, err := svc.Forecast("public", "m2", "", "", "batch", 10)
	if err != nil {
		t.Fatal(err)
	}
	if f.Health != "healthy" {
		t.Fatalf("adequate sample should be healthy: %+v", f)
	}
	// Actual p50 is ~ 100 + 19*8 = 252ms; EWMA prediction should be within range.
	if f.P50WaitMs < 100 || f.P50WaitMs > 500 {
		t.Fatalf("forecast p50 out of plausible range: %v", f.P50WaitMs)
	}
	if f.SampleN < 30 {
		t.Fatalf("sample count too low: %d", f.SampleN)
	}
	if math.IsNaN(f.MAEP50Ms) || f.MAEP50Ms < 0 {
		t.Fatalf("calibration invalid: %+v", f)
	}
}

// Stratum groups bounded dimensions deterministically.
func TestStratumDeterministic(t *testing.T) {
	a := Stratum("public", "m1", "kr-seoul", "a100", "interactive-normal", "s")
	b := Stratum("public", "m1", "kr-seoul", "a100", "interactive-normal", "s")
	if a != b {
		t.Fatalf("stratum not deterministic: %q vs %q", a, b)
	}
}

func itoa(i int) string {
	return string(rune(97+i%26)) + string(rune(97+(i%26+7)%26)) + string(rune(48+i%10)) + string(rune(48+i/10%10))
}

var _ = sort.Float64s
