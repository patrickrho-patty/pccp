package models

// Hardened dev-environment lifecycle (PAT-1452): ephemeral / persistent /
// pinned-workspace modes governed at scoped policy, immutable signed
// environment templates + repository mappings, runner-pool capacity registry,
// and environment/workspace inventory with readiness, drift, and evidence.
// Conversation state and environment state are deliberately separate models —
// resume never restores an environment.

// SandboxEnvironmentTemplate is an immutable, versioned, signed environment
// template: verified image reference + digest, toolchains/limits/security
// profile, bootstrap manifest, and supported repository mappings. Secrets are
// never baked into templates.
type SandboxEnvironmentTemplate struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	TemplateID     string `gorm:"type:varchar(64);uniqueIndex;not null" json:"template_id"`
	Version        uint64 `json:"version"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	NameKo         string `gorm:"type:varchar(255)" json:"name_ko,omitempty"`
	// Immutable image reference + verified digest.
	ImageRef    string `gorm:"type:varchar(512)" json:"image_ref"`
	ImageDigest string `gorm:"type:varchar(128)" json:"image_digest"`
	// Toolchains / SDKs / deps (JSON array).
	Toolchains string `gorm:"type:text" json:"toolchains,omitempty"`
	// Resource defaults and limits (JSON).
	ResourceDefaults string `gorm:"type:text" json:"resource_defaults,omitempty"`
	ResourceLimits   string `gorm:"type:text" json:"resource_limits,omitempty"`
	// Filesystem mounts + workspace layout (JSON).
	WSLayout string `gorm:"type:text" json:"workspace_layout,omitempty"`
	// Runtime security profile (JSON) — capabilities contract.
	SecurityProfile string `gorm:"type:text" json:"security_profile,omitempty"`
	// Bootstrap manifest (versioned, signed, bounded).
	BootstrapManifest  string `gorm:"type:text" json:"bootstrap_manifest,omitempty"`
	BootstrapVersion   uint64 `json:"bootstrap_version,omitempty"`
	BootstrapDigest    string `gorm:"type:varchar(128)" json:"bootstrap_digest,omitempty"`
	BootstrapSignature string `gorm:"type:text" json:"bootstrap_signature,omitempty"`
	// Supported repository mappings (JSON array of repo IDs).
	RepoMappings string `gorm:"type:text" json:"repo_mappings,omitempty"`
	// Lifecycle: draft|active|retired
	Status     string `gorm:"type:varchar(16);default:'active'" json:"status"`
	ApprovedBy string `gorm:"type:varchar(64)" json:"approved_by,omitempty"`
}

func (SandboxEnvironmentTemplate) TableName() string { return "sandbox_environment_templates" }

// SandboxLifecyclePolicy is the effective lifecycle-mode selection at a scope
// (org/team/project/repository/pool). More specific policy may strengthen but
// never weaken Patty's critical baseline.
type SandboxLifecyclePolicy struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Scope          string `gorm:"type:varchar(16);index" json:"scope"` // org|team|project|repository|pool
	ScopeID        string `gorm:"type:varchar(64);index" json:"scope_id,omitempty"`
	// Mode: ephemeral|persistent|pinned.
	Mode       string `gorm:"type:varchar(16);not null" json:"mode"`
	TemplateID string `gorm:"type:varchar(64)" json:"template_id,omitempty"`
	// Priority this policy applies at (higher = narrower). Strengthen-only:
	// a narrower scope cannot pick a weaker isolation mode than its parent.
	Priority  int    `gorm:"default:0" json:"priority"`
	CreatedBy string `gorm:"type:varchar(64)" json:"created_by,omitempty"`
}

func (SandboxLifecyclePolicy) TableName() string { return "sandbox_lifecycle_policies" }

// SandboxRunner is one registered execution-capacity unit (Docker host, k8s
// cluster, VM, dedicated host, or governed workstation). Runtime-specific
// behavior stays behind adapters; PCCP stores normalized capability/compliance.
type SandboxRunner struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	RunnerID       string `gorm:"type:varchar(64);uniqueIndex;not null" json:"runner_id"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	RuntimeType    string `gorm:"type:varchar(24);not null" json:"runtime_type"` // docker|kubernetes|vm|dedicated_host|workstation
	// Normalized capabilities (JSON): network_isolation, mount_policy,
	// secret_injection, snapshots, process_containment, non_root.
	Capabilities string `gorm:"type:text" json:"capabilities,omitempty"`
	// Capacity.
	MaxConcurrency int `gorm:"default:8" json:"max_concurrency"`
	ActiveCount    int `gorm:"default:0" json:"active_count"`
	// Reachability + compliance.
	Status       string `gorm:"type:varchar(16);default:'ok'" json:"status"` // ok|degraded|offline|noncompliant
	LastSeenAt   string `gorm:"type:timestamp" json:"last_seen_at,omitempty"`
	Compliance   string `gorm:"type:varchar(32)" json:"compliance,omitempty"`           // compliant|unsupported_control|pending
	PinnedUserID string `gorm:"type:varchar(64);index" json:"pinned_user_id,omitempty"` // for workstation runners
	OwnerGroup   string `gorm:"type:varchar(128)" json:"owner_group,omitempty"`
}

