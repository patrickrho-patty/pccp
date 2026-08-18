package models

import "gorm.io/gorm"

// SecurityLockdown is durable desired state for emergency containment. The
// unique tenant/scope/target row is activated or released in place so session
// creation and relay admission can fail closed across process restarts.
type SecurityLockdown struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);uniqueIndex:idx_lockdown_scope,priority:1;not null" json:"organization_id"`
	Scope          string `gorm:"type:varchar(16);uniqueIndex:idx_lockdown_scope,priority:2;not null" json:"scope"`
	ProjectID      string `gorm:"type:varchar(64);uniqueIndex:idx_lockdown_scope,priority:3;not null;default:''" json:"project_id,omitempty"`
	Status         string `gorm:"type:varchar(16);index;not null" json:"status"`
	Reason         string `gorm:"type:text" json:"reason"`
	ActivatedBy    string `gorm:"type:varchar(64)" json:"activated_by"`
	ActivatedAt    string `gorm:"type:timestamp" json:"activated_at"`
	ReleasedBy     string `gorm:"type:varchar(64)" json:"released_by,omitempty"`
	ReleasedAt     string `gorm:"type:timestamp" json:"released_at,omitempty"`
}

func ActiveSecurityLockdown(db *gorm.DB, organizationID, projectID string) (bool, error) {
	query := db.Model(&SecurityLockdown{}).Where("organization_id = ? AND status = ?", organizationID, "active")
	if projectID == "" {
		query = query.Where("scope = ?", "org")
	} else {
		query = query.Where("scope = ? OR (scope = ? AND project_id = ?)", "org", "project", projectID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
