package scheduler

import "sync"

// rl.go implements the post-training/RL serving tier (spec §14 row 29,
// S7 optional tier): rollout pools are strictly separated from serving
// traffic; in-place weight updates arrive as directives executed
// out-of-band from inference.

// RLPoolManager separates rollout workers from serving workers.
type RLPoolManager struct {
	mu sync.RWMutex
	rl map[string]bool
}

// NewRLPoolManager builds the manager.
func NewRLPoolManager() *RLPoolManager {
	return &RLPoolManager{rl: make(map[string]bool)}
}

// MarkRL assigns a worker to the rollout pool.
func (p *RLPoolManager) MarkRL(workerID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rl[workerID] = true
}

// MarkServing assigns a worker to the serving pool (default).
func (p *RLPoolManager) MarkServing(workerID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.rl, workerID)
}

// IsRL reports whether the worker belongs to the rollout pool.
func (p *RLPoolManager) IsRL(workerID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.rl[workerID]
}

// PlaceRollout filters a candidate set to RL-pool workers only —
// serving workers never receive rollout traffic.
func (p *RLPoolManager) PlaceRollout(candidates []string) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []string
	for _, w := range candidates {
		if p.rl[w] {
			out = append(out, w)
		}
	}
	return out
}

// WeightUpdateDirective issues the in-place weight-update action for an
// RL worker (executed by the engine out-of-band from serving traffic).
func (p *RLPoolManager) WeightUpdateDirective(workerID, checkpoint string) LifecycleDirective {
	return LifecycleDirective{
		Action:   "weight_update",
		WorkerID: workerID,
		Reason:   checkpoint,
	}
}
