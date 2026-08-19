package models

import (
	"gorm.io/gorm"
)

// Governed enterprise model distribution campaigns (PAT-1444): signed
// desired state + customer-side outbound pull over the existing signed
// model registry. Entitlement grants discovery; campaign targets track
// desired vs observed state; the agent reconciler reports receipts.

// ModelPackageEntitlement grants one organization access to one
// immutable package digest. Grant ≠ deployment authorization.
type ModelPackageEntitlement struct {
	gorm.Model
	OrganizationID string `gorm:"index:idx_mpe_org_pkg" json:"organization_id"`
	PackageID      string `gorm:"index:idx_mpe_org_pkg" json:"package_id"`
	GrantedBy      string `json:"granted_by"`
	Reason         string `json:"reason"`
	GrantedAt      string `json:"granted_at"`
	Revoked        bool   `json:"revoked"`
	RevokedAt      string `json:"revoked_at"`
}

// ModelDistributionCampaign is the target-specific rollout policy that
// replaces the package-level `release` string (PAT-1444).
type ModelDistributionCampaign struct {
	gorm.Model
	PackageID       string `gorm:"index" json:"package_id"`
	ManifestDigest  string `json:"manifest_digest"` // immutable content address
	// JSON targets: [{"organization_id":"o1","environments":["prod-eu"]}]
	TargetsJSON     string `gorm:"type:text" json:"targets_json"`
	// JSON rings: {"canary":{"percentage":10,"membership":["o1"]},"beta":{},"stable":{"percentage":100}}
	RingsJSON       string `gorm:"type:text" json:"rings_json"`
	StartTime       string `json:"start_time"`
	MaintenanceWindow string `json:"maintenance_window"`
	Deadline        string `json:"deadline"`
	State           string `json:"state"` // draft|active|paused|completed|cancelled
	MaxConcurrent   int    `json:"max_concurrent_downloads"`
	BandwidthPolicy string `json:"bandwidth_policy"`
	// Manual approval is the default; automatic rollout requires an
	// explicit bounded delegation recorded here.
	DelegationJSON  string `gorm:"type:text" json:"delegation_json"` // {"auto":false,"scope":"","expires_at":""}
	// JSON health gates: {"error_rate":0.02,"load_success":1.0,"attested":true,"observation_minutes":30}
	HealthGatesJSON string `gorm:"type:text" json:"health_gates_json"`
	RollbackVersionsJSON string `gorm:"type:text" json:"rollback_versions_json"`
	Reason          string `json:"reason"`
	CreatedBy       string `json:"created_by"`
	ExpectedEpoch   int    `json:"expected_epoch"`
}

// ModelCampaignTarget tracks desired vs observed state per target.
// Silence is never success: stale agents appear offline_unknown.
type ModelCampaignTarget struct {
	gorm.Model
	CampaignID      string `gorm:"index" json:"campaign_id"`
	OrganizationID  string `gorm:"index" json:"organization_id"`
	Environment     string `json:"environment"`
	Ring            string `json:"ring"`
	DesiredState    string `json:"desired_state"` // staged|canary|active|rolled_back
	ObservedState   string `json:"observed_state"` // see mdObserved* states
	ProgressBytes   int64  `json:"progress_bytes"`
	ProgressShards  int    `json:"progress_shards"`
	CurrentDigest   string `json:"current_digest"`
	PreviousDigest  string `json:"previous_digest"`
	ReasonCode      string `json:"reason_code"`
	LastContact     string `json:"last_contact"`
	AgentReceipt    string `json:"agent_receipt"` // signed receipt ref
	ApprovalState   string `json:"approval_state"` // required|granted|declined
	ApprovedBy      string `json:"approved_by"`
}

// ModelArtifactLease is a short-lived artifact transfer authorization
// bound to (organization, digest). Possession of an expired lease is
// not a durable entitlement.
type ModelArtifactLease struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	PackageID      string `json:"package_id"`
	Token          string `gorm:"uniqueIndex" json:"-"`
	ExpiresAt      string `json:"expires_at"`
	IssuedAt       string `json:"issued_at"`
	Used           bool   `json:"used"`
}

// ModelCampaignHealthEvidence is the objective, versioned health-gate
// evidence a promotion decision relies on. Missing/stale evidence fails
// closed for automatic promotion.
type ModelCampaignHealthEvidence struct {
	gorm.Model
	CampaignID     string `gorm:"index" json:"campaign_id"`
	OrganizationID string `json:"organization_id"`
	LoadSuccess    float64 `json:"load_success"`
	ErrorRate      float64 `json:"error_rate"`
	Attested       bool    `json:"attested"`
	ObservationMinutes int `json:"observation_minutes"`
	RecordedAt     string `json:"recorded_at"`
}
