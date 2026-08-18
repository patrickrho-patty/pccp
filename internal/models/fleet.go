package models

import "time"

// FleetBulkOperation makes a synchronous fleet request retry-safe. The
// unique tenant/key pair reserves one execution; completed outcomes are
// replayed to the caller instead of executing harness mutations again.
type FleetBulkOperation struct {
	Base
	OrganizationID string    `gorm:"type:varchar(64);uniqueIndex:idx_fleet_bulk_org_key,priority:1;not null" json:"-"`
	IdempotencyKey string    `gorm:"type:varchar(128);uniqueIndex:idx_fleet_bulk_org_key,priority:2;not null" json:"-"`
	RequestDigest  string    `gorm:"type:varchar(64);not null" json:"-"`
	Status         string    `gorm:"type:varchar(32);index;not null" json:"-"`
	ResponseJSON   string    `gorm:"type:text" json:"-"`
	HTTPStatus     int       `gorm:"not null;default:0" json:"-"`
	LeaseExpiresAt time.Time `gorm:"index" json:"-"`
}

// FleetBulkTargetOutcome is the durable per-target checkpoint for a bulk
// operation. A crashed worker leaves an explicit indeterminate target instead
// of allowing a blind retry of a potentially destructive action.
type FleetBulkTargetOutcome struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"-"`
	OperationID    string `gorm:"type:varchar(64);uniqueIndex:idx_fleet_bulk_target,priority:1;not null" json:"-"`
	HarnessID      string `gorm:"type:varchar(64);uniqueIndex:idx_fleet_bulk_target,priority:2;not null" json:"harness_id"`
	Result         string `gorm:"type:varchar(32);index;not null" json:"result"`
	Error          string `gorm:"type:text" json:"error,omitempty"`
}
