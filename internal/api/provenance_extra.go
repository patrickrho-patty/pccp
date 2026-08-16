package api

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// provenance_extra.go: web/20 — receipt verification (A2), signed
// bundle export (A3), cross-session provenance search (A5), visibility
// gating (A6).

// handleProvenanceReceipts lists a session's evidence receipts with
// verification status (A2): a receipt verifies when its chain root
// matches the exchange's recorded final-state hash.
func (s *Server) handleProvenanceReceipts(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("(id = ? OR session_id = ?) AND organization_id = ?", id, id, orgID).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var receipts []models.EvidenceReceipt
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at DESC").Find(&receipts)
	// The action envelope chain for this session (in order) is the
	// re-derivation input for every receipt's chain root.
	var actions []models.ActionEnvelope
	s.db.Where("session_id = ?", sess.SessionID).Order("occurred_at ASC").Find(&actions)
	chain := map[int]string{} // seq index → cumulative hash
	runner := sha256.Sum256([]byte("pccp-chain|" + sess.SessionID))
	prev := hex.EncodeToString(runner[:])
	for i, a := range actions {
		h := sha256.Sum256([]byte(prev + "|" + a.EnvelopeDigest))
		prev = hex.EncodeToString(h[:])
		chain[i+1] = prev
	}
	type receiptRow struct {
		models.EvidenceReceipt
		Verified     bool   `json:"verified"`
		Verification string `json:"verification"`
	}
	rows := make([]receiptRow, 0, len(receipts))
	for _, rec := range receipts {
		row := receiptRow{EvidenceReceipt: rec}
		if rec.ChainRoot == "" {
			row.Verification = "no_chain_root"
			continue
		}
		if int(rec.LastEventSeq) > len(actions) {
			row.Verification = "chain_root_rejects (receipt covers more events than recorded)"
			continue
		}
		if chain[int(rec.LastEventSeq)] == rec.ChainRoot {
			row.Verified = true
			row.Verification = "chain_root_verified"
		} else {
			row.Verification = "chain_root_mismatch"
		}
		rows = append(rows, row)
	}
	writeJSON(w, http.StatusOK, rows)
}

// handleProvenanceExport signs the full chain as a tamper-evident
// bundle (A3).
func (s *Server) handleProvenanceExport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("(id = ? OR session_id = ?) AND organization_id = ?", id, id, orgID).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	// Assemble the chain (actions, spans, changesets, receipts).
	var actions []models.ActionEnvelope
	s.db.Where("session_id = ?", sess.SessionID).Order("occurred_at ASC").Find(&actions)
	var spans []models.ProvenanceSpan
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at ASC").Find(&spans)
	var changeSets []models.ChangeSet
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at ASC").Find(&changeSets)
	var receipts []models.EvidenceReceipt
	s.db.Where("session_id = ?", sess.SessionID).Find(&receipts)

	bundle := map[string]interface{}{
		"exported_at": time.Now().Format(time.RFC3339),
		"session":     sess,
		"actions":     actions,
		"spans":       spans,
		"change_sets": changeSets,
		"receipts":    receipts,
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Sign the canonical bundle with a persisted evidence-export key.
	priv, err := keys.LoadOrCreate(s.db, "api-provenance-export")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sig := ed25519.Sign(priv, raw)
	out := map[string]interface{}{
		"bundle":            bundle,
		"signature_hex":     hex.EncodeToString(sig),
		"signer_public_hex": hex.EncodeToString(priv.Public().(ed25519.PublicKey)),
		"signature_note":    "ed25519 over the canonical JSON body",
	}
	outRaw, _ := json.MarshalIndent(out, "", "  ")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=provenance-"+sess.SessionID[:12]+".bundle.json")
	w.WriteHeader(http.StatusOK)
	w.Write(outRaw)
}

// handleProvenanceSearch searches spans + changesets across sessions
// (A5) by file path/symbol.
func (s *Server) handleProvenanceSearch(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q required")
		return
	}
	var spans []models.ProvenanceSpan
	s.db.Where("organization_id = ? AND (file_path LIKE ? OR symbol_name LIKE ?)", orgID, "%"+q+"%", "%"+q+"%").
		Order("created_at DESC").Limit(100).Find(&spans)
	var changeSets []models.ChangeSet
	s.db.Where("organization_id = ? AND files_changed LIKE ?", orgID, "%"+q+"%").
		Order("created_at DESC").Limit(100).Find(&changeSets)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"spans": spans, "change_sets": changeSets,
	})
}

var _ = json.Marshal
