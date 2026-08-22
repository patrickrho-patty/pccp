package models

// SSO realm-to-realm migration compatibility layer (PAT-1564).
// identity mapping, migration manifests, waves, and reconciliation results.
// Authorization always uses immutable provider subject + issuer + Patty user
// ID; email is an attribute, never a durable identity key. No password,
// access token, refresh token, session, or private key is ever stored here.

// SSOIdentityLink maps one legacy (Keycloak) identity to exactly one
// Patty user, deterministically keyed by issuer + subject. Ambiguous matches
// are rejected at link time; a link is never guessed from email alone.
type SSOIdentityLink struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	// Source-realm identity. legacy_* names remain part of the persisted/wire contract.
	LegacyIssuer  string `gorm:"type:varchar(512);not null" json:"legacy_issuer"`
	LegacySubject string `gorm:"type:varchar(255);not null" json:"legacy_subject"`
	// Target-realm issuer + subject established at cutover.
	TargetIssuer  string `gorm:"type:varchar(512)" json:"target_issuer,omitempty"`
	TargetSubject string `gorm:"type:varchar(255)" json:"target_subject,omitempty"`
	// Patty user binding (stable internal user ID).
	PattyUserID string `gorm:"type:varchar(64);index;not null" json:"patty_user_id"`
	// Resolution state: linked|ambiguous|unlinked|disabled.
	Status         string `gorm:"type:varchar(16);default:'unlinked'" json:"status"`
	ResolvedBy     string `gorm:"type:varchar(64)" json:"resolved_by,omitempty"`
	ResolvedAt     string `gorm:"type:timestamp" json:"resolved_at,omitempty"`
	ResolutionNote string `gorm:"type:text" json:"resolution_note,omitempty"`
}

func (SSOIdentityLink) TableName() string { return "sso_identity_links" }

// SSOMigrationItem is one discovered source-realm entity (realm/user/app/client)
// with owner, criticality, test/rollback plan, and migration status.
// Stores ONLY non-secret metadata — never credentials.
type SSOMigrationItem struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ManifestID     string `gorm:"type:varchar(64);index;not null" json:"manifest_id"`
	Kind           string `gorm:"type:varchar(32);not null" json:"kind"` // realm|user|group|role|client|machine|flow|idp
	LegacyKey      string `gorm:"type:varchar(512);index;not null" json:"legacy_key"`
	LegacyName     string `gorm:"type:varchar(512)" json:"legacy_name,omitempty"`
	OwnerID        string `gorm:"type:varchar(64)" json:"owner_id,omitempty"`
	Criticality    string `gorm:"type:varchar(16)" json:"criticality,omitempty"`       // low|medium|high|critical
	Protocol       string `gorm:"type:varchar(16)" json:"protocol,omitempty"`          // oidc|saml
	Status         string `gorm:"type:varchar(24);default:'discovered'" json:"status"` // discovered|planned|migrated|confirmed|disposition
	Disposition    string `gorm:"type:varchar(32)" json:"disposition,omitempty"`       // keep|compat_bridge|retire|blocked
	TestPlan       string `gorm:"type:text" json:"test_plan,omitempty"`
	RollbackPlan   string `gorm:"type:text" json:"rollback_plan,omitempty"`
	Notes          string `gorm:"type:text" json:"notes,omitempty"`
}

func (SSOMigrationItem) TableName() string { return "sso_migration_items" }

const (
	SSOMigrationItemKindUser = "user"

	SSOMigrationDispositionKeep         = "keep"
	SSOMigrationDispositionCompatBridge = "compat_bridge"
	SSOMigrationDispositionRetire       = "retire"

	SSOLinkStatusLinked    = "linked"
	SSOLinkStatusAmbiguous = "ambiguous"
	SSOLinkStatusUnlinked  = "unlinked"
	SSOLinkStatusDisabled  = "disabled"

	SSOManifestStatusInventory  = "inventory"
	SSOManifestStatusReconciled = "reconciled"
	SSOManifestStatusWaveReady  = "wave_ready"

	SSOWaveStatusSignedOff = "signed_off"

	SSOBridgeDecisionLinkedSession = "linked_issued_session"
	SSOBridgeDecisionAmbiguous     = "ambiguous"
	SSOBridgeDecisionUnlinked      = "unlinked"
	SSOBridgeDecisionDisabled      = "disabled"
)

