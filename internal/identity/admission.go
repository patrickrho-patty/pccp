package identity

import (
	"errors"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/config"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sovereign"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrUserSeatLimit    = errors.New("identity: user seat limit reached")
	ErrHarnessSeatLimit = errors.New("identity: harness seat limit reached")
)

// LockOrganizationForAdmission serializes roster mutations for one tenant.
func LockOrganizationForAdmission(tx *gorm.DB, orgID string) (*models.Organization, error) {
	var org models.Organization
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&org, "id = ?", orgID).Error; err != nil {
		return nil, fmt.Errorf("identity: lock organization for admission: %w", err)
	}
	return &org, nil
}

// RequireUserSeatWithDB validates the current signed entitlement and capacity.
// The caller must retain the organization lock through the roster mutation.
func RequireUserSeatWithDB(tx *gorm.DB, org models.Organization) error {
	return requireUserSeatWithDB(tx, org, "")
}

// AdmitExternalUserWithDB is the cross-package active-roster seam. It owns the
// organization lock and canonical immutable-identity recheck.
func AdmitExternalUserWithDB(tx *gorm.DB, user *models.User, conflictDoNothing bool) (*models.User, bool, error) {
	if user == nil {
		return nil, false, fmt.Errorf("identity: admitted user is required")
	}
	org, err := LockOrganizationForAdmission(tx, user.OrganizationID)
	if err != nil {
		return nil, false, err
	}
	external := NormalizeExternalIdentity(user.ExternalIssuer, user.ExternalID)
	if external.Issuer != "" && external.Subject != "" && user.AuthMethod != "" {
		existing, findErr := FindUserByExternalIdentity(tx, user.OrganizationID, user.AuthMethod, external)
		if findErr == nil {
			return existing, false, nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil, false, findErr
		}
	}
	created, err := createAdmittedUserWithLockedOrganization(tx, *org, user, conflictDoNothing)
	if err != nil || created || !conflictDoNothing {
		return user, created, err
	}
	existing, findErr := FindUserByExternalIdentity(tx, user.OrganizationID, user.AuthMethod, external)
	if findErr != nil {
		return nil, false, findErr
	}
	return existing, false, nil
}

func createAdmittedUserWithLockedOrganization(tx *gorm.DB, org models.Organization, user *models.User, conflictDoNothing bool) (bool, error) {
	if user == nil || user.OrganizationID != org.ID {
		return false, fmt.Errorf("identity: admitted user organization mismatch")
	}
	if user.Status != models.UserStatusOffboarded {
		if err := RequireUserSeatWithDB(tx, org); err != nil {
			return false, err
		}
	}
	query := tx
	if conflictDoNothing {
		query = query.Clauses(clause.OnConflict{DoNothing: true})
	}
	result := query.Create(user)
	return result.RowsAffected == 1, result.Error
}

func requireUserSeatWithDB(tx *gorm.DB, org models.Organization, existingUserID string) error {
	maxSeats := org.MaxUserSeats
	if config.GetProfile(org.Profile).Name == "sovereign" {
		entitlement, err := sovereign.ValidateActiveEntitlementWithDB(tx, org.ID, config.SovereignDeploymentID(), "user-management", "", time.Now().UTC())
		if err != nil {
			return fmt.Errorf("%w: sovereign entitlement unavailable: %v", ErrUserSeatLimit, err)
		}
		maxSeats = entitlement.MaxUserSeats
	}
	if maxSeats <= 0 {
		return nil
	}
	var used int64
	query := tx.Model(&models.User{}).Where("organization_id = ? AND status != ?", org.ID, models.UserStatusOffboarded)
	if existingUserID != "" {
		query = query.Where("id != ?", existingUserID)
	}
	if err := query.Count(&used).Error; err != nil {
		return err
	}
	if used >= int64(maxSeats) {
		return fmt.Errorf("%w:%d:%d", ErrUserSeatLimit, used, maxSeats)
	}
	return nil
}

// RequireHarnessSeatWithDB validates the same signed entitlement and capacity
// gate used by enrollment. Callers issuing a one-time grant must retain the
// organization lock so a grant cannot bypass sovereign or seat policy.
func RequireHarnessSeatWithDB(tx *gorm.DB, org models.Organization) error {
	maxSeats := org.MaxHarnessSeats
	entitlement, err := RequireHarnessEntitlementWithDB(tx, org)
	if err != nil {
		return err
	}
	if entitlement != nil {
		maxSeats = entitlement.MaxHarnessSeats
	}
	if maxSeats <= 0 {
		return nil
	}
	var used int64
	if err := tx.Model(&models.Harness{}).Where("organization_id = ? AND status != 'revoked'", org.ID).Count(&used).Error; err != nil {
		return err
	}
	if used >= int64(maxSeats) {
		return fmt.Errorf("%w:%d:%d", ErrHarnessSeatLimit, used, maxSeats)
	}
	return nil
}

// RequireHarnessEntitlementWithDB revalidates the signed sovereign authority
// without applying new-seat capacity. Renewal uses this fail-closed gate.
func RequireHarnessEntitlementWithDB(tx *gorm.DB, org models.Organization) (*sovereign.OfflineEntitlement, error) {
	if config.GetProfile(org.Profile).Name != "sovereign" {
		return nil, nil
	}
	entitlement, err := sovereign.ValidateActiveEntitlementWithDB(tx, org.ID, config.SovereignDeploymentID(), "harness-enrollment", "", time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: sovereign entitlement unavailable: %v", ErrHarnessSeatLimit, err)
	}
	return entitlement, nil
}
