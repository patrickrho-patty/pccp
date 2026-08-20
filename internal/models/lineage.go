package models

import (
	"gorm.io/gorm"
)

// Read-only Patty Git / GitLab / GitHub code-lineage observation
// (PAT-1453). Observation-only: no provider-side mutations exist in this
// domain. Attribution is claimed only when a digest-verifiable binding
// to Patty's change evidence exists.

// SCMProviderConnection is one tenant-scoped managed service identity
// against a provider (base URL supports self-managed GHES/GitLab).
type SCMProviderConnection struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	Provider       string `json:"provider"` // patty_git | gitlab | github
	BaseURL        string `json:"base_url"`
	// Credential material lives in the secret store; only a masked
	// reference is exposed and it is never exportable via the UI.
	CredentialRef    string `json:"credential_ref"`
	WebhookSecret    string `json:"-"`
	WebhookVerified  bool   `json:"webhook_verified"`
	SyncCursor       string `json:"sync_cursor"`
	LastReconciliation string `json:"last_reconciliation"`
	Health            string `json:"health"` // healthy|stale|revoked|degraded
	KnownGaps         string `json:"known_gaps"`
}

// ObservedRepositoryEvent is a normalized provider event, deduplicated
// by (provider, provider_event_id) so replays and duplicate deliveries
// are inert. Raw provider identity is preserved for audit/replay.
type ObservedRepositoryEvent struct {
	gorm.Model
	OrganizationID   string `gorm:"index" json:"organization_id"`
	ConnectionID     uint   `gorm:"index" json:"connection_id"`
	Provider         string `json:"provider"`
	// Dedup is scoped per connection: provider event IDs are not
	// globally unique across self-hosted instances (PAT-1453).
	ProviderEventID  string `gorm:"uniqueIndex:idx_ore_conn_event" json:"provider_event_id"`
	ProviderDeliveryID string `gorm:"uniqueIndex:idx_ore_conn_event" json:"provider_delivery_id"`
	ProviderRepoID   string `gorm:"index" json:"provider_repo_id"`
	EventType        string `json:"event_type"` // push|force_push|branch_create|branch_delete|pr_opened|pr_merged|review|check|default_branch_change|repo_transferred|access_revoked
	Actor            string `json:"actor"`
	Ref              string `json:"ref"`
	CommitSHA        string `json:"commit_sha"`
	PayloadDigest    string `json:"payload_digest"`
	IngestedAt       string `json:"ingested_at"`
}

// CommitAttribution binds a repository commit to Patty's recorded change
// evidence. The binding is digest-verifiable ONLY — timestamp proximity,
// commit-message conventions, author identity, or branch names are never
// sufficient (PAT-1453 evidence authority).
type CommitAttribution struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	ProviderRepoID string `gorm:"index" json:"provider_repo_id"`
	CommitSHA      string `gorm:"index" json:"commit_sha"`
	ParentSHAsJSON string `json:"parent_shas_json"`
	// Distinct facts (never collapsed).
	GitAuthor    string `json:"git_author"`
	GitCommitter string `json:"git_committer"`
	// Evidence binding (empty = imported/unverifiable).
	ChangeSetID    string `json:"changeset_id"`
	EvidenceDigest string `json:"evidence_digest"` // must equal the recorded change-set diff digest
	SessionID      string `json:"session_id"`
	HarnessID      string `json:"harness_id"`
	UserID         string `json:"user_id"`
	// Lineage category — later edits never overwrite earlier categories;
	// history accumulates.
	Lineage           string `json:"lineage"` // ai_created|human_created|human_modified_ai|ai_modified_human|mixed|imported_unverifiable
	DerivationVersion int    `json:"derivation_version"`
	Authoritative     bool   `json:"authoritative"` // true only with a digest-verified binding
	ObservedAt        string `json:"observed_at"`
}
