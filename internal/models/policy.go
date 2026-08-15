package models

// PolicyRule is a persisted, per-org governance rule authored in the Policy
// console (PRD §13). Each rule binds a domain (models/tools/data/scm/network/
// session) to a scope (org/project/repo/team) with an enabled flag and a config
// payload. Replaces the previous localStorage-only rule store.
type PolicyRule struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Domain         string `gorm:"type:varchar(64);not null" json:"domain"` // models, tools, data, scm, network, session
	TemplateID     string `gorm:"type:varchar(128)" json:"template_id"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	NameEn         string `gorm:"type:varchar(255)" json:"nameEn"`
	Description    string `gorm:"type:text" json:"desc"`
	Scope          string `gorm:"type:varchar(64);default:'org'" json:"scope"` // org, project, repo, team
	ScopeName      string `gorm:"type:varchar(255)" json:"scopeName"`
	Enabled        bool   `gorm:"default:true" json:"enabled"`
	// Status is the approval lifecycle (policy C1, §46.2): new rules
	// are drafts; approving publishes them into the active epoch.
	Status     string `gorm:"type:varchar(32);default:'draft'" json:"status"` // draft, approved, rejected
	ConfigJSON string `gorm:"type:text" json:"config"`
}

// PolicyTemplate is a server-side, versioned policy template
// (policy UX2): the 6-domain catalog is seeded once per org and can be
// edited/administered instead of living as page constants.
type PolicyTemplate struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	TemplateID     string `gorm:"type:varchar(128);not null" json:"template_id"`
	Domain         string `gorm:"type:varchar(64);not null" json:"domain"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	NameEn         string `gorm:"type:varchar(255)" json:"nameEn"`
	Description    string `gorm:"type:text" json:"desc"`
	ConfigJSON     string `gorm:"type:text" json:"config"`
	Version        string `gorm:"type:varchar(32);default:'1'" json:"version"`
	Enabled        bool   `gorm:"default:true" json:"enabled"`
}

// TableName overrides for the per-org template uniqueness.
func (PolicyTemplate) TableName() string { return "policy_templates" }

// PolicyAcknowledgement is one user's ack of a policy epoch
// (policy C2, §33.6): when an epoch requires acknowledgement, sessions
// from unacked users are gated until they ack.
type PolicyAcknowledgement struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	EpochID        string `gorm:"type:varchar(64);index;not null" json:"epoch_id"`
	UserID         string `gorm:"type:varchar(64);index;not null" json:"user_id"`
	AckedAt        string `gorm:"type:timestamp" json:"acked_at"`
}

// TableName overrides for the acknowledgement table.
func (PolicyAcknowledgement) TableName() string { return "policy_acknowledgements" }

// PolicyException is an exception-marketplace request (policy C5,
// §33.8): a scoped carve-out from the org policy that a second actor
// must approve before it takes effect.
type PolicyException struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Scope          string `gorm:"type:varchar(64)" json:"scope"` // project, repo, user
	ScopeID        string `gorm:"type:varchar(64)" json:"scope_id"`
	ScopeName      string `gorm:"type:varchar(255)" json:"scopeName"`
	RuleIDsJSON    string `gorm:"type:text" json:"rule_ids"`
	Reason         string `gorm:"type:text" json:"reason"`
	RequestedBy    string `gorm:"type:varchar(64)" json:"requested_by"`
	Status         string `gorm:"type:varchar(32);default:'pending'" json:"status"` // pending, approved, denied
	DecidedBy      string `gorm:"type:varchar(64)" json:"decided_by,omitempty"`
	DecisionReason string `gorm:"type:text" json:"decision_reason,omitempty"`
	DecidedAt      string `gorm:"type:timestamp" json:"decided_at,omitempty"`
}

// TableName overrides for the exception marketplace table.
func (PolicyException) TableName() string { return "policy_exceptions" }
