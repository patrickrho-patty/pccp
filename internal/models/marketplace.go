package models

import (
	"gorm.io/gorm"
)

// Governed public Patty Code marketplace (PAT-1438) — PCCP half: the
// persistent registry, trust tiers, automated submission checks,
// moderation/abuse response, and per-harness install inventory. This
// replaces the in-memory mcpmarket service as the catalog source of
// truth. The desktop Discover/Installed UI + planner/apply remain
// harness-side and consume this control plane.

// MarketPublisher is a verifiable publisher identity.
type MarketPublisher struct {
	gorm.Model
	PublisherID string `gorm:"uniqueIndex" json:"publisher_id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	// Owning organization: publishing and version submission verify
	// ownership against this binding (PAT-1438 trust-chain protection).
	OrganizationID string `gorm:"index" json:"organization_id"`
	// Verified publishers have completed identity verification; official
	// is Patty itself. Trust flows from publisher state → listing label.
	TrustState string `json:"trust_state"` // unverified|verified|official|revoked
	VerifiedAt string `json:"verified_at"`
}

// MarketListing is one catalog entry (skill | plugin | mcp).
type MarketListing struct {
	gorm.Model
	PublisherID string `gorm:"index" json:"publisher_id"`
	Slug        string `gorm:"uniqueIndex" json:"slug"`
	Name        string `json:"name"`
	NameKo      string `json:"name_ko"`
	Type        string `json:"type"` // skill|plugin|mcp
	Category    string `json:"category"`
	Description string `gorm:"type:text" json:"description"`
	// Trust label is derived from publisher trust + review state and is
	// NEVER influenced by featured/sponsored (PAT-1438 anti-corruption).
	TrustLabel string `json:"trust_label"` // community|verified_publisher|reviewed|official
	// Editorial featuring and sponsored placement are separate fields.
	Featured      bool   `json:"featured"`
	Sponsored     bool   `json:"sponsored"`
	Status        string `json:"status"` // active|blocked|removed
	LatestVersion string `json:"latest_version"`
	InstallCount  int64  `json:"install_count"`
}

// MarketListingVersion is an immutable content-addressed release: the
// (slug, version, content_hash) triple is unique — bytes change ⇒ a
// new version is mandatory.
type MarketListingVersion struct {
	gorm.Model
	// (slug, version) is UNIQUE — the immutability invariant is enforced
	// at the schema level, not just check-then-insert.
	Slug        string `gorm:"uniqueIndex:idx_mlv_slug_ver" json:"slug"`
	Version     string `gorm:"uniqueIndex:idx_mlv_slug_ver" json:"version"`
	ContentHash string `gorm:"uniqueIndex" json:"content_hash"`
	// Versioned manifest with the shared capability/permission vocabulary.
	ManifestJSON string `gorm:"type:text" json:"manifest_json"`
	Changelog    string `json:"changelog"`
	// Automated check results (malware/secrets/manifest/deps/impersonation).
	ChecksJSON  string `gorm:"type:text" json:"checks_json"`
	State       string `json:"state"` // pending|active|quarantined
	SubmittedBy string `json:"submitted_by"`
	SubmittedAt string `json:"submitted_at"`
}

// MarketReport is a user abuse/broken report against a listing/version.
type MarketReport struct {
	gorm.Model
	Slug     string `gorm:"index" json:"slug"`
	Version  string `json:"version"`
	Kind     string `json:"kind"` // malicious|deceptive|abandoned|impersonating|broken
	Detail   string `json:"detail"`
	Reporter string `json:"reporter"`
	State    string `json:"state"` // open|resolved|dismissed
}

// MarketInstallRecord is the governed per-harness install inventory.
type MarketInstallRecord struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	HarnessID      string `gorm:"index" json:"harness_id"`
	Slug           string `json:"slug"`
	Version        string `json:"version"`
	ContentHash    string `json:"content_hash"`
	State          string `json:"state"` // installed|disabled|broken|quarantined|needs_approval
	Pinned         bool   `json:"pinned"`
	// Previous verified version/hash for one-step rollback.
	PreviousVersion string `json:"previous_version"`
	PreviousHash    string `json:"previous_hash"`
	Warned          bool   `json:"warned"` // installed-user warning issued
}
