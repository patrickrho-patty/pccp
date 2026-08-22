package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type migrationSQLLogger struct {
	logger.Interface
	statements []string
}

func (l *migrationSQLLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, _ := fc()
	l.statements = append(l.statements, sql)
}

func TestUsageLedgerMigrationIsOneTimeAndLeavesCleanSchemaEmpty(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:usage-migration?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.Session{}, &models.UsageRecord{}, &models.ModelPackage{}, &models.OrgSetting{}, &models.BillingFXRate{}, &schemaMigration{}); err != nil {
		t.Fatal(err)
	}
	var empty int64
	if err := database.Model(&models.UsageRecord{}).Count(&empty).Error; err != nil || empty != 0 {
		t.Fatalf("schema migration populated business data: count=%d err=%v", empty, err)
	}

	now := time.Now().UTC().Add(-time.Hour)
	if err := database.Exec(`INSERT INTO usage_records (id, created_at, organization_id, metric_type, quantity, unit, cost_micros, currency, occurred_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-usage", now, "org-1", "tokens_in", 10, "tokens", 50, "KRW", now.Format(time.RFC3339)).Error; err != nil {
		t.Fatal(err)
	}
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: "org-1"}, ProjectID: "project-original", SessionID: "session-project", HarnessID: "harness-1", UserID: "user-1", Status: "active"}
	if err := database.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	projectUsage := models.UsageRecord{OrganizationID: "org-1", SessionID: session.SessionID, MetricType: "tokens_in", Quantity: 1, Unit: "tokens", OccurredAt: now.Format(time.RFC3339)}
	if err := database.Create(&projectUsage).Error; err != nil {
		t.Fatal(err)
	}
	projectUsageID := projectUsage.ID
	internalSessionUsage := models.UsageRecord{OrganizationID: "org-1", SessionID: session.ID, MetricType: "tokens_out", Quantity: 1, Unit: "tokens", OccurredAt: now.Format(time.RFC3339)}
	if err := database.Create(&internalSessionUsage).Error; err != nil {
		t.Fatal(err)
	}
	internalSessionUsageID := internalSessionUsage.ID
	legacyPackage := models.ModelPackage{PackageID: "legacy-price", Name: "legacy", PriceInputPer1K: 1.25, PriceOutputPer1K: 2.5}
	if err := database.Create(&legacyPackage).Error; err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(database); err != nil {
		t.Fatal(err)
	}
	if err := database.First(&projectUsage, "id = ?", projectUsage.ID).Error; err != nil {
		t.Fatal(err)
	}
	if projectUsage.ProjectID != "project-original" {
		t.Fatalf("project snapshot after migration = %q, want original project", projectUsage.ProjectID)
	}
	if err := database.First(&internalSessionUsage, "id = ?", internalSessionUsageID).Error; err != nil {
		t.Fatal(err)
	}
	if internalSessionUsage.ProjectID != "project-original" {
		t.Fatalf("internal session project snapshot = %q, want original project", internalSessionUsage.ProjectID)
	}
	if err := database.Model(&session).Update("project_id", "project-reassigned").Error; err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(database); err != nil {
		t.Fatalf("second migration run must be a no-op: %v", err)
	}

	var record models.UsageRecord
	if err := database.First(&record, "id = ?", "legacy-usage").Error; err != nil {
		t.Fatal(err)
	}
	if record.MeteredAt == nil || record.TimingSource != "reported" || record.PricingState != models.UsagePricingPriced {
		t.Fatalf("legacy ledger was not normalized once: %+v", record)
	}
	projectUsage = models.UsageRecord{}
	if err := database.First(&projectUsage, "id = ?", projectUsageID).Error; err != nil {
		t.Fatal(err)
	}
	if projectUsage.ProjectID != "project-original" {
		t.Fatalf("one-time project snapshot = %q, want original project", projectUsage.ProjectID)
	}
	internalSessionUsage = models.UsageRecord{}
	if err := database.First(&internalSessionUsage, "id = ?", internalSessionUsageID).Error; err != nil {
		t.Fatal(err)
	}
	if internalSessionUsage.ProjectID != "project-original" {
		t.Fatalf("one-time internal session snapshot = %q, want original project", internalSessionUsage.ProjectID)
	}
	if err := database.First(&legacyPackage, "id = ?", legacyPackage.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !legacyPackage.PriceInputConfigured || !legacyPackage.PriceOutputConfigured || legacyPackage.PriceInputMicrosPer1K != 1_250_000 || legacyPackage.PriceOutputMicrosPer1K != 2_500_000 || legacyPackage.PriceVersion != "legacy-v1" || legacyPackage.PriceSource != "legacy_model_package" {
		t.Fatalf("legacy model pricing was not migrated exactly: %+v", legacyPackage)
	}
	var migrations int64
	if err := database.Model(&schemaMigration{}).Where("name = ?", "20260818_usage_ledger_v2").Count(&migrations).Error; err != nil || migrations != 1 {
		t.Fatalf("migration marker count=%d err=%v", migrations, err)
	}
}

func TestPublicAccountAuthorityRevisionMigrationAddsIntegerColumn(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:account-authority-revision?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`CREATE TABLE subscriptions (id text primary key, revision varchar(64))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migratePublicAccountAuthorityRevision(database); err != nil {
		t.Fatal(err)
	}
	if !database.Migrator().HasColumn(&models.Subscription{}, "AuthorityRevision") {
		t.Fatal("authority_revision column was not added")
	}
}

