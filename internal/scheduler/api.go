package scheduler

import (
	"encoding/json"
	"net/http"
	"strings"
)

// NewHTTPHandler returns the scheduler's admin HTTP API (DARI scheduler §9):
// health, worker read-through, and per-worker evidence. Admin auth is a
// bearer token; empty token disables auth (dev).
func NewHTTPHandler(svc *Scheduler, adminToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if adminToken == "" {
			return true
		}
		auth := r.Header.Get("Authorization")
		if auth == "Bearer "+adminToken {
			return true
		}
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}

	mux.HandleFunc("/api/v1/workers", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svc.Registry.List())
	})

	mux.HandleFunc("/api/v1/workers/", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		workerID := strings.TrimPrefix(r.URL.Path, "/api/v1/workers/")
		if workerID == "" {
			http.Error(w, `{"error":"missing worker id"}`, http.StatusBadRequest)
			return
		}
		entry, ok := svc.Registry.Get(workerID)
		if !ok {
			http.Error(w, `{"error":"worker not found"}`, http.StatusNotFound)
			return
		}
		var events []EvidenceEvent
		for _, e := range svc.Evidence.Events() {
			if e.WorkerID == workerID {
				events = append(events, e)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entry":  entry,
			"events": events,
		})
	})

	return mux
}
