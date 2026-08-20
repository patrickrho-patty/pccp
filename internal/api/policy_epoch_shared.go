package api

// Shared signed-policy-epoch mechanics for governance features (PAT-1456,
// PAT-1455). Both managed skill policy and managed system-prompt additions
// issue an immutable, signed epoch distributed to every active harness over
// the relay directive carrier. These helpers centralize the common
// operational core so the two features cannot drift.

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// policyIssuer is the persisted per-service signing identity shared by the
// skill-policy and system-prompt epoch issuers. A single issuer keeps
// verification simple and the identity durable across restarts.
const policyIssuer = "policy-issuer"

// signPolicyPayload derives the digest and detached COSE signature for an
// epoch body using the shared policy issuer.
func signPolicyPayload(db *gorm.DB, body []byte) (string, string, error) {
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	priv, err := keys.LoadOrCreate(db, policyIssuer)
	if err != nil {
		return "", "", err
	}
	sig, err := dari.COSESign1Hex(body, priv, []byte(policyIssuer))
	if err != nil {
		return "", "", err
	}
	return digest, sig, nil
}

// policyEpochPayload is the canonical signed epoch body for a governance
// feature.
type policyEpochPayload struct {
	OrganizationID string      `json:"organization_id"`
	Kind           string      `json:"kind"`
	Content        interface{} `json:"content"`
	Enforcement    bool        `json:"enforcement,omitempty"`
	IssuedAt       string      `json:"issued_at"`
}

// pushEpochToActiveHarnesses delivers a signed epoch directive to every active
// (or enrolled) harness in the org and returns the count delivered. A harness
// with no live relay channel is counted as not delivered (honest reporting).
func (s *Server) pushEpochToActiveHarnesses(orgID, commandType, message string, payload map[string]interface{}) int {
	var targets []string
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status IN ?", orgID, []string{"active", "enrolled"}).Pluck("harness_id", &targets)
	delivered := 0
	for _, hid := range targets {
		if err := s.pushRelayDirective(commandType, orgID, hid, message, payload); err == nil {
			delivered++
		}
	}
	return delivered
}

// epochDirective builds the directive payload shared by epoch deliveries.
func epochDirective(epochID string, epochNumber uint64, digest, signature string, enforcement bool) map[string]interface{} {
	return map[string]interface{}{
		"epoch_id": epochID, "epoch_number": epochNumber,
		"digest": digest, "signature_hex": signature, "enforcement": enforcement,
	}
}

// writePolicyEpochError converts signing/persistence errors to HTTP responses.
func writePolicyEpochError(w http.ResponseWriter, prefix string, err error) {
	writeError(w, http.StatusInternalServerError, prefix+": "+err.Error())
}
