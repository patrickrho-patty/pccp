package identity

import (
	"errors"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNormalizeIssuerContract(t *testing.T) {
	tests := map[string]string{
		"https://issuer.example":    "https://issuer.example",
		" https://issuer.example/ ": "https://issuer.example",
		"https://issuer.example///": "https://issuer.example",
	}
	for raw, expected := range tests {
		if got := NormalizeIssuer(raw); got != expected {
			t.Errorf("NormalizeIssuer(%q) = %q, want %q", raw, got, expected)
		}
	}
}

func TestLinkedTargetIdentityRequiresOneExplicitActiveMapping(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/external.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.SSOIdentityLink{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{
		AuditBase: models.AuditBase{Base: models.Base{ID: "user-1"}, OrganizationID: "org-1"},
		Email:     "member@example.com", Status: models.UserStatusActive, AuthMethod: "scim",
		ExternalIssuer: "scim", ExternalID: "customer-subject", ExternalIssuerVerified: true,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	target := NormalizeExternalIdentity("https://login.patty.io/realms/patty/", " patty-subject ")
	if _, err := FindActiveUserByLinkedTargetIdentity(db, "org-1", target); !errors.Is(err, ErrExternalIdentityUnlinked) {
		t.Fatalf("unlinked target identity error = %v", err)
	}
	link := models.SSOIdentityLink{
		OrganizationID: "org-1", LegacyIssuer: "https://customer-idp.example", LegacySubject: "customer-subject",
		TargetIssuer: target.Issuer, TargetSubject: target.Subject, PattyUserID: user.ID, Status: models.SSOLinkStatusLinked,
	}
	if err := db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	resolved, err := FindActiveUserByLinkedTargetIdentity(db, "org-1", target)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != user.ID {
		t.Fatalf("resolved user = %s, want %s", resolved.ID, user.ID)
	}
	if err := db.Model(&user).Update("status", models.UserStatusSuspended).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := FindActiveUserByLinkedTargetIdentity(db, "org-1", target); err == nil {
		t.Fatal("suspended linked user was resolved")
	}
}

func TestResolveLinkedSourceIdentityUsesImmutableTuple(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/source.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.SSOIdentityLink{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{AuditBase: models.AuditBase{Base: models.Base{ID: "user-source"}, OrganizationID: "org-source"}, Email: "shared@example.com", Status: models.UserStatusActive, AuthMethod: "scim", ExternalIssuer: "scim", ExternalID: "scim-sub"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	source := NormalizeExternalIdentity("https://customer.example/", "subject")
	if _, _, err := ResolveLinkedSourceIdentity(db, "org-source", source); !errors.Is(err, ErrExternalIdentityUnlinked) {
		t.Fatalf("unlinked error=%v", err)
	}
	if err := db.Create(&models.SSOIdentityLink{OrganizationID: "org-source", LegacyIssuer: source.Issuer, LegacySubject: source.Subject, PattyUserID: user.ID, Status: models.SSOLinkStatusLinked}).Error; err != nil {
		t.Fatal(err)
	}
	_, resolved, err := ResolveLinkedSourceIdentity(db, "org-source", source)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != user.ID {
		t.Fatalf("resolved=%s want=%s", resolved.ID, user.ID)
	}
}
