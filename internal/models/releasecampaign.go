package models

import (
	"time"

	"gorm.io/gorm"
)

// Governed harness update campaigns (PAT-1449): release catalog with
// verifiable build identity, target/minimum separation, deterministic
// rings, per-harness derived states, and scoped expiring exceptions.

// HarnessRelease is one immutable entry in the approved release catalog.
// Identity is the (ReleaseID, ArtifactDigest) pair — a self-reported
// version string alone never qualifies.
type HarnessRelease struct {
	gorm.Model
	ReleaseID      string `gorm:"uniqueIndex" json:"release_id"` // immutable
	Version        string `json:"version"`                       // canonical semver
	BuildProfile   string `json:"build_profile"`                 // public|enterprise|sovereign
	Platform       string `json:"platform"`                      // "darwin/arm64/tarball" etc
	ArtifactDigest string `json:"artifact_digest"`               // sha256:…
	SignatureKeyID string `json:"signature_key_id"`
	Channel        string `json:"channel"` // stable|beta|canary
	NotesKo        string `json:"notes_ko"`
	PublishedAt    string `json:"published_at"`
	Revoked        bool   `json:"revoked"`
	RevokedAt      string `json:"revoked_at"`
	RevokedReason  string `json:"revoked_reason"`
}

// HarnessUpdateCampaign is a staged release campaign. Target version =
// what eligible harnesses should install; minimum version = the oldest
// still permitted to operate. Percentage membership is deterministic via
// CohortSeed so a harness never jumps cohorts across heartbeats.
type HarnessUpdateCampaign struct {
	gorm.Model
	ReleaseID     string `gorm:"index" json:"release_id"`
	TargetVersion string `json:"target_version"`
	MinVersion    string `json:"min_version"`
	Ring          string `json:"ring"`       // canary|beta|stable
	Percentage    int    `json:"percentage"` // 0-100 deterministic cohort
	CohortSeed    string `json:"cohort_seed"`
	StartTime     string `json:"start_time"`
	Deadline      string `json:"deadline"` // enforcement: restricted after this
	Severity      string `json:"severity"` // ordinary|emergency
	State         string `json:"state"`    // draft|active|paused|cancelled|completed
	// JSON: {"self_managed":true,"package_manager":true,"managed_direct":true,"compliance_only":true,"air_gapped":true}
	DeliveryModesJSON string `gorm:"type:text" json:"delivery_modes_json"`
	MaintenanceWindow string `json:"maintenance_window"`
	// JSON thresholds: {"crash_rate":0.05,"update_failure_rate":0.1,"startup_failure_rate":0.05}
	HealthThresholdsJSON string `gorm:"type:text" json:"health_thresholds_json"`
	Reason               string `json:"reason"`
	CreatedBy            string `json:"created_by"`
	ExpectedEpoch        int    `json:"expected_epoch"` // CAS on operator mutations
}

// HarnessCampaignTarget is the derived per-harness campaign state. States
// are computed from campaign + release + heartbeat + attestation +
// exception evidence — never edited as free strings.
type HarnessCampaignTarget struct {
	gorm.Model
	CampaignID        string `gorm:"index" json:"campaign_id"`
	HarnessID         string `gorm:"index" json:"harness_id"`
	OrganizationID    string `gorm:"index" json:"organization_id"`
	State             string `json:"state"` // supported|update_available|update_required_grace|restricted|revoked|updating|verifying|rollback_required|repair_required|unknown_or_tampered
	ReasonCode        string `json:"reason_code"`
	ReportedVersion   string `json:"reported_version"`
	ReportedReleaseID string `json:"reported_release_id"`
	ReportedDigest    string `json:"reported_digest"`
	ComputedAt        string `json:"computed_at"`
}

// HarnessVersionException is a managed, temporary, expiring exception for
// ordinary customer-controlled deadlines. It can never bypass the hosted
// floor, revoked artifacts, or unknown identity.
type HarnessVersionException struct {
	gorm.Model
	OrganizationID       string    `gorm:"index" json:"organization_id"`
	HarnessIDsJSON       string    `gorm:"type:text" json:"harness_ids_json"`
	CurrentVersion       string    `json:"current_version"`
	TargetVersion        string    `json:"target_version"`
	Reason               string    `json:"reason"`
	Owner                string    `json:"owner"`
	ApprovedBy           string    `json:"approved_by"`
	CompensatingControls string    `json:"compensating_controls"`
	StartedAt            string    `json:"started_at"`
	ExpiresAt            string    `json:"expires_at"`
	Revoked              bool      `json:"revoked"`
	RevokedReason        string    `json:"revoked_reason"`
	CreatedAt            time.Time `json:"created_at"`
}

// HarnessHeartbeatReport is the verifiable build/installation identity
// bound to the authenticated harness session (PAT-1449 release identity).
type HarnessHeartbeatReport struct {
	gorm.Model
	HarnessID         string `gorm:"index" json:"harness_id"`
	OrganizationID    string `gorm:"index" json:"organization_id"`
	BuildProfile      string `json:"build_profile"`
	Version           string `json:"version"`
	ReleaseID         string `json:"release_id"`
	SourceRevision    string `json:"source_revision"`
	BuildTimestamp    string `json:"build_timestamp"`
	SignatureKeyID    string `json:"signature_key_id"`
	ExecutableDigest  string `json:"executable_digest"`
	OS                string `json:"os"`
	Arch              string `json:"arch"`
	Packaging         string `json:"packaging"`
	InstallationOwner string `json:"installation_owner"`
	PolicyEpoch       int    `json:"policy_epoch"`
	ReportedAt        string `json:"reported_at"`
}
