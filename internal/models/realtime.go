package models

import "time"

// RealtimeEvent is the bounded durable replay log for organization-scoped
// SSE delivery. PayloadJSON contains the already-redacted event envelope.
type RealtimeEvent struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);uniqueIndex:idx_realtime_org_sequence,priority:1;index;not null" json:"organization_id"`
	Sequence       uint64 `gorm:"uniqueIndex:idx_realtime_org_sequence,priority:2;not null" json:"sequence"`
	EventType      string `gorm:"type:varchar(128);not null" json:"event_type"`
	PayloadJSON    string `gorm:"type:text;not null" json:"payload_json"`
	OccurredAt     string `gorm:"type:timestamp;not null" json:"occurred_at"`
}

// RealtimeSequence is the database-serialized durable cursor allocator for an
// organization's replay stream. It is intentionally separate from event rows
// so concurrent replicas never race on MAX(sequence)+1.
type RealtimeSequence struct {
	OrganizationID string    `gorm:"type:varchar(64);primaryKey" json:"-"`
	Sequence       uint64    `gorm:"not null" json:"-"`
	CreatedAt      time.Time `json:"-"`
	UpdatedAt      time.Time `json:"-"`
}

// RealtimeStreamTicket is a short-lived, single-use authorization grant for
// opening an SSE stream. The console JWT is never placed in an EventSource URL.
type RealtimeStreamTicket struct {
	Base
	OrganizationID string     `gorm:"type:varchar(64);index;not null" json:"-"`
	ActorID        string     `gorm:"type:varchar(255);index;not null" json:"-"`
	ActorEmail     string     `gorm:"type:varchar(255);index" json:"-"`
	UserID         string     `gorm:"type:varchar(64);index" json:"-"`
	LifecycleEpoch uint64     `json:"-"`
	Transcript     bool       `gorm:"not null;default:false" json:"-"`
	ExpiresAt      time.Time  `gorm:"index;not null" json:"-"`
	ConsumedAt     *time.Time `gorm:"index" json:"-"`
}

// RealtimeTransientEvent is an encrypted, short-lived cross-replica carrier
// for transcript-bearing frames. It is never used for replay or evidence.
type RealtimeTransientEvent struct {
	Base
	OrganizationID      string    `gorm:"type:varchar(64);index:idx_realtime_transient_org_time,priority:1;not null" json:"-"`
	PublishedAtUnixNano int64     `gorm:"index:idx_realtime_transient_org_time,priority:2;not null" json:"-"`
	EventType           string    `gorm:"type:varchar(128);not null" json:"-"`
	Ciphertext          string    `gorm:"type:text;not null" json:"-"`
	Nonce               string    `gorm:"type:text;not null" json:"-"`
	ExpiresAt           time.Time `gorm:"index;not null" json:"-"`
}