func (SandboxRunner) TableName() string { return "sandbox_runners" }

// SandboxEnvironment is a durable environment/workspace identity. For
// ephemeral it is created and destroyed per session; for persistent it
// survives sessions keyed by user+repository; for pinned it is bound to one
// workstation. Never holds credentials.
type SandboxEnvironment struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	EnvironmentID  string `gorm:"type:varchar(64);uniqueIndex;not null" json:"environment_id"`
	// WorkspaceIdentity is the reattachment key (user+repository for
	// persistent, user+workstation for pinned, "" for ephemeral).
	WorkspaceIdentity string `gorm:"type:varchar(255);index" json:"workspace_identity,omitempty"`
	UserID            string `gorm:"type:varchar(64);index;not null" json:"user_id"`
	RepositoryID      string `gorm:"type:varchar(64);index" json:"repository_id,omitempty"`
	HarnessID         string `gorm:"type:varchar(64);index" json:"harness_id,omitempty"` // pinned workstation / placing harness
	RunnerID          string `gorm:"type:varchar(64);index" json:"runner_id,omitempty"`
	// Mode: ephemeral|persistent|pinned.
	Mode             string `gorm:"type:varchar(16);not null" json:"mode"`
	TemplateID       string `gorm:"type:varchar(64);index" json:"template_id,omitempty"`
	TemplateVersion  uint64 `json:"template_version,omitempty"`
	TemplateDigest   string `gorm:"type:varchar(128)" json:"template_digest,omitempty"`
	BootstrapVersion uint64 `json:"bootstrap_version,omitempty"`
	// Initial revision checked out at prep.
	InitialRevision string `gorm:"type:varchar(128)" json:"initial_revision,omitempty"`
	// Status: preparing|ready|attached|paused|quarantined|draining|destroyed|unavailable|failed
	Status string `gorm:"type:varchar(24);index;not null;default:'preparing'" json:"status"`
	// Drift: none|template|bootstrap|config|secrets|policy
	DriftStatus string `gorm:"type:varchar(24);default:'none'" json:"drift_status,omitempty"`
	// Effective policy version at last attach.
	PolicyEpochID string `gorm:"type:varchar(64)" json:"policy_epoch_id,omitempty"`
	// Attached session (only one writable attach allowed unless collaboration).
	AttachedSessionID string `gorm:"type:varchar(64);index" json:"attached_session_id,omitempty"`
	CreatedForSession string `gorm:"type:varchar(64)" json:"created_for_session,omitempty"`
	// Readiness + evidence.
	Ready  bool   `gorm:"default:false" json:"ready"`
	Reason string `gorm:"type:text" json:"reason,omitempty"`
	// Lifecycle timestamps.
	ReadyAt     string `gorm:"type:timestamp" json:"ready_at,omitempty"`
	ExpireAt    string `gorm:"type:timestamp" json:"expire_at,omitempty"`
	DestroyedAt string `gorm:"type:timestamp" json:"destroyed_at,omitempty"`
	Utilization string `gorm:"type:varchar(32)" json:"utilization,omitempty"`
}

func (SandboxEnvironment) TableName() string { return "sandbox_environments" }
