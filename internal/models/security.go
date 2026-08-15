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
