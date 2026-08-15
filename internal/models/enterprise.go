package models

// EnterpriseHarnessFeature represents an enterprise-only capability reported
// by the harness to PCCP. These are NOT available in the public edition.
// PRD §33 Korean Enterprise-Specific Differentiators.
type EnterpriseHarnessFeature struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	HarnessID      string `gorm:"type:varchar(64);index" json:"harness_id,omitempty"`
	SessionID      string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	// Feature identification
	FeatureKey    string `gorm:"type:varchar(64);not null;index" json:"feature_key"`
	FeatureName   string `gorm:"type:varchar(255)" json:"feature_name"`
	FeatureNameKo string `gorm:"type:varchar(255)" json:"feature_name_ko"`
	Category      string `gorm:"type:varchar(32);not null" json:"category"` // governance, security, compliance, identity, audit
	PRDRef        string `gorm:"type:varchar(32)" json:"prd_ref,omitempty"`
	// Status
	Enabled  bool   `gorm:"default:true" json:"enabled"`
	Enforced bool   `gorm:"default:false" json:"enforced"`                   // if true, harness blocks work without it
	Status   string `gorm:"type:varchar(32);default:'active'" json:"status"` // active, disabled, violated
	// Last report from harness
	LastReportedAt string `gorm:"type:timestamp" json:"last_reported_at,omitempty"`
	LastValue      string `gorm:"type:text" json:"last_value,omitempty"` // JSON payload from harness
	ViolationCount int    `gorm:"default:0" json:"violation_count"`
	// Config
	Config string `gorm:"type:text" json:"config,omitempty"` // JSON config for this feature
}

// TableName override
func (EnterpriseHarnessFeature) TableName() string { return "enterprise_harness_features" }

// EnterpriseFeatureViolation records when a harness violates an enterprise policy
type EnterpriseFeatureViolation struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	HarnessID      string `gorm:"type:varchar(64);index" json:"harness_id"`
	SessionID      string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	FeatureKey     string `gorm:"type:varchar(64);not null;index" json:"feature_key"`
	Severity       string `gorm:"type:varchar(16);not null" json:"severity"` // warning, high, critical
	Description    string `gorm:"type:text" json:"description"`
	DescriptionKo  string `gorm:"type:text" json:"description_ko,omitempty"`
	Resolved       bool   `gorm:"default:false" json:"resolved"`
	OccurredAt     string `gorm:"type:timestamp" json:"occurred_at"`
}

func (EnterpriseFeatureViolation) TableName() string { return "enterprise_feature_violations" }
