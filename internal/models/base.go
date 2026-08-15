package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base is the common embedded struct for all domain models.
// Every relevant object carries the universal labels per PRD §37.3.
type Base struct {
	ID        string         `gorm:"type:varchar(64);primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// BeforeCreate generates a prefixed ULID-like UUID if ID is empty.
func (b *Base) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
	return nil
}

// TimestampedModel embeds Base with soft-delete support.
type TimestampedModel struct {
	Base
}

// AuditBase adds universal labels (PRD §37.3) on top of Base.
type AuditBase struct {
	Base
	OrganizationID    string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ProjectID         string `gorm:"type:varchar(64);index" json:"project_id,omitempty"`
	Classification    string `gorm:"type:varchar(64);default:'internal'" json:"classification"`
	OwnerID           string `gorm:"type:varchar(64)" json:"owner_id,omitempty"`
	RetentionPolicy   string `gorm:"type:varchar(64)" json:"retention_profile,omitempty"`
	LegalHold         bool   `gorm:"default:false" json:"legal_hold"`
	AccessLabels      string `gorm:"type:text" json:"access_labels,omitempty"` // JSON array
	RegionRestriction string `gorm:"type:varchar(128)" json:"region_restriction,omitempty"`
	EncryptionKeyRef  string `gorm:"type:varchar(128)" json:"encryption_key_ref,omitempty"`
	SourceProvenance  string `gorm:"type:text" json:"source_provenance,omitempty"` // JSON
	ArchiveState      string `gorm:"type:varchar(32);default:'active'" json:"archive_state"`
}

// GenerateID generates a prefixed UUID.
func GenerateID(prefix string) string {
	return prefix + "_" + uuid.New().String()[:26]
}

// IDWithPrefix sets an ID with the given prefix if empty.
func IDWithPrefix(prefix string, id *string) {
	if *id == "" {
		*id = GenerateID(prefix)
	}
}

// ServiceSigningKey is the persisted per-service signing identity
// (policy issuer, receipt signer, registry publish key). Created
// once, reused across restarts.
type ServiceSigningKey struct {
	Base
	Service      string `gorm:"type:varchar(64);uniqueIndex;not null" json:"service"`
	PrivateHex   string `gorm:"type:varchar(256);not null" json:"-"`
	CreatedAtRFC string `gorm:"type:timestamp" json:"created_at"`
}

// TableName overrides the table name.
func (ServiceSigningKey) TableName() string { return "service_signing_keys" }
