package scheduler

import (
	"fmt"
	"sync"
	"time"
)

// canary.go implements PAT-1445 §canary: one independent capability at a
// time, explicit success thresholds, observation windows, automatic
// pause/rollback, and operator review with audit evidence. The candidate
// computes in shadow first; the controller promotes it to scoped-active
// only after the shadow evidence clears the thresholds, and pauses it on
// regression — pause always wins over promote.

// Canary states.
const (
	CanaryShadow     = "shadow"     // candidate computes, never decides
	CanaryEvaluating = "evaluating" // gathering shadow evidence in-window
	CanaryActive     = "active"     // promoted: decides for its scope
	CanaryPaused     = "paused"     // auto-rollback: back to shadow
)

// Canary audit event types (transitions land on the signed evidence log).
const (
	EventCanaryActive = "canary.active"
	EventCanaryPaused = "canary.paused"
	EventCanaryShadow = "canary.shadow"
)

// CanaryConfig gates one capability's promotion.
type CanaryConfig struct {
	Capability   string        // capability name (e.g. "stage-planner/v1")
	Candidate    Router        // the shadowed candidate
	ScopePool    string        // model/pool scope ("" = scheduler-wide, not default)
	MinSamples   int           // receipts required before any promotion
	MinAgreement float64       // shadow agreement rate required to promote/stay
	Window       time.Duration // observation window over the receipt stream
}

// CanaryController evaluates the shadow receipt stream and drives the
// capability's state machine. Safe for concurrent use.
type CanaryController struct {
	mu       sync.Mutex
	cfg      CanaryConfig
	state    string
	evidence *EvidenceLog
	now      func() time.Time
}

// NewCanaryController builds the controller in the shadow state.
func NewCanaryController(cfg CanaryConfig, ev *EvidenceLog) *CanaryController {
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 100
	}
	if cfg.MinAgreement <= 0 || cfg.MinAgreement > 1 {
		cfg.MinAgreement = 0.95
	}
	if cfg.Window <= 0 {
		cfg.Window = 30 * time.Minute
	}
	return &CanaryController{
		cfg:      cfg,
		state:    CanaryShadow,
		evidence: ev,
		now:      time.Now,
	}
}

// SetNow injects a clock (deterministic tests).
func (c *CanaryController) SetNow(fn func() time.Time) { c.now = fn }

// State returns the current canary state.
func (c *CanaryController) State() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Active reports whether the candidate currently decides for its scope.
func (c *CanaryController) Active() bool { return c.State() == CanaryActive }

// Capability returns the gated capability name.
func (c *CanaryController) Capability() string { return c.cfg.Capability }

// Evaluate consumes the shadow receipt stream and applies transitions:
// below MinSamples the controller waits; with enough in-window samples,
// agreement at/above threshold promotes, below threshold pauses. A
// paused canary ignores further evaluation until an operator resets it.
func (c *CanaryController) Evaluate(receipts []RoutingReceipt) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == CanaryPaused {
		return c.state
	}
	cutoff := c.now().Add(-c.cfg.Window)
	samples, agree := 0, 0
	for _, r := range receipts {
		if r.Shadow == nil || r.Shadow.CandidateVersion != c.cfg.Candidate.Version() {
			continue
		}
		if time.UnixMilli(r.AtUnixMs).Before(cutoff) {
			continue
		}
		samples++
		if r.Shadow.Agree {
			agree++
		}
	}
	if c.state == CanaryShadow {
		c.state = CanaryEvaluating
	}
	if samples < c.cfg.MinSamples {
		return c.state
	}
	rate := float64(agree) / float64(samples)
	if rate >= c.cfg.MinAgreement {
		if c.state != CanaryActive {
			c.transitionLocked(CanaryActive, EventCanaryActive,
				fmt.Sprintf("promoted: %d samples, agreement %.3f >= %.3f", samples, rate, c.cfg.MinAgreement))
		}
		return c.state
	}
	c.transitionLocked(CanaryPaused, EventCanaryPaused,
		fmt.Sprintf("auto-pause: agreement %.3f < %.3f over %d samples", rate, c.cfg.MinAgreement, samples))
	return c.state
}

// Reset returns a paused canary to shadow (operator review required —
// the audit log records who reset it).
func (c *CanaryController) Reset(operator string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != CanaryPaused {
		return
	}
	c.transitionLocked(CanaryShadow, EventCanaryShadow, "operator reset by "+operator)
}

// transitionLocked records the state change on the signed evidence log.
func (c *CanaryController) transitionLocked(to, eventType, reason string) {
	c.state = to
	if c.evidence != nil {
		c.evidence.Emit(eventType, c.cfg.Capability, reason)
	}
}
