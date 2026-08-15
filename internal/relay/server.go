package relay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/webbinding"
)

// Server is the Relay HTTP API server.
// In Phase 0, the Relay exposes an HTTP API that the Harness and Control Plane
// can call. This is NOT the final DARI wire protocol (which uses QUIC/TCP with
// CBOR framing), but provides the same governance semantics for initial integration.
type Server struct {
	svc *Service
	// webBinding mounts the dari.web/1 carrier when configured.
	webBinding *webbinding.Server
}

// SetWebBinding installs the dari.web/1 browser carrier.
func (s *Server) SetWebBinding(w *webbinding.Server) { s.webBinding = w }

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
	mux.HandleFunc("/v1/harnesses/revoke", s.handleRevokeHarness)
	mux.HandleFunc("/v1/broadcasts", s.handleBroadcast)
	mux.HandleFunc("/v1/admin/directives", s.handleAdminDirective)
	mux.HandleFunc("/v1/sovereign/advisories", s.handleSovereignAdvisory)
	// dari.web/1 constrained WebSocket fallback carrier (Task 13). The
	// governance handler routes AI_OPEN envelopes through the SAME
	// GovernInference path as the native transport.
	if s.webBinding != nil {
		upgrader := websocket.Upgrader{
			CheckOrigin:     func(r *http.Request) bool { return true }, // origin policy enforced inside
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
		}
		mux.Handle("/dari.web/1", s.webBinding.HTTPHandler(upgrader))
	}

	return mux
}

// handleRevokeHarness revokes a harness and propagates the revocation
// to every attached DARI listener — active governed streams terminate
// (Task 6 Step 3).
func (s *Server) handleRevokeHarness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		OrganizationID string `json:"organization_id"`
		HarnessID      string `json:"harness_id"`
		Reason         string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.OrganizationID == "" || req.HarnessID == "" {
		writeError(w, http.StatusBadRequest, "organization_id and harness_id are required")
		return
	}
	if req.Reason == "" {
		req.Reason = "revoked via relay admin API"
	}
	if err := s.svc.RevokeHarness(req.OrganizationID, req.HarnessID, req.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, "revoke failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "harness_id": req.HarnessID})
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

// handleBroadcast pushes a governed broadcast to the org's online
// DARI sessions (E2 production wiring: the comms surfaces are no
// longer relay-side dead code).
func (s *Server) handleBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		OrgID    string `json:"org_id"`
		Severity string `json:"severity"`
		Body     string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrgID == "" || req.Body == "" {
		writeError(w, http.StatusBadRequest, "org_id and body are required")
		return
	}
	if req.Severity == "" {
		req.Severity = "info"
	}
	body := BuildBroadcastMessage("", "pccp-policy", req.Body, req.Severity, time.Now())
	sent := s.svc.BroadcastToOrg(req.OrgID, body)
	writeJSON(w, http.StatusOK, map[string]any{"delivered": sent})
}

// handleAdminDirective signs + delivers an admin directive to the
// target harness (E5). Signature verification happens connector-side
// under the AUTH_ACK policy issuer key.
func (s *Server) handleAdminDirective(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		OrgID       string `json:"org_id"`
		Target      string `json:"target"`
		CommandType string `json:"command_type"`
		Reason      string `json:"reason"`
		IssuedBy    string `json:"issued_by"`
		PayloadB64  string `json:"payload_b64"`
		NotAfterMs  int64  `json:"not_after_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Target == "" || req.CommandType == "" {
		writeError(w, http.StatusBadRequest, "target and command_type are required")
		return
	}
	payload, err := base64.StdEncoding.DecodeString(req.PayloadB64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "payload_b64 is not valid base64")
		return
	}
	body, err := s.svc.BuildAdminDirective(req.OrgID, req.CommandType, req.Target, req.Reason, req.IssuedBy, payload, req.NotAfterMs, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "signing failed")
		return
	}
	sent := s.svc.DeliverDirectiveToHarness(req.Target, body)
	writeJSON(w, http.StatusOK, map[string]any{"delivered": sent})
}

// handleSovereignAdvisory pushes an offline advisory to every
// connected session (E3 air-gap mode).
func (s *Server) handleSovereignAdvisory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	sent := s.svc.DeliverSovereignAdvisoryToAll([]byte(req.Body))
	writeJSON(w, http.StatusOK, map[string]any{"delivered": sent})
}
