package models

// Managed system-prompt additions (PAT-1455). One current prompt-addition
// document per target scope (org/team/fleet/user), immutable version history,
// and a signed epoch for distribution. Prompt bodies are static instruction
// content — never interpolation of credentials/attributes/DB values.

// SystemPromptDocument is the current managed prompt-addition document for one
// target (organization/team/fleet/user). Active if Enabled and not Disabled.
type SystemPromptDocument struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	// ScopeTarget: empty for org scope, team id / fleet id / user id otherwise.
	Scope   string `gorm:"type:varchar(16);index;not null" json:"scope"` // org|team|fleet|user
	ScopeID string `gorm:"type:varchar(64);index" json:"scope_id,omitempty"`
	// Title is a short admin-facing name for the addition.
	Title string `gorm:"type:varchar(255)" json:"title,omitempty"`
	// Content is the managed instruction text appended for the scope.
	Content string `gorm:"type:text" json:"content"`
	// Version is the current immutable version number (monotonic).
	Version uint64 `json:"version"`
	// Digest is the SHA-256 of canonical(scope, scope_id, content).
	Digest string `gorm:"type:varchar(128);index" json:"digest"`
	// Enabled toggles whether the current version is active.
	Enabled bool `gorm:"default:true" json:"enabled"`
	// DisabledReason records why an addition was disabled (target deleted, etc).
	DisabledReason string `gorm:"type:text" json:"disabled_reason,omitempty"`
	CreatedBy      string `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	// EpochID is the prompt-policy epoch this version was distributed in.
	EpochID string `gorm:"type:varchar(64);index" json:"epoch_id,omitempty"`
}

func (SystemPromptDocument) TableName() string { return "system_prompt_documents" }

// SystemPromptVersion is an immutable version of a managed addition. Restore
// creates a NEW version with the restored content; history is never rewritten.
type SystemPromptVersion struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	DocumentID     string `gorm:"type:varchar(64);index;not null" json:"document_id"`
	Scope          string `gorm:"type:varchar(16);index" json:"scope"`
	ScopeID        string `gorm:"type:varchar(64);index" json:"scope_id,omitempty"`
	Version        uint64 `json:"version"`
	Content        string `gorm:"type:text" json:"content"`
	Digest         string `gorm:"type:varchar(128)" json:"digest"`
	CreatedBy      string `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	// RestoredFrom points at the version whose content was restored (0 = new edit).
	RestoredFrom uint64 `json:"restored_from,omitempty"`
}

func (SystemPromptVersion) TableName() string { return "system_prompt_versions" }

// SystemPromptEpoch is an immutable, signed snapshot of the org's effective
// managed prompt additions, delivered over the relay directive carrier.
type SystemPromptEpoch struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	EpochID        string `gorm:"type:varchar(64);uniqueIndex;not null" json:"epoch_id"`
	EpochNumber    uint64 `gorm:"index" json:"epoch_number"`
	// AdditionsJSON is the canonical serialized effective addition set
	// included in the digest/signature.
	AdditionsJSON string `gorm:"type:text" json:"additions,omitempty"`
	Digest        string `gorm:"type:varchar(128)" json:"digest,omitempty"`
	SignatureHex  string `gorm:"type:text" json:"signature,omitempty"`
	CreatedBy     string `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	SupersededBy  string `gorm:"type:varchar(64)" json:"superseded_by,omitempty"`
	Status        string `gorm:"type:varchar(32);default:'active'" json:"status"`
	EffectiveAt   string `gorm:"type:timestamp" json:"effective_at"`
}

func (SystemPromptEpoch) TableName() string { return "system_prompt_epochs" }
