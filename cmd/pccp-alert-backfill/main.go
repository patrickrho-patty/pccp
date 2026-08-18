// Command pccp-alert-backfill migrates legacy plaintext alert endpoint
// credentials to envelope-encrypted storage. It uses the same KeyProvider
// configuration as pccp-server and fails nonzero on any incomplete batch.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/db"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

type BackfillReport struct {
	Candidates  int
	Migrated    int
	Fingerprint int
	Verified    int
}

type preparedUpdate struct {
	original models.AlertEndpoint
	values   map[string]interface{}
	legacy   bool
}

func main() {
	dsn := flag.String("database-url", "", "Database DSN override (otherwise standard PCCP_DB_* configuration)")
	kekFile := flag.String("kek-file", strings.TrimSpace(os.Getenv(keymgmt.AlertKEKFileEnv)), "KEK file (or PCCP_ALERT_KEK_FILE)")
	kekID := flag.String("kek-id", strings.TrimSpace(os.Getenv(keymgmt.AlertKEKIDEnv)), "KEK identifier (or PCCP_ALERT_KEK_ID)")
	fingerprintKeyFile := flag.String("fingerprint-key-file", strings.TrimSpace(os.Getenv(keymgmt.AlertFingerprintKeyFileEnv)), "stable credential fingerprint key file (or PCCP_ALERT_FINGERPRINT_KEY_FILE)")
	batch := flag.Int("batch-size", 200, "Rows per transaction")
	approve := flag.Bool("approve", false, "Apply changes (omit for dry-run)")
	flag.Parse()

	provider, configured, err := keymgmt.LoadProviderFromEnvironment(func(key string) string {
		switch key {
		case keymgmt.AlertKEKFileEnv:
			return *kekFile
		case keymgmt.AlertKEKIDEnv:
			return *kekID
		case keymgmt.AlertFingerprintKeyFileEnv:
			return *fingerprintKeyFile
		default:
			return os.Getenv(key)
		}
	})
	if err != nil {
		log.Fatalf("provider initialization failed: %v", err)
	}
	if !configured {
		log.Fatalf("provider is not configured")
	}

	dbConfig := db.ConfigFromEnv()
	if strings.TrimSpace(*dsn) != "" {
		dbConfig.DSN = strings.TrimSpace(*dsn)
		if strings.HasPrefix(dbConfig.DSN, "postgres://") || strings.HasPrefix(dbConfig.DSN, "postgresql://") {
			dbConfig.Driver = "postgres"
		}
	}
	database, err := db.New(dbConfig)
	if err != nil {
		log.Fatalf("database open failed: %v", err)
	}
	if *batch <= 0 {
		log.Fatalf("batch size must be positive")
	}

	var candidates int64
	if err := candidateAlertEndpoints(database).Count(&candidates).Error; err != nil {
		log.Fatalf("candidate count failed: %v", err)
	}
	if !*approve {
		fmt.Printf("dry-run: candidates=%d; re-run with --approve to apply\n", candidates)
		return
	}
	report, err := BackfillAlertEndpoints(database, provider, *batch)
	if err != nil {
		log.Fatalf("backfill failed after migrated=%d fingerprinted=%d: %v", report.Migrated, report.Fingerprint, err)
	}
	fmt.Printf("backfill complete: candidates=%d migrated=%d fingerprinted=%d verified=%d\n", report.Candidates, report.Migrated, report.Fingerprint, report.Verified)
}

func candidateAlertEndpoints(db *gorm.DB) *gorm.DB {
	return db.Model(&models.AlertEndpoint{}).
		Where("target <> '' OR (target_enc <> '' AND ((credential_id IS NULL OR credential_id = '' OR credential_id NOT LIKE 'hm:%') OR target_binding_version < ?))", keymgmt.AlertBindingVersion)
}

