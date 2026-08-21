package models

// Project groups repositories and sessions under an organizational unit.
type Project struct {
	AuditBase
	Name                string `gorm:"type:varchar(255);not null" json:"name"`
	NameKo              string `gorm:"type:varchar(255)" json:"name_ko"`
	Slug                string `gorm:"type:varchar(128);index" json:"slug"`
	Description         string `gorm:"type:text" json:"description,omitempty"`
	Status              string `gorm:"type:varchar(32);default:'active'" json:"status"`
	AllowedModelClasses string `gorm:"type:text" json:"allowed_model_classes,omitempty"` // JSON array
	PolicyPackID        string `gorm:"type:varchar(64);index" json:"policy_pack_id,omitempty"`
	// Korean enterprise attributes
	ProjectCode    string `gorm:"type:varchar(64)" json:"project_code,omitempty"`
	GroupAffiliate string `gorm:"type:varchar(255)" json:"group_affiliate,omitempty"`
}

// Repository is a Git/SCM repository under control plane governance (PRD §18.1).
type Repository struct {
	AuditBase
	ProjectID     string `gorm:"type:varchar(64);index;not null" json:"project_id"`
	Name          string `gorm:"type:varchar(255);not null" json:"name"`
	FullName      string `gorm:"type:varchar(512)" json:"full_name"` // e.g. payment-service
	CloneURL      string `gorm:"type:varchar(512)" json:"clone_url,omitempty"`
	SCMType       string `gorm:"type:varchar(32);default:'git'" json:"scm_type"` // git, github, gitlab
	SCMProvider   string `gorm:"type:varchar(128)" json:"scm_provider,omitempty"`
	DefaultBranch string `gorm:"type:varchar(128);default:'main'" json:"default_branch"`
	Sensitivity   string `gorm:"type:varchar(32);default:'internal'" json:"sensitivity"` // public, internal, confidential, restricted
	Status        string `gorm:"type:varchar(32);default:'active'" json:"status"`
	// SCM integration state (repositories C1/UX9): populated by the
	// sync pipeline, not enroll-time metadata.
	LastSyncAt   string `gorm:"type:timestamp" json:"last_sync_at,omitempty"`
	LastCommitAt string `gorm:"type:timestamp" json:"last_commit_at,omitempty"`
	SyncStatus   string `gorm:"type:varchar(32)" json:"sync_status,omitempty"` // never, syncing, synced, failed
	// Latest-attempt evidence (PAT-1493): distinct from last_sync_at, which
	// only records the last *successful* sync.
	LastSyncAttemptAt string `gorm:"type:timestamp" json:"last_sync_attempt_at,omitempty"`
	LastSyncHead      string `gorm:"type:varchar(64)" json:"last_sync_head,omitempty"`
	LastSyncError     string `gorm:"type:text" json:"last_sync_error,omitempty"`
	WebhookSecret     string `gorm:"type:varchar(128)" json:"-"`
}

// Branch tracks a repository branch for governance (PRD §18.5).
type Branch struct {
	Base
	RepositoryID     string `gorm:"type:varchar(64);index;not null" json:"repository_id"`
	Name             string `gorm:"type:varchar(255);not null" json:"name"`
	ProtectionLevel  string `gorm:"type:varchar(32);default:'standard'" json:"protection_level"` // standard, protected, locked
	RequiresApproval bool   `gorm:"default:false" json:"requires_approval"`
	BaselineCommit   string `gorm:"type:varchar(64)" json:"baseline_commit,omitempty"`
	Status           string `gorm:"type:varchar(32);default:'active'" json:"status"`
}

// RepoBaseline is the immutable task baseline (PRD §18.3).
type RepoBaseline struct {
	Base
	RepositoryID  string `gorm:"type:varchar(64);index;not null" json:"repository_id"`
	Branch        string `gorm:"type:varchar(255);not null" json:"branch"`
	CommitSHA     string `gorm:"type:varchar(64);not null" json:"commit_sha"`
	CommitMessage string `gorm:"type:text" json:"commit_message,omitempty"`
	AuthorName    string `gorm:"type:varchar(255)" json:"author_name,omitempty"`
	AuthorEmail   string `gorm:"type:varchar(255)" json:"author_email,omitempty"`
	CommittedAt   string `gorm:"type:timestamp" json:"committed_at"`
	TreeDigest    string `gorm:"type:varchar(128)" json:"tree_digest,omitempty"` // content-addressed digest of the tree
	OrgID         string `gorm:"type:varchar(64);index" json:"org_id"`
	CreatedBy     string `gorm:"type:varchar(64)" json:"created_by,omitempty"` // session ID
}

