package scheduler

import (
	"sync"
	"time"
)

// pdcontrol.go implements the PAT-1445 WS2 dynamic-capacity controller:
// whether a model engages prefill/decode disaggregation is decided with
// hysteresis around the sustained prefill-share threshold, a cooldown
// between flips, and a minimum co-located capacity invariant — so load
// reversals never oscillate the fleet (WS2 §dynamic capacity, §edge
// cases: P/D imbalance reversal). It also owns the TBT-guarded prefill
// deflection check. The controller RECOMMENDS state; actuation (role
// changes via signed lifecycle directives) stays with S7.

// PDController is the dynamic P/D engagement brain. Safe for concurrent use.
type PDController struct {
	planner   *PDPlanner
	predictor *LatencyPredictorPair

	engageAbove  float64       // sustained prefill share to engage disaggregation
	releaseBelow float64       // release back below this share (hysteresis band)
	cooldown     time.Duration // minimum time between engagement flips
	minColocated int           // co-located workers retained per model, always

	mu       sync.Mutex
	engaged  map[string]bool
	lastFlip map[string]time.Time
	now      func() time.Time
}

// NewPDController builds the controller with reference parameters
// (WS2: hysteresis band 0.55/0.65, 2-minute cooldown, one co-located
// worker always retained).
func NewPDController(planner *PDPlanner, predictor *LatencyPredictorPair) *PDController {
	return &PDController{
		planner:      planner,
		predictor:    predictor,
		engageAbove:  0.65,
		releaseBelow: 0.55,
		cooldown:     2 * time.Minute,
		minColocated: 1,
		engaged:      make(map[string]bool),
		lastFlip:     make(map[string]time.Time),
		now:          time.Now,
	}
}

// SetNow injects a clock (deterministic tests).
func (c *PDController) SetNow(fn func() time.Time) { c.now = fn }

// MinColocated returns the per-model co-located capacity floor.
func (c *PDController) MinColocated() int { return c.minColocated }

// Engaged reports whether disaggregation is currently engaged for a model.
func (c *PDController) Engaged(model string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.engaged[model]
}

// Evaluate folds one sustained prefill-share sample into the engagement
// state: engage above engageAbove, release below releaseBelow, never flip
// inside the cooldown window (hysteresis + cooldown per WS2).
func (c *PDController) Evaluate(model string, share float64) bool {
	if c.planner != nil {
		c.planner.ObservePrefillShare(model, share)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	engaged := c.engaged[model]
	last, flipped := c.lastFlip[model]
	if flipped && c.now().Sub(last) < c.cooldown {
		return engaged // inside cooldown: hold the current state
	}
	switch {
	case !engaged && share > c.engageAbove:
		c.engaged[model] = true
		c.lastFlip[model] = c.now()
		return true
	case engaged && share < c.releaseBelow:
		c.engaged[model] = false
		c.lastFlip[model] = c.now()
		return false
	}
	return engaged
}

// MayDeflect reports whether a decode worker may execute a bounded
// chunked prefill right now (WS2 §prefill deflection): only when the
// predictor has enough evidence to trust its TBT estimate (low-volume
// models withhold learned estimates) AND the predicted TBT violation
// risk for the decode side stays below maxRisk — existing decode TBT
// SLOs are protected first. No predictor, no deflection (conservative).
func (c *PDController) MayDeflect(configID string, f PredictorFeatures, tbtSLOMs, maxRisk float64) bool {
	const minEvidence = 30 // completions before a TBT estimate is trusted
	if c.predictor == nil || tbtSLOMs <= 0 {
		return false
	}
	if c.predictor.TPOT.Evidence(configID) < minEvidence {
		return false
	}
	return c.predictor.PITLViolation(configID, f, tbtSLOMs) < maxRisk
}
