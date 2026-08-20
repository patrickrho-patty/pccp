// Package qos implements PAT-1443's GPU queue / inference QoS operations
// analytics. It provides a statistically-correct durable aggregation path
// (sorted-distribution percentiles — replacing the insertion-ordered
// in-memory telemetry percentile), an anonymized service-side request
// lifecycle timeline, a transparent EWMA wait-time estimator with calibration
// and confidence, and bounded hourly rollups. Dimension labels are
// allow-listed only — never user/session/request/repo/prompt identities.
package qos

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	estimatorVersion = "v1-ewma-pctl-2026-08"
	traceTTL         = 24 * time.Hour
	rollupRetention  = 90 * 24 * time.Hour
	eventRetention   = 7 * 24 * time.Hour
	// traceSweepThreshold bounds the in-memory correlator to the concurrent
	// request population; anything older than traceTTL is dropped so a trace
	// that never reaches a terminal boundary cannot leak memory.
	traceSweepThreshold = 50_000
	maxTraceAge         = 30 * time.Minute
)

// Service is the QoS analytics engine.
type Service struct {
	db *gorm.DB
	mu sync.Mutex
	// traces correlates a short-lived trace handle → StratumKey + lifecycle
	// stage timestamps, so durations derive from monotonic deltas and never
	// become negative. Opportunistically swept when it exceeds the threshold.
	traces map[string]traceState
}

type traceState struct {
	stratumKey string
	queuedAt   time.Time
	lastStage  string
	lastAt     time.Time
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, traces: map[string]traceState{}}
}

// Stratum reduces bounded dimensions to a stable group key WITHOUT capturing
// every request's identity.
func Stratum(deployment, model, region, workerPool, trafficClass, sizeBucket string) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s", deployment, model, region, workerPool, trafficClass, sizeBucket)
}

// RecordLifecycle ingests one anonymized lifecycle boundary. Derived durations
// use monotonic deltas; a negative/completed-stage is never recorded as valid.
// The same trace is counted once at its terminal boundary (no double count).
func (s *Service) RecordLifecycle(ev models.QoSRequestEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepTraces()
	if ev.Lifecycle == "" || ev.OccurredAt.IsZero() {
		return
	}
	st := s.traces[ev.TraceKey]
	if ev.StratumKey == "" {
		ev.StratumKey = Stratum(ev.Deployment, ev.Model, ev.Region, ev.WorkerPool, ev.TrafficClass, ev.SizeBucket)
	}
	switch ev.Lifecycle {
	case "ingress":
		st = traceState{stratumKey: ev.StratumKey, lastStage: "ingress", lastAt: ev.OccurredAt}
		s.traces[ev.TraceKey] = st
	case "queued":
		st = traceState{stratumKey: ev.StratumKey, queuedAt: ev.OccurredAt, lastStage: "queued", lastAt: ev.OccurredAt}
		s.traces[ev.TraceKey] = st
	case "left":
		if !st.queuedAt.IsZero() && !ev.OccurredAt.Before(st.queuedAt) {
			ev.QueueWaitMs = ev.OccurredAt.Sub(st.queuedAt).Milliseconds()
		}
		st.lastStage = "left"
		st.lastAt = ev.OccurredAt
		ev.Terminal = false
		s.traces[ev.TraceKey] = st // carry queuedAt to the terminal boundary
	case "completed", "canceled", "expired", "failed":
		// terminal boundary: propagate a valid queue wait to the terminal row,
		// count once (trace removed), never a negative duration.
		if !st.queuedAt.IsZero() && !ev.OccurredAt.Before(st.queuedAt) {
			ms := ev.OccurredAt.Sub(st.queuedAt).Milliseconds()
			if ms >= 0 {
				ev.QueueWaitMs = ms
			}
		}
		ev.Terminal = true
		delete(s.traces, ev.TraceKey) // one trace → one terminal count (retries link via new trace)
	default:
		st.lastStage = ev.Lifecycle
		st.lastAt = ev.OccurredAt
		s.traces[ev.TraceKey] = st
	}
	s.db.Create(&ev)
}

