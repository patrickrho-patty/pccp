package models

import (
	"gorm.io/gorm"
)

// Evidence-hardened admin search (PAT-1451): authorization before
// candidates, four separately governed domains, immutable locators,
// masked sensitive content with separately authorized reveal.

// EvidenceSearchGrant records the dedicated evidence-search permission
// per administrator, separate from generic admin status. ScopeKind+
// ScopeRef bound the searchable universe (own tenant only in this
// build; cross-tenant search is not grantable here).
type EvidenceSearchGrant struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	AdminEmail     string `gorm:"index" json:"admin_email"`
	ScopeKind      string `json:"scope_kind"` // organization|project|repository
	ScopeRef       string `json:"scope_ref"`
	CanReveal      bool   `json:"can_reveal"` // separate sensitive-reveal permission
	GrantedBy      string `json:"granted_by"`
	Reason         string `json:"reason"`
	ExpiresAt      string `json:"expires_at"`
	Revoked        bool   `json:"revoked"`
}

// EvidenceSearchAudit records every query and sensitive reveal —
// administrator, scope, query, filters, result counts, reveals.
type EvidenceSearchAudit struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	AdminEmail     string `json:"admin_email"`
	Kind           string `json:"kind"` // query|open|reveal|summary
	Query          string `json:"query"`
	Domains        string `json:"domains"`
	ResultCounts   string `json:"result_counts"`
	OccurredAt     string `json:"occurred_at"`
}
