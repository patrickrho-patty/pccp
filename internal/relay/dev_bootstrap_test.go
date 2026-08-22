package relay

import (
	"errors"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

func TestEnsureDevBootstrapEnrollmentUser(t *testing.T) {
	t.Run("production leaves an unknown user unresolved", func(t *testing.T) {
		t.Setenv("PCCP_DEV_BOOTSTRAP", "0")
		db := setupGovernedTestDB(t)
		svc, err := New(db, "", "relay-production-enrollment")
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.ensureDevBootstrapEnrollmentUser("org-prod", "user-prod"); err != nil {
			t.Fatal(err)
		}
		var user models.User
		if err := db.Where("organization_id = ? AND id = ?", "org-prod", "user-prod").First(&user).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("production mode created an enrollment user: %v", err)
		}
		var org models.Organization
		if err := db.First(&org, "id = ?", "org-prod").Error; !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("production mode created an enrollment organization: %v", err)
		}
	})

	t.Run("development creates an active actor without reactivating existing users", func(t *testing.T) {
		t.Setenv("PCCP_DEV_BOOTSTRAP", "1")
		db := setupGovernedTestDB(t)
		svc, err := New(db, "", "relay-development-enrollment")
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.ensureDevBootstrapEnrollmentUser("org-dev", "user-dev"); err != nil {
			t.Fatal(err)
		}
		var user models.User
		if err := db.Where("organization_id = ? AND id = ?", "org-dev", "user-dev").First(&user).Error; err != nil {
			t.Fatal(err)
		}
		if user.Status != models.UserStatusActive {
			t.Fatalf("bootstrap user status = %q", user.Status)
		}
		var org models.Organization
		if err := db.First(&org, "id = ?", "org-dev").Error; err != nil {
			t.Fatalf("bootstrap organization: %v", err)
		}
		if org.Status != "active" || org.Profile != "enterprise" {
			t.Fatalf("bootstrap organization = %+v", org)
		}

		if err := db.Model(&user).Update("status", models.UserStatusSuspended).Error; err != nil {
			t.Fatal(err)
		}
		if err := svc.ensureDevBootstrapEnrollmentUser("org-dev", "user-dev"); err != nil {
			t.Fatal(err)
		}
		if err := db.First(&user, "id = ?", "user-dev").Error; err != nil {
			t.Fatal(err)
		}
		if user.Status != models.UserStatusSuspended {
			t.Fatalf("bootstrap reactivated existing user: %q", user.Status)
		}
	})
}