// BackfillAlertEndpoints migrates candidate rows transactionally by batch,
// then decrypts and verifies every encrypted row. Plaintext rows came from the
// previously exposed API representation, so they are disabled and marked for
// upstream provider rotation before they may deliver again.
func BackfillAlertEndpoints(db *gorm.DB, provider keymgmt.KeyProvider, batchSize int) (BackfillReport, error) {
	report := BackfillReport{}
	if db == nil || provider == nil {
		return report, fmt.Errorf("backfill requires database and provider")
	}
	if batchSize <= 0 {
		return report, fmt.Errorf("batch size must be positive")
	}
	cursor := ""
	for {
		var rows []models.AlertEndpoint
		query := candidateAlertEndpoints(db).Order("id ASC").Limit(batchSize)
		if cursor != "" {
			query = query.Where("id > ?", cursor)
		}
		if err := query.Find(&rows).Error; err != nil {
			return report, fmt.Errorf("scan candidates: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		updates := make([]preparedUpdate, 0, len(rows))
		for _, row := range rows {
			plaintext, err := keymgmt.OpenAlertSecretForMigration(provider, row.TargetEnc, row.TargetKEKID, strings.TrimSpace(row.Target),
				row.TargetBindingVersion, row.CredentialID, keymgmt.AlertSecretContext{
					OrganizationID: row.OrganizationID, EndpointID: row.ID, ProviderType: row.Type,
				})
			if err != nil {
				return report, fmt.Errorf("open endpoint %s: %w", row.ID, err)
			}
			encoded, kekID, credentialID, bindingVersion, err := keymgmt.SealAlertSecret(provider, plaintext, keymgmt.AlertSecretContext{
				OrganizationID: row.OrganizationID, EndpointID: row.ID, ProviderType: row.Type,
			})
			if err != nil {
				return report, fmt.Errorf("seal endpoint %s: %w", row.ID, err)
			}
			values := map[string]interface{}{
				"target": "", "target_enc": encoded, "target_kek_id": kekID,
				"target_binding_version": bindingVersion, "credential_id": credentialID,
			}
			legacy := strings.TrimSpace(row.Target) != ""
			if legacy {
				values["enabled"] = false
				values["rotation_required"] = true
			}
			updates = append(updates, preparedUpdate{original: row, values: values, legacy: legacy})
		}
		err := applyPreparedUpdates(db, updates)
		if err != nil {
			return report, err
		}
		for _, update := range updates {
			if update.legacy {
				report.Migrated++
			} else {
				report.Fingerprint++
			}
		}
		report.Candidates += len(updates)
		cursor = rows[len(rows)-1].ID
	}

	var plaintextRemaining int64
	if err := db.Model(&models.AlertEndpoint{}).Where("target <> ''").Count(&plaintextRemaining).Error; err != nil {
		return report, fmt.Errorf("verify plaintext residue: %w", err)
	}
	if plaintextRemaining != 0 {
		return report, fmt.Errorf("verification failed: %d plaintext credentials remain", plaintextRemaining)
	}
	cursor = ""
	for {
		var encrypted []models.AlertEndpoint
		query := db.Where("target_enc <> ''").Order("id ASC").Limit(batchSize)
		if cursor != "" {
			query = query.Where("id > ?", cursor)
		}
		if err := query.Find(&encrypted).Error; err != nil {
			return report, fmt.Errorf("verify scan: %w", err)
		}
		if len(encrypted) == 0 {
			break
		}
		for _, row := range encrypted {
			plaintext, err := keymgmt.OpenAlertSecret(provider, row.TargetEnc, row.TargetKEKID, "", row.TargetBindingVersion,
				row.CredentialID, keymgmt.AlertSecretContext{OrganizationID: row.OrganizationID, EndpointID: row.ID, ProviderType: row.Type})
			if err != nil || plaintext == "" {
				return report, fmt.Errorf("verify endpoint %s: encrypted credential invalid", row.ID)
			}
			if row.RotationRequired && row.Enabled {
				return report, fmt.Errorf("verify endpoint %s: exposed credential remains enabled", row.ID)
			}
			report.Verified++
		}
		cursor = encrypted[len(encrypted)-1].ID
	}
	return report, nil
}

func applyPreparedUpdates(db *gorm.DB, updates []preparedUpdate) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, update := range updates {
			row := update.original
			query := tx.Model(&models.AlertEndpoint{}).Where("id = ? AND organization_id = ?", row.ID, row.OrganizationID)
			for column, value := range map[string]string{
				"target": row.Target, "target_enc": row.TargetEnc, "target_kek_id": row.TargetKEKID, "credential_id": row.CredentialID,
			} {
				query = query.Where("("+column+" = ? OR ("+column+" IS NULL AND ? = ''))", value, value)
			}
			query = query.Where("(target_binding_version = ? OR (target_binding_version IS NULL AND ? = 0))", row.TargetBindingVersion, row.TargetBindingVersion)
			result := query.Updates(update.values)
			if result.Error != nil {
				return fmt.Errorf("update endpoint %s: %w", row.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("endpoint %s changed during migration", row.ID)
			}
		}
		return nil
	})
}
