package models

import "time"

// Patty Reference (PAT-1404) — governed technical-documentation retrieval.
// These models are the package contract + registry + derived index for PCCP's
// side of the system: canonical normalized chunks with stable citations,
// governed source registry with authority/licensing, signed packages with
// digest + signature + tombstone support, and an atomic active corpus with
// lineage/rollback. Corpus content never leaves a tenant boundary; private
// sources never enter Patty infrastructure.

// ReferenceSource is a governed corpus source (Tier 1/2/3 or tenant-private).
// Membership is a registry decision, not a hard-coded trigger list.
type ReferenceSource struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	SourceID       string `gorm:"type:varchar(64);uniqueIndex;not null" json:"source_id"`
	// Canonical names + aliases (Korean/English) for resolve/search.
	Name          string `gorm:"type:varchar(255);not null" json:"name"`
	NameKo        string `gorm:"type:varchar(255)" json:"name_ko,omitempty"`
	Aliases       string `gorm:"type:text" json:"aliases,omitempty"` // JSON array
	LibraryID     string `gorm:"type:varchar(255);index" json:"library_id,omitempty"`
	Tier          string `gorm:"type:varchar(16);not null" json:"tier"`      // tier1|tier2|tier3|tenant
	Authority     string `gorm:"type:varchar(16);not null" json:"authority"` // official|vendor|customer|community_reviewed
	VersionScheme string `gorm:"type:varchar(16)" json:"version_scheme"`     // semver|date|unversioned
	// Licensing / redistribution policy enforced at package-build + export.
	License         string    `gorm:"type:text" json:"license"`
	Redistributable bool      `gorm:"default:true" json:"redistributable"`
	Acquisition     string    `gorm:"type:varchar(32)" json:"acquisition"` // crawl|import|customer|connector
	UpdatePolicy    string    `gorm:"type:varchar(255)" json:"update_policy,omitempty"`
	OwnerID         string    `gorm:"type:varchar(64)" json:"owner_id,omitempty"`
	Status          string    `gorm:"type:varchar(16);default:'active'" json:"status"` // active|degraded|removed
	RemovedAt       time.Time `json:"removed_at,omitempty"`
	// Canonical source URL or private local identity.
	CanonicalURL  string    `gorm:"type:text" json:"canonical_url,omitempty"`
	Private       bool      `gorm:"default:false" json:"private"`
	EffectiveDate time.Time `json:"effective_date,omitempty"`
	Freshness     string    `gorm:"type:varchar(64)" json:"freshness,omitempty"`
}

func (ReferenceSource) TableName() string { return "reference_sources" }

// ReferenceChunk is one normalized, bounded document chunk with a stable
// citation. Code blocks are preserved byte-for-byte; prose normalization never
// touches code/identifiers.
type ReferenceChunk struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	PackageID      string `gorm:"type:varchar(64);index;not null" json:"package_id"`
	SourceID       string `gorm:"type:varchar(64);index;not null" json:"source_id"`
	ChunkID        string `gorm:"type:varchar(128);index;not null" json:"chunk_id"`
	// Stable citation identity.
	DocPath string `gorm:"type:text" json:"doc_path"`
	TitleKo string `gorm:"type:varchar(512)" json:"title_ko,omitempty"`
	TitleEn string `gorm:"type:varchar(512)" json:"title_en,omitempty"`
	// Body keeps prose; code is preserved in CodeLang + Code.
	Body     string `gorm:"type:text" json:"body"`
	CodeLang string `gorm:"type:varchar(64)" json:"code_lang,omitempty"`
	Code     string `gorm:"type:text" json:"code,omitempty"`
	// Version + provenance.
	Version       string `gorm:"type:varchar(64);index" json:"version,omitempty"`
	LibraryID     string `gorm:"type:varchar(255);index" json:"library_id,omitempty"`
	EffectiveDate string `gorm:"type:varchar(32)" json:"effective_date,omitempty"`
	ImportAt      string `gorm:"type:varchar(32)" json:"import_at,omitempty"`
	CanonicalURL  string `gorm:"type:text" json:"canonical_url,omitempty"`
	ChunkHash     string `gorm:"type:varchar(128);index" json:"chunk_hash"`
	LineStart     int    `gorm:"default:0" json:"line_start,omitempty"`
	LineEnd       int    `gorm:"default:0" json:"line_end,omitempty"`
	Authority     string `gorm:"type:varchar(16)" json:"authority"`
	// Search tokenization (lowercased, whitespace-joined, dual Korean/English).
	Tokens string `gorm:"type:text;index" json:"-"`
}

