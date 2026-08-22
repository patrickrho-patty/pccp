package sso

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/ssomigrate"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDB(t *testing.T) *gorm.DB {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(append(models.AllModels(), &identity.AdminCredentials{})...)
	return db
}

func TestProvisionUserFromSSO(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")

	// Create org first
	org := models.Organization{Name: "Test", Slug: "sso-test", Status: "active"}
	db.Create(&org)

	// Provision new user
	user, err := svc.ProvisionUserFromSSO(org.ID, "https://idp.example", &SAMLResponse{
		UserID: "sso-user-001",
		Email:  "kim@patty.dev",
		Name:   "Kim Gaebal",
		NameKo: "김개발",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.NameKo != "김개발" {
		t.Fatal("expected Korean name")
	}
	if user.AuthMethod != "saml" {
		t.Fatal("expected saml auth method")
	}
	if user.Locale != "ko-KR" {
		t.Fatal("expected Korean locale")
	}

	// Provisioning same user again should update, not create
	user2, err := svc.ProvisionUserFromSSO(org.ID, "https://idp.example", &SAMLResponse{
		UserID: "sso-user-001",
		Email:  "kim@patty.dev",
		Name:   "Kim Updated",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user2.ID != user.ID {
		t.Fatal("should return same user")
	}
}

func TestSCIMProvisioning(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")

	org := models.Organization{Name: "SCIM Test", Slug: "scim-test", Status: "active"}
	db.Create(&org)

	// Fail closed: unconfigured SCIM refuses every request.
	unconfigured := httptest.NewRequest("POST", "/scim?org="+org.ID, nil)
	rec := httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, unconfigured)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured SCIM must 503, got %d", rec.Code)
	}

	svc.ConfigureSCIMTokenForOrganization(org.ID, "scim-admin-token")

	// No/incorrect bearer token refused.
	noAuth := httptest.NewRequest("POST", "/scim?org="+org.ID, strings.NewReader(`{"userName":"scim-1"}`))
	rec = httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, noAuth)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token-less SCIM must 401, got %d", rec.Code)
	}

	// The credential itself supplies immutable org scope.
	withAuth := httptest.NewRequest("POST", "/scim", strings.NewReader(`{"userName":"scim-unscoped","email":"unscoped@example.com"}`))
	withAuth.Header.Set("Authorization", "Bearer scim-admin-token")
	rec = httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, withAuth)
	if rec.Code != http.StatusCreated {
		t.Fatalf("tenant-bound SCIM without org query must 201, got %d", rec.Code)
	}

	// Authorized + scoped creation succeeds.
	create := httptest.NewRequest("POST", "/scim?org="+org.ID, strings.NewReader(`{"userName":"scim-1","email":"s@e.com","displayName":"SCIM One"}`))
	create.Header.Set("Authorization", "Bearer scim-admin-token")
	rec = httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("authorized SCIM create must 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Cross-org delete refused.
	var created models.User
	db.Where("organization_id = ?", org.ID).First(&created)
	del := httptest.NewRequest("DELETE", "/scim/users?userID="+created.ID+"&org=other-org", nil)
	del.Header.Set("Authorization", "Bearer scim-admin-token")
	rec = httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, del)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-org SCIM delete must 403, got %d", rec.Code)
	}
}

