package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Conversation is a chat conversation between users (PRD §21.4).
type Conversation struct {
	AuditBase
	Type             string `gorm:"type:varchar(32);not null" json:"type"` // direct, group, channel, session
	Title            string `gorm:"type:varchar(255)" json:"title,omitempty"`
	TitleKo          string `gorm:"type:varchar(255)" json:"title_ko,omitempty"`
	ProjectID        string `gorm:"type:varchar(64);index" json:"project_id,omitempty"`
	SessionID        string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	ParticipantsJSON string `gorm:"type:text" json:"participants"` // JSON array of user IDs
	EncryptionKeyRef string `gorm:"type:varchar(128)" json:"encryption_key_ref,omitempty"`
	LastMessageAt    string `gorm:"type:timestamp" json:"last_message_at,omitempty"`
	Status           string `gorm:"type:varchar(32);default:'active'" json:"status"`
}

// Message is a chat message (PRD §21.4).
type Message struct {
	Base
	ConversationID   string `gorm:"type:varchar(64);index;not null" json:"conversation_id"`
	SenderID         string `gorm:"type:varchar(64);index;not null" json:"sender_id"`
	SenderType       string `gorm:"type:varchar(32)" json:"sender_type"`                 // user, system, bot
	ContentType      string `gorm:"type:varchar(32);default:'text'" json:"content_type"` // text, code, file_ref, ai_context_link
	Content          string `gorm:"type:text" json:"content"`
	ContentEncrypted string `gorm:"type:text" json:"content_encrypted,omitempty"`
	ParentMessageID  string `gorm:"type:varchar(64)" json:"parent_message_id,omitempty"`
	// AI context linking (PRD §21.6)
	LinkedSessionID         string `gorm:"type:varchar(64)" json:"linked_session_id,omitempty"`
	LinkedExchangeID        string `gorm:"type:varchar(64)" json:"linked_exchange_id,omitempty"`
	RequiresContextExchange bool   `gorm:"default:false" json:"requires_context_exchange"`
	// Metadata
	MentionsJSON  string `gorm:"type:text" json:"mentions,omitempty"`  // JSON array of user IDs
	ReactionsJSON string `gorm:"type:text" json:"reactions,omitempty"` // JSON {emoji: [user_ids]}
	Edited        bool   `gorm:"default:false" json:"edited"`
	DeletedBy     string `gorm:"type:varchar(64)" json:"deleted_by,omitempty"`
	DeliveredAt   string `gorm:"type:timestamp" json:"delivered_at,omitempty"`
	ReadByJSON    string `gorm:"type:text" json:"read_by,omitempty"` // JSON array of user IDs
}

// Presence tracks user online/offline status (PRD §21.3).
type Presence struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	UserID         string `gorm:"type:varchar(64);index;not null" json:"user_id"`
	Status         string `gorm:"type:varchar(32);default:'offline'" json:"status"` // online, away, busy, offline
	Activity       string `gorm:"type:varchar(255)" json:"activity,omitempty"`      // "coding in payment-service"
	HarnessID      string `gorm:"type:varchar(64)" json:"harness_id,omitempty"`
	LastActiveAt   string `gorm:"type:timestamp" json:"last_active_at"`
}

// FileTransfer represents a managed file transfer (PRD §23).
type FileTransfer struct {
	AuditBase
	ConversationID   string `gorm:"type:varchar(64);index" json:"conversation_id,omitempty"`
	SessionID        string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	SenderID         string `gorm:"type:varchar(64);not null" json:"sender_id"`
	RecipientID      string `gorm:"type:varchar(64)" json:"recipient_id,omitempty"`
	FileName         string `gorm:"type:varchar(255);not null" json:"file_name"`
	FileSize         int64  `json:"file_size"`
	FileType         string `gorm:"type:varchar(64)" json:"file_type"`
	FileHash         string `gorm:"type:varchar(128)" json:"file_hash"`
	StoragePath      string `gorm:"type:varchar(512)" json:"storage_path,omitempty"`
	EncryptionKeyRef string `gorm:"type:varchar(128)" json:"encryption_key_ref,omitempty"`
	// Policy
	ScanStatus       string `gorm:"type:varchar(32);default:'pending'" json:"scan_status"` // pending, clean, blocked, failed
	ScanFindingsJSON string `gorm:"type:text" json:"scan_findings,omitempty"`
	Classification   string `gorm:"type:varchar(32);default:'internal'" json:"classification"`
	// Transfer state
	Status      string `gorm:"type:varchar(32);default:'pending'" json:"status"` // pending, uploading, scanning, ready, downloading, completed, rejected, expired
	ExpiresAt   string `gorm:"type:timestamp" json:"expires_at,omitempty"`
	CompletedAt string `gorm:"type:timestamp" json:"completed_at,omitempty"`
	// Delivery evidence (PAT-1511): accepted/downloaded timestamps
	// persist who pulled the file and when so admins can audit
	// exfiltration paths.
	AcceptedAt   string `gorm:"type:timestamp" json:"accepted_at,omitempty"`
	DownloadedAt string `gorm:"type:timestamp" json:"downloaded_at,omitempty"`
	DownloadCount int    `gorm:"default:0" json:"download_count"`
}

