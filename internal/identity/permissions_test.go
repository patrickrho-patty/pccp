package identity

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestConsoleTokenRejectsScopedTicketAndNonHS256Token(t *testing.T) {
	const secret = "permission-test-secret"
	auth := NewAuthService(nil, secret)
	valid, err := auth.IssueTokenWithPermissions("operator@example.com", "org-1", "member", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.VerifyToken(valid); err != nil {
		t.Fatalf("valid console token rejected: %v", err)
	}

	now := time.Now()
	scoped := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		Email: "operator@example.com", OrganizationID: "org-1", Role: "member",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-1", Issuer: "pccp-live-sse",
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
	})
	scopedToken, err := scoped.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.VerifyToken(scopedToken); err == nil {
		t.Fatal("scoped SSE ticket was accepted as a console bearer")
	}

	wrongAlgorithm := jwt.NewWithClaims(jwt.SigningMethodHS384, &Claims{
		Email: "operator@example.com", OrganizationID: "org-1", Role: "member", Purpose: consoleTokenPurpose,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: consoleTokenIssuer, Audience: jwt.ClaimStrings{consoleTokenAudience},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
	})
	wrongAlgorithmToken, err := wrongAlgorithm.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.VerifyToken(wrongAlgorithmToken); err == nil {
		t.Fatal("non-HS256 token was accepted as a console bearer")
	}
}

func TestConsoleTokenIntrospectionRejectsOrgAndGrantRevocation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:console_grant_introspection?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&AdminCredentials{}, &models.Organization{}, &models.User{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "Grant Org", Slug: "grant-org", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "operator@example.com", Status: models.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	permissions, _ := json.Marshal([]string{"security.alert_endpoint.test"})
	credential := AdminCredentials{
		Email: user.Email, Password: "sso-only", OrganizationID: org.ID, UserID: user.ID,
		Role: "security_operator", PermissionsJSON: string(permissions),
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	auth := NewAuthService(db, "permission-test-secret")
	token, err := auth.IssueTokenForUser(user.ID, org.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.VerifyToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.ValidateClaimsLifecycle(claims); err != nil {
		t.Fatalf("current grants rejected: %v", err)
	}
	if err := db.Model(&credential).Update("permissions_json", `[]`).Error; err != nil {
		t.Fatal(err)
	}
	if err := auth.ValidateClaimsLifecycle(claims); err == nil {
		t.Fatal("bearer retained a permission after its authoritative grant was removed")
	}

	fresh, err := auth.IssueTokenForUser(user.ID, org.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	freshClaims, err := auth.VerifyToken(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&org).Update("status", "inactive").Error; err != nil {
		t.Fatal(err)
	}
	if err := auth.ValidateClaimsLifecycle(freshClaims); err == nil {
		t.Fatal("bearer remained valid after organization deactivation")
	}
}

func TestIssueTokenWithPermissionsRoundTripsActionGrants(t *testing.T) {
	auth := NewAuthService(nil, "permission-test-secret")
	token, err := auth.IssueTokenWithPermissions("operator@example.com", "org-1", "security_operator", []string{"security.alert_endpoint.disable"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.VerifyToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.Permissions) != 1 || claims.Permissions[0] != "security.alert_endpoint.disable" {
		t.Fatalf("permission grants were not preserved: %+v", claims.Permissions)
	}
}

func TestIssueTokenResolvesDurablePermissionsForSSOIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:sso_permissions?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&AdminCredentials{}, &models.User{}); err != nil {
		t.Fatal(err)
	}
	auth := NewAuthService(db, "permission-test-secret")
	user := models.User{AuditBase: models.AuditBase{OrganizationID: "org-1"}, Email: "operator@example.com", Status: models.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := auth.SetPermissions("org-1", "operator@example.com", "security_operator", []string{"security.alert_endpoint.test"}); err != nil {
		t.Fatal(err)
	}
	// SAML/OIDC callbacks mint from the exact verified managed user.
	token, err := auth.IssueTokenForUser(user.ID, "org-1", "member")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.VerifyToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Role != "security_operator" || len(claims.Permissions) != 1 || claims.Permissions[0] != "security.alert_endpoint.test" {
		t.Fatalf("SSO token did not resolve durable grants: %+v", claims)
	}
}

func TestIssueTokenForUserNeverInheritsUnlinkedEmailCredential(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:sso_immutable_subject?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&AdminCredentials{}, &models.User{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{AuditBase: models.AuditBase{OrganizationID: "org-1"}, Email: "shared@example.com", Status: models.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	legacyAdmin := AdminCredentials{Email: user.Email, Password: "unusable", OrganizationID: "org-1", Role: "super_admin"}
	if err := db.Create(&legacyAdmin).Error; err != nil {
		t.Fatal(err)
	}
	auth := NewAuthService(db, "permission-test-secret")
	token, err := auth.IssueTokenForUser(user.ID, "org-1", "member")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.VerifyToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != user.ID || claims.Role != "member" || len(claims.Permissions) != 0 {
		t.Fatalf("unlinked email credential leaked into SSO token: %+v", claims)
	}
}

func TestSetPermissionsPreservesExistingPrimaryRole(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:preserve_permission_role?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&AdminCredentials{}); err != nil {
		t.Fatal(err)
	}
	existing := AdminCredentials{Email: "owner@example.com", Password: "hash", OrganizationID: "org-1", Role: "owner", Name: "Owner"}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	auth := NewAuthService(db, "permission-test-secret")
	if err := auth.SetPermissions("org-1", existing.Email, "security_operator", []string{"security.alert_endpoint.test"}); err != nil {
		t.Fatal(err)
	}
	var stored AdminCredentials
	if err := db.Where("organization_id = ? AND email = ?", "org-1", existing.Email).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Role != "owner" {
		t.Fatalf("grant update downgraded the primary role: %q", stored.Role)
	}
}
