package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
)

func TestCreateUserEnforcesOrganizationSeatLimit(t *testing.T) {
	service, db := lifecycleTestService(t)
	org := models.Organization{Name: "Seats", Slug: "user-seats", Profile: "enterprise", Status: "active", MaxUserSeats: 1}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(org.ID, "first@example.com", "First", "", "local", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(org.ID, "second@example.com", "Second", "", "local", ""); !errors.Is(err, ErrUserSeatLimit) {
		t.Fatalf("second creation error = %v, want ErrUserSeatLimit", err)
	}
}

func TestEnrollHarnessEnforcesOrganizationSeatLimit(t *testing.T) {
	service, db := lifecycleTestService(t)
	org := models.Organization{Name: "Harness seats", Slug: "harness-seats", Profile: "enterprise", Status: "active", MaxHarnessSeats: 1}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "owner@example.com", Status: models.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	request := EnrollHarnessRequest{OrganizationID: org.ID, UserID: user.ID, HarnessID: "first", PublicKeyHex: hex.EncodeToString(pub)}
	if _, _, err := service.EnrollHarness(request); err != nil {
		t.Fatal(err)
	}
	request.HarnessID = "second"
	if _, _, err := service.EnrollHarness(request); !errors.Is(err, ErrHarnessSeatLimit) {
		t.Fatalf("second enrollment error = %v, want ErrHarnessSeatLimit", err)
	}
}
