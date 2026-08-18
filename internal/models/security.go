package models

import "time"

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
	// Pattern (07 A1): a custom rule's Go regex. Empty = built-in
	// detector class (Type field).
	Pattern string `gorm:"type:text" json:"pattern,omitempty"`
}

// SecurityRuleOverride is a scoped DELTA on top of the org catalog
// (PAT-1432). The org-level SecurityRule rows remain the catalog +
// org toggles; this table carries narrower overrides at team /
// user / harness level. Precedence when the relay pushes packs:
// Harness > User > Team > Organization — the most specific scope
// wins for the rules it names.
//
// Nil Enabled / empty Severity / empty Action mean "inherit" from
// the next-wider scope; at least one field must be set on write.
type SecurityRuleOverride struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);uniqueIndex:idx_secrule_ov_org_scope_rule,priority:1;not null" json:"organization_id"`
	ScopeLevel     string `gorm:"type:varchar(16);uniqueIndex:idx_secrule_ov_org_scope_rule,priority:2;not null" json:"scope_level"` // team, user, harness
	ScopeID        string `gorm:"type:varchar(64);uniqueIndex:idx_secrule_ov_org_scope_rule,priority:3;not null" json:"scope_id"`
	RuleID         string `gorm:"type:varchar(64);uniqueIndex:idx_secrule_ov_org_scope_rule,priority:4;not null" json:"rule_id"`
	Enabled        *bool  `gorm:"column:enabled" json:"enabled,omitempty"`    // nil = inherit
	Severity       string `gorm:"type:varchar(32)" json:"severity,omitempty"` // empty = inherit
	Action         string `gorm:"type:varchar(32)" json:"action,omitempty"`   // empty = inherit
}

// TableName overrides for scoped rule overrides.
func (SecurityRuleOverride) TableName() string { return "security_rule_overrides" }

// AlertEndpoint is an alert-routing destination (security C2/C3,
// §10C.14): Slack webhooks, generic webhooks (on-call), and SIEM
// forwarders receive findings as they are recorded.
type AlertEndpoint struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	Type           string `gorm:"type:varchar(32)" json:"type"` // slack, webhook, siem
	// Target is the legacy plaintext column retained for the dual-read
	// window only (PAT-1502 PR 2). New writes go to TargetEnc; legacy
	// rows are migrated by the backfill command, then Target is
	// dropped in a later PR. Never marshalled to JSON.
	Target string `gorm:"type:varchar(1024)" json:"-"`
	// TargetEnc is base64(JSON(keymgmt.Envelope)) for the encrypted URL.
	// When non-empty, the dispatch path decrypts via the configured
	// KeyProvider. When empty AND Target is non-empty, the dispatch
	// path falls back to Target with a startup warning. Both must
	// not be empty in production.
	TargetEnc   string `gorm:"type:text" json:"-"`
	TargetKEKID string `gorm:"type:varchar(128);index" json:"-"`
	// TargetBindingVersion identifies the row-bound authenticated-data format.
	// Version 1 binds ciphertext to organization, endpoint ID, and provider type.
	TargetBindingVersion int `gorm:"default:0" json:"-"`
	// CredentialID is derived from the plaintext credential before sealing, so
	// it remains stable across envelope nonce and KEK changes without requiring
	// reads to decrypt the secret.
	CredentialID     string     `gorm:"type:varchar(72);index" json:"-"`
	RotationRequired bool       `gorm:"default:false;index" json:"-"`
	LastRotatedAt    *time.Time `gorm:"type:timestamp" json:"-"`
	LastTestAt       *time.Time `gorm:"type:timestamp" json:"-"`
	LastTestStatus   string     `gorm:"type:varchar(32)" json:"-"`
	SeveritiesJSON   string     `gorm:"type:text" json:"severities,omitempty"` // JSON array of severities to route
	Enabled          bool       `gorm:"default:true" json:"enabled"`
}

// TableName overrides for alert endpoints.
func (AlertEndpoint) TableName() string { return "alert_endpoints" }

// AlertDeliveryJob is the durable outbox between finding persistence and
// external webhook delivery. Relay request processing never performs network
// I/O; workers claim these rows and retry bounded failures asynchronously.
type AlertDeliveryJob struct {
	Base
	OrganizationID string    `gorm:"type:varchar(64);index:idx_alert_job_ready,priority:1;not null" json:"organization_id"`
	EndpointID     string    `gorm:"type:varchar(64);index;not null" json:"endpoint_id"`
	FindingID      string    `gorm:"type:varchar(64);index;not null" json:"finding_id"`
	Status         string    `gorm:"type:varchar(24);index:idx_alert_job_ready,priority:2;not null;default:'pending'" json:"status"`
	Attempts       int       `gorm:"default:0" json:"attempts"`
	AvailableAt    time.Time `gorm:"index:idx_alert_job_ready,priority:3" json:"available_at"`
	LastReason     string    `gorm:"type:varchar(64)" json:"last_reason,omitempty"`
}

func (AlertDeliveryJob) TableName() string { return "alert_delivery_jobs" }

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
