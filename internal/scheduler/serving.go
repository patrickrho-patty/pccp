package scheduler

import (
	"context"
	"encoding/json"
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
	batch      *BatchGateway
	autoscale  *Autoscaler
	prober     *HealthProber
}

// NewServing assembles the serving stack. The dispatcher's forwarder is
// the production DARI client to worker PIAs; the rewriter starts empty
// (admin config populates aliases/splits). Batch/autoscale/health are
// built here so the binary composes them with one call.
func NewServing() *Serving {
	d := NewDispatcher(nil)
	d.SetForwarder(NewDARIForwarder(nil, 0))
	g := NewGateway(d, nil)
	return &Serving{
		Gateway:    g,
		Dispatcher: d,
		batch:      NewBatchGateway(DefaultBatchConfig()),
		autoscale:  NewAutoscaler(DefaultAutoscaleConfig()),
		prober:     NewHealthProber(DefaultHealthConfig()),
	}
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
	// PAT-1445 internal views (KV directory, P/D capacity, programs, shadow).
	mux.Handle("/api/v1/kvdir", views)
	mux.Handle("/api/v1/pd", views)
	mux.Handle("/api/v1/programs", views)
	mux.Handle("/api/v1/shadow", views)
	mux.Handle("/api/v1/stages", views)

	// S9 batch gateway: submit/status/cancel (slack-gated dispatch).
	batch := svc.Serving.Batch()
	mux.HandleFunc("/api/v1/batch", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var job BatchJob
			if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
				writeGatewayError(w, http.StatusBadRequest, "invalid job")
				return
			}
			submitted, err := batch.Submit(job)
			if err != nil {
				writeGatewayError(w, http.StatusTooManyRequests, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(submitted)
		case http.MethodGet:
			id := r.URL.Query().Get("id")
			status, ok := batch.Status(id)
			if !ok {
				writeGatewayError(w, http.StatusNotFound, "job not found")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"id": id, "status": string(status)})
		case http.MethodDelete:
			id := r.URL.Query().Get("id")
			if !batch.Cancel(id) {
				writeGatewayError(w, http.StatusNotFound, "job not found")
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			writeGatewayError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	return mux
}

// selectorFor rebuilds the worker selector over the shared fleet (the
// card-driven worker view SyncRouter feeds; heartbeats refresh loads).
func (s *Serving) selectorFor(svc *Scheduler) *WorkerSelector {
	sel := NewWorkerSelector(svc.Fleet)
	for _, e := range svc.Registry.List() {
		sel.Upsert(e, 0)
		sel.SetLoad(e.Card.WorkerID, int(e.Card.ActiveSeqs), 0)
	}
	return sel
}

// Batch returns the scheduler's batch gateway (S9 wiring).
func (s *Serving) Batch() *BatchGateway { return s.batch }

// Autoscaler returns the dual-loop autoscaler (S7 wiring).
func (s *Serving) Autoscaler() *Autoscaler { return s.autoscale }

// HealthProber returns the active health prober (S8 wiring).
func (s *Serving) HealthProber() *HealthProber { return s.prober }
