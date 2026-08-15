package models

// Organization is the top-level tenant entity (PRD §8.1, §12).
type Organization struct {
	Base
	Name             string `gorm:"type:varchar(255);not null" json:"name"`
	NameKo           string `gorm:"type:varchar(255)" json:"name_ko"` // Korean name
	Slug             string `gorm:"type:varchar(128);uniqueIndex;not null" json:"slug"`
	Type             string `gorm:"type:varchar(32);default:'enterprise'" json:"type"` // enterprise, government, sme
	ParentOrgID      string `gorm:"type:varchar(64);index" json:"parent_org_id,omitempty"`
	Profile          string `gorm:"type:varchar(32);default:'enterprise'" json:"profile"` // enterprise, public, sovereign
	Status           string `gorm:"type:varchar(32);default:'active'" json:"status"`
	SSOConfig        string `gorm:"type:text" json:"sso_config,omitempty"` // JSON: OIDC/SAML config
	PolicyPackID     string `gorm:"type:varchar(64);index" json:"policy_pack_id,omitempty"`
	DefaultRetention string `gorm:"type:varchar(64)" json:"default_retention,omitempty"`
	DataRegion       string `gorm:"type:varchar(64)" json:"data_region,omitempty"`
	// Korean enterprise hierarchy (PRD §12)
	BusinessRegistrationNo string `gorm:"type:varchar(32)" json:"business_registration_no,omitempty"`
	GroupCompany           bool   `gorm:"default:false" json:"group_company"`
	// Seat management (Enterprise/Government licensing)
	MaxUserSeats    int    `gorm:"default:50" json:"max_user_seats"`
	MaxHarnessSeats int    `gorm:"default:100" json:"max_harness_seats"`
	PlanTier        string `gorm:"type:varchar(32);default:'team'" json:"plan_tier"` // team, business, enterprise
	PlanRenewalDate string `gorm:"type:timestamp" json:"plan_renewal_date,omitempty"`
	BillingContact  string `gorm:"type:varchar(255)" json:"billing_contact,omitempty"`
}

// BusinessUnit represents organizational hierarchy (PRD §12.1).
type BusinessUnit struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ParentUnitID   string `gorm:"type:varchar(64);index" json:"parent_unit_id,omitempty"`
	Name           string `gorm:"type:varchar(255);not null" json:"name"`
	NameKo         string `gorm:"type:varchar(255)" json:"name_ko"`
	Type           string `gorm:"type:varchar(32)" json:"type"` // affiliate, business_unit, department, team
	Level          int    `json:"level"`                        // depth in hierarchy
}

