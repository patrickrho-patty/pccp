package scheduler

import (
	"sync"
	"time"
)

// program.go implements PAT-1445 WS3 agent-program-aware scheduling:
// opaque program/turn continuity and tool-pause signals drive KV
// residency decisions through the WS1 directory. The registry holds NO
// content — opaque identifiers, timing, and cache-residency decisions
// only; a client hint is never an authorization or resource guarantee.

// KVAction is the residency decision for a program's cache state.
type KVAction string

const (
	KVActionNone     KVAction = "none"     // no known cache state to act on
	KVActionRetain   KVAction = "retain"   // short pause: keep HBM residence
	KVActionDemote   KVAction = "demote"   // long pause: release HBM, keep state
	KVActionPrefetch KVAction = "prefetch" // continuation likely: promote back
)

// pauseRetainBelowMs bounds the "short pause" bucket: pauses estimated
// shorter than this retain HBM residence; longer estimates demote so
// unrelated traffic keeps fair capacity (WS3 §tool-pause-aware KV).
const pauseRetainBelowMs = 30_000

// programState is one program's scheduling continuity.
type programState struct {
	namespace    string
	turns        int
	paused       bool
	pausedAt     time.Time
	pauseEMAMs   float64 // bounded historical pause-duration estimate
	pauseSamples int
	prefixHash   string // last cache identity the program used (when known)
	identity     CacheIdentity
	workerID     string // worker holding the program's KV residence
	predictErrs  int    // pause-duration mispredictions (calibration)
}

// ProgramRegistry tracks agent-program scheduling state. Safe for
// concurrent use.
type ProgramRegistry struct {
	mu       sync.Mutex
	programs map[string]*programState
	dir      *KVDirectory
	now      func() time.Time
}

// NewProgramRegistry builds the registry over the KV directory (nil
// directory = decisions are computed but not applied).
func NewProgramRegistry(dir *KVDirectory) *ProgramRegistry {
	return &ProgramRegistry{
		programs: make(map[string]*programState),
		dir:      dir,
		now:      time.Now,
	}
}

// SetNow injects a clock (deterministic tests).
func (r *ProgramRegistry) SetNow(fn func() time.Time) { r.now = fn }

// Turn records one program turn's scheduling metadata. When the program
// was tool-paused, the turn is a continuation: the registry restores the
// program's KV residence (prefetch) and folds the observed pause into
// the bounded estimate (WS3 predicted-vs-actual calibration).
func (r *ProgramRegistry) Turn(programID, namespace, prefixHash string, id CacheIdentity, workerID string, turnSeq int) KVAction {
	if programID == "" {
		return KVActionNone
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.programs[programID]
	if p == nil {
		p = &programState{}
		r.programs[programID] = p
	}
	p.namespace = namespace
	p.turns++
	if prefixHash != "" {
		p.prefixHash = prefixHash
		p.identity = id
	}
	if workerID != "" {
		p.workerID = workerID
	}
	if !p.paused {
		return KVActionNone
	}
	// Continuation after a pause: calibrate the estimate and restore.
	observed := r.now().Sub(p.pausedAt).Milliseconds()
	p.paused = false
	if p.pauseSamples > 0 {
		est := p.pauseEMAMs
		p.pauseEMAMs = est*0.7 + float64(observed)*0.3
		if observed < int64(est)/2 {
			p.predictErrs++ // resumed far earlier than predicted
		}
	} else {
		p.pauseEMAMs = float64(observed)
	}
	p.pauseSamples++
	if r.dir == nil || p.prefixHash == "" || p.workerID == "" {
		return KVActionNone
	}
	r.dir.Promote(p.workerID, p.namespace, p.prefixHash, p.identity, L1GPU)
	return KVActionPrefetch
}

// ToolPaused marks the program waiting on a tool call: estimate the
// likely pause from bounded history and decide the KV residency action —
// short pauses retain HBM, long pauses demote to L2 so unrelated traffic
// keeps fair capacity while governed state survives (WS3 §tool-pause).
func (r *ProgramRegistry) ToolPaused(programID string) KVAction {
	if programID == "" {
		return KVActionNone
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.programs[programID]
	if p == nil {
		return KVActionNone
	}
	p.paused = true
	p.pausedAt = r.now()
	if r.dir == nil || p.prefixHash == "" || p.workerID == "" {
		return KVActionNone
	}
	if p.pauseSamples > 0 && p.pauseEMAMs >= pauseRetainBelowMs {
		r.dir.Demote(p.workerID, p.namespace, p.prefixHash, p.identity, L2CPU)
		return KVActionDemote
	}
	// Short (or unestimated) pause: retain residence but keep the
	// directory's last-use fresh so the sweep cannot reap it mid-pause.
	r.dir.Hit(p.namespace, p.prefixHash, p.identity)
	return KVActionRetain
}

// Paused reports whether a program is currently tool-paused.
func (r *ProgramRegistry) Paused(programID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.programs[programID]
	return p != nil && p.paused
}

// Stats exposes bounded registry counters for observability.
func (r *ProgramRegistry) Stats() (programs, paused, predictErrs int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	programs = len(r.programs)
	for _, p := range r.programs {
		if p.paused {
			paused++
		}
		predictErrs += p.predictErrs
	}
	return programs, paused, predictErrs
}
