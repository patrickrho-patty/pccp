package pia

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// Server is the PIA HTTP server that exposes an OpenAI-compatible API
// for the Relay to call. It translates Relay requests to the local serving
// engine and wraps them with lease verification.
type Server struct {
	svc *Service
}

// NewServer creates a new PIA HTTP server.
func NewServer(svc *Service) *Server {
	return &Server{svc: svc}
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/models", s.handleListModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"status":       "ok",
		"pia_peer_id":  s.svc.PeerID(),
		"has_lease":    s.svc.HasValidLease(),
		"endpoint_id":  s.svc.EndpointID(),
		"public_key":   s.svc.PublicKeyHex(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models := map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{
				"id":       "patty-kocoder-v1",
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "patty",
			},
		},
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req InferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Verify lease before processing
	if !s.svc.HasValidLease() {
		// Try to refresh
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := s.svc.RequestLease(ctx); err != nil {
			writeError(w, http.StatusForbidden, "no valid endpoint lease: "+err.Error())
			return
		}
	}

	// Process the inference request
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	resp, err := s.svc.HandleInference(ctx, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListenAndServe starts the PIA HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	log.Printf("pia: listening on %s", addr)
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 130 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return srv.ListenAndServe()
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
