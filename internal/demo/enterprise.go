// Package demo contains explicit, deterministic demo fixtures. Nothing in
// this package is called by server startup; operators invoke the dedicated
// pccp-demo-seed command when they want the Enterprise/Government console
// populated.
package demo

import (
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	DemoOrganizationID = "org_demo_enterprise_2026"
	DemoNow            = "2026-08-18T12:00:00Z"
	DemoAdminEmail     = "demo-admin@patty.dev"
	DemoAdminPassword  = "1234"
)

var demoNowTime = mustParseTime(DemoNow)

func mustParseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}

func demoAt(hours int) string {
	return demoNowTime.Add(time.Duration(hours) * time.Hour).Format(time.RFC3339)
}

func base(id string, offset int) models.Base {
	t := demoNowTime.Add(time.Duration(offset) * time.Hour)
	return models.Base{ID: id, CreatedAt: t, UpdatedAt: t}
}

func auditBase(id, orgID string, offset int) models.AuditBase {
	return models.AuditBase{
		Base:            base(id, offset),
		OrganizationID:  orgID,
		Classification:  "internal",
		RetentionPolicy: "enterprise-365d",
		AccessLabels:    `["enterprise-console"]`,
		ArchiveState:    "active",
	}
}

// ensureByID is intentionally ID-based. Every row in this fixture has a
// stable primary key, so rerunning the command neither duplicates rows nor
// changes timestamps or relationship edges.
func ensureByID(tx *gorm.DB, id string, value interface{}) error {
	lookup := tx.Where("id = ?", id).Limit(1).Find(value)
	if lookup.Error != nil {
		return lookup.Error
	}
	if lookup.RowsAffected > 0 {
		return nil
	}
	return tx.Create(value).Error
}

func ensureAdmin(tx *gorm.DB, orgID string) error {
	var existing identity.AdminCredentials
	lookup := tx.Where("email = ?", DemoAdminEmail).Limit(1).Find(&existing)
	if lookup.Error != nil {
		return lookup.Error
	}
	if lookup.RowsAffected > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(DemoAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash demo admin password: %w", err)
	}
	return tx.Create(&identity.AdminCredentials{
		Base:           base("admin_demo_patty", 0),
		Email:          DemoAdminEmail,
		Password:       string(hash),
		OrganizationID: orgID,
		Name:           "Enterprise Console Admin",
		Role:           "super_admin",
	}).Error
}

