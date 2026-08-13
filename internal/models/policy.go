package models

// PolicyRule is a persisted, per-org governance rule authored in the Policy
// console (PRD §13). Each rule binds a domain (models/tools/data/scm/network/
// session) to a scope (org/project/repo/team) with an enabled flag and a config
// payload. Replaces the previous localStorage-only rule store.
type PolicyRule struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Domain         string `gorm:"type:varchar(64);not null" json:"domain"`     // models, tools, data, scm, network, session
	TemplateID     string `gorm:"type:varchar(128)" json:"template_id"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	NameEn         string `gorm:"type:varchar(255)" json:"nameEn"`
	Description    string `gorm:"type:text" json:"desc"`
	Scope          string `gorm:"type:varchar(64);default:'org'" json:"scope"` // org, project, repo, team
	ScopeName      string `gorm:"type:varchar(255)" json:"scopeName"`
	Enabled        bool   `gorm:"default:true" json:"enabled"`
	ConfigJSON     string `gorm:"type:text" json:"config"`
}
