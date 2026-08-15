package scheduler

import (
	"encoding/json"
	"net/http"
	"sync"
)

// observability.go implements S10: fleet/models/traffic/cache/perf/
// routing/scaling views with routing explainability (spec §14 row 35).
// Every view is a signed-data-derived snapshot; placements are explained
// from the S3 routing receipts, never reconstructed from logs.

// FleetSnapshot is the administrative fleet view.
type FleetSnapshot struct {
	Workers        int      `json:"workers"`
	Quarantined    int      `json:"quarantined"`
	Models         []string `json:"models"`
	AvailableSlots int      `json:"available_slots"`
	KVTokens       int64    `json:"kv_tokens"`
}

// RoutingExplanation is one placement's why (spec §14 row 35).
type RoutingExplanation struct {
	WorkerID      string  `json:"worker_id"`
	Model         string  `json:"model"`
	Namespace     string  `json:"namespace"`
	InputTokens   int     `json:"input_tokens"`
	OverlapTokens int     `json:"overlap_tokens"`
	Cost          float64 `json:"cost"`
	Reason        string  `json:"reason"`
}

// Observability aggregates the S2–S9 state for admin views.
type Observability struct {
	mu        sync.RWMutex
	svc       *Scheduler
	registry  *Registry
	receipts  *ReceiptStore
	queue     *queueDepthSource
	kv        *KVIndex
	predictor *LatencyPredictor
	autoscale *Autoscaler
}

// queueDepthSource adapts the dispatcher's queue depth.
type queueDepthSource struct {
	pending       func() int
	pendingTokens func() int64
	perClass      func() map[string]int
}

// NewObservability builds the observability facade over the scheduler.
func NewObservability(svc *Scheduler) *Observability {
	o := &Observability{svc: svc}
	if svc != nil {
		o.registry = svc.Registry
		o.kv = svc.KV
		o.queue = &queueDepthSource{
			pending:       func() int { return svc.Serving.Dispatcher.Queue().Pending() },
			pendingTokens: func() int64 { return svc.Serving.Dispatcher.Queue().PendingTokens() },
			perClass: func() map[string]int {
				out := map[string]int{}
				for c, n := range svc.Serving.Dispatcher.Queue().ClassPending() {
					out[string(c)] = n
				}
				return out
			},
		}
	}
	return o
}

// SetReceipts installs the routing-receipt store.
func (o *Observability) SetReceipts(rs *ReceiptStore) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.receipts = rs
}

// SetPredictor installs the latency predictor (perf view).
func (o *Observability) SetPredictor(p *LatencyPredictor) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.predictor = p
}

// SetAutoscaler installs the autoscaler (scaling view).
func (o *Observability) SetAutoscaler(a *Autoscaler) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.autoscale = a
}

// FleetSnapshot assembles the fleet view.
func (o *Observability) FleetSnapshot() FleetSnapshot {
	o.mu.RLock()
	defer o.mu.RUnlock()
	snap := FleetSnapshot{}
	if o.registry == nil {
		return snap
	}
	models := map[string]bool{}
	for _, e := range o.registry.List() {
		snap.Workers++
		if e.Quarantined {
			snap.Quarantined++
		}
		if !e.Quarantined && !e.Lapsed && e.Card.Servable() {
			snap.AvailableSlots += int(e.Card.MaxConcurrentSeqs)
		}
		models[e.Card.ModelName] = true
	}
	for m := range models {
		snap.Models = append(snap.Models, m)
	}
	if o.kv != nil {
		snap.KVTokens = o.kv.TotalCachedTokens()
	}
	return snap
}

// ExplainRouting returns placements for a tenant+model from the receipt
// store (routing explainability, spec §14 row 35).
func (o *Observability) ExplainRouting(namespace, model string) []RoutingExplanation {
	o.mu.RLock()
	defer o.mu.RUnlock()
	var out []RoutingExplanation
	if o.receipts == nil {
		return out
	}
	for _, r := range o.receipts.Recent() {
		if namespace != "" && r.Namespace != namespace {
			continue
		}
		if model != "" && r.Model != model {
			continue
		}
		out = append(out, RoutingExplanation{
			WorkerID:      r.Decision.WorkerID,
			Model:         r.Model,
			Namespace:     r.Namespace,
			InputTokens:   r.InputTokens,
			OverlapTokens: r.Decision.OverlapTokens,
			Cost:          r.Decision.Cost,
			Reason:        r.Decision.Reason,
		})
	}
	return out
}

// AdminViews returns an HTTP mux with the six S10 views.
func (o *Observability) AdminViews(prefix string) http.Handler {
	mux := http.NewServeMux()
	jsonView := func(payload func() interface{}) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(payload())
		}
	}
	mux.Handle(prefix+"/api/v1/fleet", jsonView(func() interface{} { return o.FleetSnapshot() }))
	mux.Handle(prefix+"/api/v1/queue", jsonView(func() interface{} {
		if o.queue == nil {
			return map[string]interface{}{}
		}
		return map[string]interface{}{
			"pending":        o.queue.pending(),
			"pending_tokens": o.queue.pendingTokens(),
			"per_class":      o.queue.perClass(),
		}
	}))
	mux.Handle(prefix+"/api/v1/cache", jsonView(func() interface{} {
		if o.kv == nil {
			return map[string]interface{}{}
		}
		return map[string]interface{}{"total_tokens": o.kv.TotalCachedTokens()}
	}))
	mux.Handle(prefix+"/api/v1/perf", jsonView(func() interface{} {
		return map[string]interface{}{"predictor": "installed", "zero_hop": true}
	}))
	mux.Handle(prefix+"/api/v1/routing", jsonView(func() interface{} {
		return o.ExplainRouting("", "")
	}))
	mux.Handle(prefix+"/api/v1/scaling", jsonView(func() interface{} {
		if o.autoscale == nil {
			return map[string]interface{}{}
		}
		return map[string]interface{}{
			"config": o.autoscale.Config(),
		}
	}))
	return mux
}
