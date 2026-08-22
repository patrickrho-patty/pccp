package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func lifecycleTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "identity-lifecycle.db")), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(append(models.AllModels(), &AdminCredentials{})...); err != nil {
		t.Fatal(err)
	}
	service, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return service, db
}

func TestGenerateEnrollmentCodeRequiresActiveTenantUser(t *testing.T) {
	service, db := lifecycleTestService(t)
	org := models.Organization{Name: "Enrollment", Slug: "enrollment-active", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "suspended@corp.kr", Name: "Suspended", Status: models.UserStatusSuspended}
	db.Create(&user)

	if _, err := service.GenerateEnrollmentCode(org.ID, user.ID, time.Hour); err == nil {
		t.Fatal("suspended user received an enrollment code")
	}
	if _, err := service.GenerateEnrollmentCode("other-org", user.ID, time.Hour); err == nil {
		t.Fatal("cross-tenant user received an enrollment code")
	}
	var count int64
	db.Model(&models.EnrollmentCode{}).Where("user_id = ?", user.ID).Count(&count)
	if count != 0 {
		t.Fatalf("rejected enrollment persisted %d codes", count)
	}
}

func TestSuspendedUserCannotEnrollHarnessOrOpenSession(t *testing.T) {
	service, db := lifecycleTestService(t)
	org := models.Organization{Name: "Standing", Slug: "standing-gate", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "blocked@corp.kr", Name: "Blocked", Status: models.UserStatusSuspended}
	db.Create(&user)
	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := service.EnrollHarness(EnrollHarnessRequest{
		OrganizationID: org.ID,
		UserID:         user.ID,
		HarnessID:      "blocked-harness",
		PublicKeyHex:   hex.EncodeToString(publicKey),
	}); err == nil {
		t.Fatal("suspended user enrolled a harness")
	}
	if _, err := service.OpenSession(org.ID, "blocked-harness", user.ID, "", "", "", "", "", "", ""); err == nil {
		t.Fatal("suspended user opened a session")
	}
	var harnesses, sessions int64
	db.Model(&models.Harness{}).Where("organization_id = ?", org.ID).Count(&harnesses)
	db.Model(&models.Session{}).Where("organization_id = ?", org.ID).Count(&sessions)
	if harnesses != 0 || sessions != 0 {
		t.Fatalf("denied standing checks persisted harnesses=%d sessions=%d", harnesses, sessions)
	}
}

func TestOpenProtocolSessionWithDBPersistsRequestedAuthorityBinding(t *testing.T) {
	service, db := lifecycleTestService(t)
	org := models.Organization{Name: "Protocol", Slug: "protocol-session", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "protocol@corp.kr", Name: "Protocol", Status: models.UserStatusActive}
	db.Create(&user)
	harness := models.Harness{OrganizationID: org.ID, HarnessID: "hrn-protocol", Status: "enrolled", AllowedUsers: `["` + user.ID + `"]`}
	db.Create(&harness)

	var opened *models.Session
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		opened, err = service.OpenProtocolSessionWithDB(tx, org.ID, harness.HarnessID, user.ID,
			"sess-from-wire", "repo-1", "feature/protocol", "model-1")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened.SessionID != "sess-from-wire" || opened.HarnessID != harness.HarnessID || opened.UserID != user.ID || opened.Status != "active" {
		t.Fatalf("unexpected protocol session binding: %+v", opened)
	}

	var persisted models.Session
	if err := db.Where("organization_id = ? AND session_id = ?", org.ID, "sess-from-wire").First(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.RepositoryID != "repo-1" || persisted.Branch != "feature/protocol" || persisted.ModelClass != "model-1" {
		t.Fatalf("protocol metadata not persisted: %+v", persisted)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		refreshed, err := service.OpenProtocolSessionWithDB(tx, org.ID, harness.HarnessID, user.ID,
			"sess-from-wire", "repo-1", "feature/protocol", "model-1")
		if err == nil && refreshed.ID != opened.ID {
			t.Fatalf("refresh created a second session: %s != %s", refreshed.ID, opened.ID)
		}
		return err
	}); err != nil {
		t.Fatalf("matching wire session refresh failed: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := service.OpenProtocolSessionWithDB(tx, org.ID, "other-harness", user.ID,
			"sess-from-wire", "repo-1", "feature/protocol", "model-1")
		return err
	}); err == nil {
		t.Fatal("wire session ID was rebound to another harness")
	}
	if err := db.Model(&models.Session{}).Where("id = ?", opened.ID).Update("status", "closed").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := service.OpenProtocolSessionWithDB(tx, org.ID, harness.HarnessID, user.ID,
			"sess-from-wire", "repo-1", "feature/protocol", "model-1")
		return err
	}); err == nil {
		t.Fatal("terminal wire session was reopened")
	}
}

