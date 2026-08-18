package models

import "time"

// RelayControlEvent is the shared durable carrier for control-plane commands.
// Every relay replica observes the same row; local listener delivery is never
// dependent on which pod received the original HTTP request.
type RelayControlEvent struct {
	Base
	OrganizationID string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	HarnessID      string    `gorm:"type:varchar(64);index;not null" json:"harness_id"`
	Kind           string    `gorm:"type:varchar(32);index;not null" json:"kind"`
	CommandType    string    `gorm:"type:varchar(64)" json:"command_type,omitempty"`
	Reason         string    `gorm:"type:text" json:"reason,omitempty"`
	Body           []byte    `gorm:"type:bytea" json:"-"`
	ExpiresAt      time.Time `gorm:"index;not null" json:"expires_at"`
}

// RelayControlAck records a replica's processing result. Directive rows with
// no local connection remain unacknowledged and may be delivered after the
// harness reconnects to that replica; revocations are acknowledged by every
// replica after its local cache/listener state is refreshed.
type RelayControlAck struct {
	Base
	EventID   string    `gorm:"type:varchar(64);uniqueIndex:idx_relay_control_ack,priority:1;not null" json:"event_id"`
	RelayID   string    `gorm:"type:varchar(64);uniqueIndex:idx_relay_control_ack,priority:2;not null" json:"relay_id"`
	Delivered int       `json:"delivered"`
	AppliedAt time.Time `gorm:"not null" json:"applied_at"`
}
