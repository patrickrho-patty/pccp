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
	OrganizationID string `gorm:"type:varchar(64);index;not null;uniqueIndex:idx_policy_ack_org_epoch_user,priority:1" json:"organization_id"`
	EpochID        string `gorm:"type:varchar(64);index;not null;uniqueIndex:idx_policy_ack_org_epoch_user,priority:2" json:"epoch_id"`
	UserID         string `gorm:"type:varchar(64);index;not null;uniqueIndex:idx_policy_ack_org_epoch_user,priority:3" json:"user_id"`
	AckedAt        string `gorm:"type:timestamp" json:"acked_at"`
}

// TableName overrides for the acknowledgement table.
func (PolicyAcknowledgement) TableName() string { return "policy_acknowledgements" }

// PolicyException is an exception-marketplace request (policy C5,
// §33.8): a scoped carve-out from the org policy that a second actor
// must approve before it takes effect. Fields added for the
// evidence-backed approval flow (PAT-1506): justification, evidence
// JSON, compensating controls, time-bounded validity, current vs
// proposed rule values for the diff preview, and the multi-party
// approval chain.
type PolicyException struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Scope          string `gorm:"type:varchar(64)" json:"scope"` // project, repo, user
	ScopeID        string `gorm:"type:varchar(64)" json:"scope_id"`
	ScopeName      string `gorm:"type:varchar(255)" json:"scopeName"`
	RuleIDsJSON    string `gorm:"type:text" json:"rule_ids"`
	Reason         string `gorm:"type:text" json:"reason"`
	RequestedBy    string `gorm:"type:varchar(64)" json:"requested_by"`
	Status         string `gorm:"type:varchar(32);default:'pending'" json:"status"` // pending, approved, denied, expired, revoked
	DecidedBy      string `gorm:"type:varchar(64)" json:"decided_by,omitempty"`
	DecisionReason string `gorm:"type:text" json:"decision_reason,omitempty"`
	DecidedAt      string `gorm:"type:timestamp" json:"decided_at,omitempty"`

	// Evidence-backed approval payload (PAT-1506).
	JustificationKo      string `gorm:"type:text" json:"justification_ko,omitempty"`
	EvidenceJSON         string `gorm:"type:text" json:"evidence,omitempty"`         // [{type, ref, title}]
	CompensatingControls  string `gorm:"type:text" json:"compensating_controls,omitempty"` // free-text Korean list
	ResourceDestination   string `gorm:"type:varchar(255)" json:"resource_destination,omitempty"`
	SeverityLabel         string `gorm:"type:varchar(32)" json:"severity_label,omitempty"` // informational risk label
	CurrentRuleValuesJSON string `gorm:"type:text" json:"current_rule_values,omitempty"`  // [{rule_id, value}]
	ProposedRuleValuesJSON string `gorm:"type:text" json:"proposed_rule_values,omitempty"` // [{rule_id, value}]
	ConditionsJSON        string `gorm:"type:text" json:"conditions,omitempty"`           // [{type, text}]
	RequestedStart        string `gorm:"type:varchar(32)" json:"requested_start,omitempty"` // ISO instant
	ExpiresAt             string `gorm:"type:timestamp;index" json:"expires_at,omitempty"`
	RenewedFromID         string `gorm:"type:varchar(64);index" json:"renewed_from_id,omitempty"` // links renewal chain
	RequiredApproverRoles string `gorm:"type:varchar(255)" json:"required_approver_roles,omitempty"` // CSV role list (multi-party)
	ApproversJSON         string `gorm:"type:text" json:"approvers,omitempty"`          // [{role, user_id, at}]
	// Affected scope counters computed at approval time (denormalized so
	// detail reads don't have to walk the graph).
	AffectedSessions int `gorm:"default:0" json:"affected_sessions"`
	AffectedHarnesses int `gorm:"default:0" json:"affected_harnesses"`
	// New epoch propagated on approval — list/ack clients can roll to it.
	PublishedEpochID string `gorm:"type:varchar(64);index" json:"published_epoch_id,omitempty"`
}

// TableName overrides for the exception marketplace table.
func (PolicyException) TableName() string { return "policy_exceptions" }

// Active reports whether the exception is currently in force (approved
// and not past its expiry). Used by listing/detail to surface expired
// exceptions clearly without mutating the row.
func (e PolicyException) Active(now string) bool {
	if e.Status != "approved" { return false }
	if e.ExpiresAt == "" { return true }
	return now < e.ExpiresAt
}