// User is an authenticated human user (PRD §8.1).
type User struct {
	AuditBase
	Email          string `gorm:"type:varchar(255);uniqueIndex:idx_email_org" json:"email"`
	EmailKo        string `gorm:"type:varchar(255)" json:"email_ko,omitempty"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	NameKo         string `gorm:"type:varchar(255)" json:"name_ko"` // Korean name (PRD §44.3)
	EmployeeId     string `gorm:"type:varchar(128)" json:"employee_id,omitempty"`
	Title          string `gorm:"type:varchar(255)" json:"title,omitempty"`
	TitleKo        string `gorm:"type:varchar(255)" json:"title_ko,omitempty"`
	Status         string `gorm:"type:varchar(32);default:'active'" json:"status"`      // active, suspended, offboarded
	AuthMethod     string `gorm:"type:varchar(32)" json:"auth_method"`                  // oidc, saml, ldap, local
	ExternalID     string `gorm:"type:varchar(255);index" json:"external_id,omitempty"` // SSO subject
	MFAEnrolled    bool   `gorm:"default:false" json:"mfa_enrolled"`
	WebAuthnCredID string `gorm:"type:text" json:"webauthn_credential_id,omitempty"`
	// Korean enterprise attributes (PRD §12.2)
	BusinessUnitID  string  `gorm:"type:varchar(64);index" json:"business_unit_id,omitempty"`
	ContractorInfo  string  `gorm:"type:text" json:"contractor_info,omitempty"` // JSON
	OffboardingDate *string `gorm:"type:date" json:"offboarding_date,omitempty"`
	Locale          string  `gorm:"type:varchar(10);default:'ko-KR'" json:"locale"`
	Timezone        string  `gorm:"type:varchar(64);default:'Asia/Seoul'" json:"timezone"`
	LastLoginAt     *string `gorm:"type:timestamp" json:"last_login_at,omitempty"`
}

// Role defines authorization roles (PRD §8.1).
type Role struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Name           string `gorm:"type:varchar(128);not null" json:"name"`
	NameKo         string `gorm:"type:varchar(128)" json:"name_ko"`
	Permissions    string `gorm:"type:text" json:"permissions"` // JSON array of permission strings
	IsSystem       bool   `gorm:"default:false" json:"is_system"`
}

// UserRole is the join table for user-role assignments.
type UserRole struct {
	Base
	UserID         string `gorm:"type:varchar(64);index;not null" json:"user_id"`
	RoleID         string `gorm:"type:varchar(64);index;not null" json:"role_id"`
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Scope          string `gorm:"type:varchar(64)" json:"scope,omitempty"` // project, org, global
	ScopeID        string `gorm:"type:varchar(64)" json:"scope_id,omitempty"`
}

// Device represents a physical or virtual device (PRD §8.1).
type Device struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	UserID         string `gorm:"type:varchar(64);index" json:"user_id,omitempty"`
	Hostname       string `gorm:"type:varchar(255)" json:"hostname"`
	OS             string `gorm:"type:varchar(64)" json:"os"`
	OSVersion      string `gorm:"type:varchar(128)" json:"os_version"`
	Arch           string `gorm:"type:varchar(32)" json:"arch"` // amd64, arm64
	MDMEnrolled    bool   `gorm:"default:false" json:"mdm_enrolled"`
	MDMPosture     string `gorm:"type:text" json:"mdm_posture,omitempty"`
	PublicKey      string `gorm:"type:text" json:"public_key"` // Ed25519 public key (hex)
	NetworkZone    string `gorm:"type:varchar(128)" json:"network_zone,omitempty"`
	IPAddress      string `gorm:"type:varchar(64)" json:"ip_address,omitempty"`
	Status         string `gorm:"type:varchar(32);default:'active'" json:"status"` // active, revoked, quarantined
	FirstSeen      string `gorm:"type:timestamp" json:"first_seen"`
	LastSeen       string `gorm:"type:timestamp" json:"last_seen"`
}

// Harness is an enrolled Patty Code Harness instance (PRD §8.4).
type Harness struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	DeviceID       string `gorm:"type:varchar(64);index" json:"device_id"`
	HarnessID      string `gorm:"type:varchar(64);uniqueIndex;not null" json:"harness_id"` // peer ID
	Name           string `gorm:"type:varchar(255)" json:"name"`
	// Build info
	BinaryVersion    string `gorm:"type:varchar(64)" json:"binary_version"`
	BinaryHash       string `gorm:"type:varchar(128)" json:"binary_hash"`
	ExtensionVersion string `gorm:"type:varchar(64)" json:"extension_version,omitempty"`
	CLIVersion       string `gorm:"type:varchar(64)" json:"cli_version,omitempty"`
	BuildChannel     string `gorm:"type:varchar(32);default:'stable'" json:"build_channel"`
	// Identity
	PublicKey        string `gorm:"type:text;not null" json:"public_key"`  // Ed25519 public key (hex)
	CredentialJSON   string `gorm:"type:text" json:"credential,omitempty"` // PPC (COSE-Sign1 CBOR, hex)
	CredentialDigest string `gorm:"type:varchar(128)" json:"credential_digest"`
	// Authorization
	AllowedUsers  string `gorm:"type:text" json:"allowed_users,omitempty"` // JSON array of user IDs
	PolicyProfile string `gorm:"type:varchar(64)" json:"policy_profile"`
	LicenseState  string `gorm:"type:varchar(32)" json:"license_state,omitempty"`
	// Lifecycle
	Status           string `gorm:"type:varchar(32);default:'pending'" json:"status"`    // pending, enrolled, active, quarantined, revoked
	RiskState        string `gorm:"type:varchar(32);default:'normal'" json:"risk_state"` // normal, elevated, high
	RevocationReason string `gorm:"type:text" json:"revocation_reason,omitempty"`
	EnrollmentMode   string `gorm:"type:varchar(32)" json:"enrollment_mode"` // sso, pre_approved, offline
	EnrolledAt       string `gorm:"type:timestamp" json:"enrolled_at,omitempty"`
	LastHeartbeat    string `gorm:"type:timestamp" json:"last_heartbeat,omitempty"`
	LastAttestation  string `gorm:"type:timestamp" json:"last_attestation,omitempty"`
	// Network
	CPEndpoint  string `gorm:"type:varchar(255)" json:"cp_endpoint,omitempty"`
	NetworkZone string `gorm:"type:varchar(128)" json:"network_zone,omitempty"`
}

// EnrollmentCode is a one-time enrollment code (PRD §17.2).
type EnrollmentCode struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Code           string `gorm:"type:varchar(128);uniqueIndex;not null" json:"-"`
	UserID         string `gorm:"type:varchar(64)" json:"user_id,omitempty"`
	ExpiresAt      string `gorm:"type:timestamp" json:"expires_at"`
	Used           bool   `gorm:"default:false" json:"used"`
	UsedBy         string `gorm:"type:varchar(64)" json:"used_by,omitempty"` // harness ID
}

// HarnessStatusPermitted is the single predicate for harness standing
// on live paths (connect, governed exchange). Any status outside this
// set fails closed.
func HarnessStatusPermitted(status string) bool {
	return status == "enrolled" || status == "active"
}

// CredentialRevocationRecord is the durable revocation ledger: the
// monotonic epoch and every revoked credential serial. The relay
// rebuilds its in-process view from this table at boot so revocation
// survives restarts (DARI §F.7).
type CredentialRevocationRecord struct {
	Base
	Serial       string `gorm:"type:varchar(128);uniqueIndex;not null" json:"serial"`
	RevokedEpoch uint64 `json:"revoked_epoch"`
	Reason       string `gorm:"type:text" json:"reason,omitempty"`
	RevokedAtRFC string `gorm:"type:timestamp" json:"revoked_at"`
}

// TableName overrides the table name.
func (CredentialRevocationRecord) TableName() string { return "credential_revocations" }

// SandboxRecord is the durable sandbox definition + lifecycle state
// (PRD §31). The governance snapshot's sandbox rows (E4) and the
// admin UI read THIS table — not audit-event reconstruction.
type SandboxRecord struct {
	Base
	OrganizationID  string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	SessionID       string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	UserID          string `gorm:"type:varchar(64)" json:"user_id,omitempty"`
	Mode            string `gorm:"type:varchar(32)" json:"mode"`
	BaseImage       string `gorm:"type:varchar(255)" json:"base_image"`
	ImageDigest     string `gorm:"type:varchar(128)" json:"image_digest,omitempty"`
	CPULimit        string `gorm:"type:varchar(32)" json:"cpu_limit,omitempty"`
	MemoryLimitMB   int    `json:"memory_limit_mb,omitempty"`
	NetworkPolicy   string `gorm:"type:varchar(64)" json:"network_policy,omitempty"`
	Status          string `gorm:"type:varchar(32);index" json:"status"`
	RuntimeProvider string `gorm:"type:varchar(128)" json:"runtime_provider,omitempty"`
	// ResourceLimitsJSON carries the full limits map when set.
	ResourceLimitsJSON string `gorm:"type:text" json:"resource_limits_json,omitempty"`
}

// TableName overrides the table name.
func (SandboxRecord) TableName() string { return "sandbox_records" }