func TestOIDCResolvesSCIMUserOnlyThroughExplicitIssuerSubjectLink(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	org := models.Organization{Name: "SCIM Subject Link", Slug: "scim-subject-link", Status: "active"}
	db.Create(&org)
	scim := models.User{
		AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "subject@corp.example", Name: "Subject",
		Status: models.UserStatusActive, AuthMethod: "scim", ExternalID: "immutable-subject", ExternalIssuer: "scim", ExternalIssuerVerified: true,
	}
	db.Create(&scim)
	if _, err := ssomigrate.New(db).LinkIdentity(org.ID, ssomigrate.IdentityLinkRequest{
		LegacyIssuer: "https://idp.corp.example", LegacySubject: "immutable-subject", PattyUserID: scim.ID,
		TargetIssuer: "https://login.patty.io/realms/patty", TargetSubject: "patty-subject",
		Note: "governance-approved customer identity reconciliation",
	}, "admin"); err != nil {
		t.Fatal(err)
	}

	resolved, err := svc.ProvisionOIDCUser(org.ID, "https://idp.corp.example", &OIDCUserInfo{
		Sub: "immutable-subject", Email: scim.Email, EmailVerified: true, Name: "Subject",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != scim.ID {
		t.Fatalf("OIDC subject created a second account: got %s want %s", resolved.ID, scim.ID)
	}
	var link models.SSOIdentityLink
	if err := db.Where("organization_id = ? AND legacy_issuer = ? AND legacy_subject = ?", org.ID, "https://idp.corp.example", "immutable-subject").First(&link).Error; err != nil {
		t.Fatal(err)
	}
	if link.PattyUserID != scim.ID || link.Status != "linked" {
		t.Fatalf("identity link = %+v", link)
	}
}

func TestOIDCUnverifiedEmailCannotBindSCIMUser(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	org := models.Organization{Name: "SCIM Unverified", Slug: "scim-unverified", Status: "active"}
	db.Create(&org)
	db.Create(&models.User{
		AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "shared@corp.example", Name: "SCIM",
		Status: models.UserStatusActive, AuthMethod: "scim", ExternalID: "scim-subject", ExternalIssuer: "scim", ExternalIssuerVerified: true,
	})

	if _, err := svc.ProvisionOIDCUser(org.ID, "https://idp.corp.example", &OIDCUserInfo{
		Sub: "attacker-subject", Email: "shared@corp.example", EmailVerified: false,
	}); err == nil {
		t.Fatal("unverified email bound or provisioned an account")
	}
	var users int64
	db.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&users)
	if users != 1 {
		t.Fatalf("unverified collision created an account: users=%d", users)
	}
}

func TestOIDCDoesNotCorrelateSCIMByRawSubjectOrVerifiedEmail(t *testing.T) {
	for _, test := range []struct {
		name, oidcSubject, oidcEmail, scimSubject, scimEmail string
	}{
		{name: "raw subject equality", oidcSubject: "shared-subject", oidcEmail: "oidc@corp.example", scimSubject: "shared-subject", scimEmail: "scim@corp.example"},
		{name: "verified email equality", oidcSubject: "oidc-subject", oidcEmail: "shared@corp.example", scimSubject: "scim-subject", scimEmail: "shared@corp.example"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupDB(t)
			svc := New(db, "test-secret")
			org := models.Organization{Name: test.name, Slug: strings.ReplaceAll(test.name, " ", "-"), Status: "active"}
			if err := db.Create(&org).Error; err != nil {
				t.Fatal(err)
			}
			scim := models.User{
				AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: test.scimEmail, Name: "SCIM",
				Status: models.UserStatusActive, AuthMethod: "scim", ExternalID: test.scimSubject,
				ExternalIssuer: "scim", ExternalIssuerVerified: true,
			}
			if err := db.Create(&scim).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ProvisionOIDCUser(org.ID, "https://idp.corp.example", &OIDCUserInfo{
				Sub: test.oidcSubject, Email: test.oidcEmail, EmailVerified: true,
			}); err == nil || !strings.Contains(err.Error(), "administrator resolution") {
				t.Fatalf("unlinked cross-protocol identity was accepted: %v", err)
			}
			var users int64
			if err := db.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&users).Error; err != nil {
				t.Fatal(err)
			}
			if users != 1 {
				t.Fatalf("unlinked identity created a duplicate user: %d", users)
			}
		})
	}
}

func TestConcurrentSAMLFirstLoginRechecksIdentityAfterOrganizationLock(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	org := models.Organization{Name: "Concurrent SSO", Slug: "concurrent-sso", Status: "active", MaxUserSeats: 1}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	winner := models.User{
		AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "winner@corp.example", Name: "Winner",
		Status: models.UserStatusActive, AuthMethod: "saml", ExternalIssuer: "https://idp.example",
		ExternalID: "race-subject", ExternalIssuerVerified: true,
	}
	var once sync.Once
	callback := "insert_concurrent_sso_winner_before_org_lock"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "organizations" {
			once.Do(func() {
				if err := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Create(&winner).Error; err != nil {
					tx.AddError(err)
				}
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })

	resolved, err := svc.ProvisionUserFromSSO(org.ID, "https://idp.example", &SAMLResponse{
		UserID: "race-subject", Email: "winner@corp.example", Name: "Winner Updated",
	})
	if err != nil {
		t.Fatalf("same-identity concurrent first login was charged a second seat: %v", err)
	}
	if resolved.ID != winner.ID {
		t.Fatalf("resolved user = %s, want concurrent winner %s", resolved.ID, winner.ID)
	}
	var users int64
	if err := db.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users != 1 {
		t.Fatalf("concurrent first login created %d users", users)
	}
}

func TestOIDCDuplicateSCIMEmailStillRequiresExplicitIdentityLink(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	org := models.Organization{Name: "SCIM Ambiguous", Slug: "scim-ambiguous", Status: "active"}
	db.Create(&org)
	for _, subject := range []string{"scim-one", "scim-two"} {
		db.Create(&models.User{
			AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "ambiguous@corp.example", Name: subject,
			Status: models.UserStatusActive, AuthMethod: "scim", ExternalID: subject, ExternalIssuer: "scim", ExternalIssuerVerified: true,
		})
	}

	if _, err := svc.ProvisionOIDCUser(org.ID, "https://idp.corp.example", &OIDCUserInfo{
		Sub: "new-oidc-subject", Email: "ambiguous@corp.example", EmailVerified: true,
	}); err == nil {
		t.Fatal("ambiguous verified email was silently resolved")
	}
	var users int64
	db.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&users)
	if users != 2 {
		t.Fatalf("ambiguous collision created an account: users=%d", users)
	}
	var resolution models.SSOIdentityLink
	if err := db.Where("organization_id = ? AND legacy_issuer = ? AND legacy_subject = ?", org.ID, "https://idp.corp.example", "new-oidc-subject").First(&resolution).Error; err != nil {
		t.Fatal("ambiguous collision did not create an admin-resolution item")
	}
	if resolution.Status != models.SSOLinkStatusUnlinked || resolution.PattyUserID != "" {
		t.Fatalf("unlinked resolution = %+v", resolution)
	}
}

