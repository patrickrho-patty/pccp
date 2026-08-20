package models

import (
	"time"
)

// SkillPolicyAssignment is one administrator decision for a skill at one scope
// (PAT-1456). It is the persisted form of skillpolicy.Assignment: canonical
// skill/package identity + approved content digest, a scope, and one of
// Required / Optional / Blocked. Policy identity is the canonical
// identity + digest, never a display name or path, so renaming/copying/moving/
// shadowing a blocked skill cannot bypass the decision.
type SkillPolicyAssignment struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	// ScopeTarget identifies the assignment target within the scope:
	// empty for org scope, team id for team scope, fleet/harness id for
	// fleet scope, user id for user scope.
	Scope   string `gorm:"type:varchar(16);index;not null" json:"scope"` // org|team|fleet|user
	ScopeID string `gorm:"type:varchar(64);index" json:"scope_id,omitempty"`
	// SkillIdentity is the canonical package/skill identity (id@package).
	SkillIdentity string `gorm:"type:varchar(255);index;not null" json:"skill_identity"`
	// Digest is the approved content digest. An empty digest means the
	// identity is unapproved/unverified (Blocked once enforcement is on).
	Digest string `gorm:"type:varchar(128)" json:"digest,omitempty"`
	// State is one of required|optional|blocked.
	State string `gorm:"type:varchar(16);not null" json:"state"`
	// Reason records why this assignment was made (optional).
	Reason string `gorm:"type:text" json:"reason,omitempty"`
	// CreatedBy is the admin user id that issued the assignment.
	CreatedBy string `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	// EpochID is the skill-policy epoch this assignment belongs to once
	// delivered (immutable snapshot identity).
	EpochID string `gorm:"type:varchar(64);index" json:"epoch_id,omitempty"`
}

func (SkillPolicyAssignment) TableName() string { return "skill_policy_assignments" }

// HarnessSkillReport is a durable per-harness skill inventory report
// (PAT-1456). Every managed harness reports every discovered skill — built-in,
// project, user/global, plugin, custom-path, or managed-package — including
// disabled and shadowed entries. Local skill bodies never leave the deployment;
// only metadata + content digests are reported to PCCP.
type HarnessSkillReport struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	HarnessID      string `gorm:"type:varchar(64);index;not null" json:"harness_id"`
	SessionID      string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	// SkillIdentity is the canonical package/skill identity (id@package).
	SkillIdentity string `gorm:"type:varchar(255);index;not null" json:"skill_identity"`
	// Display fields for the admin inventory view.
	DisplayName   string `gorm:"type:varchar(255)" json:"display_name,omitempty"`
	Description   string `gorm:"type:text" json:"description,omitempty"`
	ExecutionMode string `gorm:"type:varchar(16)" json:"execution_mode,omitempty"` // inline|subagent
	Source        string `gorm:"type:varchar(32)" json:"source,omitempty"`         // builtin|project|user|plugin|custom|managed
	PluginPackage string `gorm:"type:varchar(255)" json:"plugin_package,omitempty"`
	PluginVersion string `gorm:"type:varchar(64)" json:"plugin_version,omitempty"`
	PackageDigest string `gorm:"type:varchar(128)" json:"package_digest,omitempty"`
	// ContentDigest is the cryptographic digest of the skill body.
	ContentDigest string `gorm:"type:varchar(128);index" json:"content_digest,omitempty"`
	// Requested metadata (skills may request more than they are granted).
	RequestedModel string    `gorm:"type:varchar(64)" json:"requested_model,omitempty"`
	RequestedTools string    `gorm:"type:text" json:"requested_tools,omitempty"` // JSON array
	ReadOnly       bool      `gorm:"default:false" json:"read_only,omitempty"`
	Enabled        bool      `gorm:"default:false" json:"enabled"`
	Shadowed       bool      `gorm:"default:false" json:"shadowed,omitempty"`
	Duplicate      bool      `gorm:"default:false" json:"duplicate,omitempty"`
	SignatureState string    `gorm:"type:varchar(32)" json:"signature_state,omitempty"` // verified|unverified|unsigned
	DiscoveredAt   time.Time `json:"discovered_at,omitempty"`
	LastVerifiedAt time.Time `json:"last_verified_at,omitempty"`
}

func (HarnessSkillReport) TableName() string { return "harness_skill_reports" }

// SkillPolicyEpoch is an immutable, signed snapshot of an organization's
// effective skill policy, delivered to harnesses over the RelayControlEvent
// carrier. A new epoch is issued on every assignment change.
type SkillPolicyEpoch struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	EpochID        string `gorm:"type:varchar(64);uniqueIndex;not null" json:"epoch_id"`
	EpochNumber    uint64 `gorm:"index" json:"epoch_number"`
	// AssignmentsJSON is the canonical serialized assignment set included in
	// the digest/signature.
	AssignmentsJSON string `gorm:"type:text" json:"assignments,omitempty"`
	// Digest is the SHA-256 over the canonical assignments payload.
	Digest string `gorm:"type:varchar(128)" json:"digest,omitempty"`
	// SignatureHex is a detached COSE-Sign1 hex signature over the digest.
	SignatureHex string `gorm:"type:text" json:"signature,omitempty"`
	// EnforcementEnabled records whether the epoch was issued with
	// fail-closed enforcement (unknown skills blocked).
	EnforcementEnabled bool   `gorm:"default:true" json:"enforcement_enabled"`
	CreatedBy          string `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	SupersededBy       string `gorm:"type:varchar(64)" json:"superseded_by,omitempty"`
	Status             string `gorm:"type:varchar(32);default:'active'" json:"status"`
	EffectiveAt        string `gorm:"type:timestamp" json:"effective_at"`
}

func (SkillPolicyEpoch) TableName() string { return "skill_policy_epochs" }
