package relay

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// Server is the Relay HTTP API server.
// In Phase 0, the Relay exposes an HTTP API that the Harness and Control Plane
// can call. This is NOT the final DARI wire protocol (which uses QUIC/TCP with
// CBOR framing), but provides the same governance semantics for initial integration.
type Server struct {
	svc *Service
}

// NewServer creates a new Relay HTTP server.
func NewServer(svc *Service) *Server {
	return &Server{svc: svc}
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/enroll", s.handleEnrollHarness)
	mux.HandleFunc("/v1/exchanges", s.handleOpenExchange)
	mux.HandleFunc("/v1/exchanges/", s.handleExchangeAction)
	mux.HandleFunc("/v1/inference", s.handleInference)
	mux.HandleFunc("/v1/provenance/changesets", s.handleListChangeSets)

	return mux
}

// handleListChangeSets surfaces the connector-ingested changesets for
// operational verification of the provenance pipeline (B1).
func (s *Server) handleListChangeSets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var rows []models.ChangeSet
	s.svc.db.Limit(100).Order("created_at DESC").Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, cs := range rows {
		out = append(out, map[string]any{
			"id":              cs.ID,
			"organization_id": cs.OrganizationID,
			"session_id":      cs.SessionID,
			"repository_id":   cs.RepositoryID,
			"files_changed":   cs.FilesChanged,
			"created_at":      cs.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"changesets": out})
}

// handleEnrollHarness is the harness enrollment entry point (A1): a
// harness presents its Ed25519 public key and receives a CA-issued
// COSE-Sign1 peer credential. This is the admin-plane half of the
// enrollment-code flow; the DARI data plane verifies the credential
// on connect.
func (s *Server) handleEnrollHarness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req identity.EnrollHarnessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.OrganizationID == "" || req.HarnessID == "" || req.PublicKeyHex == "" {
		writeError(w, http.StatusBadRequest, "organization_id, harness_id, and public_key_hex are required")
		return
	}
	if req.UserID == "" {
		req.UserID = "user-" + req.HarnessID
	}
	harness, cred, err := s.svc.Identity().EnrollHarness(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enrollment failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"harness_id":      harness.HarnessID,
		"organization_id": harness.OrganizationID,
		"credential_hex":  harness.CredentialJSON,
		"serial":          cred.Serial,
		"issuer":          cred.Issuer,
		"expires_ms":      cred.NotAfter,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"relay_id": s.svc.RelayID(),
		"time":     time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleOpenExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req OpenExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	exchange, verdict, err := s.svc.OpenExchange(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"error":    err.Error(),
			"verdict":  string(verdict),
			"exchange": exchange,
		})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"exchange": exchange,
		"verdict":  string(verdict),
	})
}

func (s *Server) handleExchangeAction(w http.ResponseWriter, r *http.Request) {
	// Route: /v1/exchanges/{id}/close
	// or:    /v1/exchanges/{id}/inference
	parts := splitPath(r.URL.Path)
	if len(parts) < 4 {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	exchangeID := parts[2]
	action := parts[3]

	switch action {
	case "close":
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		receipt, err := s.svc.CloseExchange(ctx, exchangeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, receipt)

	default:
		writeError(w, http.StatusBadRequest, "unknown exchange action: "+action)
	}
}

func (s *Server) handleInference(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req InferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	resp, err := s.svc.RouteInference(ctx, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListenAndServe starts the Relay HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	log.Printf("relay: listening on %s", addr)
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

func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, c := range path {
		if c == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