func TestSSOProfileRefreshCannotResurrectSuspendedUser(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	org := models.Organization{Name: "SSO Race", Slug: "sso-race", Status: "active"}
	db.Create(&org)
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "race@corp.kr", Name: "Before", Status: "active", AuthMethod: "saml", ExternalID: "race-sub", ExternalIssuer: "https://idp.example"}
	db.Create(&user)

	var once sync.Once
	callbackName := "pat1489_suspend_before_sso_profile_update"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			once.Do(func() {
				// The initial SSO lookup has already materialized its active row.
				// Commit the competing lifecycle change before the profile helper
				// obtains its own lifecycle lock.
				if err := db.Exec("UPDATE users SET status = ? WHERE id = ?", "suspended", user.ID).Error; err != nil {
					tx.AddError(err)
				}
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	if _, err := svc.ProvisionUserFromSSO(org.ID, "https://idp.example", &SAMLResponse{UserID: "race-sub", Email: "race@corp.kr", Name: "After"}); err == nil {
		t.Fatal("stale SSO refresh succeeded after lifecycle state changed")
	}
	var reloaded models.User
	db.First(&reloaded, "id = ?", user.ID)
	if reloaded.Status != "suspended" {
		t.Fatalf("SSO refresh resurrected status to %q", reloaded.Status)
	}
}

func TestSSOAndSCIMEmailRefreshKeepConsoleCredentialLifecycleBound(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	org := models.Organization{Name: "Identity Link", Slug: "identity-link", Status: "active"}
	db.Create(&org)

	samlUser := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "saml-old@corp.kr", Name: "SAML", Status: models.UserStatusActive, AuthMethod: "saml", ExternalID: "saml-link", ExternalIssuer: "https://idp.example"}
	db.Create(&samlUser)
	db.Create(&identity.AdminCredentials{Email: samlUser.Email, Password: "unused", OrganizationID: org.ID, UserID: samlUser.ID, Role: "member"})
	if _, err := svc.ProvisionUserFromSSO(org.ID, "https://idp.example", &SAMLResponse{UserID: samlUser.ExternalID, Email: "saml-new@corp.kr", Name: "SAML"}); err != nil {
		t.Fatal(err)
	}
	assertCredentialMoved := func(oldEmail, newEmail string) {
		t.Helper()
		var oldCount, newCount int64
		db.Model(&identity.AdminCredentials{}).Where("organization_id = ? AND LOWER(email) = LOWER(?)", org.ID, oldEmail).Count(&oldCount)
		db.Model(&identity.AdminCredentials{}).Where("organization_id = ? AND LOWER(email) = LOWER(?)", org.ID, newEmail).Count(&newCount)
		if oldCount != 0 || newCount != 1 {
			t.Fatalf("console identity link did not move %q -> %q: old=%d new=%d", oldEmail, newEmail, oldCount, newCount)
		}
	}
	assertCredentialMoved("saml-old@corp.kr", "saml-new@corp.kr")

	scimUser := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "scim-old@corp.kr", Name: "SCIM", Status: models.UserStatusActive, AuthMethod: "scim", ExternalID: "scim-link", ExternalIssuer: "scim"}
	db.Create(&scimUser)
	db.Create(&identity.AdminCredentials{Email: scimUser.Email, Password: "unused", OrganizationID: org.ID, UserID: scimUser.ID, Role: "member"})
	active := true
	if _, err := svc.provisionSCIMUser(org.ID, &SCIMUser{ExternalID: scimUser.ExternalID, Email: "scim-new@corp.kr", DisplayName: "SCIM", Active: &active}); err != nil {
		t.Fatal(err)
	}
	assertCredentialMoved("scim-old@corp.kr", "scim-new@corp.kr")

	result, err := identity.TransitionUserLifecycle(db, identity.UserLifecycleMutation{
		OrganizationID: org.ID, UserID: scimUser.ID, To: models.UserStatusOffboarded,
		Reason: "SCIM removal", ActorID: "scim", ActorType: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RevokedConsoleAccess != 1 {
		t.Fatalf("offboarding revoked %d linked console credentials", result.RevokedConsoleAccess)
	}
}

func TestSCIMExplicitInactiveAndDeleteUseCanonicalOffboarding(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	org := models.Organization{Name: "SCIM Lifecycle", Slug: "scim-life", Status: "active"}
	db.Create(&org)
	svc.ConfigureSCIMTokenForOrganization(org.ID, "scim-admin-token")

	req := httptest.NewRequest("POST", "/scim?org="+org.ID, strings.NewReader(`{"userName":"inactive-sub","email":"inactive@corp.kr","displayName":"Inactive","active":false}`))
	req.Header.Set("Authorization", "Bearer scim-admin-token")
	rec := httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("inactive SCIM create got %d: %s", rec.Code, rec.Body.String())
	}
	var inactive models.User
	if err := db.Where("organization_id = ? AND external_id = ?", org.ID, "inactive-sub").First(&inactive).Error; err != nil {
		t.Fatal(err)
	}
	if inactive.AuthMethod != "scim" || inactive.Status != "offboarded" {
		t.Fatalf("explicit inactive SCIM user = auth %q status %q", inactive.AuthMethod, inactive.Status)
	}

	activeUser := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "delete@corp.kr", Name: "Delete", Status: "active", AuthMethod: "scim", ExternalID: "delete-sub", ExternalIssuer: "scim"}
	db.Create(&activeUser)
	session := models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, UserID: activeUser.ID, HarnessID: "peer-scim", SessionID: "scim-session", Status: "active"}
	db.Create(&session)
	lease := models.CapabilityLease{OrganizationID: org.ID, LeaseID: "scim-lease", SubjectPeerID: "peer-scim", UserID: activeUser.ID, SessionID: session.SessionID, PolicyEpochID: "epoch", NotBefore: "2026-01-01T00:00:00Z", NotAfter: "2027-01-01T00:00:00Z", Status: "active"}
	db.Create(&lease)

	del := httptest.NewRequest("DELETE", "/scim/Users/"+activeUser.ID+"?org="+org.ID, nil)
	del.Header.Set("Authorization", "Bearer scim-admin-token")
	rec = httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, del)
	if rec.Code != http.StatusOK {
		t.Fatalf("SCIM delete got %d: %s", rec.Code, rec.Body.String())
	}
	db.First(&session, "id = ?", session.ID)
	db.First(&lease, "id = ?", lease.ID)
	if session.Status != "terminated" || lease.Status != "revoked" {
		t.Fatalf("SCIM delete left access: session=%s lease=%s", session.Status, lease.Status)
	}
	var audit models.AuditEvent
	if err := db.Where("resource_id = ? AND event_type = ?", activeUser.ID, "cp.user.offboarded").First(&audit).Error; err != nil {
		t.Fatalf("SCIM offboard audit missing: %v", err)
	}
	if audit.ActorID != "scim" || !strings.Contains(audit.Details, "SCIM deprovisioning") {
		t.Fatalf("SCIM audit missing actor/reason: %+v", audit)
	}
}

