package models

// OrgSetting is a durable per-organization key/value setting
// (harnesses C2 forced version, policy C2 acknowledgement campaigns).
// Settings previously lived only in audit events, which made them
// unqueryable for live enforcement.
type OrgSetting struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);uniqueIndex:idx_orgsetting_org_key,priority:1;not null" json:"organization_id"`
	Key            string `gorm:"type:varchar(128);uniqueIndex:idx_orgsetting_org_key,priority:2;not null" json:"key"`
	Value          string `gorm:"type:text" json:"value"`
}
