package models

// LegalHold pins audit/provenance records so retention/deletion cannot
// touch them while a hold is active (web/17 C, §40.5).
type LegalHold struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ResourceType   string `gorm:"type:varchar(32);index;not null" json:"resource_type"` // audit_event, session, user, repository
	ResourceID     string `gorm:"type:varchar(64);index;not null" json:"resource_id"`
	Reason         string `gorm:"type:text" json:"reason"`
	PlacedBy       string `gorm:"type:varchar(128)" json:"placed_by"`
	Status         string `gorm:"type:varchar(16);default:'active'" json:"status"` // active, lifted
	LiftedAt       string `gorm:"type:timestamp" json:"lifted_at,omitempty"`
}
