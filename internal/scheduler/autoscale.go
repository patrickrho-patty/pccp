package scheduler

import (
	"sort"
	"sync"
	"time"
)

// autoscale.go implements S7 dual-loop autoscaling (spec §12.3.8):
// a long forecast loop (minutes) sets the warm capacity floor from
// history; a fast loop (seconds) scales up on live fleet signals; warm
// spare is always maintained. Scaling decisions are issued as signed
// lifecycle directives (spec §13.7) — the fleet never scales on raw GPU
// utilization.

// ForecastSample is one historical traffic observation.
type ForecastSample struct {
	At           time.Time
	ActiveTokens int
}

// AutoscaleConfig tunes both loops.
type AutoscaleConfig struct {
	WarmSpareReplicas     int
	TokensPerReplica      int
	ForecastWindowMinutes int
	PrewarmLeadMinutes    int
	BurstQueueTokens      int // fast-loop trigger: queued token debit
	BurstKVUtilization    float64
	ScaleToZeroIdle       time.Duration
	FastLoopScaleStep     int
}

// DefaultAutoscaleConfig returns reference parameters.
func DefaultAutoscaleConfig() AutoscaleConfig {
	return AutoscaleConfig{
		WarmSpareReplicas:     1,
		TokensPerReplica:      20000,
		ForecastWindowMinutes: 30,
		PrewarmLeadMinutes:    30,
		BurstQueueTokens:      500000,
		BurstKVUtilization:    0.9,
		ScaleToZeroIdle:       time.Hour,
		FastLoopScaleStep:     2,
	}
}