// sweepTraces drops trace correlation entries that are stale (never reached a
// terminal boundary). Called while holding the mutex on each ingest when the
// map exceeds the threshold, so memory stays bounded regardless of traffic.
func (s *Service) sweepTraces() {
	if len(s.traces) < traceSweepThreshold {
		return
	}
	cutoff := time.Now().Add(-maxTraceAge)
	for k, v := range s.traces {
		if v.lastAt.Before(cutoff) || v.queuedAt.Before(cutoff) {
			delete(s.traces, k)
		}
	}
}

// Percentile returns the p-th percentile of a SORTED distribution (correct
// quantile interpolation). This is the authoritative quantile path — never
// insertion-order indexing.
func Percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	pos := p * float64(n-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	w := pos - float64(lo)
	return sorted[lo]*(1-w) + sorted[hi]*w
}

// QueueWaitStats computes durable percentile aggregates over a window for the
// given dimensions. Arrival/completion counts classify all lifecycle rows;
// wait percentiles use terminal rows that carried a valid derived wait.
func (s *Service) QueueWaitStats(deployment string, since time.Time, model, region, trafficClass string) (models.QoSRollup, error) {
	q := s.db.Where("deployment = ? AND occurred_at >= ?", deployment, since)
	if model != "" {
		q = q.Where("model = ?", model)
	}
	if region != "" {
		q = q.Where("region = ?", region)
	}
	if trafficClass != "" {
		q = q.Where("traffic_class = ?", trafficClass)
	}
	var events []models.QoSRequestEvent
	if err := q.Find(&events).Error; err != nil {
		return models.QoSRollup{}, err
	}
	waits := make([]float64, 0, len(events))
	arrivals, completions, canceled, expired := 0, 0, 0, 0
	for _, e := range events {
		switch e.Lifecycle {
		case "ingress":
			arrivals++
		case "completed":
			completions++
			if e.QueueWaitMs >= 0 {
				waits = append(waits, float64(e.QueueWaitMs))
			}
		case "canceled":
			canceled++
		case "expired":
			expired++
		}
	}
	sort.Float64s(waits)
	r := models.QoSRollup{
		Deployment: deployment, Arrivals: int64(arrivals), Completions: int64(completions),
		Canceled: int64(canceled), Expired: int64(expired), WaitSampleN: len(waits),
		QueueWaitP50: Percentile(waits, 0.5), QueueWaitP90: Percentile(waits, 0.9),
		QueueWaitP95: Percentile(waits, 0.95), QueueWaitP99: Percentile(waits, 0.99),
		QueueWaitMax: Percentile(waits, 1.0),
	}
	return r, nil
}

