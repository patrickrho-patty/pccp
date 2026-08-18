package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrgSetting is a durable per-organization key/value setting
// (harnesses C2 forced version, policy C2 acknowledgement campaigns).
// Settings previously lived only in audit events, which made them
// unqueryable for live enforcement.
type OrgSetting struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);uniqueIndex:idx_orgsetting_org_key,priority:1;not null" json:"organization_id"`
	Key            string `gorm:"type:varchar(128);uniqueIndex:idx_orgsetting_org_key,priority:2;not null" json:"key"`
	Value          string `gorm:"type:text" json:"value"`
}

// BillingFXRate is an immutable, effective-dated exchange-rate snapshot.
// OrgSetting remains the administrator's current configuration; reports read
// this history so changing that configuration cannot rewrite past totals.
type BillingFXRate struct {
	Base
	OrganizationID string    `gorm:"type:varchar(64);uniqueIndex:idx_billing_fx_version,priority:1;not null" json:"organization_id"`
	SourceCurrency string    `gorm:"type:varchar(8);uniqueIndex:idx_billing_fx_version,priority:2;not null" json:"source_currency"`
	TargetCurrency string    `gorm:"type:varchar(8);uniqueIndex:idx_billing_fx_version,priority:3;not null" json:"target_currency"`
	Version        string    `gorm:"type:varchar(128);uniqueIndex:idx_billing_fx_version,priority:4;not null" json:"version"`
	Rate           string    `gorm:"type:varchar(128);not null" json:"rate"`
	Source         string    `gorm:"type:varchar(255);not null" json:"source"`
	EffectiveAt    time.Time `gorm:"type:timestamp;not null" json:"effective_at"`
}

type billingFXRateSetting struct {
	Rate    string `json:"rate"`
	AsOf    string `json:"as_of"`
	Source  string `json:"source"`
	Version string `json:"version"`
}

// AfterSave snapshots valid billing rate configurations in the same database
// transaction as their authoritative setting write. Reports therefore remain
// read-only and cannot miss a version between two setting changes.
func (setting *OrgSetting) AfterSave(tx *gorm.DB) error {
	return PersistBillingFXRateHistory(tx, setting)
}

// PersistBillingFXRateHistory records every valid, versioned configured rate.
// The unique version key makes retries and migration backfills idempotent.
func PersistBillingFXRateHistory(tx *gorm.DB, setting *OrgSetting) error {
	if setting == nil || setting.Key != "billing.fx_rates" {
		return nil
	}
	var configured map[string]billingFXRateSetting
	if err := json.Unmarshal([]byte(setting.Value), &configured); err != nil {
		return nil
	}
	rows := make([]BillingFXRate, 0, len(configured))
	for pair, rateDef := range configured {
		parts := strings.Split(pair, "->")
		if len(parts) != 2 || strings.TrimSpace(rateDef.Version) == "" || strings.TrimSpace(rateDef.Source) == "" {
			continue
		}
		effectiveAt, err := time.Parse("2006-01-02", strings.TrimSpace(rateDef.AsOf))
		if err != nil {
			continue
		}
		rate := new(big.Rat)
		if _, ok := rate.SetString(rateDef.Rate); !ok || rate.Sign() <= 0 {
			continue
		}
		row := BillingFXRate{
			OrganizationID: setting.OrganizationID,
			SourceCurrency: strings.ToUpper(strings.TrimSpace(parts[0])),
			TargetCurrency: strings.ToUpper(strings.TrimSpace(parts[1])),
			Version:        strings.TrimSpace(rateDef.Version),
			Rate:           strings.TrimSpace(rateDef.Rate),
			Source:         strings.TrimSpace(rateDef.Source),
			EffectiveAt:    effectiveAt.UTC(),
		}
		var existing BillingFXRate
		err = tx.Where("organization_id = ? AND source_currency = ? AND target_currency = ? AND version = ?", row.OrganizationID, row.SourceCurrency, row.TargetCurrency, row.Version).First(&existing).Error
		if err == nil {
			if existing.Rate != row.Rate || existing.Source != row.Source || !existing.EffectiveAt.Equal(row.EffectiveAt) {
				return fmt.Errorf("billing FX version %s for %s->%s is immutable", row.Version, row.SourceCurrency, row.TargetCurrency)
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var latest BillingFXRate
		err = tx.Where("organization_id = ? AND source_currency = ? AND target_currency = ?", row.OrganizationID, row.SourceCurrency, row.TargetCurrency).
			Order("effective_at DESC, created_at DESC, id DESC").First(&latest).Error
		if err == nil && !row.EffectiveAt.After(latest.EffectiveAt) {
			return fmt.Errorf("billing FX effective date for %s->%s must advance beyond %s", row.SourceCurrency, row.TargetCurrency, latest.EffectiveAt.UTC().Format("2006-01-02"))
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}