// LifecycleDirective is a signed action to the fleet (S7 engine
// lifecycle; spec §13.7 pre-warm directives).
type LifecycleDirective struct {
	Action   string `json:"action"` // prewarm | sleep | wake | drain | terminate
	WorkerID string `json:"worker_id,omitempty"`
	Model    string `json:"model,omitempty"`
	ETAUnix  int64  `json:"eta_unix,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Autoscaler implements the dual loop. Safe for concurrent use.
type Autoscaler struct {
	mu       sync.Mutex
	cfg      AutoscaleConfig
	history  []ForecastSample
	fleet    FleetSignals
	lastFast time.Time
}

// NewAutoscaler builds an autoscaler.
func NewAutoscaler(cfg AutoscaleConfig) *Autoscaler {
	return &Autoscaler{cfg: cfg}
}

// Config returns the autoscaler configuration.
func (a *Autoscaler) Config() AutoscaleConfig { return a.cfg }

// TrainForecast installs the historical traffic series (long loop).
func (a *Autoscaler) TrainForecast(samples []ForecastSample) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = append([]ForecastSample(nil), samples...)
	sort.Slice(a.history, func(i, j int) bool { return a.history[i].At.Before(a.history[j].At) })
}

// SetFleet updates the live fleet signals (fast loop input).
func (a *Autoscaler) SetFleet(s FleetSignals) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fleet = s
	a.lastFast = time.Now()
}

// WarmFloor predicts the token demand for now from the same time-of-day
// history (spec §12.3.8 long loop: traffic forecast, time-of-day/MAU
// patterns → warm capacity floor).
func (a *Autoscaler) WarmFloor(now time.Time) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	window := time.Duration(a.cfg.ForecastWindowMinutes) * time.Minute
	var total, n int
	for _, s := range a.history {
		// Same time-of-day across history (any date).
		if sameTimeOfDay(s.At, now, window) {
			total += s.ActiveTokens
			n++
		}
	}
	if n == 0 {
		// No matching history: fall back to the recent average.
		var recentTotal, recentN int
		for _, s := range a.history {
			if now.Sub(s.At) < 24*time.Hour*7 {
				recentTotal += s.ActiveTokens
				recentN++
			}
		}
		if recentN == 0 {
			return 0
		}
		return recentTotal / recentN
	}
	return total / n
}

// sameTimeOfDay reports whether t falls within ±window/2 of the
// time-of-day of ref.
func sameTimeOfDay(ref, t time.Time, window time.Duration) bool {
	refTOD := ref.Hour()*3600 + ref.Minute()*60 + ref.Second()
	tTOD := t.Hour()*3600 + t.Minute()*60 + t.Second()
	half := int(window.Seconds() / 2)
	diff := refTOD - tTOD
	if diff < 0 {
		diff = -diff
	}
	// Handle midnight wrap.
	if diff > 12*3600 {
		diff = 24*3600 - diff
	}
	return diff <= half
}

// FastLoopDemand returns the scale-up demand driven by live pressure
// (queue tokens, KV%, TTFT, ITL, active prefills). Zero when healthy.
func (a *Autoscaler) FastLoopDemand() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.fleet
	demand := 0
	if f.QueuedTokens > int64(a.cfg.BurstQueueTokens) {
		demand += a.cfg.FastLoopScaleStep
	}
	if f.KVUtilization > a.cfg.BurstKVUtilization {
		demand += a.cfg.FastLoopScaleStep
	}
	if f.P95TTFTMs > 1500 || f.P95ITLMs > 150 {
		demand += a.cfg.FastLoopScaleStep
	}
	return demand
}

// TargetReplicas computes the desired fleet size: forecast floor plus
// fast-loop demand, always at least available + warm spare (spec §12.3.8:
// warm spare is core, not optional).
func (a *Autoscaler) TargetReplicas(now time.Time, f FleetSignals) int {
	floorTokens := a.WarmFloor(now)
	floor := floorTokens / a.cfg.TokensPerReplica
	demand := a.FastLoopDemand()
	target := floor + demand + a.cfg.WarmSpareReplicas
	if f.AvailableReplicas+a.cfg.WarmSpareReplicas > target {
		target = f.AvailableReplicas + a.cfg.WarmSpareReplicas
	}
	if target < a.cfg.WarmSpareReplicas {
		target = a.cfg.WarmSpareReplicas
	}
	return target
}

// ShouldScaleToZero reports whether the fleet has been idle long enough
// (spec §14 row 39 — scale-to-zero with a cooldown, never on a blip).
func (a *Autoscaler) ShouldScaleToZero(lastActivity, now time.Time) bool {
	if lastActivity.IsZero() {
		return false
	}
	return now.Sub(lastActivity) > a.cfg.ScaleToZeroIdle
}

// PrewarmDirective issues the signed pre-warm action before a predicted
// burst (spec §13.7): the long loop does not just predict counts — it
// warms standby workers BEFORE the traffic arrives.
func (a *Autoscaler) PrewarmDirective(now time.Time) *LifecycleDirective {
	a.mu.Lock()
	defer a.mu.Unlock()
	lead := time.Duration(a.cfg.PrewarmLeadMinutes) * time.Minute
	best := -1
	bestDelta := int64(1 << 62)
	for i, s := range a.history {
		// Look ahead: historical bursts whose time-of-day lands between
		// now and now+lead.
		burstAt := time.Date(now.Year(), now.Month(), now.Day(), s.At.Hour(), s.At.Minute(), 0, 0, now.Location())
		delta := burstAt.Unix() - now.Unix()
		if delta <= 0 || delta > int64(lead.Seconds()) {
			continue
		}
		if delta < bestDelta {
			bestDelta = delta
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	// Only fire inside the prewarm lead window.
	if bestDelta > int64(lead.Seconds()) {
		return nil
	}
	s := a.history[best]
	burstAt := time.Date(now.Year(), now.Month(), now.Day(), s.At.Hour(), s.At.Minute(), 0, 0, now.Location())
	replicas := s.ActiveTokens / a.cfg.TokensPerReplica
	if replicas < 1 {
		replicas = 1
	}
	return &LifecycleDirective{
		Action:  "prewarm",
		ETAUnix: burstAt.Unix(),
		Reason:  "forecasted burst",
	}
}