func TestExpiredContractorSweepCannotResurrectConcurrentOffboard(t *testing.T) {
	service, db := lifecycleTestService(t)
	org := models.Organization{Name: "Contractors", Slug: "contractor-race", Status: "active"}
	db.Create(&org)
	user := models.User{
		AuditBase: models.AuditBase{OrganizationID: org.ID},
		Email:     "contractor@corp.kr", Name: "Contractor", Status: models.UserStatusActive,
		ContractorInfo: `{"contract_end":"2020-01-01"}`,
	}
	db.Create(&user)

	var once sync.Once
	callbackName := "pat1489_offboard_before_expiry_lock"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			once.Do(func() {
				if err := db.Exec("UPDATE users SET status = ? WHERE id = ?", models.UserStatusOffboarded, user.ID).Error; err != nil {
					tx.AddError(err)
				}
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	if changed := service.SweepExpiredContractors(); changed != 0 {
		t.Fatalf("expiry sweep counted %d transitions after losing CAS", changed)
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", user.ID)
	if reloaded.Status != models.UserStatusOffboarded {
		t.Fatalf("expiry sweep resurrected user as %s", reloaded.Status)
	}
	var auditCount int64
	db.Model(&models.AuditEvent{}).Where("resource_id = ? AND event_type = ?", user.ID, "cp.user.contract_expired").Count(&auditCount)
	if auditCount != 0 {
		t.Fatalf("lost expiry race emitted %d success audits", auditCount)
	}
}

func TestExpiredContractorSweepOffboardsAndRevokesRuntimeAccess(t *testing.T) {
	service, db := lifecycleTestService(t)
	org := models.Organization{Name: "Contractors", Slug: "contractor-end", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "ended@corp.kr", Name: "Ended", Status: models.UserStatusActive, ContractorInfo: `{"contract_end":"2020-01-01"}`}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, UserID: user.ID, SessionID: "contract-session", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if changed := service.SweepExpiredContractors(); changed != 1 {
		t.Fatalf("expired contractor transitions = %d, want 1", changed)
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", user.ID)
	if reloaded.Status != models.UserStatusOffboarded {
		t.Fatalf("expired contractor status = %s, want offboarded", reloaded.Status)
	}
	var session models.Session
	db.First(&session, "session_id = ?", "contract-session")
	if session.Status != "terminated" {
		t.Fatalf("expired contractor session = %s, want terminated", session.Status)
	}
}

func TestIdempotentOffboardReconcilesResidualAccess(t *testing.T) {
	_, db := lifecycleTestService(t)
	org := models.Organization{Name: "Reconcile", Slug: "reconcile", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "gone@corp.kr", Name: "Gone", Status: models.UserStatusOffboarded}
	db.Create(&user)
	sess := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, UserID: user.ID, HarnessID: "peer-late", SessionID: "late-session", Status: "unexpected-live-state"}
	db.Create(&sess)
	member := models.ProjectMember{OrganizationID: org.ID, ProjectID: "project-late", UserID: user.ID, Role: "member"}
	db.Create(&member)

	result, err := TransitionUserLifecycle(db, UserLifecycleMutation{
		OrganizationID: org.ID, UserID: user.ID, To: models.UserStatusOffboarded,
		Reason: "SCIM retry", ActorID: "scim", ActorType: "system", Idempotent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ClosedSessions != 1 || result.RevokedProjectMemberships != 1 || result.RemainingAccess != 0 {
		t.Fatalf("idempotent reconciliation incomplete: %+v", result)
	}
	var reconciliationAudits int64
	db.Model(&models.AuditEvent{}).
		Where("organization_id = ? AND resource_id = ? AND event_type = ?", org.ID, user.ID, "cp.user.offboarding_reconciled").
		Count(&reconciliationAudits)
	if reconciliationAudits != 1 {
		t.Fatalf("residual access cleanup emitted %d reconciliation audits", reconciliationAudits)
	}
	if _, err := TransitionUserLifecycle(db, UserLifecycleMutation{
		OrganizationID: org.ID, UserID: user.ID, To: models.UserStatusOffboarded,
		Reason: "SCIM retry", ActorID: "scim", ActorType: "system", Idempotent: true,
	}); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.AuditEvent{}).
		Where("organization_id = ? AND resource_id = ? AND event_type = ?", org.ID, user.ID, "cp.user.offboarding_reconciled").
		Count(&reconciliationAudits)
	if reconciliationAudits != 1 {
		t.Fatalf("no-op retry duplicated reconciliation audit: %d", reconciliationAudits)
	}
	db.First(&sess, "id = ?", sess.ID)
	if sess.Status != "terminated" {
		t.Fatalf("residual session stayed %q", sess.Status)
	}
}

func TestSuspendRevokesRuntimeAuthorityButPreservesDurableRelationships(t *testing.T) {
	_, db := lifecycleTestService(t)
	org := models.Organization{Name: "Suspend", Slug: "suspend-runtime", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "suspended-runtime@corp.kr", Name: "Suspended", Status: models.UserStatusActive}
	db.Create(&user)
	device := models.Device{OrganizationID: org.ID, UserID: user.ID, Hostname: "retained", Status: "active"}
	db.Create(&device)
	harness := models.Harness{OrganizationID: org.ID, DeviceID: device.ID, HarnessID: "peer-retained", AllowedUsers: `["` + user.ID + `"]`, Status: "enrolled"}
	db.Create(&harness)
	role := models.Role{OrganizationID: org.ID, Name: "member", Permissions: `[]`}
	db.Create(&role)
	db.Create(&models.UserRole{OrganizationID: org.ID, UserID: user.ID, RoleID: role.ID, Scope: "org"})
	db.Create(&models.ProjectMember{OrganizationID: org.ID, ProjectID: "project-retained", UserID: user.ID, Role: "member"})
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, UserID: user.ID, HarnessID: harness.HarnessID, SessionID: "session-suspended", Status: "active"}
	db.Create(&session)
	lease := models.CapabilityLease{OrganizationID: org.ID, UserID: user.ID, SessionID: session.SessionID, LeaseID: "lease-suspended", Status: "active"}
	db.Create(&lease)

	result, err := TransitionUserLifecycle(db, UserLifecycleMutation{
		OrganizationID: org.ID, UserID: user.ID, To: models.UserStatusSuspended,
		Reason: "temporary access hold", ActorID: "admin-1", ActorType: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ClosedSessions != 1 || result.RevokedLeases != 1 {
		t.Fatalf("runtime authority was not fully revoked: %+v", result)
	}
	db.First(&session, "id = ?", session.ID)
	db.First(&lease, "id = ?", lease.ID)
	if session.Status != "terminated" || lease.Status != "revoked" {
		t.Fatalf("runtime authority remains: session=%q lease=%q", session.Status, lease.Status)
	}
	var roles, memberships, devices, harnesses int64
	db.Model(&models.UserRole{}).Where("organization_id = ? AND user_id = ?", org.ID, user.ID).Count(&roles)
	db.Model(&models.ProjectMember{}).Where("organization_id = ? AND user_id = ?", org.ID, user.ID).Count(&memberships)
	db.Model(&models.Device{}).Where("organization_id = ? AND user_id = ? AND status != ?", org.ID, user.ID, "revoked").Count(&devices)
	db.Model(&models.Harness{}).Where("organization_id = ? AND harness_id = ? AND status != ?", org.ID, harness.HarnessID, "revoked").Count(&harnesses)
	if roles != 1 || memberships != 1 || devices != 1 || harnesses != 1 {
		t.Fatalf("suspension removed durable relationships: roles=%d memberships=%d devices=%d harnesses=%d", roles, memberships, devices, harnesses)
	}
}

func TestOffboardRevokesCompleteAccessGraphAndIgnoresUnrelatedMalformedHarness(t *testing.T) {
	_, db := lifecycleTestService(t)
	org := models.Organization{Name: "Complete", Slug: "complete", Status: "active"}
	db.Create(&org)
	target := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "target@corp.kr", Name: "Target", Status: models.UserStatusActive}
	other := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "other@corp.kr", Name: "Other", Status: models.UserStatusActive}
	db.Create(&target)
	db.Create(&other)
	db.Create(&AdminCredentials{Email: target.Email, Password: "unused", OrganizationID: org.ID, Role: "admin"})
	db.Create(&AdminCredentials{Email: other.Email, Password: "unused", OrganizationID: org.ID, Role: "admin"})
	device := models.Device{OrganizationID: org.ID, UserID: target.ID, Hostname: "owned", Status: "active"}
	otherDevice := models.Device{OrganizationID: org.ID, UserID: other.ID, Hostname: "shared-owner", Status: "active"}
	db.Create(&device)
	db.Create(&otherDevice)
	bound := models.Harness{OrganizationID: org.ID, DeviceID: device.ID, HarnessID: "peer-owned", AllowedUsers: `["` + target.ID + `"]`, Status: "enrolled"}
	shared := models.Harness{OrganizationID: org.ID, DeviceID: otherDevice.ID, HarnessID: "peer-shared", AllowedUsers: `["` + target.ID + `"]`, Status: "enrolled"}
	unrelated := models.Harness{OrganizationID: org.ID, HarnessID: "peer-unrelated", AllowedUsers: `{malformed`, Status: "enrolled"}
	db.Create(&bound)
	db.Create(&shared)
	db.Create(&unrelated)
	db.Create(&models.ProjectMember{OrganizationID: org.ID, ProjectID: "project-1", UserID: target.ID, Role: "member"})

	result, err := TransitionUserLifecycle(db, UserLifecycleMutation{
		OrganizationID: org.ID, UserID: target.ID, To: models.UserStatusOffboarded,
		Reason: "departure", ActorID: other.ID, ActorType: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RevokedDevices != 1 || result.RevokedHarnesses != 1 || result.RevokedProjectMemberships != 1 || result.RevokedConsoleAccess != 1 || result.RemainingAccess != 0 {
		t.Fatalf("incomplete access graph cleanup: %+v", result)
	}
	var remainingAdmin int64
	db.Model(&AdminCredentials{}).Where("organization_id = ? AND LOWER(email) = LOWER(?)", org.ID, target.Email).Count(&remainingAdmin)
	if remainingAdmin != 0 {
		t.Fatalf("target retained %d console credentials", remainingAdmin)
	}
	db.First(&unrelated, "id = ?", unrelated.ID)
	if unrelated.Status != "enrolled" {
		t.Fatalf("unrelated malformed harness was changed to %q", unrelated.Status)
	}
	db.First(&shared, "id = ?", shared.ID)
	if shared.Status != "enrolled" || shared.AllowedUsers != "[]" {
		t.Fatalf("other user's device harness was damaged: status=%q users=%q", shared.Status, shared.AllowedUsers)
	}
}

func TestLifecycleProtectsLastOrganizationAdministrator(t *testing.T) {
	_, db := lifecycleTestService(t)
	org := models.Organization{Name: "Admins", Slug: "admins", Status: "active"}
	db.Create(&org)
	admin := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "only-admin@corp.kr", Name: "Only", Status: models.UserStatusActive}
	db.Create(&admin)
	db.Create(&AdminCredentials{Email: admin.Email, Password: "unused", OrganizationID: org.ID, Role: "super_admin"})
	// Multiple credentials linked to one human remain one administrator
	// principal; they must not defeat the final-admin guard.
	db.Create(&AdminCredentials{Email: "only-admin-alias@corp.kr", Password: "unused", OrganizationID: org.ID, UserID: admin.ID, Role: "admin"})

	_, err := TransitionUserLifecycle(db, UserLifecycleMutation{
		OrganizationID: org.ID, UserID: admin.ID, To: models.UserStatusSuspended,
		Reason: "mistake", ActorID: "system", ActorType: "system",
	})
	if !errors.Is(err, ErrLifecycleLastAdmin) {
		t.Fatalf("last administrator transition error = %v", err)
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", admin.ID)
	if reloaded.Status != models.UserStatusActive {
		t.Fatalf("last administrator changed to %q", reloaded.Status)
	}
}

func TestReplaceUserRolesIsAtomicAndRejectsTerminalUsers(t *testing.T) {
	service, db := lifecycleTestService(t)
	org := models.Organization{Name: "Roles", Slug: "roles", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "roles@corp.kr", Name: "Roles", Status: models.UserStatusActive}
	db.Create(&user)
	oldRole := models.Role{OrganizationID: org.ID, Name: "old", Permissions: `[]`}
	validRole := models.Role{OrganizationID: org.ID, Name: "valid", Permissions: `[]`}
	db.Create(&oldRole)
	db.Create(&validRole)
	db.Create(&models.UserRole{OrganizationID: org.ID, UserID: user.ID, RoleID: oldRole.ID, Scope: "org"})

	err := service.ReplaceUserRoles(org.ID, user.ID, []RoleAssignment{
		{RoleID: validRole.ID, Scope: "org"},
		{RoleID: "missing-role", Scope: "org"},
	}, "admin-1")
	if err == nil {
		t.Fatal("invalid replacement unexpectedly succeeded")
	}
	var assignments []models.UserRole
	db.Where("organization_id = ? AND user_id = ?", org.ID, user.ID).Find(&assignments)
	if len(assignments) != 1 || assignments[0].RoleID != oldRole.ID {
		t.Fatalf("failed replacement partially changed roles: %+v", assignments)
	}

	db.Model(&user).Update("status", models.UserStatusOffboarded)
	if err := service.ReplaceUserRoles(org.ID, user.ID, nil, "admin-1"); !errors.Is(err, ErrUserReadOnly) {
		t.Fatalf("terminal role replacement error = %v", err)
	}
}

func TestValidateActiveHarnessUserBindingIsTenantAndLifecycleScoped(t *testing.T) {
	_, db := lifecycleTestService(t)
	org := models.Organization{Name: "Binding", Slug: "binding", Status: "active"}
	other := models.Organization{Name: "Other", Slug: "binding-other", Status: "active"}
	db.Create(&org)
	db.Create(&other)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "bound@corp.kr", Name: "Bound", Status: models.UserStatusActive}
	db.Create(&user)
	harness := models.Harness{OrganizationID: org.ID, HarnessID: "bound-harness", PublicKey: "key", AllowedUsers: `["` + user.ID + `"]`, Status: "enrolled"}
	db.Create(&harness)

	if err := db.Transaction(func(tx *gorm.DB) error {
		return ValidateActiveHarnessUserBinding(tx, org.ID, harness.HarnessID, user.ID)
	}); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return ValidateActiveHarnessUserBinding(tx, other.ID, harness.HarnessID, user.ID)
	}); err == nil {
		t.Fatal("cross-tenant binding accepted")
	}
	db.Model(&user).Update("status", models.UserStatusSuspended)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return ValidateActiveHarnessUserBinding(tx, org.ID, harness.HarnessID, user.ID)
	}); err == nil {
		t.Fatal("suspended user binding accepted")
	}
	db.Model(&user).Update("status", models.UserStatusActive)
	db.Model(&harness).Update("allowed_users", `not-json`)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return ValidateActiveHarnessUserBinding(tx, org.ID, harness.HarnessID, user.ID)
	}); err == nil {
		t.Fatal("malformed harness binding accepted")
	}
}
