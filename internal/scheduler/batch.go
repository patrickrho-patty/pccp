package scheduler

import (
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// batch.go implements S9: the batch/async gateway with slack-capacity
// filling and token-level pause/resume (spec §13.10, §14 rows 26–27).
// Batch work is admitted only into slack; pause yields to interactive
// instantly; resume re-submits from the last produced token (the warm
// prefix is reused — cheap precisely because the KV index knows it).

// BatchStatus is a batch job's lifecycle state.
type BatchStatus string

const (
	BatchQueued    BatchStatus = "queued"
	BatchRunning   BatchStatus = "running"
	BatchPaused    BatchStatus = "paused"
	BatchCompleted BatchStatus = "completed"
	BatchFailed    BatchStatus = "failed"
	BatchExpired   BatchStatus = "expired"
	BatchCancelled BatchStatus = "cancelled"
)

// BatchConfig tunes the gateway.
type BatchConfig struct {
	MaxConcurrentJobs int
	DefaultJobTTL     time.Duration
	MaxPayloadBytes   int
}

// DefaultBatchConfig returns reference parameters.
func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		MaxConcurrentJobs: 1000,
		DefaultJobTTL:     24 * time.Hour,
		MaxPayloadBytes:   16 << 20,
	}
}

// BatchJob is one submitted batch inference job.
type BatchJob struct {
	ID          string
	Tenant      string
	Model       string
	Payload     []byte
	Deadline    time.Time
	Status      BatchStatus
	Produced    int // tokens produced so far (pause/resume cursor)
	SubmittedAt time.Time
}

// BatchGateway manages job lifecycle, tenant quotas, slack gating, and
// pause/resume. Safe for concurrent use.
type BatchGateway struct {
	mu        sync.Mutex
	cfg       BatchConfig
	jobs      map[string]*BatchJob
	queue     []string
	quotas    map[string]int
	tenantUse map[string]int
	saturated bool
}

// NewBatchGateway builds the gateway.
func NewBatchGateway(cfg BatchConfig) *BatchGateway {
	return &BatchGateway{
		cfg:       cfg,
		jobs:      make(map[string]*BatchJob),
		quotas:    make(map[string]int),
		tenantUse: make(map[string]int),
	}
}

// SetTenantQuota caps a tenant's concurrent batch jobs.
func (g *BatchGateway) SetTenantQuota(tenant string, n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.quotas[tenant] = n
}

// Submit admits a job (quota-checked; defaults applied).
func (g *BatchGateway) Submit(job BatchJob) (BatchJob, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if job.ID == "" {
		job.ID = dari.GenerateID("btj")
	}
	if _, exists := g.jobs[job.ID]; exists {
		return BatchJob{}, fmt.Errorf("batch: job %s already exists", job.ID)
	}
	if job.Deadline.IsZero() {
		job.Deadline = time.Now().Add(g.cfg.DefaultJobTTL)
	}
	if len(job.Payload) > g.cfg.MaxPayloadBytes {
		return BatchJob{}, fmt.Errorf("batch: payload %d bytes exceeds limit %d", len(job.Payload), g.cfg.MaxPayloadBytes)
	}
	if quota, ok := g.quotas[job.Tenant]; ok && g.tenantUse[job.Tenant] >= quota {
		return BatchJob{}, fmt.Errorf("batch: tenant %s quota exhausted", job.Tenant)
	}
	job.Status = BatchQueued
	job.SubmittedAt = time.Now()
	g.jobs[job.ID] = &job
	g.queue = append(g.queue, job.ID)
	g.tenantUse[job.Tenant]++
	return job, nil
}

// Status returns a job's lifecycle state.
func (g *BatchGateway) Status(jobID string) (BatchStatus, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	j, ok := g.jobs[jobID]
	if !ok {
		return "", false
	}
	return j.Status, true
}

// SetFleetSaturated flips the slack gate (fed by the overload policy).
func (g *BatchGateway) SetFleetSaturated(sat bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.saturated = sat
}

// DispatchOne releases the oldest queued job when the fleet has slack
// (spec §13.10: batch work admitted only into slack). Expired jobs are
// dropped; expired status is recorded. Returns nil when nothing runs.
func (g *BatchGateway) DispatchOne() *BatchJob {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.saturated {
		return nil
	}
	now := time.Now()
	for len(g.queue) > 0 {
		id := g.queue[0]
		g.queue = g.queue[1:]
		j, ok := g.jobs[id]
		if !ok {
			continue
		}
		if now.After(j.Deadline) {
			j.Status = BatchExpired
			g.tenantUse[j.Tenant]--
			continue
		}
		if j.Status != BatchQueued {
			continue
		}
		j.Status = BatchRunning
		out := *j
		return &out
	}
	return nil
}

// Pause suspends a running job at the given produced-token cursor
// (token-level pause yields to interactive instantly, spec §13.10).
func (g *BatchGateway) Pause(jobID string, producedTokens int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if j, ok := g.jobs[jobID]; ok && j.Status == BatchRunning {
		j.Status = BatchPaused
		j.Produced = producedTokens
	}
}

// ResumeFrom returns the token cursor a resumed job restarts from (the
// warm prefix, reused because the KV index knows it is warm).
func (g *BatchGateway) ResumeFrom(jobID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if j, ok := g.jobs[jobID]; ok {
		return j.Produced
	}
	return 0
}

// Resume requeues a paused job.
func (g *BatchGateway) Resume(jobID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if j, ok := g.jobs[jobID]; ok && j.Status == BatchPaused {
		j.Status = BatchQueued
		g.queue = append(g.queue, jobID)
	}
}

// Cancel removes a queued/paused job.
func (g *BatchGateway) Cancel(jobID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	j, ok := g.jobs[jobID]
	if !ok || j.Status == BatchCompleted || j.Status == BatchFailed || j.Status == BatchCancelled {
		return false
	}
	j.Status = BatchCancelled
	g.tenantUse[j.Tenant]--
	return true
}

// Complete marks a job finished.
func (g *BatchGateway) Complete(jobID string, failed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if j, ok := g.jobs[jobID]; ok {
		if failed {
			j.Status = BatchFailed
		} else {
			j.Status = BatchCompleted
		}
		g.tenantUse[j.Tenant]--
	}
}
