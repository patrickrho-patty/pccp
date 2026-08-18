package db

import (
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type schemaMigration struct {
	Name      string    `gorm:"primaryKey;type:varchar(128)"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigration) TableName() string { return "pccp_schema_migrations" }

func runMigrations(db *gorm.DB) error {
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		if err := runPostgresMigration(db, "20260818_usage_ledger_v2", migrateUsageLedgerV2); err != nil {
			return err
		}
	} else if err := runMigration(db, "20260818_usage_ledger_v2", migrateUsageLedgerV2); err != nil {
		return err
	}
	if err := runMigration(db, "20260818_model_pricing_exact_v1", migrateModelPricingExact); err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		if err := runPostgresMigration(db, "20260818_usage_project_snapshot_v1", migrateUsageProjectSnapshot); err != nil {
			return err
		}
	} else if err := runMigration(db, "20260818_usage_project_snapshot_v1", migrateUsageProjectSnapshot); err != nil {
		return err
	}
	if err := runMigration(db, "20260818_billing_fx_history_v1", migrateBillingFXHistory); err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		if err := runPostgresMigration(db, "20260818_user_identity_normalization_v1", migrateUserIdentityNormalization); err != nil {
			return err
		}
	} else if err := runMigration(db, "20260818_user_identity_normalization_v1", migrateUserIdentityNormalization); err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		if err := runPostgresMigration(db, "20260818_external_identity_namespace_v1", migrateExternalIdentityNamespace); err != nil {
			return err
		}
	} else if err := runMigration(db, "20260818_external_identity_namespace_v1", migrateExternalIdentityNamespace); err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		if err := runPostgresMigration(db, "20260818_external_identity_namespace_v2", migrateExternalIdentityNamespaceV2); err != nil {
			return err
		}
	} else if err := runMigration(db, "20260818_external_identity_namespace_v2", migrateExternalIdentityNamespaceV2); err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		if err := runPostgresMigration(db, "20260818_billing_fx_report_index_v1", migrateBillingFXReportIndex); err != nil {
			return err
		}
		return runPostgresMigration(db, "20260818_session_due_indexes_v1", migrateSessionDueIndexes)
	}
	return runMigration(db, "20260818_billing_fx_report_index_v1", migrateBillingFXReportIndex)
}

func migrateSessionDueIndexes(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.Session{}) || db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_sessions_ttl_due ON sessions ((opened_at + make_interval(secs => session_ttl))) WHERE session_ttl > 0 AND status IN ('pending','active','idle','paused')`,
		`CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_sessions_idle_due ON sessions ((COALESCE(last_activity_at, opened_at) + make_interval(secs => idle_ttl))) WHERE idle_ttl > 0 AND status = 'active'`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateExternalIdentityNamespaceV2(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.User{}) {
		return nil
	}
	if err := db.Model(&models.User{}).
		Where("auth_method = ? AND external_id <> '' AND external_issuer = ?", "scim", "scim").
		Update("external_issuer_verified", true).Error; err != nil {
		return err
	}
	var unresolved int64
	if err := db.Model(&models.User{}).
		Where("auth_method IN ? AND external_id <> '' AND (external_issuer_verified = ? OR TRIM(external_issuer) = '')", []string{"oidc", "saml"}, false).
		Count(&unresolved).Error; err != nil {
		return err
	}
	if unresolved > 0 {
		return fmt.Errorf("%d SSO identities lack an explicitly verified provider namespace; map or quarantine them before migration", unresolved)
	}
	return nil
}

func migrateExternalIdentityNamespace(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.User{}) {
		return nil
	}
	if err := db.Model(&models.User{}).
		Where("auth_method = ? AND external_id <> '' AND (external_issuer IS NULL OR external_issuer = '')", "scim").
		Update("external_issuer", "scim").Error; err != nil {
		return err
	}
	var unresolved int64
	if err := db.Model(&models.User{}).
		Where("auth_method IN ? AND external_id <> '' AND (external_issuer IS NULL OR external_issuer = '')", []string{"oidc", "saml"}).
		Count(&unresolved).Error; err != nil {
		return err
	}
	if unresolved > 0 {
		return fmt.Errorf("%d SSO identities have no verified issuer; map or quarantine them explicitly before migration", unresolved)
	}
	if db.Dialector.Name() == "postgres" {
		return db.Exec(`CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_users_external_identity
			ON users (organization_id, auth_method, external_issuer, external_id)
			WHERE external_id IS NOT NULL AND BTRIM(external_id) <> ''`).Error
	}
	return db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_external_identity
		ON users (organization_id, auth_method, external_issuer, external_id)
		WHERE external_id IS NOT NULL AND TRIM(external_id) <> ''`).Error
}

func migrateUserIdentityNormalization(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.User{}) {
		return nil
	}
	var duplicates int64
	if err := db.Raw(`SELECT COUNT(*) FROM (
		SELECT organization_id, LOWER(TRIM(email)) AS normalized_email
		FROM users GROUP BY organization_id, LOWER(TRIM(email)) HAVING COUNT(*) > 1
	) AS duplicate_identities`).Scan(&duplicates).Error; err != nil {
		return err
	}
	if duplicates > 0 {
		return fmt.Errorf("%d organization(s) contain case-variant duplicate user emails; reconcile them before migration", duplicates)
	}
	if err := db.Exec("UPDATE users SET email = LOWER(TRIM(email))").Error; err != nil {
		return err
	}
	if db.Migrator().HasTable("admin_credentials") {
		if err := db.Exec("UPDATE admin_credentials SET email = LOWER(TRIM(email))").Error; err != nil {
			return err
		}
	}
	if db.Dialector.Name() == "postgres" {
		if err := db.Exec("DROP INDEX CONCURRENTLY IF EXISTS idx_email_org").Error; err != nil {
			return err
		}
		return db.Exec("CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_users_org_email_normalized ON users (organization_id, LOWER(email))").Error
	}
	if err := db.Exec("DROP INDEX IF EXISTS idx_email_org").Error; err != nil {
		return err
	}
	return db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_users_org_email_normalized ON users (organization_id, LOWER(email))").Error
}

func runMigration(db *gorm.DB, name string, migrate func(*gorm.DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		claim := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Name: name, AppliedAt: time.Now().UTC()})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			return nil
		}
		if err := migrate(tx); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	})
}

// runPostgresMigration pins one connection, serializes competing application
// instances with an advisory lock, and records completion only after online
// DDL/backfills finish. PostgreSQL's CONCURRENTLY statements cannot run in the
// transaction used by runMigration.
func runPostgresMigration(db *gorm.DB, name string, migrate func(*gorm.DB) error) error {
	return db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("SELECT pg_advisory_lock(hashtextextended(?, 0))", name).Error; err != nil {
			return fmt.Errorf("%s lock: %w", name, err)
		}
		defer conn.Exec("SELECT pg_advisory_unlock(hashtextextended(?, 0))", name)

		var applied int64
		if err := conn.Model(&schemaMigration{}).Where("name = ?", name).Count(&applied).Error; err != nil {
			return err
		}
		if applied > 0 {
			return nil
		}
		if err := migrate(conn); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return conn.Create(&schemaMigration{Name: name, AppliedAt: time.Now().UTC()}).Error
	})
}

func migrateUsageLedgerV2(tx *gorm.DB) error {
	if tx.Dialector.Name() == "postgres" {
		return migrateUsageLedgerPostgres(tx)
	}
	reportedPredicate := "occurred_at IS NOT NULL"
	if tx.Dialector.Name() == "sqlite" {
		reportedPredicate += " AND occurred_at <> ''"
	}
	statements := []struct {
		query string
		args  []interface{}
	}{
		{"UPDATE usage_records SET metered_at = occurred_at, timing_source = 'reported' WHERE metered_at IS NULL AND " + reportedPredicate, nil},
		{"UPDATE usage_records SET metered_at = created_at, timing_source = 'created_at_fallback' WHERE metered_at IS NULL", nil},
		{"UPDATE usage_records SET timing_source = 'reported' WHERE timing_source IS NULL OR timing_source = ''", nil},
		{"UPDATE usage_records SET pricing_state = ? WHERE (pricing_state IS NULL OR pricing_state = '') AND cost_micros <> 0", []interface{}{models.UsagePricingPriced}},
		{"UPDATE usage_records SET pricing_state = ? WHERE pricing_state IS NULL OR pricing_state = ''", []interface{}{models.UsagePricingUnpriced}},
		{"DROP INDEX IF EXISTS idx_usage_org_metered", nil},
		{"DROP INDEX IF EXISTS idx_usage_org_user_metered", nil},
		{"DROP INDEX IF EXISTS idx_usage_org_session_metered", nil},
		{"CREATE INDEX IF NOT EXISTS idx_usage_org_metered_id ON usage_records (organization_id, metered_at, id)", nil},
		{"CREATE INDEX IF NOT EXISTS idx_usage_org_user_metered_id ON usage_records (organization_id, user_id, metered_at, id)", nil},
		{"CREATE INDEX IF NOT EXISTS idx_usage_org_session_metered_id ON usage_records (organization_id, session_id, metered_at, id)", nil},
		{"CREATE INDEX IF NOT EXISTS idx_sessions_org_project_session ON sessions (organization_id, project_id, session_id)", nil},
	}
	for _, statement := range statements {
		if err := tx.Exec(statement.query, statement.args...).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateUsageLedgerPostgres(db *gorm.DB) error {
	updates := []struct {
		set, predicate string
		args           []interface{}
	}{
		{"metered_at = occurred_at, timing_source = 'reported'", "metered_at IS NULL AND occurred_at IS NOT NULL", nil},
		{"metered_at = created_at, timing_source = 'created_at_fallback'", "metered_at IS NULL", nil},
		{"timing_source = 'reported'", "timing_source IS NULL OR timing_source = ''", nil},
		{"pricing_state = ?", "(pricing_state IS NULL OR pricing_state = '') AND cost_micros <> 0", []interface{}{models.UsagePricingPriced}},
		{"pricing_state = ?", "pricing_state IS NULL OR pricing_state = ''", []interface{}{models.UsagePricingUnpriced}},
	}
	for _, update := range updates {
		query := fmt.Sprintf(`WITH batch AS (
			SELECT ctid FROM usage_records
			WHERE %s
			LIMIT 10000 FOR UPDATE SKIP LOCKED
		)
		UPDATE usage_records AS records SET %s
		FROM batch WHERE records.ctid = batch.ctid`, update.predicate, update.set)
		for {
			result := db.Exec(query, update.args...)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				break
			}
		}
	}
	statements := []string{
		"DROP INDEX CONCURRENTLY IF EXISTS idx_usage_org_metered",
		"DROP INDEX CONCURRENTLY IF EXISTS idx_usage_org_user_metered",
		"DROP INDEX CONCURRENTLY IF EXISTS idx_usage_org_session_metered",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_org_metered_id ON usage_records (organization_id, metered_at, id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_org_user_metered_id ON usage_records (organization_id, user_id, metered_at, id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_org_session_metered_id ON usage_records (organization_id, session_id, metered_at, id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_sessions_org_project_session ON sessions (organization_id, project_id, session_id)",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateBillingFXHistory(tx *gorm.DB) error {
	var settings []models.OrgSetting
	if err := tx.Where("key = ?", "billing.fx_rates").Find(&settings).Error; err != nil {
		return err
	}
	for i := range settings {
		if err := models.PersistBillingFXRateHistory(tx, &settings[i]); err != nil {
			return err
		}
	}
	return nil
}

func migrateBillingFXReportIndex(db *gorm.DB) error {
	if db.Dialector.Name() == "postgres" {
		if err := db.Exec("DROP INDEX CONCURRENTLY IF EXISTS idx_billing_fx_lookup").Error; err != nil {
			return err
		}
		return db.Exec("CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_billing_fx_report ON billing_fx_rates (organization_id, target_currency, source_currency, effective_at DESC, created_at DESC)").Error
	}
	if err := db.Exec("DROP INDEX IF EXISTS idx_billing_fx_lookup").Error; err != nil {
		return err
	}
	return db.Exec("CREATE INDEX IF NOT EXISTS idx_billing_fx_report ON billing_fx_rates (organization_id, target_currency, source_currency, effective_at DESC, created_at DESC)").Error
}

func migrateModelPricingExact(db *gorm.DB) error {
	statements := []string{
		`UPDATE model_packages
		 SET price_input_micros_per1_k = CAST(ROUND(price_input_per1_k * 1000000) AS BIGINT),
		     price_input_configured = true,
		     price_version = CASE WHEN price_version IS NULL OR price_version = '' THEN 'legacy-v1' ELSE price_version END,
		     price_source = CASE WHEN price_source IS NULL OR price_source = '' THEN 'legacy_model_package' ELSE price_source END
		 WHERE price_input_per1_k > 0 AND price_input_configured = false`,
		`UPDATE model_packages
		 SET price_output_micros_per1_k = CAST(ROUND(price_output_per1_k * 1000000) AS BIGINT),
		     price_output_configured = true,
		     price_version = CASE WHEN price_version IS NULL OR price_version = '' THEN 'legacy-v1' ELSE price_version END,
		     price_source = CASE WHEN price_source IS NULL OR price_source = '' THEN 'legacy_model_package' ELSE price_source END
		 WHERE price_output_per1_k > 0 AND price_output_configured = false`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateUsageProjectSnapshot(db *gorm.DB) error {
	if db.Dialector.Name() == "postgres" {
		for _, sessionColumn := range []string{"session_id", "id"} {
			query := fmt.Sprintf(`WITH batch AS (
			SELECT usage_records.ctid, sessions.project_id
			FROM usage_records
			JOIN sessions ON sessions.organization_id = usage_records.organization_id
				AND sessions.%s = usage_records.session_id
			WHERE (usage_records.project_id IS NULL OR usage_records.project_id = '')
				AND usage_records.session_id IS NOT NULL AND usage_records.session_id <> ''
				AND sessions.project_id IS NOT NULL AND sessions.project_id <> ''
			LIMIT 10000 FOR UPDATE OF usage_records SKIP LOCKED
		)
		UPDATE usage_records AS records SET project_id = batch.project_id
		FROM batch WHERE records.ctid = batch.ctid`, sessionColumn)
			for {
				result := db.Exec(query)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					break
				}
			}
		}
		return db.Exec("CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_org_project_metered_id ON usage_records (organization_id, project_id, metered_at, id)").Error
	}
	for _, sessionColumn := range []string{"session_id", "id"} {
		query := fmt.Sprintf(`UPDATE usage_records
		SET project_id = (
			SELECT sessions.project_id FROM sessions
			WHERE sessions.organization_id = usage_records.organization_id
				AND sessions.%s = usage_records.session_id
				AND sessions.project_id IS NOT NULL AND sessions.project_id <> ''
			LIMIT 1
		)
		WHERE (project_id IS NULL OR project_id = '')
			AND session_id IS NOT NULL AND session_id <> ''
			AND EXISTS (
				SELECT 1 FROM sessions
				WHERE sessions.organization_id = usage_records.organization_id
					AND sessions.%s = usage_records.session_id
					AND sessions.project_id IS NOT NULL AND sessions.project_id <> ''
			)`, sessionColumn, sessionColumn)
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	return db.Exec("CREATE INDEX IF NOT EXISTS idx_usage_org_project_metered_id ON usage_records (organization_id, project_id, metered_at, id)").Error
}
