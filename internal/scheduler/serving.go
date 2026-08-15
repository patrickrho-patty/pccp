package scheduler

import (
	"context"
	"net/http"
)

// serving.go wires the S2 serving stack: gateway (OpenAI/Anthropic
// ingress), dispatcher (queue + late binding + overload gate), and the
// worker-facing DARI forwarder into the scheduler process. This is the
// production shape of the unified gateway (spec §14 rows 1–3).

// Serving is the composed S2 runtime.
type Serving struct {
	Gateway    *Gateway
	Dispatcher *Dispatcher
}

// NewServing assembles the serving stack. The dispatcher's forwarder is
// the production DARI client to worker PIAs; the rewriter starts empty
// (admin config populates aliases/splits).
func NewServing() *Serving {
	d := NewDispatcher(nil)
	d.SetForwarder(NewDARIForwarder(nil, 0))
	g := NewGateway(d, nil)
	return &Serving{Gateway: g, Dispatcher: d}
}

// Start runs the dispatch loop until ctx ends.
func (s *Serving) Start(ctx context.Context) {
	go s.Dispatcher.RunDispatchLoop(ctx)
}

// NewServingHandler returns the combined HTTP handler: the admin
// read-through API (S1.5), the unified gateway ingress (S2), and the
// S10 observability views.
func NewServingHandler(svc *Scheduler, adminToken string) http.Handler {
	admin := NewHTTPHandler(svc, adminToken)
	serving := svc.Serving

	mux := http.NewServeMux()
	mux.Handle("/api/", admin)
	mux.Handle("/healthz", admin)
	mux.HandleFunc("/v1/chat/completions", serving.Gateway.HandleChatCompletions)
	mux.HandleFunc("/v1/messages", serving.Gateway.HandleAnthropicMessages)
	mux.HandleFunc("/v1/models", serving.Gateway.HandleModelDiscovery)
	mux.HandleFunc("/v1/embeddings", serving.Gateway.HandleEmbeddings)

	// S10 observability views (fleet/queue/cache/perf/routing/scaling).
	obs := NewObservability(svc)
	views := obs.AdminViews("")
	mux.Handle("/api/v1/fleet", views)
	mux.Handle("/api/v1/queue", views)
	mux.Handle("/api/v1/cache", views)
	mux.Handle("/api/v1/perf", views)
	mux.Handle("/api/v1/routing", views)
	mux.Handle("/api/v1/scaling", views)
	return mux
}

// selectorFor rebuilds the worker selector from the live registry
// (card-driven worker discovery; heartbeats refresh loads). The selector
// is the dispatch-side view of the same signed cards the registry holds.
func (s *Serving) selectorFor(svc *Scheduler) *WorkerSelector {
	sel := NewWorkerSelector()
	for _, e := range svc.Registry.List() {
		sel.Upsert(e, 0)
		sel.SetLoad(e.Card.WorkerID, int(e.Card.ActiveSeqs), 0)
	}
	return sel
}