func TestPostgresUsageLedgerMigrationIsOnlineAndBatched(t *testing.T) {
	capture := &migrationSQLLogger{Interface: logger.Default.LogMode(logger.Info)}
	database, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=test password=test dbname=test port=5432 sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: capture})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateUsageLedgerV2(database); err != nil {
		t.Fatal(err)
	}
	sqlText := strings.Join(capture.statements, "\n")
	if !strings.Contains(sqlText, "CREATE INDEX CONCURRENTLY") {
		t.Fatalf("PostgreSQL migration must build indexes without blocking writes:\n%s", sqlText)
	}
	if !strings.Contains(sqlText, "FOR UPDATE SKIP LOCKED") || !strings.Contains(sqlText, "LIMIT") {
		t.Fatalf("PostgreSQL ledger normalization must be resumable and batched:\n%s", sqlText)
	}
	if strings.Contains(sqlText, "DROP INDEX IF EXISTS idx_usage_org_metered") && !strings.Contains(sqlText, "DROP INDEX CONCURRENTLY IF EXISTS idx_usage_org_metered") {
		t.Fatalf("PostgreSQL migration must not take a blocking index drop:\n%s", sqlText)
	}
	capture.statements = nil
	if err := migrateUsageProjectSnapshot(database); err != nil {
		t.Fatal(err)
	}
	sqlText = strings.Join(capture.statements, "\n")
	if !strings.Contains(sqlText, "sessions.session_id = usage_records.session_id") || !strings.Contains(sqlText, "sessions.id = usage_records.session_id") {
		t.Fatalf("project backfill must cover both canonical and internal session identifiers:\n%s", sqlText)
	}
	if !strings.Contains(sqlText, "FOR UPDATE OF usage_records SKIP LOCKED") || !strings.Contains(sqlText, "CREATE INDEX CONCURRENTLY") {
		t.Fatalf("project backfill and index must remain online:\n%s", sqlText)
	}
	capture.statements = nil
	if err := migrateBillingFXReportIndex(database); err != nil {
		t.Fatal(err)
	}
	sqlText = strings.Join(capture.statements, "\n")
	if !strings.Contains(sqlText, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_billing_fx_report ON billing_fx_rates (organization_id, target_currency, source_currency, effective_at DESC, created_at DESC)") {
		t.Fatalf("FX report lookup index does not match the tenant/target/source/effective query:\n%s", sqlText)
	}
}

func TestExternalIdentityMigrationUsesOnlyAuthoritativeProviderNamespace(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:external-identity-migration?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.Organization{}, &models.User{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{
		Name: "Identity Migration", Slug: "identity-migration", Status: "active",
		SSOConfig: `{"mode":"oidc","issuer":"https://issuer.example"}`,
	}
	if err := database.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	oidcUser := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "oidc@example.com", Status: "active", AuthMethod: "oidc", ExternalID: "shared-sub"}
	samlUser := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "saml@example.com", Status: "active", AuthMethod: "saml", ExternalID: "saml-sub"}
	scimUser := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "scim@example.com", Status: "active", AuthMethod: "scim", ExternalID: "scim-sub"}
	if err := database.Create(&oidcUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&samlUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&scimUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateExternalIdentityNamespace(database); err == nil {
		t.Fatal("issuer-less OIDC/SAML identities were guessed instead of requiring an explicit migration mapping")
	}
	if err := database.Model(&models.User{}).Where("id = ?", oidcUser.ID).Update("external_issuer", "https://issuer.example").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", samlUser.ID).Update("external_issuer", "https://legacy-saml.example").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateExternalIdentityNamespace(database); err != nil {
		t.Fatal(err)
	}
	if err := database.First(&oidcUser, "id = ?", oidcUser.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&samlUser, "id = ?", samlUser.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&scimUser, "id = ?", scimUser.ID).Error; err != nil {
		t.Fatal(err)
	}
	if oidcUser.ExternalIssuer != "https://issuer.example" || scimUser.ExternalIssuer != "scim" {
		t.Fatalf("authoritative namespaces not backfilled: oidc=%q scim=%q", oidcUser.ExternalIssuer, scimUser.ExternalIssuer)
	}
	if samlUser.ExternalIssuer != "https://legacy-saml.example" {
		t.Fatalf("explicit SAML issuer mapping was not preserved: %q", samlUser.ExternalIssuer)
	}
	duplicate := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "duplicate@example.com", Status: "active", AuthMethod: "oidc", ExternalIssuer: "https://issuer.example", ExternalID: "shared-sub"}
	if err := database.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate provider-scoped external identity was accepted")
	}
	otherIssuer := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "other@example.com", Status: "active", AuthMethod: "oidc", ExternalIssuer: "https://other-issuer.example", ExternalID: "shared-sub"}
	if err := database.Create(&otherIssuer).Error; err != nil {
		t.Fatalf("different verified issuer namespace was rejected: %v", err)
	}
}