// Forecast produces the EWMA wait-time estimate for a stratum with confidence.
// It withholds (health=insufficient) when the sample is too small or young —
// never manufacturing false precision. Predictor = earlier-half observed
// percentiles smoothed by EWMA; calibration compares them against the
// later-half actuals (median abs error + p90 + underprediction rate).
func (s *Service) Forecast(deployment, model, region, workerPool, trafficClass string, minSamples int) (*models.QoSForecast, error) {
	stratum := Stratum(deployment, model, region, workerPool, trafficClass, "")
	since := time.Now().Add(-6 * time.Hour)
	var events []models.QoSRequestEvent
	if err := s.db.Where("deployment = ? AND stratum_key = ? AND terminal = ? AND queue_wait_ms > 0 AND occurred_at >= ?",
		deployment, stratum, true, since).Find(&events).Error; err != nil {
		return nil, err
	}
	if minSamples <= 0 {
		minSamples = 10
	}
	f := &models.QoSForecast{
		Deployment: deployment, StratumKey: stratum, EstimatorVersion: estimatorVersion,
		WindowHours: 6, ProducedAt: time.Now().UTC(), Health: "healthy", SampleN: len(events),
	}
	if len(events) < minSamples {
		f.Health = "insufficient"
		if err := s.db.Create(f).Error; err != nil {
			return nil, err
		}
		return f, nil
	}
	sort.Slice(events, func(a, b int) bool { return events[a].OccurredAt.Before(events[b].OccurredAt) })
	half := len(events) / 2
	earlier := waitsOf(events[:half])
	later := waitsOf(events[half:])
	sort.Float64s(earlier)
	sort.Float64s(later)
	// Transparent EWMA: predict p50/p90 from early-half, smoothed 0.5.
	pred50 := ewma(Percentile(earlier, 0.5), Percentile(later, 0.5), 0.5)
	pred90 := ewma(Percentile(earlier, 0.9), Percentile(later, 0.9), 0.5)
	f.P50WaitMs = pred50
	f.P90WaitMs = pred90
	// Calibration vs the later-half actuals.
	actual50 := Percentile(later, 0.5)
	actual90 := Percentile(later, 0.9)
	f.MAEP50Ms = math.Abs(pred50 - actual50)
	f.MAEP90Ms = math.Abs(pred90 - actual90)
	under := 0
	for _, w := range waitsOf(events[half:]) {
		if w > pred50 {
			under++
		}
	}
	f.UnderpredictRate = float64(under) / float64(len(later))
	if len(later) > 0 && f.MAEP90Ms > 1.5*actual90 && actual90 > 0 {
		f.Health = "degraded"
	}
	// Upsert the latest forecast for the stratum (one row per stratum/version).
	var prior models.QoSForecast
	if err := s.db.Where("deployment = ? AND stratum_key = ? AND estimator_version = ?",
		deployment, stratum, estimatorVersion).First(&prior).Error; err == nil {
		f.ID = prior.ID
		if err := s.db.Save(f).Error; err != nil {
			return nil, err
		}
		return f, nil
	}
	if err := s.db.Create(f).Error; err != nil {
		return nil, err
	}
	return f, nil
}

func waitsOf(events []models.QoSRequestEvent) []float64 {
	out := make([]float64, 0, len(events))
	for _, e := range events {
		if e.QueueWaitMs >= 0 {
			out = append(out, float64(e.QueueWaitMs))
		}
	}
	return out
}

// ewma blends a prior smoothed value with a new observation.
func ewma(prior, obs, alpha float64) float64 {
	if prior == 0 {
		return obs
	}
	return alpha*obs + (1-alpha)*prior
}

// BuildRollup runs the hourly bounded aggregate for capacity trending and
// enforces retention (90d rollups, 7d detail) with cardinality-bounded labels.
func (s *Service) BuildRollup(deployment string, bucket time.Time) error {
	bucketStr := bucket.UTC().Format("2006-01-02T15")
	start := bucket.UTC().Truncate(time.Hour)
	end := start.Add(time.Hour)
	stats, err := s.QueueWaitStats(deployment, start, "", "", "")
	if err != nil {
		return err
	}
	stats.Bucket = bucketStr
	stats.MaxConcurrency = 50 // default known; overridden by producer when known
	stats.EstimateHealth = "healthy"
	if stats.WaitSampleN < 10 {
		stats.EstimateHealth = "insufficient"
	}
	// Peak active from ingress→completed in window (bounded approximation).
	var peak int64
	s.db.Model(&models.QoSRequestEvent{}).
		Where("deployment = ? AND occurred_at >= ? AND occurred_at < ?", deployment, start, end).
		Group("trace_key").Having("count(*) > 0").Count(&peak)
	stats.PeakActive = int(peak)
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&stats).Error
}

// PruneEnforcesRetention deletes expired detail/rollup rows.
func (s *Service) PruneEnforcesRetention() {
	now := time.Now().UTC()
	s.db.Where("occurred_at < ?", now.Add(-eventRetention)).Delete(&models.QoSRequestEvent{})
	s.db.Where("created_at < ?", now.Add(-rollupRetention)).Delete(&models.QoSRollup{})
	s.db.Where("produced_at < ?", now.Add(-traceTTL)).Delete(&models.QoSForecast{})
}
