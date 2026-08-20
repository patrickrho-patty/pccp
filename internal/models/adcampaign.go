package models

import (
	"gorm.io/gorm"
)

// Public terminal ads (PAT-1435) — PCCP half: campaign control,
// integer-safe CPM accounting, signed catalog delivery, and
// privacy-preserving anonymous measurement. Deliberately separate from
// the identity-bearing event spine and from operational broadcasts.

// AdCampaign is one Patty-operated non-personalized text campaign.
// All money is integer minor currency units; spend = validated
// impressions × CPM / 1000, transactionally capped by budget+ceiling.
type AdCampaign struct {
	gorm.Model
	Advertiser string `json:"advertiser"`
	Category   string `json:"category"`
	State      string `json:"state"` // draft|active|paused|ended

	HeadlineEn string `gorm:"type:varchar(120)" json:"headline_en"`
	BodyEn     string `gorm:"type:varchar(200)" json:"body_en"`
	HeadlineKo string `gorm:"type:varchar(120)" json:"headline_ko"`
	BodyKo     string `gorm:"type:varchar(200)" json:"body_ko"`

	// Reviewed HTTPS destination + normalized display domain.
	DestinationURL  string `gorm:"type:varchar(500)" json:"destination_url"`
	DisplayDomain   string `gorm:"type:varchar(255)" json:"display_domain"`

	CreativeRevision int `json:"creative_revision"`

	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`

	Weight          int   `json:"weight"`
	ImpressionCeiling int `json:"impression_ceiling"` // 0 = uncapped (budget-bound)
	CpmMinor        int64 `json:"cpm_minor"`          // minor units per 1000 impressions
	Currency        string `json:"currency"`
	BudgetMinor     int64 `json:"budget_minor"`

	ValidatedImpressions int64 `json:"validated_impressions"`
	Clicks               int64 `json:"clicks"`

	CreatedBy string `json:"created_by"`
}

// AdMeasurementEvent is the anonymous at-most-once measurement record.
// Contains ONLY campaign/revision/event-id/type/timestamp/catalog
// revision — no user, harness, org, session, device, or installation
// identifiers (PAT-1435 privacy contract).
type AdMeasurementEvent struct {
	gorm.Model
	EventID     string `gorm:"uniqueIndex" json:"event_id"` // random idempotency id
	CampaignID  uint   `gorm:"index" json:"campaign_id"`
	CreativeRevision int `json:"creative_revision"`
	Type        string `json:"type"` // impression|click
	Timestamp   string `json:"timestamp"`
	CatalogRevision int `json:"catalog_revision"`
	Counted     bool   `json:"counted"` // false = duplicate/rejected
}

// AdCatalogSnapshot is the signed versioned catalog served to public
// harness builds (pattern shared with the status page).
type AdCatalogSnapshot struct {
	gorm.Model
	Revision   int    `json:"revision"`
	PayloadJSON string `gorm:"type:text" json:"payload_json"`
	Signature  string `json:"signature"`
	KeyID      string `json:"key_id"`
	GeneratedAt string `json:"generated_at"`
	ExpiresAt  string `json:"expires_at"`
}
