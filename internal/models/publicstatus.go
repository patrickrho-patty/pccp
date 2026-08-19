package models

import (
	"time"

	"gorm.io/gorm"
)

// Public service status page (PAT-1439).
//
// Korean-first public view of Patty service availability: two public
// components (Patty Code, Patty Web), measured colors with anti-flapping,
// manually authored Korean incidents, 90-day uptime rollups, anonymous
// subscriptions, and a signed/versioned public snapshot. This model is
// deliberately separate from the private tenant security-incident schema.

// PublicStatusComponent is one row per public product. The registry is
// versioned so Patty Web can be activated without redesigning the page.
type PublicStatusComponent struct {
	ID              string `gorm:"primaryKey" json:"id"` // "patty_code" | "patty_web"
	NameKo          string `json:"name_ko"`
	Active          bool   `json:"active"` // Patty Web stays hidden until launch
	RegistryVersion int    `json:"registry_version"`

	// Machine-controlled measured state (PAT-1439: machines control
	// measured status; humans control public explanations).
	MeasuredColor       string `json:"measured_color"` // green|yellow|orange|red|blue|gray
	LastImpact          string `json:"last_impact"`    // none|partial|severe|widespread of last failing sample
	ConsecutiveFailures int    `json:"consecutive_failures"`
	ConsecutiveSuccesses int    `json:"consecutive_successes"`
	LastObservationAt   string `json:"last_observation_at"`
	LastHealthyAt       string `json:"last_healthy_at"`

	// Operator override (healthier overrides must expire; worsening
	// overrides apply immediately).
	OverrideColor       string `json:"override_color"`
	OverrideReason      string `json:"override_reason"`
	OverrideExpiresAt   string `json:"override_expires_at"`
	OverrideFalsePosAck bool   `json:"override_false_positive_ack"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PublicStatusObservation is an append-only synthetic-probe or SLI sample.
// WindowSeconds is the evaluation window the sample represents so daily
// rollups can derive measured impact duration rather than incident copy.
type PublicStatusObservation struct {
	gorm.Model
	ComponentID   string `gorm:"index" json:"component_id"`
	Source        string `json:"source"`  // synthetic | sli
	Region        string `json:"region"`  // corroboration requires >1 region for global red
	Success       bool   `json:"success"` // journey completion
	Impact        string `json:"impact"`  // none | partial | severe | widespread
	LatencyMS     int64  `json:"latency_ms"`
	WindowSeconds int    `json:"window_seconds"`
	Maintenance   bool   `json:"maintenance"` // planned maintenance window sample
	Detail        string `json:"detail"`      // safe private detail; never published
	ObservedAt    string `json:"observed_at"`
}

// PublicIncident is a public operational incident with a Korean-first
// lifecycle. Not coupled to tenant SecurityFinding.
type PublicIncident struct {
	gorm.Model
	Slug        string `gorm:"uniqueIndex" json:"slug"` // stable permalink
	TitleKo     string `json:"title_ko"`
	Components  string `json:"components"` // JSON array of component IDs
	Impact      string `json:"impact"`     // none|partial|severe|widespread (measured)
	State       string `json:"state"`      // investigating|mitigating|monitoring|resolved|maintenance_scheduled|maintenance_in_progress
	Major       bool   `json:"major"`
	Published   bool   `json:"published"`
	StartedAt   string `json:"started_at"`
	DetectedAt  string `json:"detected_at"`
	MitigatedAt string `json:"mitigated_at"`
	ResolvedAt  string `json:"resolved_at"`
	// Major-incident cadence anchors: first human update within 15 min of
	// confirmation, then at least every 30 min until monitoring/resolved.
	ConfirmedMajorAt string `json:"confirmed_major_at"`
	LastUpdateAt     string `json:"last_update_at"`
	NextUpdateDueAt  string `json:"next_update_due_at"`
}

// PublicIncidentUpdate is a timestamped Korean update. Corrections append;
// history is never silently rewritten.
type PublicIncidentUpdate struct {
	gorm.Model
	IncidentID    uint   `gorm:"index" json:"incident_id"`
	BodyKo        string `json:"body_ko"`
	StateAtUpdate string `json:"state_at_update"`
	AuthorEmail   string `json:"author_email"`
	IsCorrection  bool   `json:"is_correction"`
}

// PublicStatusDailyRollup is one published daily aggregate per component
// (KST day). Corrections create a new Version row rather than editing.
type PublicStatusDailyRollup struct {
	gorm.Model
	ComponentID        string  `gorm:"index:idx_ps_rollup_comp_date"`
	DateKST            string  `gorm:"index:idx_ps_rollup_comp_date"` // YYYY-MM-DD (KST); corrections append new Version rows
	AvailabilityPct    float64 `json:"availability_pct"`
	ImpactedSeconds    int     `json:"impacted_seconds"`
	MaintenanceSeconds int     `json:"maintenance_seconds"`
	NoDataSeconds      int     `json:"no_data_seconds"`
	IncidentIDs        string  `json:"incident_ids"` // JSON array
	Version            int     `json:"version"`
	CorrectedBy        uint    `json:"corrected_by"` // 0 = current
}

// PublicStatusSnapshot is the signed, versioned snapshot served by the
// independently available public origin. The page keeps loading the last
// valid snapshot and flags staleness at read time.
type PublicStatusSnapshot struct {
	gorm.Model
	Version      int    `json:"version"`
	PayloadJSON  string `gorm:"type:text" json:"payload_json"`
	Signature    string `json:"signature"`
	KeyID        string `json:"key_id"`
	GeneratedAt  string `json:"generated_at"`
}

// PublicStatusSubscriber is an anonymous subscription (email/SMS/RSS/
// webhook) per component. Destinations require verification; webhook
// payloads are HMAC-signed with the per-subscriber secret.
type PublicStatusSubscriber struct {
	gorm.Model
	ComponentID       string `gorm:"index" json:"component_id"`
	Channel           string `json:"channel"` // email|sms|webhook|rss
	Destination       string `json:"destination"`
	Verified          bool   `json:"verified"`
	VerifyToken       string `gorm:"uniqueIndex" json:"-"`
	UnsubscribeToken  string `gorm:"uniqueIndex" json:"-"`
	WebhookSecret     string `json:"-"`
	Bounced           bool   `json:"bounced"`
	FailureCount      int    `json:"failure_count"`
	LastNotifiedAt    string `json:"last_notified_at"`
	RateLimitedUntil  string `json:"rate_limited_until"`
}

// PublicStatusNotification is the durable outbound notification record for
// subscriber delivery (idempotent per subscriber+incident+transition).
type PublicStatusNotification struct {
	gorm.Model
	SubscriberID uint   `gorm:"index" json:"subscriber_id"`
	IncidentID   uint   `gorm:"index" json:"incident_id"`
	Transition   string `json:"transition"` // published|update|resolved|maintenance
	IdempotencyKey string `gorm:"uniqueIndex" json:"idempotency_key"`
	State         string `json:"state"` // queued|sent|failed|suppressed
	Attempts      int    `json:"attempts"`
	LastError     string `json:"last_error"`
	SentAt        string `json:"sent_at"`
}