// SSOMigrationManifest is one idempotent migration inventory + wave carrier.
type SSOMigrationManifest struct {
	Base
	OrganizationID  string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ManifestID      string `gorm:"type:varchar(64);not null" json:"manifest_id"`
	Name            string `gorm:"type:varchar(255)" json:"name"`
	Source          string `gorm:"type:varchar(64)" json:"source,omitempty"`                   // human-readable inventory source, e.g. "keycloak-estate-2026-08"
	SourceIssuer    string `gorm:"type:varchar(512);not null;default:''" json:"source_issuer"` // canonical issuer used for every identity reconciliation
	Wave            int    `gorm:"default:1" json:"wave"`
	ImportID        string `gorm:"type:varchar(128)" json:"import_id"` // tenant-scoped idempotent replay key
	InventoryDigest string `gorm:"type:varchar(64)" json:"-"`
	CreatedBy       string `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	ItemCount       int    `json:"item_count,omitempty"`
	// Reconciliation summary (non-secret counts only).
	SourceCount    int    `json:"source_count,omitempty"`
	TargetCount    int    `json:"target_count,omitempty"`
	LinkedCount    int    `json:"linked_count,omitempty"`
	AmbiguousCount int    `json:"ambiguous_count,omitempty"`
	ExcludedCount  int    `json:"excluded_count,omitempty"`
	ConflictCount  int    `json:"conflict_count,omitempty"`
	Status         string `gorm:"type:varchar(24);default:'inventory'" json:"status"` // inventory|reconciled|wave_ready|completed
	ReconciledAt   string `gorm:"type:timestamp" json:"reconciled_at,omitempty"`
	AuthorizedBy   string `gorm:"type:varchar(64)" json:"authorized_by,omitempty"`
}

func (SSOMigrationManifest) TableName() string { return "sso_migration_manifests" }

// SSOMigrationWave is one controlled cutover wave: apps, owner sign-off,
// automated/user-journey tests, rollback decision window, registration/monitor
// hooks, and a rehearsed rollback.
type SSOMigrationWave struct {
	Base
	OrganizationID    string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ManifestID        string `gorm:"type:varchar(64);index" json:"manifest_id"`
	Wave              int    `json:"wave"`
	Name              string `gorm:"type:varchar(255)" json:"name"`
	OwnerID           string `gorm:"type:varchar(64)" json:"owner_id"`
	Apps              string `gorm:"type:text" json:"apps,omitempty"`                  // JSON array
	Status            string `gorm:"type:varchar(24);default:'planned'" json:"status"` // planned|testing|staged|signed_off|rolled_back
	SignOffBy         string `gorm:"type:varchar(64)" json:"sign_off_by,omitempty"`
	SignOffAt         string `gorm:"type:timestamp" json:"sign_off_at,omitempty"`
	RollbackWindow    string `gorm:"type:varchar(32)" json:"rollback_window,omitempty"`
	RollbackTrainedAt string `gorm:"type:timestamp" json:"rollback_trained_at,omitempty"`
}

func (SSOMigrationWave) TableName() string { return "sso_migration_waves" }

// SSOMigrationBridgeEvent is the auditable result of one legacy→target
// authentication via the compatibility bridge. It NEVER contains credentials
// or tokens — only counts, issuer/subject mapping status, and a new-session
// decision.
type SSOMigrationBridgeEvent struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ManifestID     string `gorm:"type:varchar(64);index" json:"manifest_id"`
	LegacyIssuer   string `gorm:"type:varchar(512)" json:"legacy_issuer"`
	LegacySubject  string `gorm:"type:varchar(255)" json:"legacy_subject"`
	// Decision: linked_issued_session|ambiguous|unlinked|disabled|rejected
	Decision         string `gorm:"type:varchar(32);not null" json:"decision"`
	PattyUserID      string `gorm:"type:varchar(64)" json:"patty_user_id,omitempty"`
	NewSessionIssued bool   `gorm:"default:false" json:"new_session_issued"`
	Note             string `gorm:"type:text" json:"note,omitempty"`
}

func (SSOMigrationBridgeEvent) TableName() string { return "sso_migration_bridge_events" }
