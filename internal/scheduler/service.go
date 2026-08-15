package scheduler

import (
	"sort"
	"sync"
	"time"
)

// Service implements the Fair Scheduler (PCCP v2 §10C.7).
// Weighted fairness across accounts prevents one user's subagents
// from monopolizing a shared GPU pool.
type Service struct {
	mu      sync.Mutex
	queues  map[string]*AccountQueue // accountID → queue
	pending []QueuedRequest          // FIFO + priority queue
}

// AccountQueue tracks an account's queue position and slot usage.
type AccountQueue struct {
	AccountID       string
	PlanWeight      int
	QueueAge        time.Duration
	EnqueuedAt      time.Time
	ActiveSlots     int
	MaxSlots        int
	CurrentCLU      float64
	BurstCLU        float64
	SustainedWindow float64
	Class           string // INTERACTIVE, SUBAGENT, HEAVY_CONTEXT, BACKGROUND
}

// QueuedRequest represents a request waiting for admission.
type QueuedRequest struct {
	AccountID    string
	CatalogModel string
	Class        string
	EstimatedCLU float64
	EnqueuedAt   time.Time
	Priority     float64
}

// New creates a new fair scheduler.
func New() *Service {
	return &Service{queues: make(map[string]*AccountQueue), pending: []QueuedRequest{}}
}

// Enqueue adds a request to the scheduler queue.
func (s *Service) Enqueue(req QueuedRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req.EnqueuedAt.IsZero() {
		req.EnqueuedAt = time.Now()
	}

	// Ensure account exists in queues
	if _, ok := s.queues[req.AccountID]; !ok {
		s.queues[req.AccountID] = &AccountQueue{
			AccountID:  req.AccountID,
			EnqueuedAt: time.Now(),
		}
	}

	s.pending = append(s.pending, req)
}

// computePriority calculates the weighted priority for a queued request.
// Higher priority = admitted sooner.
// Per §10C.7: considers queue age, plan weight, active slots, CLU, class.
func (s *Service) computePriority(aq *AccountQueue, req QueuedRequest) float64 {
	ageBonus := time.Since(aq.EnqueuedAt).Seconds() * 0.1

	planWeight := float64(aq.PlanWeight)
	if planWeight == 0 {
		planWeight = 1
	}

	// Penalize accounts that are already using many slots
	slotPenalty := float64(aq.ActiveSlots) * 5.0

	// Penalize high CLU usage
	cluPenalty := aq.CurrentCLU * 0.01

	// Interactive requests get priority over background
	classBonus := 0.0
	switch req.Class {
	case "INTERACTIVE":
		classBonus = 10.0
	case "SUBAGENT":
		classBonus = 5.0
	case "HEAVY_CONTEXT":
		classBonus = 2.0
	case "BACKGROUND":
		classBonus = 0.0
	}

	// Starvation prevention: if queue age is very long, boost priority
	starvationBonus := 0.0
	if ageBonus > 30 {
		starvationBonus = ageBonus * 2
	}

	priority := planWeight + ageBonus + classBonus - slotPenalty - cluPenalty + starvationBonus
	return priority
}

// Admit selects the next request to admit based on weighted fairness.
// Returns nil if no request can be admitted (all accounts at capacity).
func (s *Service) Admit(maxGlobalSlots int) *QueuedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Count total active slots
	totalActive := 0
	for _, aq := range s.queues {
		totalActive += aq.ActiveSlots
	}
	if totalActive >= maxGlobalSlots {
		return nil
	}

	if len(s.pending) == 0 {
		return nil
	}

	// Score all pending requests
	type scoredReq struct {
		idx      int
		priority float64
	}
	var scored []scoredReq
	for i, req := range s.pending {
		aq := s.queues[req.AccountID]
		if aq != nil && aq.ActiveSlots >= aq.MaxSlots && aq.MaxSlots > 0 {
			continue // Account at capacity
		}
		scored = append(scored, scoredReq{i, s.computePriority(aq, req)})
	}

	if len(scored) == 0 {
		return nil
	}

	// Sort by priority (highest first)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].priority > scored[j].priority
	})

	// Admit the winner
	winnerIdx := scored[0].idx
	winner := s.pending[winnerIdx]

	// Remove from pending
	s.pending = append(s.pending[:winnerIdx], s.pending[winnerIdx+1:]...)

	// Update account queue
	if aq, ok := s.queues[winner.AccountID]; ok {
		aq.ActiveSlots++
	}

	return &winner
}

// Release frees a slot for an account.
func (s *Service) Release(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if aq, ok := s.queues[accountID]; ok {
		if aq.ActiveSlots > 0 {
			aq.ActiveSlots--
		}
	}
}

// UpdateAccount updates the scheduler's view of an account's state.
func (s *Service) UpdateAccount(accountID string, planWeight, maxSlots int, class string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	aq, ok := s.queues[accountID]
	if !ok {
		aq = &AccountQueue{AccountID: accountID}
		s.queues[accountID] = aq
	}
	aq.PlanWeight = planWeight
	aq.MaxSlots = maxSlots
	aq.Class = class
}

// UpdateCLU updates the current Compute Load Units for an account.
func (s *Service) UpdateCLU(accountID string, clu float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if aq, ok := s.queues[accountID]; ok {
		aq.CurrentCLU = clu
	}
}

// QueueDepth returns the total number of accounts with active queues.
func (s *Service) QueueDepth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queues)
}

// ActiveSlots returns the total active slots across all accounts.
func (s *Service) ActiveSlots() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, aq := range s.queues {
		total += aq.ActiveSlots
	}
	return total
}

// QueueStats returns per-account queue statistics.
type QueueStats struct {
	AccountID   string  `json:"account_id"`
	ActiveSlots int     `json:"active_slots"`
	MaxSlots    int     `json:"max_slots"`
	CurrentCLU  float64 `json:"current_clu"`
	Class       string  `json:"class"`
}

// GetStats returns queue statistics for all accounts.
func (s *Service) GetStats() []QueueStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	var stats []QueueStats
	for _, aq := range s.queues {
		stats = append(stats, QueueStats{
			AccountID:   aq.AccountID,
			ActiveSlots: aq.ActiveSlots,
			MaxSlots:    aq.MaxSlots,
			CurrentCLU:  aq.CurrentCLU,
			Class:       aq.Class,
		})
	}
	return stats
}
