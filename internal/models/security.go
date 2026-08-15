package models

// SecurityRule is a persisted, per-org toggleable detection/DLP rule (PRD §16).
// The detection catalogs and patterns live in internal/security; this record
// stores the admin's enabled/action override so toggles stick and disabled
// rules are honored by CheckContext (instead of the previous hardcoded GET +
// audit-only PUT).
type SecurityRule struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);uniqueIndex:idx_secrule_org_rule,priority:1;not null" json:"organization_id"`
	RuleID         string `gorm:"type:varchar(64);uniqueIndex:idx_secrule_org_rule,priority:2;not null" json:"rule_id"`
	Type           string `gorm:"type:varchar(64)" json:"type"`     // korean_pii, secret, prompt_injection, sensitive_path
	Severity       string `gorm:"type:varchar(32)" json:"severity"` // medium, high, critical
	Name           string `gorm:"type:varchar(255)" json:"name"`
	NameKo         string `gorm:"type:varchar(255)" json:"name_ko"`
	Enabled        bool   `gorm:"default:true" json:"enabled"`
	Action         string `gorm:"type:varchar(32);default:'block'" json:"action"` // block, mask, review
}

// AlertEndpoint is an alert-routing destination (security C2/C3,
// §10C.14): Slack webhooks, generic webhooks (on-call), and SIEM
// forwarders receive findings as they are recorded.
type AlertEndpoint struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	Type           string `gorm:"type:varchar(32)" json:"type"` // slack, webhook, siem
	Target         string `gorm:"type:varchar(1024)" json:"target"`
	SeveritiesJSON string `gorm:"type:text" json:"severities,omitempty"` // JSON array of severities to route
	Enabled        bool   `gorm:"default:true" json:"enabled"`
}

// TableName overrides for alert endpoints.
func (AlertEndpoint) TableName() string { return "alert_endpoints" }

// PIILexicon is the versioned, org-overridable Korean-PII lexicon
// (security C5, §16.3): patterns are no longer code-only constants —
// an org can publish its own version and the detector prefers it.
type PIILexicon struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Version        string `gorm:"type:varchar(32);default:'1'" json:"version"`
	PatternsJSON   string `gorm:"type:text" json:"patterns,omitempty"` // map rule_id → regex
	UpdatedBy      string `gorm:"type:varchar(64)" json:"updated_by,omitempty"`
	Enabled        bool   `gorm:"default:true" json:"enabled"`
}

// TableName overrides for the lexicon table.
func (PIILexicon) TableName() string { return "pii_lexicons" }
