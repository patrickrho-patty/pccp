package models

// SupportCase is a support ticket bound to a subscriber (web/11 A5,
// web/12 A7).
type SupportCase struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index" json:"organization_id"`
	AccountID      string `gorm:"type:varchar(64);index;not null" json:"account_id"`
	Subject        string `gorm:"type:varchar(255)" json:"subject"`
	Description    string `gorm:"type:text" json:"description,omitempty"`
	Priority       string `gorm:"type:varchar(16);default:'normal'" json:"priority"` // low, normal, high, urgent
	Status         string `gorm:"type:varchar(32);default:'open'" json:"status"`     // open, in_progress, resolved, closed
	TimelineJSON   string `gorm:"type:text" json:"timeline,omitempty"`               // JSON array of {at, actor, note}
}

// AbuseCase is a Trust & Safety case lifecycle row (web/12 A6).
type AbuseCase struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index" json:"organization_id"`
	AccountID      string `gorm:"type:varchar(64);index;not null" json:"account_id"`
	Category       string `gorm:"type:varchar(32)" json:"category"` // account_sharing, prompt_abuse, billing_abuse, tos
	Severity       string `gorm:"type:varchar(16);default:'medium'" json:"severity"`
	Description    string `gorm:"type:text" json:"description,omitempty"`
	Status         string `gorm:"type:varchar(32);default:'open'" json:"status"` // open, investigating, action_taken, closed
	Decision       string `gorm:"type:text" json:"decision,omitempty"`
}
