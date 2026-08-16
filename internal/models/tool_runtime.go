package models

// Tool is a registered development tool (PRD §17.1).
type Tool struct {
	AuditBase
	Name             string `gorm:"type:varchar(128);not null" json:"name"`
	NameKo           string `gorm:"type:varchar(128)" json:"name_ko,omitempty"`
	Category         string `gorm:"type:varchar(64)" json:"category"` // read, write, execute, network
	ToolClass        string `gorm:"type:varchar(64)" json:"tool_class"`
	Signature        string `gorm:"type:varchar(128)" json:"signature,omitempty"` // tool integrity digest
	AllowedByDefault bool   `gorm:"default:false" json:"allowed_by_default"`
	RequiresApproval bool   `gorm:"default:true" json:"requires_approval"`
	DangerLevel      string `gorm:"type:varchar(32);default:'low'" json:"danger_level"` // low, medium, high, critical
	Status           string `gorm:"type:varchar(32);default:'active'" json:"status"`
}

// Approval is a governance approval for an exchange or action (PRD §13.3, DARI §25).
type Approval struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ExchangeID     string `gorm:"type:varchar(64);index" json:"exchange_id,omitempty"`
	SessionID      string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	ActionID       string `gorm:"type:varchar(64)" json:"action_id,omitempty"`
	ApprovalType   string `gorm:"type:varchar(64);not null" json:"approval_type"` // tool_use, file_write, model_use, network
	RequestedBy    string `gorm:"type:varchar(64)" json:"requested_by"`           // user ID
	// Approvers
	ReviewerID         string `gorm:"type:varchar(64)" json:"reviewer_id,omitempty"`
	SecurityReviewerID string `gorm:"type:varchar(64)" json:"security_reviewer_id,omitempty"`
	// Decision
	Decision       string `gorm:"type:varchar(32);default:'pending'" json:"decision"` // pending, approved, rejected, expired
	DecisionReason string `gorm:"type:text" json:"decision_reason,omitempty"`
	Conditions     string `gorm:"type:text" json:"conditions,omitempty"` // JSON
	DecidedAt      string `gorm:"type:timestamp" json:"decided_at,omitempty"`
	DecidedBy      string `gorm:"type:varchar(64)" json:"decided_by,omitempty"`
	ExpiresAt      string `gorm:"type:timestamp" json:"expires_at"`
}

// SecurityFinding is a security alert or finding (PRD §15.2).
type SecurityFinding struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	SessionID      string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	ExchangeID     string `gorm:"type:varchar(64)" json:"exchange_id,omitempty"`
	FindingType    string `gorm:"type:varchar(64);not null;index" json:"finding_type"` // pii_leak, secret_exposure, injection, etc.
	Severity       string `gorm:"type:varchar(16);not null" json:"severity"`           // info, low, medium, high, critical
	Title          string `gorm:"type:varchar(255)" json:"title"`
	TitleKo        string `gorm:"type:varchar(255)" json:"title_ko,omitempty"`
	Description    string `gorm:"type:text" json:"description,omitempty"`
	DescriptionKo  string `gorm:"type:text" json:"description_ko,omitempty"`
	EvidenceJSON   string `gorm:"type:text" json:"evidence,omitempty"`
	RuleID         string `gorm:"type:varchar(128)" json:"rule_id,omitempty"`
	Status         string `gorm:"type:varchar(32);default:'open'" json:"status"`     // open, investigating, resolved, false_positive, suppressed
	ContainsAction string `gorm:"type:varchar(64)" json:"contains_action,omitempty"` // quarantine, terminate, isolate
	// Direction marks which side of the exchange produced the finding
	// (security C4): request scans are the outbound context; response
	// scans catch exfiltration in model output.
	Direction string `gorm:"type:varchar(16);default:'request'" json:"direction,omitempty"`
	// Suppress / accept-risk workflow (security C1): findings can be
	// suppressed with a reason + expiry; the sweep reopens them.
	Suppressed     bool   `gorm:"default:false" json:"suppressed"`
	SuppressReason string `gorm:"type:text" json:"suppress_reason,omitempty"`
	SuppressExpiry string `gorm:"type:timestamp" json:"suppress_expiry,omitempty"`
	SuppressedBy   string `gorm:"type:varchar(64)" json:"suppressed_by,omitempty"`
	OccurredAt     string `gorm:"type:timestamp" json:"occurred_at"`
}

