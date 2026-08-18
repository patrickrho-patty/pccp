package api

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// AlertEndpointCreateRequest is the body the API accepts on POST
// /security/alerts. The Target field is write-only: the server reads
// it for persistence/delivery and never echoes it back. PAT-1502 PR 1.
type AlertEndpointCreateRequest struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"` // slack, webhook, siem
	Target     string   `json:"target"`
	Severities []string `json:"severities"`
	Enabled    *bool    `json:"enabled"`
}

// AlertEndpointResponse is the redacted DTO returned to clients.
// The raw URL is replaced by:
//   - secret_configured: true iff a target was submitted and stored
//   - credential_id:     opaque identifier (first 12 hex of SHA-256 over the URL) — safe to log, safe to display
//   - target_redacted:   constant "***" so the field name cannot be mistaken for a usable secret
//
// Never include the URL, the last-4 suffix, or any provider response body.
// PAT-1502 PR 1.
type AlertEndpointResponse struct {
	ID               string `json:"id"`
	OrganizationID   string `json:"organization_id"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Severities       string `json:"severities,omitempty"` // raw JSON array string, already non-secret
	Enabled          bool   `json:"enabled"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	SecretConfigured bool   `json:"secret_configured"`
	CredentialID     string `json:"credential_id,omitempty"`
	TargetRedacted   string `json:"target_redacted"` // constant "***"
}

// redactAlertEndpoint produces a client-safe view of an AlertEndpoint.
// It MUST be the only way the API returns alert endpoint data. The
// caller is responsible for ensuring the raw Target never escapes this
// boundary. PAT-1502 PR 2: secret_configured is derived from the
// encrypted envelope when present; the legacy plaintext column is the
// dual-read fallback during the backfill window.
func redactAlertEndpoint(ep models.AlertEndpoint) AlertEndpointResponse {
	return AlertEndpointResponse{
		ID:               ep.ID,
		OrganizationID:   ep.OrganizationID,
		Name:             ep.Name,
		Type:             ep.Type,
		Severities:       ep.SeveritiesJSON,
		Enabled:          ep.Enabled,
		CreatedAt:        ep.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        ep.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		SecretConfigured: strings.TrimSpace(ep.TargetEnc) != "" || strings.TrimSpace(ep.Target) != "",
		CredentialID:     credentialIDForSecret(ep.TargetEnc, ep.TargetKEKID, ep.Target),
		TargetRedacted:   "***",
	}
}

// redactAlertEndpoints applies redactAlertEndpoint over a slice.
func redactAlertEndpoints(eps []models.AlertEndpoint) []AlertEndpointResponse {
	out := make([]AlertEndpointResponse, 0, len(eps))
	for _, ep := range eps {
		out = append(out, redactAlertEndpoint(ep))
	}
	return out
}

// credentialIDForSecret derives a stable, non-secret identifier for a
// stored secret. PAT-1502 PR 2: when the encrypted envelope is present
// it identifies the row (SHA-256 over the envelope bytes); when only
// the legacy plaintext column is present it identifies the URL. The
// resulting identifier is safe to log and stable across reseals for
// the same envelope.
func credentialIDForSecret(targetEnc, targetKEKID, target string) string {
	if strings.TrimSpace(targetEnc) != "" {
		sum := sha256.Sum256([]byte(targetEnc + "|" + targetKEKID))
		return hex.EncodeToString(sum[:6])
	}
	t := strings.TrimSpace(target)
	if t == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:6])
}

// credentialIDForTarget is the PR 1 helper retained for callers that
// only have a raw URL in hand (e.g. the dispatch path inside a single
// handler where decryption has already happened).
func credentialIDForTarget(target string) string {
	t := strings.TrimSpace(target)
	if t == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:6])
}
