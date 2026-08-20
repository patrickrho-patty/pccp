package scheduler

import (
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// trace.go implements PAT-1445 governed trace capture: the content-free
// scheduling measurements needed to replay routing decisions offline
// (baseline vs candidate). No prompt text, conversation, code, file path,
// repository, tool argument, or user identity ever enters a trace event;
// request IDs are scheduler-generated opaque identifiers and the tenant
// is the bounded flow key already used by the queue (research evaluation
// §baseline: content-free governed traces).

// TraceStage is the lifecycle boundary one event records.
type TraceStage string

const (
	TraceArrived   TraceStage = "arrived"
	TraceBound     TraceStage = "bound"
	TraceCompleted TraceStage = "completed"
	TraceExpired   TraceStage = "expired"
	TraceDrained   TraceStage = "drained"
	TraceCancelled TraceStage = "cancelled"
)

// TraceEvent is one content-free scheduling measurement.
type TraceEvent struct {
	AtUnixMs             int64      `json:"at_unix_ms"`
	RequestID            string     `json:"request_id"`
	Tenant               string     `json:"tenant"`
	Model                string     `json:"model"`
	Class                string     `json:"class"`
	Region               string     `json:"region,omitempty"`
	Stage                TraceStage `json:"stage"`
	InputTokens          int        `json:"input_tokens"`
	CachedTokens         int        `json:"cached_tokens,omitempty"`
	ExpectedOutputTokens int        `json:"expected_output_tokens"`
	MediaTokens          int        `json:"media_tokens,omitempty"`
	QueueWaitMs          int64      `json:"queue_wait_ms,omitempty"`
	WorkerID             string     `json:"worker_id,omitempty"`
	OutputTokens         int        `json:"output_tokens,omitempty"`
	PlanMode             string     `json:"plan_mode,omitempty"`
	TransferMs           float64    `json:"transfer_ms,omitempty"`
	Err                  string     `json:"err,omitempty"`
}

// traceEventFor adapts a queued request into an event for the given
// stage; the recorder stamps the timestamp.
func traceEventFor(r queue.Request, stage TraceStage) TraceEvent {
	model := ""
	if rp, ok := r.Payload.(RequestPayload); ok {
		model = rp.Model
	}
	return TraceEvent{
		RequestID:            r.ID,
		Tenant:               r.Tenant,
		Model:                model,
		Class:                string(r.Class),
		Region:               r.Region,
		Stage:                stage,
		InputTokens:          r.InputTokens,
		CachedTokens:         r.CachedInputTokens,
		ExpectedOutputTokens: r.ExpectedOutputTokens,
		MediaTokens:          r.MediaTokens,
	}
}

// TraceExport is the replay-ready envelope: events plus the policy and
// estimator versions that produced them (versioned traces).
type TraceExport struct {
	Versions map[string]string `json:"versions"`
	Events   []TraceEvent      `json:"events"`
}

// TraceRecorder is a bounded ring of trace events. Safe for concurrent
// use; the dispatch hot path only ever takes its own short lock.
type TraceRecorder struct {
	mu       sync.Mutex
	log      []TraceEvent
	max      int
	versions map[string]string
	now      func() int64
}

// NewTraceRecorder builds a bounded recorder (default 4096 events).
func NewTraceRecorder(max int) *TraceRecorder {
	if max <= 0 {
		max = 4096
	}
	return &TraceRecorder{
		max:      max,
		versions: make(map[string]string),
		now:      func() int64 { return time.Now().UnixMilli() },
	}
}

// SetVersion records the policy/estimator version stamped on exports.
func (t *TraceRecorder) SetVersion(component, version string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.versions[component] = version
}

// SetNow injects a clock (deterministic tests).
func (t *TraceRecorder) SetNow(fn func() int64) { t.now = fn }

// Add appends one event, stamping the timestamp when unset.
func (t *TraceRecorder) Add(e TraceEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e.AtUnixMs == 0 {
		e.AtUnixMs = t.now()
	}
	t.log = append(t.log, e)
	if len(t.log) > t.max {
		t.log = t.log[len(t.log)-t.max:]
	}
}

// Export snapshots the events plus their producing versions.
func (t *TraceRecorder) Export() TraceExport {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := TraceExport{
		Versions: make(map[string]string, len(t.versions)),
		Events:   make([]TraceEvent, len(t.log)),
	}
	for k, v := range t.versions {
		out.Versions[k] = v
	}
	copy(out.Events, t.log)
	return out
}
