package api

import (
	"crypto/ed25519"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

// handleSchedulerConfigPublicKey distributes the CP config-signing public key
// so PIAs and the scheduler can verify signed worker configs.
func (s *Server) handleSchedulerConfigPublicKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.ext().KeyMgmt.GetOrCreateKey(keymgmt.DomainConfig, 90*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config key unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"public_key_hex": hex.EncodeToString(key.PublicKey),
	})
}

// handleSignWorkerConfig signs a PIA worker config with the CP config key
// (DARI scheduler §8). The config is the worker's authorization object:
// allowed models, backend reachability mode, and tenant binding.
func (s *Server) handleSignWorkerConfig(w http.ResponseWriter, r *http.Request) {
	var cfg scheduler.PIAWorkerConfig
	if err := decodeJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if cfg.WorkerID == "" || cfg.TenantID == "" || cfg.BackendMode == "" {
		writeError(w, http.StatusBadRequest, "worker_id, tenant_id, and backend_mode are required")
		return
	}
	key, err := s.ext().KeyMgmt.GetOrCreateKey(keymgmt.DomainConfig, 90*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config key unavailable")
		return
	}
	envelope, err := scheduler.SignConfig(ed25519.PrivateKey(key.PrivateKey), cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "signing failed")
		return
	}
	writeJSON(w, http.StatusOK, envelope)
}

// handleSchedulerRevocations serves the revocation feed the scheduler polls:
// revoked credential serials and revoked peer IDs.
func (s *Server) handleSchedulerRevocations(w http.ResponseWriter, r *http.Request) {
	epoch, serials := s.identity.RevocationSnapshot()
	serialList := make([]string, 0, len(serials))
	for serial := range serials {
		serialList = append(serialList, serial)
	}
	var peerIDs []string
	if err := s.db.Model(&models.Harness{}).
		Where("status = ?", "revoked").
		Pluck("harness_id", &peerIDs).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "revocation feed unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"epoch":            epoch,
		"revoked_serials":  serialList,
		"revoked_peer_ids": peerIDs,
	})
}

// handleSchedulerWorkersProxy is the CP read-through to the scheduler's
// worker registry (DARI scheduler §9).
func (s *Server) handleSchedulerWorkersProxy(w http.ResponseWriter, r *http.Request) {
	schedAddr := os.Getenv("PCCP_SCHED_HTTP_ADDR")
	if schedAddr == "" {
		writeError(w, http.StatusBadGateway, "scheduler not configured (PCCP_SCHED_HTTP_ADDR)")
		return
	}
	target := "http://" + schedAddr + "/api/v1/workers"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "proxy error")
		return
	}
	if token := os.Getenv("PCCP_SCHED_ADMIN_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "scheduler unreachable")
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "scheduler response unreadable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}