// Session is a working session (DARI §21).
type Session struct {
	AuditBase
	HarnessID    string `gorm:"type:varchar(64);index;not null" json:"harness_id"`
	UserID       string `gorm:"type:varchar(64);index;not null" json:"user_id"`
	ProjectID    string `gorm:"type:varchar(64);index" json:"project_id,omitempty"`
	RepositoryID string `gorm:"type:varchar(64);index" json:"repository_id,omitempty"`
	Branch       string `gorm:"type:varchar(255)" json:"branch,omitempty"`
	BaselineID   string `gorm:"type:varchar(64);index" json:"baseline_id,omitempty"`
	// Protocol state
	SessionID     string `gorm:"type:varchar(64);uniqueIndex" json:"session_id"` // DARI working session ID
	PolicyEpochID string `gorm:"type:varchar(64)" json:"policy_epoch_id"`
	LeaseID       string `gorm:"type:varchar(64);index" json:"lease_id,omitempty"`
	// Session metadata
	TaskPurpose       string `gorm:"type:text" json:"task_purpose,omitempty"`
	Title             string `gorm:"type:varchar(255)" json:"title,omitempty"`
	Status            string `gorm:"type:varchar(32);default:'pending';index:idx_session_lifecycle_sweep,priority:1" json:"status"` // pending, active, idle, closed, terminated
	ModelClass        string `gorm:"type:varchar(64)" json:"model_class,omitempty"`
	ProtectionProfile string `gorm:"type:varchar(16);default:'P0'" json:"protection_profile"`
	SessionTTL        int    `json:"session_ttl,omitempty"` // seconds
	IdleTTL           int    `json:"idle_ttl,omitempty"`    // seconds
	OpenedAt          string `gorm:"type:timestamp;index" json:"opened_at,omitempty"`
	ClosedAt          string `gorm:"type:timestamp" json:"closed_at,omitempty"`
	// LastActivityAt is the last governed-exchange touch (web/02 A4
	// idle detection); empty falls back to OpenedAt.
	LastActivityAt string `gorm:"type:timestamp;index:idx_session_lifecycle_sweep,priority:2" json:"last_activity_at,omitempty"`
}

// PromptExchange records a prompt-response cycle within a session.
type PromptExchange struct {
	Base
	SessionID      string `gorm:"type:varchar(64);index;not null" json:"session_id"`
	ExchangeID     string `gorm:"type:varchar(64);index" json:"exchange_id"` // DARI exchange ID
	PromptText     string `gorm:"type:text" json:"prompt_text,omitempty"`    // may be redacted per policy
	ResponseText   string `gorm:"type:text" json:"response_text,omitempty"`
	ModelPackageID string `gorm:"type:varchar(64)" json:"model_package_id,omitempty"`
	EndpointID     string `gorm:"type:varchar(64)" json:"endpoint_id,omitempty"`
	InputTokens    int    `json:"input_tokens,omitempty"`
	OutputTokens   int    `json:"output_tokens,omitempty"`
	LatencyMs      int    `json:"latency_ms,omitempty"`
	VerdictResult  string `gorm:"type:varchar(64)" json:"verdict_result,omitempty"`
	PolicyEpochID  string `gorm:"type:varchar(64)" json:"policy_epoch_id,omitempty"`
	Status         string `gorm:"type:varchar(32);default:'pending'" json:"status"`
	CreatedAt2     string `gorm:"column:created_at_2;type:timestamp" json:"created_at_2,omitempty"`
}

// ProjectMember is the real membership/role binding (projects B1): the
// roster that entitlement (§13) and analytics read, replacing the
// previous session-derived member count. Org-scoped with a composite
// unique (project, user) — one grant per user per project.
type ProjectMember struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ProjectID      string `gorm:"type:varchar(64);uniqueIndex:idx_pm_proj_user,priority:1;index;not null" json:"project_id"`
	UserID         string `gorm:"type:varchar(64);uniqueIndex:idx_pm_proj_user,priority:2;index;not null" json:"user_id"`
	Role           string `gorm:"type:varchar(32);default:'member'" json:"role"` // owner, admin, maintainer, member, viewer
	GrantedBy      string `gorm:"type:varchar(64)" json:"granted_by,omitempty"`
}

// TableName pins the table name explicitly.
func (ProjectMember) TableName() string { return "project_members" }

// ChangeRequest is an AI change-control queue item (projects B7, PRD
// §33.4): high-risk changesets route here for human decision instead
// of flowing straight through.
type ChangeRequest struct {
	Base
	OrganizationID string  `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ProjectID      string  `gorm:"type:varchar(64);index" json:"project_id"`
	RepositoryID   string  `gorm:"type:varchar(64);index" json:"repository_id"`
	ChangeSetID    string  `gorm:"type:varchar(64);index" json:"change_set_id"`
	SessionID      string  `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	Title          string  `gorm:"type:varchar(255)" json:"title"`
	Kind           string  `gorm:"type:varchar(64)" json:"kind"` // ai_code_change, model_change, config_change
	RiskLevel      string  `gorm:"type:varchar(32)" json:"risk_level"`
	RiskScore      float64 `json:"risk_score"`
	Status         string  `gorm:"type:varchar(32);default:'pending'" json:"status"` // pending, approved, denied
	RequestedBy    string  `gorm:"type:varchar(64)" json:"requested_by"`
	DecidedBy      string  `gorm:"type:varchar(64)" json:"decided_by,omitempty"`
	DecisionReason string  `gorm:"type:text" json:"decision_reason,omitempty"`
	DecidedAt      string  `gorm:"type:timestamp" json:"decided_at,omitempty"`
}
