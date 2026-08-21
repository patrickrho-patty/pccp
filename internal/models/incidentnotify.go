package models

import (
	"time"

	"gorm.io/gorm"
)

// Governed admin incident notifications (PAT-1454): durable SMS/email/
// Slack delivery for security findings, outages, and material system
// failures — with one incident identity, dedup, ack, escalation, content
// minimization, and per-channel Patty-managed vs customer-managed choice.

// IncidentNotifyPolicy is the per-tenant routing policy. Per-channel
// managed_by encodes the Patty-managed vs customer-managed decision;
// choosing Patty-managed is never implicit.
type IncidentNotifyPolicy struct {
	gorm.Model
	OrganizationID string `gorm:"uniqueIndex" json:"organization_id"`
	// JSON: {"critical":{"channels":["sms","email","slack"],"ack_required":true},
	//        "high":{"channels":["email","slack"],"sms":false},
	//        "medium":{"channels":["email"],"digest":true},
	//        "low":{"channels":[]}}
	RoutingJSON string `gorm:"type:text" json:"routing_json"`
	// JSON: {"email":"patty","sms":"customer","slack":"customer"}
	ManagedByJSON      string `gorm:"type:text" json:"managed_by_json"`
	AckDeadlineMinutes int    `json:"ack_deadline_minutes"`
	EscalationSteps    int    `json:"escalation_steps"`
	QuietHoursStart    string `json:"quiet_hours_start"` // "23:00" — noncritical only
	QuietHoursEnd      string `json:"quiet_hours_end"`
	AirGapped          bool   `json:"air_gapped"` // hides Patty-managed options
}

// IncidentNotifyRecipientGroup is an on-call group in the escalation order.
type IncidentNotifyRecipientGroup struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	Name           string `json:"name"`
	// JSON array: [{\"kind\":\"email\",\"target\":\"oncall@a.io\",\"verified\":true}, ...]
	MembersJSON     string `gorm:"type:text" json:"members_json"`
	EscalationOrder int    `json:"escalation_order"`
	Timezone        string `json:"timezone"`
}

// IncidentNotifyChannel is one configured outbound channel endpoint.
// Credentials are stored as an encrypted reference and masked in the UI.
type IncidentNotifyChannel struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	Channel        string `json:"channel"`         // email | sms | slack
	ManagedBy      string `json:"managed_by"`      // patty | customer
	CredentialRef  string `json:"credential_ref"`  // encrypted secret reference (never exported)
	MaskedEndpoint string `json:"masked_endpoint"` // e.g. on***@a.io
	Verified       bool   `json:"verified"`
	Healthy        bool   `json:"healthy"`
	LastFailure    string `json:"last_failure"`
}

// IncidentNotifyIncident is the correlated incident identity that stays
// stable across detection → investigation → ack → escalation → recovery
// → resolution. Source events attach here instead of creating storms.
type IncidentNotifyIncident struct {
	gorm.Model
	OrganizationID  string `gorm:"index" json:"organization_id"`
	Fingerprint     string `gorm:"uniqueIndex" json:"fingerprint"` // org+source+service+rule digest
	SourceType      string `json:"source_type"`                    // security_finding|outage|degradation|system_failure|notification_health
	Service         string `json:"service"`
	Rule            string `json:"rule"`
	Severity        string `json:"severity"` // critical|high|medium|low
	TitleKo         string `json:"title_ko"`
	SafeSummaryKo   string `json:"safe_summary_ko"`
	ScopeRef        string `json:"scope_ref"` // resource reference (evidence stays in PCCP)
	State           string `json:"state"`     // open|acknowledged|escalated|resolved|suppressed
	FirstSeenAt     string `json:"first_seen_at"`
	LastSeenAt      string `json:"last_seen_at"`
	AckedBy         string `json:"acked_by"`
	AckedVia        string `json:"acked_via"`
	AckedAt         string `json:"acked_at"`
	EscalationStep  int    `json:"escalation_step"`
	ResolvedAt      string `json:"resolved_at"`
	SuppressedUntil string `json:"suppressed_until"`
}

// IncidentNotifyJob is the durable delivery job. Idempotency key is
// stable per (incident, update kind, target, channel) so retries and
// duplicate detector emissions cannot repeat a delivery.
type IncidentNotifyJob struct {
	gorm.Model
	OrganizationID    string `gorm:"index" json:"organization_id"`
	IncidentID        uint   `gorm:"index" json:"incident_id"`
	Kind              string `json:"kind"` // notify|update|resolution|escalation|test
	Channel           string `json:"channel"`
	Target            string `json:"target"`
	IdempotencyKey    string `gorm:"uniqueIndex" json:"idempotency_key"`
	State             string `json:"state"` // queued|sent|failed|dead_letter|cancelled|suppressed
	Attempts          int    `json:"attempts"`
	MaxAttempts       int    `json:"max_attempts"`
	NextRetryAt       string `json:"next_retry_at"`
	ProviderMessageID string `json:"provider_message_id"`
	LastError         string `json:"last_error"`
	EnvelopeJSON      string `gorm:"type:text" json:"envelope_json"` // the exact safe fields sent
	SentAt            string `json:"sent_at"`
}

// IncidentNotifyReceipt normalizes provider delivery state per job.
// Provider acceptance is never treated as human acknowledgement.
type IncidentNotifyReceipt struct {
	gorm.Model
	JobID             uint   `gorm:"index" json:"job_id"`
	State             string `json:"state"` // accepted|delivered|deferred|bounced|rejected|expired|unknown
	ProviderMessageID string `json:"provider_message_id"`
	OccurredAt        string `json:"occurred_at"`
}

// IncidentNotifyAcknowledgement records ack through PCCP, protected
// email/SMS links, or verified Slack actions. Action tokens are
// single-use and short-lived.
type IncidentNotifyAcknowledgement struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	IncidentID     uint   `gorm:"index" json:"incident_id"`
	ActionToken    string `gorm:"uniqueIndex" json:"-"`
	AckedBy        string `json:"acked_by"`
	Via            string `json:"via"` // pccp|email|sms|slack
	ExpiresAt      string `json:"expires_at"`
	UsedAt         string `json:"used_at"`
}

// IncidentNotifyAudit is the tenant-scoped audit row for configuration
// changes, delivery attempts, acks, escalations, and suppressions.
type IncidentNotifyAudit struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	Action         string `json:"action"`
	IncidentID     uint   `json:"incident_id"`
	ActorEmail     string `json:"actor_email"`
	Detail         string `json:"detail"`
	OccurredAt     string `json:"occurred_at"`
}

// IncidentNotifyHealthSum mirrors the subsystem's own health so a
// notification failure is itself visible (PAT-1454).
type IncidentNotifyHealthSum struct {
	gorm.Model
	OrganizationID    string    `gorm:"index" json:"organization_id"`
	QueueDepth        int       `json:"queue_depth"`
	DeadLetters       int       `json:"dead_letters"`
	Failures24h       int       `json:"failures_24h"`
	UnhealthyChannels string    `json:"unhealthy_channels"`
	CheckedAt         string    `json:"checked_at"`
	CreatedAt         time.Time `json:"created_at"`
}