// PolicyPack is a versioned policy bundle (PRD §41).
type PolicyPack struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Name           string `gorm:"type:varchar(128);not null" json:"name"`
	NameKo         string `gorm:"type:varchar(128)" json:"name_ko,omitempty"`
	Version        string `gorm:"type:varchar(32)" json:"version"`
	Profile        string `gorm:"type:varchar(32)" json:"profile"` // enterprise, government
	// Rules as JSON
	DLPRulesJSON        string `gorm:"type:text" json:"dlp_rules,omitempty"`
	InjectionRulesJSON  string `gorm:"type:text" json:"injection_rules,omitempty"`
	ToolPolicyJSON      string `gorm:"type:text" json:"tool_policy,omitempty"`
	NetworkPolicyJSON   string `gorm:"type:text" json:"network_policy,omitempty"`
	ModelPolicyJSON     string `gorm:"type:text" json:"model_policy,omitempty"`
	ApprovalMatrixJSON  string `gorm:"type:text" json:"approval_matrix,omitempty"`
	RetentionPolicyJSON string `gorm:"type:text" json:"retention_policy,omitempty"`
	Digest              string `gorm:"type:varchar(128)" json:"digest"`
	Status              string `gorm:"type:varchar(32);default:'draft'" json:"status"` // draft, active, superseded
}

// AllModels returns all model types for auto-migration.
func AllModels() []interface{} {
	return []interface{}{
		// Identity
		&Organization{},
		&OrgSetting{},
		&BusinessUnit{},
		&User{},
		&Role{},
		&UserRole{},
		&ProjectToolAllowlist{},
		&Device{},
		&Harness{},
		&EnrollmentCode{},
		// Projects & Repos
		&Project{},
		&ProjectMember{},
		&ChangeRequest{},
		&Repository{},
		&Branch{},
		&RepoBaseline{},
		&Session{},
		&PromptExchange{},
		// Model registry
		&ModelPackage{},
		&InferenceEndpoint{},
		&EndpointAttestation{},
		&EndpointLease{},
		// Policy
		&PolicyEpoch{},
		&CapabilityLease{},
		// Compliance (web/08)
		&ComplianceEvidence{},
		&ComplianceRemediation{},
		&ComplianceAssessmentRecord{},
		&LegalHold{},
		// Durable service identities + revocations + sandboxes
		&ServiceSigningKey{},
		&CredentialRevocationRecord{},
		&SandboxRecord{},
		// Provenance
		&ActionEnvelope{},
		&ChangeSet{},
		&ProvenanceSpan{},
		&CommitBinding{},
		&AuditEvent{},
		&EvidenceReceipt{},
		// Security
		&Tool{},
		&Approval{},
		&SecurityFinding{},
		&AlertEndpoint{},
		&PIILexicon{},
		&SecurityRule{},
		&PolicyRule{},
		&PolicyTemplate{},
		&PolicyAcknowledgement{},
		&PolicyException{},
		&PolicyPack{},
		// Communications (Phase 3)
		&Conversation{},
		&Message{},
		&Presence{},
		&FileTransfer{},
		&Broadcast{},
		// Usage (Phase 4)
		&UsageRecord{},
		// v2 Model Catalog (§10A)
		&CatalogModel{},
		&CatalogEpoch{},
		// v2 Public Cloud (§10C)
		&Account{},
		&Subscription{},
		&AccountCapacityLease{},
		// Enterprise harness features (§33)
		&EnterpriseHarnessFeature{},
		&EnterpriseFeatureViolation{},
	}
}

// ProjectToolAllowlist is the per-project tool allowlist (web/14
// feature 7): a project may further restrict which registered tools
// its sessions may call.
type ProjectToolAllowlist struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ProjectID      string `gorm:"type:varchar(64);index;not null" json:"project_id"`
	ToolName       string `gorm:"type:varchar(128);not null" json:"tool_name"`
	GrantedBy      string `gorm:"type:varchar(64)" json:"granted_by,omitempty"`
}