func TestExternalIdentityV2AuditsDatabasesThatAlreadyRecordedUnsafeV1(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:external-identity-v2?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.Organization{}, &models.User{}, &schemaMigration{}); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&schemaMigration{Name: "20260818_external_identity_namespace_v1", AppliedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "Previously Migrated", Slug: "unsafe-v1", Status: "active", SSOConfig: `{"mode":"oidc","issuer":"https://replacement-idp.example"}`}
	if err := database.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	// This is the shape left by the former v1 implementation: the current IdP
	// issuer was guessed, but no durable evidence says the subject came from it.
	unsafe := models.User{
		AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "legacy@example.com", Status: "active",
		AuthMethod: "oidc", ExternalID: "legacy-subject", ExternalIssuer: "https://replacement-idp.example",
	}
	if err := database.Create(&unsafe).Error; err != nil {
		t.Fatal(err)
	}
	if err := runMigration(database, "20260818_external_identity_namespace_v2", migrateExternalIdentityNamespaceV2); err == nil {
		t.Fatal("v2 accepted an issuer guessed by the already-recorded unsafe v1 migration")
	}
	var applied int64
	if err := database.Model(&schemaMigration{}).Where("name = ?", "20260818_external_identity_namespace_v2").Count(&applied).Error; err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Fatal("failed v2 audit was incorrectly recorded as applied")
	}
	if err := database.Model(&models.User{}).Where("id = ?", unsafe.ID).Update("external_issuer_verified", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := runMigration(database, "20260818_external_identity_namespace_v2", migrateExternalIdentityNamespaceV2); err != nil {
		t.Fatalf("v2 rejected explicitly reviewed identity mapping: %v", err)
	}
}