// SeedEnterprise inserts the complete deterministic Enterprise/Government
// relationship graph. It is safe to run repeatedly on the same database.
func SeedEnterprise(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		orgID := DemoOrganizationID
		if err := ensureByID(tx, orgID, &models.Organization{
			Base:                   base(orgID, 0),
			Name:                   "Patty Enterprise Demo",
			NameKo:                 "패티 엔터프라이즈 데모",
			Slug:                   "patty-enterprise-demo",
			Type:                   "enterprise",
			Profile:                "enterprise",
			Status:                 "active",
			SSOConfig:              `{"provider":"authentik","mode":"oidc","enforced":true}`,
			DefaultRetention:       "enterprise-365d",
			DataRegion:             "kr-central",
			BusinessRegistrationNo: "demo-2026-0818",
			GroupCompany:           true,
			MaxUserSeats:           250,
			MaxHarnessSeats:        100,
			PlanTier:               "enterprise",
			PlanRenewalDate:        "2027-08-18T00:00:00Z",
			BillingContact:         "finance@patty.dev",
		}); err != nil {
			return fmt.Errorf("organization: %w", err)
		}
		if err := ensureAdmin(tx, orgID); err != nil {
			return err
		}

		// Organization hierarchy, identity, roles, and enrolled harnesses.
		for _, unit := range []models.BusinessUnit{
			{Base: base("bu_demo_platform", -48), OrganizationID: orgID, Name: "Platform Engineering", NameKo: "플랫폼 엔지니어링", Type: "business_unit", Level: 1},
			{Base: base("bu_demo_security", -47), OrganizationID: orgID, Name: "Security & Trust", NameKo: "보안·신뢰", Type: "business_unit", Level: 1},
			{Base: base("bu_demo_product", -46), OrganizationID: orgID, Name: "Product Engineering", NameKo: "제품 엔지니어링", Type: "business_unit", Level: 1},
		} {
			if err := ensureByID(tx, unit.ID, &unit); err != nil {
				return fmt.Errorf("business unit %s: %w", unit.ID, err)
			}
		}

		users := []models.User{
			{AuditBase: auditBase("usr_demo_kim", orgID, -36), Email: "demo.kim@patty.dev", EmailKo: "demo.kim@patty.dev", Name: "Kim Minseo", NameKo: "김민서", EmployeeId: "P-1001", Title: "Senior Engineer", TitleKo: "선임 엔지니어", Status: "active", AuthMethod: "oidc", ExternalID: "authentik|demo-kim", MFAEnrolled: true, BusinessUnitID: "bu_demo_platform", Locale: "ko-KR", Timezone: "Asia/Seoul", LastLoginAt: ptrString(demoAt(-1))},
			{AuditBase: auditBase("usr_demo_lee", orgID, -35), Email: "demo.lee@patty.dev", EmailKo: "demo.lee@patty.dev", Name: "Lee Jisoo", NameKo: "이지수", EmployeeId: "P-1002", Title: "QA Engineer", TitleKo: "QA 엔지니어", Status: "active", AuthMethod: "saml", ExternalID: "authentik|demo-lee", MFAEnrolled: true, BusinessUnitID: "bu_demo_product", Locale: "ko-KR", Timezone: "Asia/Seoul", LastLoginAt: ptrString(demoAt(-3))},
			{AuditBase: auditBase("usr_demo_park", orgID, -34), Email: "demo.park@patty.dev", EmailKo: "demo.park@patty.dev", Name: "Park Seoyeon", NameKo: "박서연", EmployeeId: "P-1003", Title: "Security Analyst", TitleKo: "보안 분석가", Status: "active", AuthMethod: "ldap", ExternalID: "authentik|demo-park", MFAEnrolled: true, BusinessUnitID: "bu_demo_security", Locale: "ko-KR", Timezone: "Asia/Seoul", LastLoginAt: ptrString(demoAt(-5))},
			{AuditBase: auditBase("usr_demo_choi", orgID, -33), Email: "demo.choi@patty.dev", EmailKo: "demo.choi@patty.dev", Name: "Choi Junho", NameKo: "최준호", EmployeeId: "P-1004", Title: "Engineering Lead", TitleKo: "엔지니어링 리드", Status: "suspended", AuthMethod: "oidc", ExternalID: "authentik|demo-choi", MFAEnrolled: false, BusinessUnitID: "bu_demo_product", Locale: "ko-KR", Timezone: "Asia/Seoul", LastLoginAt: ptrString(demoAt(-72))},
		}
		for i := range users {
			if err := ensureByID(tx, users[i].ID, &users[i]); err != nil {
				return fmt.Errorf("user %s: %w", users[i].ID, err)
			}
		}

		roles := []models.Role{
			{Base: base("role_demo_owner", -45), OrganizationID: orgID, Name: "Project Owner", NameKo: "프로젝트 소유자", Permissions: `["project:read","project:write","session:approve","provenance:read"]`, IsSystem: false},
			{Base: base("role_demo_reviewer", -44), OrganizationID: orgID, Name: "Security Reviewer", NameKo: "보안 검토자", Permissions: `["security:read","security:resolve","policy:approve","audit:read"]`, IsSystem: false},
			{Base: base("role_demo_operator", -43), OrganizationID: orgID, Name: "Harness Operator", NameKo: "하네스 운영자", Permissions: `["harness:read","harness:enroll","sandbox:read","session:read"]`, IsSystem: false},
			{Base: base("role_demo_project_developer", -42), OrganizationID: orgID, Name: "project-developer", NameKo: "프로젝트 개발자", Permissions: `["session:open","inference:use","project:read","repo:read"]`, IsSystem: true},
			{Base: base("role_demo_repo_reader", -41), OrganizationID: orgID, Name: "repo-reader", NameKo: "저장소 읽기", Permissions: `["session:open","inference:use","repo:read"]`, IsSystem: true},
			{Base: base("role_demo_model_user", -40), OrganizationID: orgID, Name: "model-user", NameKo: "모델 사용자", Permissions: `["session:open","inference:use"]`, IsSystem: true},
			{Base: base("role_demo_global_developer", -39), OrganizationID: orgID, Name: "global-developer", NameKo: "글로벌 개발자", Permissions: `["session:open","inference:use","project:read","repo:read","class:interactive-paid"]`, IsSystem: true},
		}
		for i := range roles {
			if err := ensureByID(tx, roles[i].ID, &roles[i]); err != nil {
				return fmt.Errorf("role %s: %w", roles[i].ID, err)
			}
		}
		for _, binding := range []models.UserRole{
			{Base: base("urole_demo_kim_owner", -32), UserID: "usr_demo_kim", RoleID: "role_demo_owner", OrganizationID: orgID, Scope: "project", ScopeID: "prj_demo_payments"},
			{Base: base("urole_demo_park_reviewer", -31), UserID: "usr_demo_park", RoleID: "role_demo_reviewer", OrganizationID: orgID, Scope: "org", ScopeID: orgID},
			{Base: base("urole_demo_lee_operator", -30), UserID: "usr_demo_lee", RoleID: "role_demo_operator", OrganizationID: orgID, Scope: "org", ScopeID: orgID},
			{Base: base("urole_demo_kim_entitlement", -29), UserID: "usr_demo_kim", RoleID: "role_demo_global_developer", OrganizationID: orgID, Scope: "org", ScopeID: orgID},
			{Base: base("urole_demo_lee_entitlement", -28), UserID: "usr_demo_lee", RoleID: "role_demo_repo_reader", OrganizationID: orgID, Scope: "project", ScopeID: "prj_demo_portal"},
		} {
			if err := ensureByID(tx, binding.ID, &binding); err != nil {
				return fmt.Errorf("user role %s: %w", binding.ID, err)
			}
		}

		devices := []models.Device{
			{Base: base("dev_demo_kim_mac", -28), OrganizationID: orgID, UserID: "usr_demo_kim", Hostname: "kim-mbp-patty", OS: "macOS", OSVersion: "15.6", Arch: "arm64", MDMEnrolled: true, MDMPosture: `{"disk_encryption":true,"screen_lock":true}`, PublicKey: "ed25519-demo-device-kim", NetworkZone: "corp-seoul", IPAddress: "10.20.1.21", Status: "active", FirstSeen: demoAt(-720), LastSeen: demoAt(-1)},
			{Base: base("dev_demo_lee_win", -27), OrganizationID: orgID, UserID: "usr_demo_lee", Hostname: "lee-win-patty", OS: "Windows", OSVersion: "11 24H2", Arch: "amd64", MDMEnrolled: true, MDMPosture: `{"disk_encryption":true,"screen_lock":true}`, PublicKey: "ed25519-demo-device-lee", NetworkZone: "corp-seoul", IPAddress: "10.20.1.22", Status: "active", FirstSeen: demoAt(-680), LastSeen: demoAt(-3)},
			{Base: base("dev_demo_park_linux", -26), OrganizationID: orgID, UserID: "usr_demo_park", Hostname: "park-secure-linux", OS: "Ubuntu", OSVersion: "24.04", Arch: "amd64", MDMEnrolled: false, MDMPosture: `{"disk_encryption":true,"screen_lock":true}`, PublicKey: "ed25519-demo-device-park", NetworkZone: "restricted-secops", IPAddress: "10.30.4.11", Status: "active", FirstSeen: demoAt(-640), LastSeen: demoAt(-5)},
		}
		for i := range devices {
			if err := ensureByID(tx, devices[i].ID, &devices[i]); err != nil {
				return fmt.Errorf("device %s: %w", devices[i].ID, err)
			}
		}
		harnesses := []models.Harness{
			{Base: base("hrn_demo_kim", -23), OrganizationID: orgID, DeviceID: "dev_demo_kim_mac", HarnessID: "hrn_demo_kim", Name: "Kim · macOS Harness", BinaryVersion: "1.8.0", BinaryHash: "sha256:harness-demo-kim", ExtensionVersion: "1.4.0", CLIVersion: "1.8.0", BuildChannel: "stable", PublicKey: "ed25519-demo-harness-kim", CredentialDigest: "sha256:credential-demo-kim", AllowedUsers: `["usr_demo_kim"]`, PolicyProfile: "enterprise-default", LicenseState: "licensed", Status: "active", RiskState: "normal", EnrollmentMode: "sso", EnrolledAt: demoAt(-600), LastHeartbeat: demoAt(-1), LastAttestation: demoAt(-2), CPEndpoint: "https://control.patty.dev", NetworkZone: "corp-seoul"},
			{Base: base("hrn_demo_lee", -22), OrganizationID: orgID, DeviceID: "dev_demo_lee_win", HarnessID: "hrn_demo_lee", Name: "Lee · Windows Harness", BinaryVersion: "1.7.4", BinaryHash: "sha256:harness-demo-lee", ExtensionVersion: "1.3.2", CLIVersion: "1.7.4", BuildChannel: "stable", PublicKey: "ed25519-demo-harness-lee", CredentialDigest: "sha256:credential-demo-lee", AllowedUsers: `["usr_demo_lee"]`, PolicyProfile: "enterprise-default", LicenseState: "licensed", Status: "active", RiskState: "normal", EnrollmentMode: "sso", EnrolledAt: demoAt(-580), LastHeartbeat: demoAt(-3), LastAttestation: demoAt(-4), CPEndpoint: "https://control.patty.dev", NetworkZone: "corp-seoul"},
			{Base: base("hrn_demo_park", -21), OrganizationID: orgID, DeviceID: "dev_demo_park_linux", HarnessID: "hrn_demo_park", Name: "Park · Security Harness", BinaryVersion: "1.8.0", BinaryHash: "sha256:harness-demo-park", ExtensionVersion: "1.4.0", CLIVersion: "1.8.0", BuildChannel: "stable", PublicKey: "ed25519-demo-harness-park", CredentialDigest: "sha256:credential-demo-park", AllowedUsers: `["usr_demo_park"]`, PolicyProfile: "restricted-secops", LicenseState: "licensed", Status: "enrolled", RiskState: "elevated", EnrollmentMode: "pre_approved", EnrolledAt: demoAt(-560), LastHeartbeat: demoAt(-5), LastAttestation: demoAt(-6), CPEndpoint: "https://control.patty.dev", NetworkZone: "restricted-secops"},
		}
		for i := range harnesses {
			if err := ensureByID(tx, harnesses[i].ID, &harnesses[i]); err != nil {
				return fmt.Errorf("harness %s: %w", harnesses[i].ID, err)
			}
		}
		if err := ensureByID(tx, "enroll_demo_2026", &models.EnrollmentCode{Base: base("enroll_demo_2026", -20), OrganizationID: orgID, Code: "PATTY-DEMO-2026", UserID: "usr_demo_kim", ExpiresAt: "2026-09-18T00:00:00Z", Used: true, UsedBy: "hrn_demo_kim"}); err != nil {
			return fmt.Errorf("enrollment code: %w", err)
		}

		// Projects, repositories, branches, baselines, memberships, and tool scopes.
		projects := []models.Project{
			{AuditBase: auditBase("prj_demo_payments", orgID, -19), Name: "Payment Service", NameKo: "결제 서비스", Slug: "payment-service", Description: "Governed refund and settlement workflows.", Status: "active", AllowedModelClasses: `["catalog_demo_kocoder"]`, ProjectCode: "PAY-26", GroupAffiliate: "Patty Commerce"},
			{AuditBase: auditBase("prj_demo_portal", orgID, -18), Name: "Customer Portal", NameKo: "고객 포털", Slug: "customer-portal", Description: "Customer-facing web application with evidence-backed changes.", Status: "active", AllowedModelClasses: `["catalog_demo_kocoder"]`, ProjectCode: "WEB-26", GroupAffiliate: "Patty Commerce"},
			{AuditBase: auditBase("prj_demo_platform", orgID, -17), Name: "Platform Controls", NameKo: "플랫폼 제어 plane", Slug: "platform-controls", Description: "Infrastructure and security policy automation.", Status: "active", AllowedModelClasses: `["catalog_demo_kocoder"]`, ProjectCode: "PLAT-26", GroupAffiliate: "Patty Technology"},
		}
		for i := range projects {
			projects[i].PolicyPackID = "pack_demo_enterprise"
			if err := ensureByID(tx, projects[i].ID, &projects[i]); err != nil {
				return fmt.Errorf("project %s: %w", projects[i].ID, err)
			}
		}
		for _, member := range []models.ProjectMember{
			{Base: base("pm_demo_payments_kim", -16), OrganizationID: orgID, ProjectID: "prj_demo_payments", UserID: "usr_demo_kim", Role: "owner", GrantedBy: "usr_demo_park"},
			{Base: base("pm_demo_payments_lee", -15), OrganizationID: orgID, ProjectID: "prj_demo_payments", UserID: "usr_demo_lee", Role: "maintainer", GrantedBy: "usr_demo_kim"},
			{Base: base("pm_demo_portal_choi", -14), OrganizationID: orgID, ProjectID: "prj_demo_portal", UserID: "usr_demo_choi", Role: "viewer", GrantedBy: "usr_demo_park"},
			{Base: base("pm_demo_platform_park", -13), OrganizationID: orgID, ProjectID: "prj_demo_platform", UserID: "usr_demo_park", Role: "owner", GrantedBy: "usr_demo_park"},
		} {
			if err := ensureByID(tx, member.ID, &member); err != nil {
				return fmt.Errorf("project member %s: %w", member.ID, err)
			}
		}
		repositories := []models.Repository{
			{AuditBase: auditBase("repo_demo_payments", orgID, -12), ProjectID: "prj_demo_payments", Name: "payment-service", FullName: "patty/payment-service", CloneURL: "https://gitlab.example/patty/payment-service.git", SCMType: "gitlab", SCMProvider: "GitLab Enterprise", DefaultBranch: "main", Sensitivity: "confidential", Status: "active", LastSyncAt: demoAt(-2), LastCommitAt: demoAt(-4), SyncStatus: "synced"},
			{AuditBase: auditBase("repo_demo_portal", orgID, -11), ProjectID: "prj_demo_portal", Name: "customer-portal", FullName: "patty/customer-portal", CloneURL: "https://github.example/patty/customer-portal.git", SCMType: "github", SCMProvider: "GitHub Enterprise", DefaultBranch: "main", Sensitivity: "internal", Status: "active", LastSyncAt: demoAt(-6), LastCommitAt: demoAt(-8), SyncStatus: "synced"},
			{AuditBase: auditBase("repo_demo_platform", orgID, -10), ProjectID: "prj_demo_platform", Name: "platform-controls", FullName: "patty/platform-controls", CloneURL: "https://git.example/patty/platform-controls.git", SCMType: "git", SCMProvider: "Patty Git", DefaultBranch: "main", Sensitivity: "restricted", Status: "active", LastSyncAt: demoAt(-10), LastCommitAt: demoAt(-12), SyncStatus: "synced"},
		}
		for i := range repositories {
			if err := ensureByID(tx, repositories[i].ID, &repositories[i]); err != nil {
				return fmt.Errorf("repository %s: %w", repositories[i].ID, err)
			}
		}
		branches := []models.Branch{
			{Base: base("branch_demo_payments_main", -9), RepositoryID: "repo_demo_payments", Name: "main", ProtectionLevel: "protected", RequiresApproval: true, BaselineCommit: "a1b2c3d4", Status: "active"},
			{Base: base("branch_demo_payments_refund", -8), RepositoryID: "repo_demo_payments", Name: "feature/refund-idempotency", ProtectionLevel: "standard", RequiresApproval: false, BaselineCommit: "d4c3b2a1", Status: "active"},
			{Base: base("branch_demo_portal_main", -7), RepositoryID: "repo_demo_portal", Name: "main", ProtectionLevel: "protected", RequiresApproval: true, BaselineCommit: "f0e1d2c3", Status: "active"},
			{Base: base("branch_demo_platform_main", -6), RepositoryID: "repo_demo_platform", Name: "main", ProtectionLevel: "locked", RequiresApproval: true, BaselineCommit: "11223344", Status: "active"},
		}
		for i := range branches {
			if err := ensureByID(tx, branches[i].ID, &branches[i]); err != nil {
				return fmt.Errorf("branch %s: %w", branches[i].ID, err)
			}
		}
		baselines := []models.RepoBaseline{
			{Base: base("base_demo_payments_main", -5), RepositoryID: "repo_demo_payments", Branch: "main", CommitSHA: "a1b2c3d4e5f6", CommitMessage: "Harden refund authorization", AuthorName: "Kim Minseo", AuthorEmail: "kim@patty.dev", CommittedAt: demoAt(-5), TreeDigest: "sha256:tree-demo-payments", OrgID: orgID, CreatedBy: "sess_demo_refund"},
			{Base: base("base_demo_portal_main", -4), RepositoryID: "repo_demo_portal", Branch: "main", CommitSHA: "f0e1d2c3b4a5", CommitMessage: "Add account recovery evidence", AuthorName: "Lee Jisoo", AuthorEmail: "lee@patty.dev", CommittedAt: demoAt(-8), TreeDigest: "sha256:tree-demo-portal", OrgID: orgID, CreatedBy: "sess_demo_portal"},
			{Base: base("base_demo_platform_main", -3), RepositoryID: "repo_demo_platform", Branch: "main", CommitSHA: "112233445566", CommitMessage: "Enforce restricted network egress", AuthorName: "Park Seoyeon", AuthorEmail: "park@patty.dev", CommittedAt: demoAt(-12), TreeDigest: "sha256:tree-demo-platform", OrgID: orgID, CreatedBy: "sess_demo_security"},
		}
		for i := range baselines {
			if err := ensureByID(tx, baselines[i].ID, &baselines[i]); err != nil {
				return fmt.Errorf("baseline %s: %w", baselines[i].ID, err)
			}
		}

		// Model package, catalog, endpoint identity and policy epochs/leases.
		modelPackage := &models.ModelPackage{
			Base: base("pmp_demo_kocoder", -40), PackageID: "pmp_demo_kocoder", ModelID: "patty-kocoder-35b", Name: "Patty KoCoder 35B", NameKo: "패티 코코더 35B", Family: "coder", Version: "2026.08.1", Release: "stable", WeightsMerkleRoot: "sha256:weights-demo-kocoder", TokenizerDigest: "sha256:tokenizer-demo-kocoder", ConfigDigest: "sha256:config-demo-kocoder", ChatTemplateDigest: "sha256:chat-demo-kocoder", QuantType: "bf16", ServingEnginesJSON: `[{"engine":"vllm","minVersion":"0.6"}]`, ContainerDigest: "sha256:container-demo-kocoder", CapabilitiesJSON: `["code","korean","tool_use","streaming"]`, EntitlementClass: "enterprise-code", MinAssuranceLevel: "L2", AllowedDataClasses: `["internal","confidential"]`, ContextWindow: 128000, ManifestDigest: "sha256:manifest-demo-kocoder", SignatureKeyID: "pccp-demo-registry", Signature: "cose-demo-kocoder", PriceInputPer1K: 0.8, PriceOutputPer1K: 2.4, State: "published", PublishedAt: demoAt(-40), Expiry: "2027-08-18T00:00:00Z",
		}
		if err := ensureByID(tx, modelPackage.ID, modelPackage); err != nil {
			return fmt.Errorf("model package: %w", err)
		}
		if err := ensureByID(tx, "catalog_demo_kocoder", &models.CatalogModel{Base: base("catalog_demo_kocoder", -39), OrganizationID: orgID, CatalogModelID: "catalog_demo_kocoder", DisplayName: "Patty KoCoder", DisplayNameKo: "패티 코코더", Description: "Enterprise code model with Korean-language governance metadata.", DescriptionKo: "한국어 거버넌스 메타데이터를 제공하는 엔터프라이즈 코드 모델입니다.", Family: "code", ReleaseChannel: "stable", Availability: "available", DefaultRank: 1, CapabilitiesJSON: `{"input":{"text":true,"file":true},"output":{"text":true},"tools":{"runtime_tools":true,"mcp":true,"approval":true},"streaming":true}`, MaxInputTokens: 128000, MaxOutputTokens: 8192, MaxTools: 64, MaxParallelTools: 8, EntitlementClass: "enterprise-code", EntitlementLabel: "Enterprise Code", EntitlementLabelKo: "엔터프라이즈 코드", MinHarnessVersion: "1.7.0", MinDARIProtocolVersion: 2, RequiredExtensions: `["provenance","policy-ack"]`, AnnouncedAt: demoAt(-39), ProductionPackageID: modelPackage.PackageID, Status: "active"}); err != nil {
			return fmt.Errorf("catalog model: %w", err)
		}
		if err := ensureByID(tx, "catalog_epoch_demo_2026", &models.CatalogEpoch{Base: base("catalog_epoch_demo_2026", -38), OrganizationID: orgID, EpochID: "catalog_epoch_demo_2026", EpochNumber: 12, GeneratedAt: DemoNow, ScopeDigest: "sha256:scope-demo-enterprise", EntitlementRevision: "ent-2026-08-18", ModelsJSON: `[{"catalog_model_id":"catalog_demo_kocoder","availability":"available"}]`, MinValiditySecs: 3600, CPSignature: "cose-demo-catalog", Status: "active"}); err != nil {
			return fmt.Errorf("catalog epoch: %w", err)
		}
		endpoint := &models.InferenceEndpoint{Base: base("ep_demo_kocoder_1", -37), OrganizationID: orgID, EndpointID: "ep_demo_kocoder_1", Name: "Seoul GPU Pool · KoCoder", PIAPeerID: "pia_demo_kocoder_1", PIABuildDigest: "sha256:pia-demo", ModelPackageID: modelPackage.PackageID, ServingEngine: "vllm", ServingEngineVer: "0.6.4", ServingURL: "https://internal-inference/patty-kocoder", NodeIdentity: "spiffe://patty.dev/node/seoul-gpu-01", WorkloadIdentity: "spiffe://patty.dev/workload/kocoder", GPUIDs: `["gpu-seoul-01","gpu-seoul-02"]`, AssuranceLevel: "L2", Status: "active", CapacityClass: "standard", AllowedOrgsJSON: fmt.Sprintf(`["%s"]`, orgID), PublicKey: "ed25519-demo-pia", EnrolledAt: demoAt(-37), LastAttestation: demoAt(-2)}
		if err := ensureByID(tx, endpoint.ID, endpoint); err != nil {
			return fmt.Errorf("endpoint: %w", err)
		}
		if err := ensureByID(tx, "attest_demo_kocoder_1", &models.EndpointAttestation{Base: base("attest_demo_kocoder_1", -36), EndpointID: endpoint.EndpointID, OrganizationID: orgID, Nonce: "nonce-demo-kocoder", ModelPackageID: modelPackage.PackageID, ModelManifestDigest: modelPackage.ManifestDigest, ModelMerkleRoot: modelPackage.WeightsMerkleRoot, TokenizerDigest: modelPackage.TokenizerDigest, RuntimeConfigDigest: "sha256:runtime-demo", PIABuildDigest: endpoint.PIABuildDigest, ServingContainerDigest: modelPackage.ContainerDigest, NodeAttestation: "tpm-demo-seoul-gpu-01", GPUAttestation: "attestation-demo-a100", Signature: "sig-demo-attestation", KeyAlgorithm: "ed25519", Verified: true, VerifiedAt: demoAt(-2), Timestamp: demoAt(-2)}); err != nil {
			return fmt.Errorf("endpoint attestation: %w", err)
		}
		if err := ensureByID(tx, "lease_demo_endpoint_1", &models.EndpointLease{Base: base("lease_demo_endpoint_1", -35), EndpointID: endpoint.EndpointID, OrganizationID: orgID, ModelPackageID: modelPackage.PackageID, LeaseID: "lease_demo_endpoint_1", PIAPeerID: endpoint.PIAPeerID, PIAPublicKey: endpoint.PublicKey, HostNodeID: "seoul-gpu-01", PermittedOrgsJSON: fmt.Sprintf(`["%s"]`, orgID), AllowedDataClasses: `["internal","confidential"]`, CapacityClass: "standard", RoutingZones: `["kr-central"]`, NotBefore: demoAt(-35), NotAfter: "2026-08-19T12:00:00Z", AttestationID: "attest_demo_kocoder_1", NonceRef: "nonce-demo-kocoder", CPSignature: "sig-demo-endpoint-lease", RevocationEpoch: 12, Status: "active", IssuedAt: demoAt(-35)}); err != nil {
			return fmt.Errorf("endpoint lease: %w", err)
		}
		if err := ensureByID(tx, "pack_demo_enterprise", &models.PolicyPack{Base: base("pack_demo_enterprise", -34), OrganizationID: orgID, Name: "Enterprise Guardrails", NameKo: "엔터프라이즈 가드레일", Version: "2026.08", Profile: "enterprise", DLPRulesJSON: `{"rrn":"block","secrets":"mask"}`, InjectionRulesJSON: `{"indirect_prompt":"review"}`, ToolPolicyJSON: `{"execute":"approval","read":"allow"}`, NetworkPolicyJSON: `{"egress":"allowlist"}`, ModelPolicyJSON: `{"allowed":["pmp_demo_kocoder"]}`, ApprovalMatrixJSON: `{"file_write":"owner","network":"security"}`, RetentionPolicyJSON: `{"audit":"365d","provenance":"365d"}`, Digest: "sha256:pack-demo-enterprise", Status: "active"}); err != nil {
			return fmt.Errorf("policy pack: %w", err)
		}
		if err := ensureByID(tx, "epoch_demo_policy_2026", &models.PolicyEpoch{Base: base("epoch_demo_policy_2026", -33), OrganizationID: orgID, EpochID: "epoch_demo_policy_2026", EpochNumber: 42, OrgPolicyDigest: "sha256:org-policy-demo", ProjectOverlayDigest: "sha256:project-overlay-demo", ModelPolicyDigest: "sha256:model-policy-demo", DLPSecurityDigest: "sha256:dlp-demo", ApprovalMatrixDigest: "sha256:approval-demo", RetentionPolicyDigest: "sha256:retention-demo", EngineVersion: "policy-engine-3.4", AllowedModelsJSON: `["pmp_demo_kocoder"]`, DomainPoliciesJSON: `{"models":{"allow":true},"tools":{"approval":true},"network":{"allowlist":true}}`, TransitionMode: "immediate", RequiresAck: true, EffectiveAt: DemoNow, Status: "active"}); err != nil {
			return fmt.Errorf("policy epoch: %w", err)
		}
		if err := ensureByID(tx, "lease_demo_capability", &models.CapabilityLease{Base: base("lease_demo_capability", -32), OrganizationID: orgID, LeaseID: "lease_demo_capability", SubjectPeerID: "hrn_demo_kim", UserID: "usr_demo_kim", SessionID: "sess_demo_refund", PolicyEpochID: "epoch_demo_policy_2026", AllowedModelPackages: `["pmp_demo_kocoder"]`, RepositoryScope: `["repo_demo_payments:main","repo_demo_payments:feature/refund-idempotency"]`, FilePathScope: `{"read":["/"],"write":["src/refunds/**"]}`, ToolClasses: `["read","write","execute"]`, NetworkDestinations: `["gitlab.example","internal-inference"]`, TokenBudget: 200000, ContextBudget: 128000, ResourceBudget: `{"gpu_seconds":3600}`, ProtectionProfile: "P1", RequiredApprovals: `["file_write","network"]`, NotBefore: demoAt(-32), NotAfter: "2026-08-19T12:00:00Z", LeaseSequence: 7, CPSignature: "sig-demo-capability", IssuedAt: demoAt(-32), Status: "active"}); err != nil {
			return fmt.Errorf("capability lease: %w", err)
		}

		// Policy templates/rules, acknowledgement, exception, and security catalog.
		templates := []models.PolicyTemplate{
			{Base: base("tpl_demo_models", -31), OrganizationID: orgID, TemplateID: "models", Domain: "models", Name: "Model Access", NameEn: "Model Access", Description: "Approved model classes and data boundaries.", ConfigJSON: `{"allowed_classes":["enterprise-code"],"require_attestation":true}`, Version: "2", Enabled: true},
			{Base: base("tpl_demo_tools", -30), OrganizationID: orgID, TemplateID: "tools", Domain: "tools", Name: "Tool Permissions", NameEn: "Tool Permissions", Description: "Tool classes and approval gates.", ConfigJSON: `{"execute":"approval","network":"allowlist"}`, Version: "2", Enabled: true},
			{Base: base("tpl_demo_data", -29), OrganizationID: orgID, TemplateID: "data", Domain: "data", Name: "Data Protection", NameEn: "Data Protection", Description: "Classification and redaction defaults.", ConfigJSON: `{"pii":"block","secrets":"mask"}`, Version: "2", Enabled: true},
			{Base: base("tpl_demo_scm", -28), OrganizationID: orgID, TemplateID: "scm", Domain: "scm", Name: "Git / SCM", NameEn: "Git / SCM", Description: "Branch and change approval rules.", ConfigJSON: `{"protected_branches":["main"],"require_review":true}`, Version: "2", Enabled: true},
			{Base: base("tpl_demo_network", -27), OrganizationID: orgID, TemplateID: "network", Domain: "network", Name: "Network", NameEn: "Network", Description: "Outbound destination allowlist.", ConfigJSON: `{"mode":"allowlist","destinations":["gitlab.example","internal-inference"]}`, Version: "2", Enabled: true},
			{Base: base("tpl_demo_session", -26), OrganizationID: orgID, TemplateID: "session", Domain: "session", Name: "Session Controls", NameEn: "Session Controls", Description: "TTL, acknowledgement, and protected session controls.", ConfigJSON: `{"ttl":28800,"requires_ack":true}`, Version: "2", Enabled: true},
		}
		for i := range templates {
			if err := ensureByID(tx, templates[i].ID, &templates[i]); err != nil {
				return fmt.Errorf("policy template %s: %w", templates[i].ID, err)
			}
		}
		for _, rule := range []models.PolicyRule{
			{Base: base("rule_demo_models", -25), OrganizationID: orgID, Domain: "models", TemplateID: "models", Name: "Approved enterprise models", NameEn: "Approved enterprise models", Description: "Only attested enterprise packages may be used.", Scope: "org", ScopeName: "Patty Enterprise Demo", Enabled: true, Status: "approved", ConfigJSON: `{"allowed_packages":["pmp_demo_kocoder"]}`},
			{Base: base("rule_demo_tools", -24), OrganizationID: orgID, Domain: "tools", TemplateID: "tools", Name: "Approval for execution", NameEn: "Approval for execution", Description: "Execution and network tools require review.", Scope: "org", ScopeName: "Patty Enterprise Demo", Enabled: true, Status: "approved", ConfigJSON: `{"danger_levels":["high","critical"]}`},
			{Base: base("rule_demo_network", -23), OrganizationID: orgID, Domain: "network", TemplateID: "network", Name: "Restricted egress", NameEn: "Restricted egress", Description: "Only named destinations are reachable.", Scope: "org", ScopeName: "Patty Enterprise Demo", Enabled: true, Status: "approved", ConfigJSON: `{"destinations":["gitlab.example","internal-inference"]}`},
		} {
			if err := ensureByID(tx, rule.ID, &rule); err != nil {
				return fmt.Errorf("policy rule %s: %w", rule.ID, err)
			}
		}
		for _, ack := range []models.PolicyAcknowledgement{
			{Base: base("ack_demo_kim", -22), OrganizationID: orgID, EpochID: "epoch_demo_policy_2026", UserID: "usr_demo_kim", AckedAt: demoAt(-20)},
			{Base: base("ack_demo_lee", -21), OrganizationID: orgID, EpochID: "epoch_demo_policy_2026", UserID: "usr_demo_lee", AckedAt: demoAt(-19)},
		} {
			if err := ensureByID(tx, ack.ID, &ack); err != nil {
				return fmt.Errorf("policy acknowledgement %s: %w", ack.ID, err)
			}
		}
		if err := ensureByID(tx, "exception_demo_network", &models.PolicyException{Base: base("exception_demo_network", -20), OrganizationID: orgID, Scope: "project", ScopeID: "prj_demo_payments", ScopeName: "Payment Service", RuleIDsJSON: `["rule_demo_network"]`, Reason: "Temporary access to the settlement provider for the demo change.", RequestedBy: "usr_demo_kim", Status: "pending"}); err != nil {
			return fmt.Errorf("policy exception: %w", err)
		}
		for _, rule := range []models.SecurityRule{
			{Base: base("secrule_demo_pii", -19), OrganizationID: orgID, RuleID: "korean_pii", Type: "korean_pii", Severity: "high", Name: "Korean personal identifiers", NameKo: "한국 개인정보", Enabled: true, Action: "block"},
			{Base: base("secrule_demo_secret", -18), OrganizationID: orgID, RuleID: "secret", Type: "secret", Severity: "critical", Name: "Credential and secret exposure", NameKo: "자격 증명·비밀정보 노출", Enabled: true, Action: "block"},
			{Base: base("secrule_demo_injection", -17), OrganizationID: orgID, RuleID: "prompt_injection", Type: "prompt_injection", Severity: "high", Name: "Prompt injection", NameKo: "프롬프트 인젝션", Enabled: true, Action: "review"},
			{Base: base("secrule_demo_path", -16), OrganizationID: orgID, RuleID: "sensitive_path", Type: "sensitive_path", Severity: "medium", Name: "Sensitive path access", NameKo: "민감 경로 접근", Enabled: true, Action: "review"},
		} {
			if err := ensureByID(tx, rule.ID, &rule); err != nil {
				return fmt.Errorf("security rule %s: %w", rule.ID, err)
			}
		}
		if err := ensureByID(tx, "lexicon_demo_ko_2026", &models.PIILexicon{Base: base("lexicon_demo_ko_2026", -15), OrganizationID: orgID, Version: "2026.08", PatternsJSON: `{"korean_rrn":"\\b[0-9]{6}-[1-4][0-9]{6}\\b","phone":"01[0-9]-[0-9]{3,4}-[0-9]{4}"}`, UpdatedBy: "usr_demo_park", Enabled: true}); err != nil {
			return fmt.Errorf("pii lexicon: %w", err)
		}
		// PAT-1502 PR 1: demo seed must not persist a fake Slack URL.
		// The metadata-only placeholder teaches the UI shape without
		// ever creating a secret-shaped string that could be mistaken
		// for a real credential. Operators wire a real endpoint by
		// rotating the secret through the create form.
		if err := ensureByID(tx, "alert_demo_slack", &models.AlertEndpoint{Base: base("alert_demo_slack", -14), OrganizationID: orgID, Name: "Security Slack (configure me)", Type: "slack", Target: "", SeveritiesJSON: `["high","critical"]`, Enabled: false}); err != nil {
			return fmt.Errorf("alert endpoint: %w", err)
		}

		// Sessions, exchanges, approvals, and the provenance/evidence chain.
		sessions := []models.Session{
			{AuditBase: auditBase("sess_demo_refund", orgID, -13), HarnessID: "hrn_demo_kim", UserID: "usr_demo_kim", ProjectID: "prj_demo_payments", RepositoryID: "repo_demo_payments", Branch: "feature/refund-idempotency", BaselineID: "base_demo_payments_main", SessionID: "sess_demo_refund", PolicyEpochID: "epoch_demo_policy_2026", LeaseID: "lease_demo_capability", TaskPurpose: "Implement idempotent refund authorization", Title: "환불 로직 구현", Status: "active", ModelClass: "patty-code-standard", ProtectionProfile: "P1", SessionTTL: 28800, IdleTTL: 1800, OpenedAt: demoAt(-12), LastActivityAt: demoAt(-1)},
			{AuditBase: auditBase("sess_demo_portal", orgID, -12), HarnessID: "hrn_demo_lee", UserID: "usr_demo_lee", ProjectID: "prj_demo_portal", RepositoryID: "repo_demo_portal", Branch: "main", BaselineID: "base_demo_portal_main", SessionID: "sess_demo_portal", PolicyEpochID: "epoch_demo_policy_2026", LeaseID: "lease_demo_endpoint_1", TaskPurpose: "Review account recovery change", Title: "계정 복구 검토", Status: "paused", ModelClass: "patty-code-standard", ProtectionProfile: "P0", SessionTTL: 14400, IdleTTL: 1200, OpenedAt: demoAt(-20), LastActivityAt: demoAt(-4)},
			{AuditBase: auditBase("sess_demo_security", orgID, -11), HarnessID: "hrn_demo_park", UserID: "usr_demo_park", ProjectID: "prj_demo_platform", RepositoryID: "repo_demo_platform", Branch: "main", BaselineID: "base_demo_platform_main", SessionID: "sess_demo_security", PolicyEpochID: "epoch_demo_policy_2026", LeaseID: "lease_demo_endpoint_1", TaskPurpose: "Investigate egress policy finding", Title: "보안 정책 분석", Status: "closed", ModelClass: "patty-code-standard", ProtectionProfile: "P1", SessionTTL: 28800, IdleTTL: 1800, OpenedAt: demoAt(-48), ClosedAt: demoAt(-36), LastActivityAt: demoAt(-36)},
		}
		for i := range sessions {
			if err := ensureByID(tx, sessions[i].ID, &sessions[i]); err != nil {
				return fmt.Errorf("session %s: %w", sessions[i].ID, err)
			}
		}
		if err := ensureByID(tx, "exchange_demo_refund_1", &models.PromptExchange{Base: base("exchange_demo_refund_1", -10), SessionID: "sess_demo_refund", ExchangeID: "exchange_demo_refund_1", PromptText: "Make the refund operation idempotent and preserve the approval trail.", ResponseText: "I will add an idempotency key, approval check, and provenance span.", ModelPackageID: modelPackage.PackageID, EndpointID: endpoint.EndpointID, InputTokens: 438, OutputTokens: 612, LatencyMs: 842, VerdictResult: "allow_with_approval", PolicyEpochID: "epoch_demo_policy_2026", Status: "completed", CreatedAt2: demoAt(-2)}); err != nil {
			return fmt.Errorf("prompt exchange: %w", err)
		}
		if err := ensureByID(tx, "approval_demo_refund", &models.Approval{Base: base("approval_demo_refund", -9), OrganizationID: orgID, ExchangeID: "exchange_demo_refund_1", SessionID: "sess_demo_refund", ActionID: "action_demo_refund_write", ApprovalType: "file_write", RequestedBy: "usr_demo_kim", ReviewerID: "usr_demo_park", Decision: "approved", DecisionReason: "Change is scoped to the protected refund module.", Conditions: `{"paths":["src/refunds/**"],"reviewers":1}`, DecidedAt: demoAt(-2), DecidedBy: "usr_demo_park", ExpiresAt: "2026-08-19T12:00:00Z"}); err != nil {
			return fmt.Errorf("approval: %w", err)
		}
		if err := ensureByID(tx, "action_demo_refund_write", &models.ActionEnvelope{Base: base("action_demo_refund_write", -8), OrganizationID: orgID, ActionID: "action_demo_refund_write", SessionID: "sess_demo_refund", ExchangeID: "exchange_demo_refund_1", UserID: "usr_demo_kim", HarnessID: "hrn_demo_kim", ModelPackageID: modelPackage.PackageID, EndpointID: endpoint.EndpointID, ProjectID: "prj_demo_payments", RepositoryID: "repo_demo_payments", Branch: "feature/refund-idempotency", PolicyEpochID: "epoch_demo_policy_2026", LeaseID: "lease_demo_capability", ActionType: "file_write", ActionPayload: `{"files":["src/refunds/idempotency.go"],"lines_added":42}`, VerdictResult: "allow_with_approval", Classification: "confidential", EnvelopeDigest: "sha256:envelope-demo-refund", CPSignature: "sig-demo-action", OccurredAt: demoAt(-2)}); err != nil {
			return fmt.Errorf("action envelope: %w", err)
		}
		if err := ensureByID(tx, "changeset_demo_refund", &models.ChangeSet{Base: base("changeset_demo_refund", -7), OrganizationID: orgID, SessionID: "sess_demo_refund", ExchangeID: "exchange_demo_refund_1", RepositoryID: "repo_demo_payments", Branch: "feature/refund-idempotency", BaselineID: "base_demo_payments_main", UserID: "usr_demo_kim", HarnessID: "hrn_demo_kim", ModelPackageID: modelPackage.PackageID, EndpointID: endpoint.EndpointID, FilesChanged: `["src/refunds/idempotency.go","src/refunds/idempotency_test.go"]`, DiffSummary: "Add idempotency key handling and approval guard.", DiffDigest: "sha256:diff-demo-refund", LinesAdded: 42, LinesRemoved: 7, AttributionState: "AI_THEN_HUMAN_EDITED", Confidence: 0.96, ChangeSetDigest: "sha256:changeset-demo-refund", Status: "committed"}); err != nil {
			return fmt.Errorf("changeset: %w", err)
		}
		for _, span := range []models.ProvenanceSpan{
			{Base: base("span_demo_refund_code", -6), OrganizationID: orgID, RepositoryID: "repo_demo_payments", ChangeSetID: "changeset_demo_refund", FilePath: "src/refunds/idempotency.go", CommitSHA: "a1b2c3d4e5f6", SymbolLang: "go", SymbolName: "refunds.AuthorizeRefund", StartLine: 18, EndLine: 59, ASTFingerprint: "sha256:ast-refund", SemanticFingerprint: "sha256:semantic-refund", AttributionState: "AI_THEN_HUMAN_EDITED", Confidence: 0.96, SessionID: "sess_demo_refund", UserID: "usr_demo_kim", HarnessID: "hrn_demo_kim", ModelPackageID: modelPackage.PackageID, EndpointID: endpoint.EndpointID, ContextRefsJSON: `["exchange_demo_refund_1"]`, ToolCallRefsJSON: `["action_demo_refund_write"]`, PolicyDecisionRefsJSON: `["epoch_demo_policy_2026"]`, SpanDigest: "sha256:span-demo-refund-code"},
			{Base: base("span_demo_refund_test", -5), OrganizationID: orgID, RepositoryID: "repo_demo_payments", ChangeSetID: "changeset_demo_refund", FilePath: "src/refunds/idempotency_test.go", CommitSHA: "a1b2c3d4e5f6", SymbolLang: "go", SymbolName: "refunds.TestAuthorizeRefundIdempotency", StartLine: 11, EndLine: 47, ASTFingerprint: "sha256:ast-refund-test", SemanticFingerprint: "sha256:semantic-refund-test", AttributionState: "AI_GENERATED", Confidence: 0.91, SessionID: "sess_demo_refund", UserID: "usr_demo_kim", HarnessID: "hrn_demo_kim", ModelPackageID: modelPackage.PackageID, EndpointID: endpoint.EndpointID, ContextRefsJSON: `["exchange_demo_refund_1"]`, SpanDigest: "sha256:span-demo-refund-test"},
		} {
			if err := ensureByID(tx, span.ID, &span); err != nil {
				return fmt.Errorf("provenance span %s: %w", span.ID, err)
			}
		}
		if err := ensureByID(tx, "binding_demo_refund", &models.CommitBinding{Base: base("binding_demo_refund", -4), OrganizationID: orgID, RepositoryID: "repo_demo_payments", CommitSHA: "a1b2c3d4e5f6", ChangeSetID: "changeset_demo_refund", SessionID: "sess_demo_refund", Branch: "feature/refund-idempotency", BoundAt: demoAt(-2), BindingDigest: "sha256:binding-demo-refund"}); err != nil {
			return fmt.Errorf("commit binding: %w", err)
		}
		if err := ensureByID(tx, "receipt_demo_refund", &models.EvidenceReceipt{Base: base("receipt_demo_refund", -3), OrganizationID: orgID, ExchangeID: "exchange_demo_refund_1", SessionID: "sess_demo_refund", FinalState: "completed", FirstEventSeq: 1, LastEventSeq: 5, ChainRoot: "sha256:audit-demo-root", ProvenanceRoot: "sha256:provenance-demo-root", PolicyEpochID: "epoch_demo_policy_2026", LeaseDigest: "sha256:lease-demo-capability", RelayIdentity: "relay-demo-seoul", ModelPackageID: modelPackage.PackageID, EndpointID: endpoint.EndpointID, KeyAlgorithm: "ed25519", Signature: "cose-demo-receipt", RedactionManifest: `{"redacted":["prompt.secret"]}`, IssuedAt: demoAt(-2), AcknowledgedAt: demoAt(-1)}); err != nil {
			return fmt.Errorf("evidence receipt: %w", err)
		}
		if err := ensureByID(tx, "cr_demo_refund", &models.ChangeRequest{Base: base("cr_demo_refund", -2), OrganizationID: orgID, ProjectID: "prj_demo_payments", RepositoryID: "repo_demo_payments", ChangeSetID: "changeset_demo_refund", SessionID: "sess_demo_refund", Title: "Approve refund idempotency patch", Kind: "ai_code_change", RiskLevel: "medium", RiskScore: 0.42, Status: "approved", RequestedBy: "usr_demo_kim", DecidedBy: "usr_demo_park", DecisionReason: "Evidence receipt and protected-path approval are present.", DecidedAt: demoAt(-1)}); err != nil {
			return fmt.Errorf("change request: %w", err)
		}

		// Security findings, usage, compliance, communications, sandboxes, tools,
		// and enterprise feature reports complete the visible graph.
		for _, finding := range []models.SecurityFinding{
			{Base: base("finding_demo_pii", -1), OrganizationID: orgID, SessionID: "sess_demo_portal", ExchangeID: "exchange_demo_portal_1", FindingType: "pii_leak", Severity: "high", Title: "Resident registration number in prompt", TitleKo: "프롬프트에서 주민등록번호 감지", Description: "A Korean personal identifier was detected in the account-recovery context.", DescriptionKo: "계정 복구 컨텍스트에서 한국 개인 식별자가 감지되었습니다.", EvidenceJSON: `{"location":"request.context[3]","masked":"900101-*******"}`, RuleID: "korean_pii", Status: "investigating", ContainsAction: "review", Direction: "request", OccurredAt: demoAt(-3)},
			{Base: base("finding_demo_secret", -2), OrganizationID: orgID, SessionID: "sess_demo_security", ExchangeID: "exchange_demo_security_1", FindingType: "secret_exposure", Severity: "critical", Title: "Cloud credential in tool output", TitleKo: "도구 출력에서 클라우드 자격 증명 노출", Description: "A credential-shaped value was returned by a diagnostic command.", DescriptionKo: "진단 명령 결과에서 자격 증명 형태의 값이 반환되었습니다.", EvidenceJSON: `{"tool":"shell.read","path":"config/local.env","masked":"AKIA********"}`, RuleID: "secret", Status: "open", ContainsAction: "quarantine", Direction: "response", OccurredAt: demoAt(-4)},
			{Base: base("finding_demo_injection", -3), OrganizationID: orgID, SessionID: "sess_demo_refund", ExchangeID: "exchange_demo_refund_1", FindingType: "prompt_injection", Severity: "medium", Title: "Untrusted instruction in repository comment", TitleKo: "저장소 주석의 신뢰할 수 없는 지시", Description: "A repository comment attempted to override the governing task policy.", DescriptionKo: "저장소 주석이 작업 정책을 덮어쓰려 했습니다.", EvidenceJSON: `{"file":"README.md","line":88}`, RuleID: "prompt_injection", Status: "resolved", ContainsAction: "review", Direction: "request", OccurredAt: demoAt(-5)},
		} {
			if err := ensureByID(tx, finding.ID, &finding); err != nil {
				return fmt.Errorf("security finding %s: %w", finding.ID, err)
			}
		}
		for _, usage := range []models.UsageRecord{
			{Base: base("usage_demo_refund_in", -2), OrganizationID: orgID, UserID: "usr_demo_kim", HarnessID: "hrn_demo_kim", SessionID: "sess_demo_refund", ModelPackageID: modelPackage.PackageID, EndpointID: endpoint.EndpointID, MetricType: "tokens_in", Quantity: 438, Unit: "tokens", CostMicros: 350, Currency: "KRW", OccurredAt: demoAt(-2)},
			{Base: base("usage_demo_refund_out", -1), OrganizationID: orgID, UserID: "usr_demo_kim", HarnessID: "hrn_demo_kim", SessionID: "sess_demo_refund", ModelPackageID: modelPackage.PackageID, EndpointID: endpoint.EndpointID, MetricType: "tokens_out", Quantity: 612, Unit: "tokens", CostMicros: 1468, Currency: "KRW", OccurredAt: demoAt(-2)},
			{Base: base("usage_demo_gpu", -3), OrganizationID: orgID, UserID: "usr_demo_lee", HarnessID: "hrn_demo_lee", SessionID: "sess_demo_portal", ModelPackageID: modelPackage.PackageID, EndpointID: endpoint.EndpointID, MetricType: "gpu_seconds", Quantity: 187, Unit: "seconds", CostMicros: 18700, Currency: "KRW", OccurredAt: demoAt(-4)},
			{Base: base("usage_demo_storage", -4), OrganizationID: orgID, UserID: "usr_demo_park", HarnessID: "hrn_demo_park", SessionID: "sess_demo_security", MetricType: "storage_bytes", Quantity: 5242880, Unit: "bytes", CostMicros: 120, Currency: "KRW", OccurredAt: demoAt(-5)},
		} {
			if err := ensureByID(tx, usage.ID, &usage); err != nil {
				return fmt.Errorf("usage record %s: %w", usage.ID, err)
			}
		}
		for _, evidence := range []models.ComplianceEvidence{
			{Base: base("evidence_demo_csap_access", -8), OrganizationID: orgID, Certification: "CSAP", ControlID: "2.1.3", Title: "Access control and least privilege", Description: "Role, harness, and capability lease records provide traceable access evidence.", Source: "audit", Reference: "/api/audit?resource_type=capability_lease", CollectedAt: demoAt(-8)},
			{Base: base("evidence_demo_ismp_prov", -7), OrganizationID: orgID, Certification: "ISMS-P", ControlID: "2.10.4", Title: "AI change provenance", Description: "Changesets, spans, commit bindings and receipts are retained for review.", Source: "provenance", Reference: "/api/repositories/repo_demo_payments/provenance", CollectedAt: demoAt(-7)},
			{Base: base("evidence_demo_csap_sandbox", -6), OrganizationID: orgID, Certification: "CSAP", ControlID: "3.4.2", Title: "Execution isolation", Description: "Sandbox definitions capture mode, image digest, limits and egress policy.", Source: "attestation", Reference: "/api/sandboxes", CollectedAt: demoAt(-6)},
		} {
			if err := ensureByID(tx, evidence.ID, &evidence); err != nil {
				return fmt.Errorf("compliance evidence %s: %w", evidence.ID, err)
			}
		}
		for _, remediation := range []models.ComplianceRemediation{
			{Base: base("remediation_demo_csap_1", -5), OrganizationID: orgID, Certification: "CSAP", ControlID: "2.4.1", Owner: "박서연 · Security", DueDate: "2026-09-05", SLA: "30d", Status: "in_progress", Notes: "Complete quarterly harness posture attestation evidence."},
			{Base: base("remediation_demo_ismp_1", -4), OrganizationID: orgID, Certification: "ISMS-P", ControlID: "2.11.2", Owner: "이지수 · QA", DueDate: "2026-09-18", SLA: "30d", Status: "open", Notes: "Attach the approved recovery-flow test report."},
		} {
			if err := ensureByID(tx, remediation.ID, &remediation); err != nil {
				return fmt.Errorf("compliance remediation %s: %w", remediation.ID, err)
			}
		}
		if err := ensureByID(tx, "assessment_demo_csap", &models.ComplianceAssessmentRecord{Base: base("assessment_demo_csap", -3), OrganizationID: orgID, Certification: "CSAP", Scope: "SaaS", Level: "일반", AssessedAt: DemoNow, OverallStatus: "partially_compliant", OpenGaps: 2, ResultsJSON: `{"passed":18,"partial":2,"failed":0}`}); err != nil {
			return fmt.Errorf("compliance assessment: %w", err)
		}

		conversation := &models.Conversation{AuditBase: auditBase("conv_demo_engineering", orgID, -3), Type: "channel", Title: "Engineering Controls", TitleKo: "엔지니어링 제어 채널", ProjectID: "prj_demo_payments", ParticipantsJSON: `["usr_demo_kim","usr_demo_lee","usr_demo_park"]`, EncryptionKeyRef: "kms://demo/conversations/engineering", LastMessageAt: demoAt(-1), Status: "active"}
		if err := ensureByID(tx, conversation.ID, conversation); err != nil {
			return fmt.Errorf("conversation: %w", err)
		}
		for _, message := range []models.Message{
			{Base: base("msg_demo_engineering_1", -2), ConversationID: conversation.ID, SenderID: "usr_demo_park", SenderType: "user", ContentType: "text", Content: "환불 변경 사항은 승인 로그와 증거 영수증까지 확인했습니다.", LinkedSessionID: "sess_demo_refund", LinkedExchangeID: "exchange_demo_refund_1", DeliveredAt: demoAt(-2), ReadByJSON: `["usr_demo_kim","usr_demo_lee"]`},
			{Base: base("msg_demo_engineering_2", -1), ConversationID: conversation.ID, SenderID: "usr_demo_kim", SenderType: "user", ContentType: "text", Content: "확인했습니다. 보호된 브랜치에 반영하겠습니다.", LinkedSessionID: "sess_demo_refund", LinkedExchangeID: "exchange_demo_refund_1", DeliveredAt: demoAt(-1), ReadByJSON: `["usr_demo_park"]`},
		} {
			if err := ensureByID(tx, message.ID, &message); err != nil {
				return fmt.Errorf("message %s: %w", message.ID, err)
			}
		}
		for _, presence := range []models.Presence{
			{Base: base("presence_demo_kim", -1), OrganizationID: orgID, UserID: "usr_demo_kim", Status: "online", Activity: "payment-service에서 작업 중", HarnessID: "hrn_demo_kim", LastActiveAt: demoAt(-1)},
			{Base: base("presence_demo_lee", -2), OrganizationID: orgID, UserID: "usr_demo_lee", Status: "away", Activity: "계정 복구 검토", HarnessID: "hrn_demo_lee", LastActiveAt: demoAt(-4)},
		} {
			if err := ensureByID(tx, presence.ID, &presence); err != nil {
				return fmt.Errorf("presence %s: %w", presence.ID, err)
			}
		}
		if err := ensureByID(tx, "file_demo_recovery_report", &models.FileTransfer{AuditBase: auditBase("file_demo_recovery_report", orgID, -2), ConversationID: conversation.ID, SessionID: "sess_demo_portal", SenderID: "usr_demo_lee", RecipientID: "usr_demo_park", FileName: "account-recovery-review.pdf", FileSize: 184320, FileType: "application/pdf", FileHash: "sha256:file-demo-recovery", StoragePath: "s3://demo/evidence/account-recovery-review.pdf", EncryptionKeyRef: "kms://demo/files/recovery", ScanStatus: "clean", Classification: "confidential", Status: "ready", ExpiresAt: "2026-09-18T00:00:00Z"}); err != nil {
			return fmt.Errorf("file transfer: %w", err)
		}
		if err := ensureByID(tx, "broadcast_demo_maintenance", &models.Broadcast{AuditBase: auditBase("broadcast_demo_maintenance", orgID, -1), Severity: "warning", Title: "Scheduled policy maintenance", TitleKo: "정책 유지보수 예정", Body: "Policy epoch 43 will be available after the review window.", BodyKo: "검토 기간 이후 정책 epoch 43이 제공됩니다.", TargetType: "org", TargetID: orgID, TargetOrgsJSON: fmt.Sprintf(`["%s"]`, orgID), RequiresAck: true, Dismissable: true, ExpiresAt: "2026-08-20T00:00:00Z", Status: "active", SentBy: "usr_demo_park", AckCount: 2, AcksJSON: `["usr_demo_kim","usr_demo_lee"]`}); err != nil {
			return fmt.Errorf("broadcast: %w", err)
		}

		for _, sandbox := range []models.SandboxRecord{
			{Base: base("sandbox_demo_refund", -2), OrganizationID: orgID, RepositoryID: "repo_demo_payments", SessionID: "sess_demo_refund", UserID: "usr_demo_kim", Mode: "remote_sandbox", BaseImage: "patty/sandbox-base:2026.08", ImageDigest: "sha256:sandbox-demo-202608", CPULimit: "2", MemoryLimitMB: 4096, NetworkPolicy: "allowlist", Status: "running", RuntimeProvider: "eks/patty-demo", ResourceLimitsJSON: `{"pids":256,"disk_mb":20480}`},
			{Base: base("sandbox_demo_security", -1), OrganizationID: orgID, RepositoryID: "repo_demo_platform", SessionID: "sess_demo_security", UserID: "usr_demo_park", Mode: "review_only", BaseImage: "patty/sandbox-base:2026.08", ImageDigest: "sha256:sandbox-demo-202608", CPULimit: "1", MemoryLimitMB: 2048, NetworkPolicy: "blocked", Status: "defined", RuntimeProvider: "none (review-only definition)", ResourceLimitsJSON: `{"pids":128,"disk_mb":10240}`},
		} {
			if err := ensureByID(tx, sandbox.ID, &sandbox); err != nil {
				return fmt.Errorf("sandbox %s: %w", sandbox.ID, err)
			}
		}
		if err := ensureByID(tx, "setting_demo_sandbox_allowlist", &models.OrgSetting{Base: base("setting_demo_sandbox_allowlist", -1), OrganizationID: orgID, Key: "sandbox.image_allowlist", Value: `["patty/sandbox-base:2026.08","patty/sandbox-base:*"]`}); err != nil {
			return fmt.Errorf("sandbox setting: %w", err)
		}

		for _, tool := range []models.Tool{
			{AuditBase: auditBase("tool_demo_shell_read", orgID, -10), Name: "shell.read", NameKo: "셸 읽기", Category: "read", ToolClass: "read", Signature: "sha256:tool-shell-read", AllowedByDefault: true, RequiresApproval: false, DangerLevel: "low", Status: "active"},
			{AuditBase: auditBase("tool_demo_shell_write", orgID, -9), Name: "shell.write", NameKo: "셸 쓰기", Category: "write", ToolClass: "write", Signature: "sha256:tool-shell-write", AllowedByDefault: false, RequiresApproval: true, DangerLevel: "high", Status: "active"},
			{AuditBase: auditBase("tool_demo_git_commit", orgID, -8), Name: "git.commit", NameKo: "Git 커밋", Category: "write", ToolClass: "scm", Signature: "sha256:tool-git-commit", AllowedByDefault: false, RequiresApproval: true, DangerLevel: "high", Status: "active"},
			{AuditBase: auditBase("tool_demo_http", orgID, -7), Name: "http.request", NameKo: "HTTP 요청", Category: "network", ToolClass: "network", Signature: "sha256:tool-http", AllowedByDefault: false, RequiresApproval: true, DangerLevel: "critical", Status: "active"},
			{AuditBase: auditBase("tool_demo_browser", orgID, -6), Name: "browser.snapshot", NameKo: "브라우저 스냅샷", Category: "read", ToolClass: "browser", Signature: "sha256:tool-browser", AllowedByDefault: false, RequiresApproval: true, DangerLevel: "medium", Status: "active"},
		} {
			if err := ensureByID(tx, tool.ID, &tool); err != nil {
				return fmt.Errorf("tool %s: %w", tool.ID, err)
			}
		}
		for _, allow := range []models.ProjectToolAllowlist{
			{Base: base("allow_demo_payments_read", -5), OrganizationID: orgID, ProjectID: "prj_demo_payments", ToolName: "shell.read", GrantedBy: "usr_demo_park"},
			{Base: base("allow_demo_payments_git", -4), OrganizationID: orgID, ProjectID: "prj_demo_payments", ToolName: "git.commit", GrantedBy: "usr_demo_park"},
			{Base: base("allow_demo_portal_browser", -3), OrganizationID: orgID, ProjectID: "prj_demo_portal", ToolName: "browser.snapshot", GrantedBy: "usr_demo_park"},
		} {
			if err := ensureByID(tx, allow.ID, &allow); err != nil {
				return fmt.Errorf("tool allowlist %s: %w", allow.ID, err)
			}
		}

		for _, feature := range []models.EnterpriseHarnessFeature{
			{Base: base("feature_demo_provenance", -5), OrganizationID: orgID, HarnessID: "hrn_demo_kim", FeatureKey: "ai_attribution", FeatureName: "AI Code Attribution", FeatureNameKo: "AI 코드 기여 추적", Category: "audit", PRDRef: "§19", Enabled: true, Enforced: true, Status: "active", LastReportedAt: demoAt(-1), LastValue: `{"spans":true,"receipts":true}`, Config: `{"retention":"365d"}`},
			{Base: base("feature_demo_sandbox", -4), OrganizationID: orgID, HarnessID: "hrn_demo_kim", FeatureKey: "sandbox_execution", FeatureName: "Mandatory Sandbox Execution", FeatureNameKo: "의무 샌드박스 실행", Category: "security", PRDRef: "§31.2", Enabled: true, Enforced: true, Status: "active", LastReportedAt: demoAt(-1), LastValue: `{"mode":"remote_sandbox"}`, Config: `{"network":"allowlist"}`},
			{Base: base("feature_demo_ack", -3), OrganizationID: orgID, HarnessID: "hrn_demo_lee", FeatureKey: "mandatory_ack", FeatureName: "Mandatory Policy Acknowledgement", FeatureNameKo: "의무 정책 확인", Category: "governance", PRDRef: "§33.6", Enabled: true, Enforced: true, Status: "active", LastReportedAt: demoAt(-3), LastValue: `{"epoch":"epoch_demo_policy_2026","acked":true}`, Config: `{"epoch":"epoch_demo_policy_2026"}`},
		} {
			if err := ensureByID(tx, feature.ID, &feature); err != nil {
				return fmt.Errorf("enterprise feature %s: %w", feature.ID, err)
			}
		}
		if err := ensureByID(tx, "violation_demo_network", &models.EnterpriseFeatureViolation{Base: base("violation_demo_network", -2), OrganizationID: orgID, HarnessID: "hrn_demo_park", SessionID: "sess_demo_security", FeatureKey: "network_egress", Severity: "high", Description: "A diagnostic request targeted a destination outside the approved allowlist.", DescriptionKo: "진단 요청이 승인된 허용 목록 외부의 목적지를 대상으로 했습니다.", Resolved: false, OccurredAt: demoAt(-4)}); err != nil {
			return fmt.Errorf("enterprise violation: %w", err)
		}

		// A short fixed audit chain makes the dashboard and audit detail useful
		// without fabricating runtime activity on every server start.
		for _, event := range []models.AuditEvent{
			{Base: base("audit_demo_login", -5), OrganizationID: orgID, EventType: "identity.login", ActorID: "usr_demo_kim", ActorType: "user", Action: "login", ResourceType: "harness", ResourceID: "hrn_demo_kim", Details: `{"method":"oidc","mfa":true}`, IPAddress: "10.20.1.21", UserAgent: "Patty Harness/1.8.0", Result: "success", OccurredAt: demoAt(-5)},
			{Base: base("audit_demo_policy", -4), OrganizationID: orgID, EventType: "policy.epoch.acknowledged", ActorID: "usr_demo_lee", ActorType: "user", Action: "acknowledge", ResourceType: "policy_epoch", ResourceID: "epoch_demo_policy_2026", Details: `{"epoch":42}`, Result: "success", OccurredAt: demoAt(-4)},
			{Base: base("audit_demo_change", -3), OrganizationID: orgID, EventType: "provenance.changeset.committed", ActorID: "usr_demo_kim", ActorType: "user", Action: "commit", ResourceType: "changeset", ResourceID: "changeset_demo_refund", Details: `{"files":2,"lines_added":42,"approval":"approval_demo_refund"}`, Result: "success", OccurredAt: demoAt(-3)},
			{Base: base("audit_demo_finding", -2), OrganizationID: orgID, EventType: "security.finding.opened", ActorID: "system-security", ActorType: "system", Action: "quarantine", ResourceType: "security_finding", ResourceID: "finding_demo_secret", Details: `{"severity":"critical","action":"quarantine"}`, Result: "success", OccurredAt: demoAt(-2)},
			{Base: base("audit_demo_violation", -1), OrganizationID: orgID, EventType: "enterprise.feature.violation", ActorID: "hrn_demo_park", ActorType: "harness", Action: "record", ResourceType: "enterprise_feature_violation", ResourceID: "violation_demo_network", Details: `{"feature":"network_egress","resolved":false}`, Result: "success", OccurredAt: demoAt(-1)},
		} {
			if err := ensureByID(tx, event.ID, &event); err != nil {
				return fmt.Errorf("audit event %s: %w", event.ID, err)
			}
		}

		return nil
	})
}

func ptrString(value string) *string { return &value }
