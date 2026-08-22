package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestPreparedEnrollmentRejectsPolicyChangedBeforeCommit(t *testing.T) {
	service, db := lifecycleTestService(t)
	org := models.Organization{Name: "Prepared", Slug: "prepared-enrollment", Profile: "enterprise", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "prepared@corp.kr", Name: "Prepared", Status: models.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.PrepareHarnessEnrollment(EnrollHarnessRequest{
		OrganizationID: org.ID,
		UserID:         user.ID,
		HarnessID:      "prepared-harness",
		PublicKeyHex:   hex.EncodeToString(publicKey),
	})
	if err != nil {
		t.Fatal(err)
	}

	policyJSON, err := json.Marshal(EnrollmentPolicy{RequireAdminApproval: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: EnrollmentPolicySettingKey, Value: string(policyJSON)}).Error; err != nil {
		t.Fatal(err)
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		var locked models.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", org.ID).Error; err != nil {
			return err
		}
		_, _, err := service.EnrollPreparedHarnessWithDB(tx, locked, prepared)
		return err
	})
	if !errors.Is(err, ErrEnrollmentPolicyDenied) {
		t.Fatalf("changed policy error = %v, want ErrEnrollmentPolicyDenied", err)
	}
	var harnesses int64
	if err := db.Model(&models.Harness{}).Where("organization_id = ?", org.ID).Count(&harnesses).Error; err != nil {
		t.Fatal(err)
	}
	if harnesses != 0 {
		t.Fatalf("stale prepared enrollment persisted %d harnesses", harnesses)
	}
}