func TestSCIMInactiveUserIsNeverPersistedActive(t *testing.T) {
	db := setupDB(t)
	svc := New(db, "test-secret")
	org := models.Organization{Name: "SCIM Atomic", Slug: "scim-atomic", Status: "active"}
	db.Create(&org)
	svc.ConfigureSCIMTokenForOrganization(org.ID, "scim-admin-token")

	callbackName := "pat1489_reject_transient_active_scim_create"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if user, ok := tx.Statement.Dest.(*models.User); ok && user.ExternalID == "inactive-atomic" && user.Status == models.UserStatusActive {
			tx.AddError(errors.New("SCIM inactive account was transiently active"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	req := httptest.NewRequest("POST", "/scim?org="+org.ID, strings.NewReader(`{"userName":"inactive-atomic","email":"inactive-atomic@corp.kr","active":false}`))
	req.Header.Set("Authorization", "Bearer scim-admin-token")
	rec := httptest.NewRecorder()
	svc.HandleSCIMRequest(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("atomic inactive SCIM create got %d: %s", rec.Code, rec.Body.String())
	}
	var user models.User
	if err := db.Where("organization_id = ? AND external_id = ?", org.ID, "inactive-atomic").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.Status != models.UserStatusOffboarded {
		t.Fatalf("inactive SCIM status = %q", user.Status)
	}
}