func TestIdentityNormalizationNeverLinksPrivilegedCredentialsByEmail(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:identity-no-email-link?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.User{}, &identity.AdminCredentials{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{AuditBase: models.AuditBase{OrganizationID: "org-1"}, Email: "same@example.com", Status: "active", AuthMethod: "oidc", ExternalIssuer: "https://issuer.example", ExternalID: "subject-1"}
	credential := identity.AdminCredentials{OrganizationID: "org-1", Email: "same@example.com", Password: "unused", Role: "super_admin"}
	if err := database.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateUserIdentityNormalization(database); err != nil {
		t.Fatal(err)
	}
	if err := database.First(&credential, "id = ?", credential.ID).Error; err != nil {
		t.Fatal(err)
	}
	if credential.UserID != "" {
		t.Fatalf("migration linked a privileged credential to a mutable email identity: %q", credential.UserID)
	}
}

func TestSSOMigrationUniqueIndexesAreTenantScoped(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:sso-migration-tenant-indexes?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE sso_identity_links (id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, legacy_issuer TEXT NOT NULL, legacy_subject TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX idx_sso_link_legacy ON sso_identity_links (legacy_issuer, legacy_subject)`,
		`CREATE TABLE sso_migration_manifests (id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, manifest_id TEXT NOT NULL, import_id TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX idx_sso_migration_manifests_manifest_id ON sso_migration_manifests (manifest_id)`,
		`CREATE UNIQUE INDEX idx_sso_migration_manifests_import_id ON sso_migration_manifests (import_id)`,
		`INSERT INTO sso_identity_links (id, organization_id, legacy_issuer, legacy_subject) VALUES ('link-1', 'org-1', 'https://issuer.example', 'subject-1')`,
		`INSERT INTO sso_migration_manifests (id, organization_id, manifest_id, import_id) VALUES ('manifest-1', 'org-1', 'manifest-key', 'import-key')`,
	}
	for _, statement := range statements {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateSSOMigrationTenantIndexes(database); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO sso_identity_links (id, organization_id, legacy_issuer, legacy_subject) VALUES ('link-2', 'org-2', 'https://issuer.example', 'subject-1')`).Error; err != nil {
		t.Fatalf("cross-tenant identity key was rejected: %v", err)
	}
	if err := database.Exec(`INSERT INTO sso_identity_links (id, organization_id, legacy_issuer, legacy_subject) VALUES ('link-3', 'org-1', 'https://issuer.example', 'subject-1')`).Error; err == nil {
		t.Fatal("same-tenant identity duplicate was accepted")
	}
	if err := database.Exec(`INSERT INTO sso_migration_manifests (id, organization_id, manifest_id, import_id) VALUES ('manifest-2', 'org-2', 'manifest-key', 'import-key')`).Error; err != nil {
		t.Fatalf("cross-tenant manifest/import keys were rejected: %v", err)
	}
	if err := database.Exec(`INSERT INTO sso_migration_manifests (id, organization_id, manifest_id, import_id) VALUES ('manifest-3', 'org-1', 'manifest-key', 'different-import')`).Error; err == nil {
		t.Fatal("same-tenant manifest key duplicate was accepted")
	}
	if err := database.Exec(`INSERT INTO sso_migration_manifests (id, organization_id, manifest_id, import_id) VALUES ('manifest-4', 'org-1', 'different-manifest', 'import-key')`).Error; err == nil {
		t.Fatal("same-tenant import key duplicate was accepted")
	}
}

func TestSSOCanonicalIssuerMigrationBackfillsOnlyExplicitURLSources(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:sso-canonical-issuer?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.SSOIdentityLink{}, &models.SSOMigrationManifest{}); err != nil {
		t.Fatal(err)
	}
	urlManifest := models.SSOMigrationManifest{OrganizationID: "org-1", ManifestID: "url", ImportID: "url", Source: "https://idp.example/"}
	namedManifest := models.SSOMigrationManifest{OrganizationID: "org-1", ManifestID: "named", ImportID: "named", Source: "keycloak-estate"}
	if err := database.Create(&urlManifest).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&namedManifest).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateSSOCanonicalIssuer(database); err != nil {
		t.Fatal(err)
	}
	if err := database.First(&urlManifest, "id = ?", urlManifest.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&namedManifest, "id = ?", namedManifest.ID).Error; err != nil {
		t.Fatal(err)
	}
	if urlManifest.SourceIssuer != "https://idp.example" || namedManifest.SourceIssuer != "" {
		t.Fatalf("unsafe issuer backfill: url=%q named=%q", urlManifest.SourceIssuer, namedManifest.SourceIssuer)
	}
}

func TestPostgresSSOMigrationIndexesReplaceGlobalConstraintWithoutGap(t *testing.T) {
	statements := ssoMigrationTenantIndexStatements(true)
	if len(statements) != 8 {
		t.Fatalf("unexpected migration statement count: %d", len(statements))
	}
	if !strings.Contains(statements[0], "CREATE UNIQUE INDEX CONCURRENTLY") || !strings.Contains(statements[0], "idx_sso_link_legacy_tenant_v1") {
		t.Fatalf("tenant replacement must be built first: %q", statements[0])
	}
	for _, name := range ssoMigrationConcurrentIndexNames {
		found := false
		for _, statement := range statements {
			if strings.HasPrefix(statement, "CREATE ") && strings.Contains(statement, name) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("concurrent index %q is not covered by retry recovery statements: %#v", name, statements)
		}
	}
	dropAt, renameAt := -1, -1
	for i, statement := range statements {
		if strings.Contains(statement, "DROP INDEX CONCURRENTLY IF EXISTS idx_sso_link_legacy") {
			dropAt = i
		}
		if strings.Contains(statement, "ALTER INDEX idx_sso_link_legacy_tenant_v1 RENAME") {
			renameAt = i
		}
	}
	if dropAt <= 0 || renameAt <= dropAt {
		t.Fatalf("unsafe replacement order: %#v", statements)
	}
}

func TestAccountAuthorityIndexAllowsEmailCollisionsButRejectsAuthorityCollisions(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:account-authority-index?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.Account{}); err != nil {
		t.Fatal(err)
	}
	if err := migrateAccountAuthorityIDIndex(database); err != nil {
		t.Fatal(err)
	}
	for _, account := range []models.Account{
		{Email: "shared@example.com", AuthorityID: "authority-1"},
		{Email: "shared@example.com", AuthorityID: "authority-2"},
		{Email: "legacy-one@example.com"},
		{Email: "legacy-two@example.com"},
	} {
		if err := database.Create(&account).Error; err != nil {
			t.Fatalf("valid account rejected: %v", err)
		}
	}
	duplicate := models.Account{Email: "other@example.com", AuthorityID: "authority-1"}
	if err := database.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate immutable authority id was accepted")
	}
}
