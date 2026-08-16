package models

// compliance.go: compliance evidence vault + remediation + persisted
// assessments (web/08 C1/C2/C3). Assessments are self-assessment records
// — certification remains the customer's process (§41 guardrail: maps
// and evidence are the product).

// ComplianceEvidence is one piece of evidence attached to a control.
type ComplianceEvidence struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Certification  string `gorm:"type:varchar(32);index;not null" json:"certification"`
	ControlID      string `gorm:"type:varchar(64);index;not null" json:"control_id"`
	Title          string `gorm:"type:varchar(255)" json:"title"`
	Description    string `gorm:"type:text" json:"description,omitempty"`
	Source         string `gorm:"type:varchar(64)" json:"source"`        // manual, audit, provenance, security, attestation
	Reference      string `gorm:"type:text" json:"reference,omitempty"`  // API path / file ref / artifact link
	Attachment     string `gorm:"type:text" json:"attachment,omitempty"` // base64 or file path (bounded)
	CollectedAt    string `gorm:"type:timestamp" json:"collected_at"`
}

// ComplianceRemediation is a gap → task with owner, due date and SLA.
type ComplianceRemediation struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Certification  string `gorm:"type:varchar(32);index;not null" json:"certification"`
	ControlID      string `gorm:"type:varchar(64);index;not null" json:"control_id"`
	Owner          string `gorm:"type:varchar(128)" json:"owner"`
	DueDate        string `gorm:"type:date" json:"due_date,omitempty"`
	SLA            string `gorm:"type:varchar(32)" json:"sla,omitempty"`         // e.g. 30d, 90d
	Status         string `gorm:"type:varchar(32);default:'open'" json:"status"` // open, in_progress, done
	Notes          string `gorm:"type:text" json:"notes,omitempty"`
}

// ComplianceAssessmentRecord persists a self-assessment snapshot.
type ComplianceAssessmentRecord struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Certification  string `gorm:"type:varchar(32);index;not null" json:"certification"`
	Scope          string `gorm:"type:varchar(32)" json:"scope,omitempty"` // SaaS, PaaS, IaaS
	Level          string `gorm:"type:varchar(32)" json:"level,omitempty"` // CSAP 간편/일반, ISMS-P 1/2/3
	AssessedAt     string `gorm:"type:timestamp" json:"assessed_at"`
	OverallStatus  string `gorm:"type:varchar(32)" json:"overall_status"`
	OpenGaps       int    `json:"open_gaps"`
	ResultsJSON    string `gorm:"type:text" json:"results,omitempty"` // JSON snapshot of ControlResults
}

// ComplianceTarget captures the admin's certification target (scope +
// level), stored as an org setting value (JSON).
type ComplianceTarget struct {
	Certification string `json:"certification"`
	Scope         string `json:"scope"`
	Level         string `json:"level"`
}