// Broadcast is a targeted/emergency/administrative message (PRD §22).
type Broadcast struct {
	AuditBase
	Severity string `gorm:"type:varchar(32);not null" json:"severity"` // info, warning, critical, emergency
	Title    string `gorm:"type:varchar(255);not null" json:"title"`
	TitleKo  string `gorm:"type:varchar(255)" json:"title_ko"`
	Body     string `gorm:"type:text" json:"body"`
	BodyKo   string `gorm:"type:text" json:"body_ko"`
	// Targeting
	TargetType     string `gorm:"type:varchar(32)" json:"target_type"` // all, org, project, group, user
	TargetID       string `gorm:"type:varchar(64)" json:"target_id,omitempty"`
	TargetOrgsJSON string `gorm:"type:text" json:"target_organizations,omitempty"` // JSON array
	// Controls
	RequiresAck bool   `gorm:"default:false" json:"requires_ack"`
	Dismissable bool   `gorm:"default:true" json:"dismissable"`
	ExpiresAt   string `gorm:"type:timestamp" json:"expires_at,omitempty"`
	// State
	Status   string `gorm:"type:varchar(32);default:'active'" json:"status"` // active, expired, cancelled
	SentBy   string `gorm:"type:varchar(64)" json:"sent_by"`
	AckCount int    `json:"ack_count"`
	AcksJSON string `gorm:"type:text" json:"acks,omitempty"` // JSON array of user IDs who acknowledged
	// Governed send (PAT-1510): the audience resolved at send time is frozen
	// so delivery/ack reporting stays reproducible; ClientToken makes
	// retries idempotent.
	AudienceJSON string `gorm:"type:text" json:"audience,omitempty"` // JSON {eligible_ids, excluded, resolved_at}
	// Partial unique index: only non-empty tokens are deduped (legacy rows
	// carry an empty token), so concurrent same-token retries cannot both
	// insert.
	ClientToken string `gorm:"type:varchar(64);index:,unique,where:client_token <> ''" json:"client_token,omitempty"`
}

// UsageRecord tracks billable usage (PRD §29).
type UsageRecord struct {
	Base
	OrganizationID string  `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	UserID         string  `gorm:"type:varchar(64);index" json:"user_id,omitempty"`
	HarnessID      string  `gorm:"type:varchar(64);index" json:"harness_id,omitempty"`
	SessionID      string  `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	ProjectID      string  `gorm:"type:varchar(64);index" json:"project_id,omitempty"`
	ExchangeID     string  `gorm:"type:varchar(64);index" json:"exchange_id,omitempty"`
	EventKey       *string `gorm:"type:varchar(128);uniqueIndex:idx_usage_event_key" json:"event_key,omitempty"`
	ModelPackageID string  `gorm:"type:varchar(64)" json:"model_package_id,omitempty"`
	EndpointID     string  `gorm:"type:varchar(64)" json:"endpoint_id,omitempty"`
	// Usage metrics
	MetricType string `gorm:"type:varchar(32);not null" json:"metric_type"` // tokens_in, tokens_out, gpu_seconds, storage_bytes
	Quantity   int64  `json:"quantity"`
	Unit       string `gorm:"type:varchar(32)" json:"unit"` // tokens, seconds, bytes
	// Cost
	CostMicros             int64  `json:"cost_micros,omitempty"` // millionths of Currency
	Currency               string `gorm:"type:varchar(8)" json:"currency,omitempty"`
	PricingState           string `gorm:"type:varchar(16);index" json:"pricing_state"` // priced, unpriced, pending, error
	Adjustment             bool   `gorm:"default:false" json:"adjustment,omitempty"`
	AppliedRateMicrosPer1K int64  `gorm:"default:0" json:"applied_rate_micros_per_1k,string,omitempty"`
	AppliedPriceVersion    string `gorm:"type:varchar(128)" json:"applied_price_version,omitempty"`
	AppliedPriceSource     string `gorm:"type:varchar(255)" json:"applied_price_source,omitempty"`
	// OccurredAt is retained for wire/backward compatibility. MeteredAt is the
	// canonical nullable timestamp used by indexed ledger queries.
	OccurredAt   string     `gorm:"type:timestamp" json:"occurred_at"`
	MeteredAt    *time.Time `gorm:"type:timestamp" json:"-"`
	TimingSource string     `gorm:"type:varchar(32);default:'reported'" json:"timing_source,omitempty"`
}

const (
	UsagePricingPriced   = "priced"
	UsagePricingUnpriced = "unpriced"
	UsagePricingPending  = "pending"
	UsagePricingError    = "error"
)

func (u *UsageRecord) BeforeSave(_ *gorm.DB) error {
	if u.MeteredAt == nil && u.OccurredAt != "" {
		occurred, err := time.Parse(time.RFC3339, u.OccurredAt)
		if err != nil {
			return fmt.Errorf("usage record occurred_at: %w", err)
		}
		if occurred.IsZero() {
			u.OccurredAt = ""
		} else {
			occurred = occurred.UTC()
			u.MeteredAt = &occurred
			u.OccurredAt = occurred.Format(time.RFC3339)
			u.TimingSource = "reported"
		}
	}
	if u.MeteredAt == nil {
		fallback := u.CreatedAt.UTC()
		if fallback.IsZero() {
			fallback = time.Now().UTC()
		}
		u.MeteredAt = &fallback
		u.TimingSource = "created_at_fallback"
	}
	if u.TimingSource == "" {
		u.TimingSource = "reported"
	}
	if u.PricingState == "" {
		if u.CostMicros != 0 {
			u.PricingState = UsagePricingPriced
		} else {
			u.PricingState = UsagePricingUnpriced
		}
	}
	return nil
}