func (ReferenceChunk) TableName() string { return "reference_chunks" }

// ReferencePackage is an imported signed documentation package (full or delta).
// Signatures/digests/schema/archive safety are validated BEFORE staging; the
// active corpus is switched atomically after admin activation.
type ReferencePackage struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	PackageID      string `gorm:"type:varchar(64);uniqueIndex;not null" json:"package_id"`
	CorpusID       string `gorm:"type:varchar(64);index" json:"corpus_id"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	SchemaVersion  string `gorm:"type:varchar(32)" json:"schema_version"`
	BasePackageID  string `gorm:"type:varchar(64)" json:"base_package_id,omitempty"` // delta base
	IsDelta        bool   `gorm:"default:false" json:"is_delta"`
	// Digest + signature over the canonical manifest.
	ManifestJSON   string `gorm:"type:text" json:"manifest,omitempty"`
	ManifestDigest string `gorm:"type:varchar(128)" json:"manifest_digest"`
	SignatureHex   string `gorm:"type:text" json:"signature,omitempty"`
	Publisher      string `gorm:"type:varchar(128)" json:"publisher,omitempty"`
	SourceCount    int    `json:"source_count,omitempty"`
	ChunkCount     int    `json:"chunk_count,omitempty"`
	State          string `gorm:"type:varchar(16);default:'staged'" json:"state"` // staged|active|rolled_back|rejected
	// Rollback lineage.
	Supersedes     string `gorm:"type:varchar(64)" json:"supersedes,omitempty"`
	ImportedBy     string `gorm:"type:varchar(64)" json:"imported_by,omitempty"`
	ImportedAt     string `gorm:"type:timestamp" json:"imported_at,omitempty"`
	ActivatedAt    string `gorm:"type:timestamp" json:"activated_at,omitempty"`
	ActivationNote string `gorm:"type:text" json:"activation_note,omitempty"`
	AirGapOnly     bool   `gorm:"default:false" json:"air_gap_only"`
	Tombstones     string `gorm:"type:text" json:"tombstones,omitempty"` // JSON array of {source_id, package_id, reason}
}

func (ReferencePackage) TableName() string { return "reference_packages" }

// ReferenceCatalogState is the active-corpus + sync state per deployment.
type ReferenceCatalogState struct {
	Base
	OrganizationID  string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Deployment      string `gorm:"type:varchar(32);not null" json:"deployment"` // public|onprem|airgap|tenant
	ActivePackageID string `gorm:"type:varchar(64)" json:"active_package_id"`
	// Sync config: sync_enabled, auto_activate, channel allowlist (JSON).
	SyncEnabled    bool   `gorm:"default:false" json:"sync_enabled"`
	AutoActivate   bool   `gorm:"default:false" json:"auto_activate"` // safe default: manual activation
	ChannelAllow   string `gorm:"type:text" json:"channel_allow,omitempty"`
	LastSyncAt     string `gorm:"type:timestamp" json:"last_sync_at,omitempty"`
	LastSyncResult string `gorm:"type:text" json:"last_sync_result,omitempty"`
}

func (ReferenceCatalogState) TableName() string { return "reference_catalog_state" }

// ReferenceAuditEvent persists retrieval/approval/export actions for audit.
type ReferenceAuditEvent struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Action         string `gorm:"type:varchar(64);not null" json:"action"` // resolve|search|get|import|activate|rollback|export
	SubjectID      string `gorm:"type:varchar(128)" json:"subject_id,omitempty"`
	ByUserID       string `gorm:"type:varchar(64)" json:"by_user_id,omitempty"`
	Details        string `gorm:"type:text" json:"details,omitempty"`
}

func (ReferenceAuditEvent) TableName() string { return "reference_audit_events" }
