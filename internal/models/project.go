package models

// Project groups repositories and sessions under an organizational unit.
type Project struct {
	AuditBase
	Name           string `gorm:"type:varchar(255);not null" json:"name"`
	NameKo         string `gorm:"type:varchar(255)" json:"name_ko"`
	Slug           string `gorm:"type:varchar(128);index" json:"slug"`
	Description    string `gorm:"type:text" json:"description,omitempty"`
	Status         string `gorm:"type:varchar(32);default:'active'" json:"status"`
	AllowedModelClasses string `gorm:"type:text" json:"allowed_model_classes,omitempty"` // JSON array
	PolicyPackID   string `gorm:"type:varchar(64);index" json:"policy_pack_id,omitempty"`
	// Korean enterprise attributes
	ProjectCode    string `gorm:"type:varchar(64)" json:"project_code,omitempty"`
	GroupAffiliate string `gorm:"type:varchar(255)" json:"group_affiliate,omitempty"`
}

// Repository is a Git/SCM repository under control plane governance (PRD §18.1).
type Repository struct {
	AuditBase
	ProjectID      string `gorm:"type:varchar(64);index;not null" json:"project_id"`
	Name           string `gorm:"type:varchar(255);not null" json:"name"`
	FullName       string `gorm:"type:varchar(512)" json:"full_name"` // e.g. payment-service
	CloneURL       string `gorm:"type:varchar(512)" json:"clone_url,omitempty"`
	SCMType        string `gorm:"type:varchar(32);default:'git'" json:"scm_type"` // git, github, gitlab
	SCMProvider    string `gorm:"type:varchar(128)" json:"scm_provider,omitempty"`
	DefaultBranch  string `gorm:"type:varchar(128);default:'main'" json:"default_branch"`
	Sensitivity    string `gorm:"type:varchar(32);default:'internal'" json:"sensitivity"` // public, internal, confidential, restricted
	Status         string `gorm:"type:varchar(32);default:'active'" json:"status"`
}

// Branch tracks a repository branch for governance (PRD §18.5).
type Branch struct {
	Base
	RepositoryID   string `gorm:"type:varchar(64);index;not null" json:"repository_id"`
	Name           string `gorm:"type:varchar(255);not null" json:"name"`
	ProtectionLevel string `gorm:"type:varchar(32);default:'standard'" json:"protection_level"` // standard, protected, locked
	RequiresApproval bool  `gorm:"default:false" json:"requires_approval"`
	BaselineCommit string `gorm:"type:varchar(64)" json:"baseline_commit,omitempty"`
	Status         string `gorm:"type:varchar(32);default:'active'" json:"status"`
}

// RepoBaseline is the immutable task baseline (PRD §18.3).
type RepoBaseline struct {
	Base
	RepositoryID   string `gorm:"type:varchar(64);index;not null" json:"repository_id"`
	Branch         string `gorm:"type:varchar(255);not null" json:"branch"`
	CommitSHA      string `gorm:"type:varchar(64);not null" json:"commit_sha"`
	CommitMessage  string `gorm:"type:text" json:"commit_message,omitempty"`
	AuthorName     string `gorm:"type:varchar(255)" json:"author_name,omitempty"`
	AuthorEmail    string `gorm:"type:varchar(255)" json:"author_email,omitempty"`
	CommittedAt    string `gorm:"type:timestamp" json:"committed_at"`
	TreeDigest     string `gorm:"type:varchar(128)" json:"tree_digest,omitempty"` // content-addressed digest of the tree
	OrgID          string `gorm:"type:varchar(64);index" json:"org_id"`
	CreatedBy      string `gorm:"type:varchar(64)" json:"created_by,omitempty"` // session ID
}

// Session is a working session (DARI §21).
type Session struct {
	AuditBase
	HarnessID      string `gorm:"type:varchar(64);index;not null" json:"harness_id"`
	UserID         string `gorm:"type:varchar(64);index;not null" json:"user_id"`
	ProjectID      string `gorm:"type:varchar(64);index" json:"project_id"`
	RepositoryID   string `gorm:"type:varchar(64);index" json:"repository_id,omitempty"`
	Branch         string `gorm:"type:varchar(255)" json:"branch,omitempty"`
	BaselineID     string `gorm:"type:varchar(64);index" json:"baseline_id,omitempty"`
	// Protocol state
	SessionID      string `gorm:"type:varchar(64);uniqueIndex" json:"session_id"` // DARI working session ID
	PolicyEpochID  string `gorm:"type:varchar(64)" json:"policy_epoch_id"`
	LeaseID        string `gorm:"type:varchar(64);index" json:"lease_id,omitempty"`
	// Session metadata
	TaskPurpose    string `gorm:"type:text" json:"task_purpose,omitempty"`
	Title          string `gorm:"type:varchar(255)" json:"title,omitempty"`
	Status         string `gorm:"type:varchar(32);default:'pending'" json:"status"` // pending, active, idle, closed, terminated
	ModelClass     string `gorm:"type:varchar(64)" json:"model_class,omitempty"`
	ProtectionProfile string `gorm:"type:varchar(16);default:'P0'" json:"protection_profile"`
	SessionTTL     int    `json:"session_ttl,omitempty"`  // seconds
	IdleTTL        int    `json:"idle_ttl,omitempty"`     // seconds
	OpenedAt       string `gorm:"type:timestamp" json:"opened_at,omitempty"`
	ClosedAt       string `gorm:"type:timestamp" json:"closed_at,omitempty"`
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
