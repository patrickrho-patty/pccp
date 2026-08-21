package scheduler

import (
	"sync"
	"time"
)

// stagequeue.go implements the WS2 stage-queue measurement plane
// (PAT-1445 criterion 7): the scheduler separately measures and reasons
// over lookup/retrieval, prefill, KV transfer, and decode queues. Depth
// and wait feed the stage planner's backpressure decisions (transfer
// contention restores co-located execution).

// StageID names one measured execution stage.
type StageID string

const (
	StageLookup   StageID = "lookup"   // admission + KV lookup (arrival → bind)
	StagePrefill  StageID = "prefill"  // prefill execution
	StageTransfer StageID = "transfer" // prefill→decode KV movement
	StageDecode   StageID = "decode"   // decode admission + execution
)

// StageQueueStat is one stage's measurement snapshot.
type StageQueueStat struct {
	Depth     int   `json:"depth"`
	AvgWaitMs int64 `json:"avg_wait_ms"`
	MaxWaitMs int64 `json:"max_wait_ms"`
	Completed int64 `json:"completed"`
}

type waitStat struct {
	totalMs   int64
	maxMs     int64
	completed int64
}

// StageQueues tracks per-stage depth and wait. Safe for concurrent use.
type StageQueues struct {
	mu      sync.Mutex
	depths  map[StageID]int
	waits   map[StageID]*waitStat
	pending map[string]int64 // requestID\x00stage → enter unix ms
	now     func() time.Time
}

// NewStageQueues builds the empty measurement plane.
func NewStageQueues() *StageQueues {
	return &StageQueues{
		depths:  make(map[StageID]int),
		waits:   make(map[StageID]*waitStat),
		pending: make(map[string]int64),
		now:     time.Now,
	}
}

// SetNow injects a clock (deterministic tests).
func (q *StageQueues) SetNow(fn func() time.Time) { q.now = fn }

// Enter marks a request entering a stage (depth +1).
func (q *StageQueues) Enter(stage StageID, requestID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.depths[stage]++
	q.pending[requestID+"\x00"+string(stage)] = q.now().UnixMilli()
}

// Leave marks a request leaving a stage (depth −1, wait recorded).
func (q *StageQueues) Leave(stage StageID, requestID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.depths[stage] > 0 {
		q.depths[stage]--
	}
	key := requestID + "\x00" + string(stage)
	entered, ok := q.pending[key]
	if !ok {
		return
	}
	delete(q.pending, key)
	wait := q.now().UnixMilli() - entered
	if wait < 0 {
		wait = 0
	}
	ws := q.waits[stage]
	if ws == nil {
		ws = &waitStat{}
		q.waits[stage] = ws
	}
	ws.totalMs += wait
	ws.completed++
	if wait > ws.maxMs {
		ws.maxMs = wait
	}
}

// Depth returns the current stage depth.
func (q *StageQueues) Depth(stage StageID) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.depths[stage]
}

// Snapshot returns every stage's measurement (all four stages, zero
// when untouched).
func (q *StageQueues) Snapshot() map[StageID]StageQueueStat {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make(map[StageID]StageQueueStat, 4)
	for _, stage := range []StageID{StageLookup, StagePrefill, StageTransfer, StageDecode} {
		stat := StageQueueStat{Depth: q.depths[stage]}
		if ws := q.waits[stage]; ws != nil && ws.completed > 0 {
			stat.AvgWaitMs = ws.totalMs / ws.completed
			stat.MaxWaitMs = ws.maxMs
			stat.Completed = ws.completed
		}
		out[stage] = stat
	}
	return out
}
