package api

import (
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
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
//   - credential_id:     truncated display form of a domain-separated fingerprint
//   - target_redacted:   constant "***" so the field name cannot be mistaken for a usable secret
//
// Never include the URL, the last-4 suffix, or any provider response body.
// PAT-1502 PR 1.
type AlertEndpointResponse struct {
	ID                         string `json:"id"`
	OrganizationID             string `json:"organization_id"`
	Name                       string `json:"name"`
	Type                       string `json:"type"`
	Severities                 string `json:"severities,omitempty"` // raw JSON array string, already non-secret
	Enabled                    bool   `json:"enabled"`
	CreatedAt                  string `json:"created_at,omitempty"`
	UpdatedAt                  string `json:"updated_at,omitempty"`
	SecretConfigured           bool   `json:"secret_configured"`
	CredentialID               string `json:"credential_id,omitempty"`
	TargetRedacted             string `json:"target_redacted"` // constant "***"
	RotationRequired           bool   `json:"rotation_required"`
	LastRotatedAt              string `json:"last_rotated_at,omitempty"`
	LastTestAt                 string `json:"last_test_at,omitempty"`
	LastTestStatus             string `json:"last_test_status,omitempty"`
	ProviderRevocationRequired bool   `json:"provider_revocation_required,omitempty"`
}

// redactAlertEndpoint produces a client-safe view of an AlertEndpoint.
// It MUST be the only way the API returns alert endpoint data. The
// caller is responsible for ensuring the raw Target never escapes this
// boundary. PAT-1502 PR 2: secret_configured is derived from the
// encrypted envelope when present; the legacy plaintext column is the
// dual-read fallback during the backfill window.
func redactAlertEndpoint(ep models.AlertEndpoint) AlertEndpointResponse {
	severities := ep.SeveritiesJSON
	if strings.TrimSpace(severities) == "" || strings.TrimSpace(severities) == "null" {
		severities = "[]"
	}
	credentialID := credentialIDForSecret(ep)
	if len(credentialID) > 18 {
		credentialID = credentialID[:18]
	}
	return AlertEndpointResponse{
		ID:               ep.ID,
		OrganizationID:   ep.OrganizationID,
		Name:             ep.Name,
		Type:             ep.Type,
		Severities:       severities,
		Enabled:          ep.Enabled,
		CreatedAt:        ep.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        ep.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		SecretConfigured: strings.TrimSpace(ep.TargetEnc) != "" || strings.TrimSpace(ep.Target) != "",
		CredentialID:     credentialID,
		TargetRedacted:   "***",
		RotationRequired: ep.RotationRequired,
		LastRotatedAt:    formatOptionalAlertTime(ep.LastRotatedAt),
		LastTestAt:       formatOptionalAlertTime(ep.LastTestAt),
		LastTestStatus:   ep.LastTestStatus,
	}
}

func formatOptionalAlertTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

// credentialIDForSecret derives a stable, non-secret identifier for a
// stored secret. New rows persist the full domain-separated fingerprint;
// legacy plaintext rows derive it during the explicit migration window.
func credentialIDForSecret(ep models.AlertEndpoint) string {
	return ep.CredentialID
}

// credentialIDForTarget is retained for fingerprint behavior tests and
// legacy migration callers that have a raw credential in hand.
func credentialIDForTarget(provider keymgmt.KeyProvider, target string) string {
	id, _ := keymgmt.CredentialID(provider, target)
	return id
}
