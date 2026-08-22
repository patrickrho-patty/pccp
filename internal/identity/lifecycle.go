package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sessionlifecycle"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrLifecycleUserNotFound = errors.New("identity: lifecycle user not found")
	ErrLifecycleInvalid      = errors.New("identity: invalid user lifecycle transition")
	ErrLifecycleStateChanged = errors.New("identity: user lifecycle state changed concurrently")
	ErrLifecycleLastAdmin    = errors.New("identity: cannot disable the last organization administrator")
	ErrLifecycleAccessRemain = errors.New("identity: offboarding left active access")
	ErrLifecycleCleanup      = errors.New("identity: lifecycle transition committed with cleanup failures")
	ErrUserReadOnly          = errors.New("identity: offboarded users are read-only")
	ErrHarnessUserBinding    = errors.New("identity: harness user binding rejected")
)

// UserLifecycleMutation is the single persisted transition contract shared by
// console API actions, SCIM deprovisioning, and system-driven expiry sweeps.
type UserLifecycleMutation struct {
	OrganizationID   string
	UserID           string
	To               string
	Reason           string
	ActorID          string
	ActorType        string
	EventType        string
	Action           string
	Idempotent       bool
	SessionLifecycle *sessionlifecycle.Service
}

// UserLifecycleResult is the committed state and access-removal evidence.
type UserLifecycleResult struct {
	User                      models.User
	From                      string
	ClosedSessions            int64
	RevokedLeases             int64
	RevokedHarnesses          int64
	RevokedDevices            int64
	RevokedEnrollments        int64
	RevokedEntitlements       int64
	RevokedProjectMemberships int64
	RevokedConsoleAccess      int64
	RemainingAccess           int64
	SessionCleanupFailures    []string
}

// LockLifecycleUser serializes every access grant/revocation with lifecycle
// transitions. Callers must invoke it inside a database transaction and keep
// all access writes in that same transaction.
func LockLifecycleUser(tx *gorm.DB, orgID, userID string) (*models.User, error) {
	var user models.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND organization_id = ?", userID, orgID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// LockActiveUser is the common fail-closed gate for capability issuance.
func LockActiveUser(tx *gorm.DB, orgID, userID string) (*models.User, error) {
	user, err := LockLifecycleUser(tx, orgID, userID)
	if err != nil {
		return nil, err
	}
	if user.Status != models.UserStatusActive {
		return nil, fmt.Errorf("%w (current: %s)", ErrUserNotActive, user.Status)
	}
	return user, nil
}

// LockMutableUser permits administrative relationship changes for active and
// suspended users while keeping the terminal offboarded state read-only.
func LockMutableUser(tx *gorm.DB, orgID, userID string) (*models.User, error) {
	user, err := LockLifecycleUser(tx, orgID, userID)
	if err != nil {
		return nil, err
	}
	if user.Status == models.UserStatusOffboarded {
		return nil, ErrUserReadOnly
	}
	return user, nil
}

// TransitionUserLifecycle performs a CAS transition and its audit record in
// one transaction. Offboarding additionally revokes every persisted access
// relationship before the transaction can commit.
func TransitionUserLifecycle(db *gorm.DB, mutation UserLifecycleMutation) (*UserLifecycleResult, error) {
	if mutation.Reason == "" {
		return nil, fmt.Errorf("%w: reason is required", ErrLifecycleInvalid)
	}

	lifecycle := mutation.SessionLifecycle
	if lifecycle == nil {
		lifecycle = sessionlifecycle.New(db)
	}
	result := &UserLifecycleResult{}
	var sessionOutcomes []sessionlifecycle.Outcome
	err := db.Transaction(func(tx *gorm.DB) error {
		var admissionOrg *models.Organization
		if mutation.To == models.UserStatusActive {
			var err error
			admissionOrg, err = LockOrganizationForAdmission(tx, mutation.OrganizationID)
			if err != nil {
				return err
			}
		}
		user, err := LockLifecycleUser(tx, mutation.OrganizationID, mutation.UserID)
		if err != nil {
			if errors.Is(err, ErrUserNotFound) {
				return ErrLifecycleUserNotFound
			}
			return err
		}
		result.User = *user
		result.From = user.Status
		if mutation.To == models.UserStatusActive {
			if err := requireUserSeatWithDB(tx, *admissionOrg, user.ID); err != nil {
				return err
			}
		}
		if user.Status == mutation.To && mutation.Idempotent {
			if mutation.To == models.UserStatusOffboarded {
				if err := revokeUserAccess(tx, mutation.OrganizationID, mutation.UserID, result, lifecycle, &sessionOutcomes, mutation); err != nil {
					return err
				}
				if result.RemainingAccess != 0 {
					return fmt.Errorf("%w: %d relationships", ErrLifecycleAccessRemain, result.RemainingAccess)
				}
				if revokedAccessCount(result) > 0 {
					details, err := json.Marshal(map[string]interface{}{
						"reason": mutation.Reason, "closed_sessions": result.ClosedSessions,
						"revoked_leases": result.RevokedLeases, "revoked_harnesses": result.RevokedHarnesses,
						"revoked_devices": result.RevokedDevices, "revoked_enrollments": result.RevokedEnrollments,
						"revoked_entitlements":        result.RevokedEntitlements,
						"revoked_project_memberships": result.RevokedProjectMemberships,
						"revoked_console_access":      result.RevokedConsoleAccess,
					})
					if err != nil {
						return err
					}
					actorType := mutation.ActorType
					if actorType == "" {
						actorType = "system"
					}
					if err := tx.Create(&models.AuditEvent{
						OrganizationID: mutation.OrganizationID,
						EventType:      "cp.user.offboarding_reconciled",
						ActorID:        mutation.ActorID,
						ActorType:      actorType,
						Action:         "reconcile_offboarded_user_access",
						ResourceType:   "user",
						ResourceID:     mutation.UserID,
						Details:        string(details),
						Result:         "success",
						OccurredAt:     time.Now().UTC().Format(time.RFC3339),
					}).Error; err != nil {
						return err
					}
				}
			}
			return nil
		}
		edge, ok := models.UserLifecycleEdgeForTransition(user.Status, mutation.To)
		if !ok {
			return fmt.Errorf("%w: %s -> %s", ErrLifecycleInvalid, user.Status, mutation.To)
		}
		if mutation.To == models.UserStatusSuspended || mutation.To == models.UserStatusOffboarded {
			if err := protectLastAdministrator(tx, user); err != nil {
				return err
			}
		}
		updates := map[string]interface{}{
			"status":          mutation.To,
			"lifecycle_epoch": gorm.Expr("lifecycle_epoch + ?", 1),
		}
		if mutation.To == models.UserStatusOffboarded {
			today := time.Now().UTC().Format("2006-01-02")
			updates["offboarding_date"] = today
		}
		updated := tx.Model(&models.User{}).
			Where("id = ? AND organization_id = ? AND status = ?", mutation.UserID, mutation.OrganizationID, user.Status).
			Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrLifecycleStateChanged
		}

		if mutation.To == models.UserStatusOffboarded {
			if err := revokeUserAccess(tx, mutation.OrganizationID, mutation.UserID, result, lifecycle, &sessionOutcomes, mutation); err != nil {
				return err
			}
			if result.RemainingAccess != 0 {
				return fmt.Errorf("%w: %d relationships", ErrLifecycleAccessRemain, result.RemainingAccess)
			}
		} else if mutation.To == models.UserStatusSuspended {
			if err := revokeUserRuntimeAccess(tx, mutation.OrganizationID, mutation.UserID, result, lifecycle, &sessionOutcomes, mutation); err != nil {
				return err
			}
		}

		eventType, action := edge.EventType, edge.AuditAction
		if mutation.EventType != "" {
			eventType = mutation.EventType
		}
		if mutation.Action != "" {
			action = mutation.Action
		}
		details, err := json.Marshal(map[string]interface{}{
			"from": user.Status, "to": mutation.To, "reason": mutation.Reason,
			"closed_sessions": result.ClosedSessions, "revoked_leases": result.RevokedLeases,
			"revoked_harnesses": result.RevokedHarnesses, "revoked_devices": result.RevokedDevices,
			"revoked_enrollments": result.RevokedEnrollments, "revoked_entitlements": result.RevokedEntitlements,
			"revoked_project_memberships": result.RevokedProjectMemberships,
			"revoked_console_access":      result.RevokedConsoleAccess,
		})
		if err != nil {
			return err
		}
		actorType := mutation.ActorType
		if actorType == "" {
			actorType = "system"
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: mutation.OrganizationID,
			EventType:      eventType,
			ActorID:        mutation.ActorID,
			ActorType:      actorType,
			Action:         action,
			ResourceType:   "user",
			ResourceID:     mutation.UserID,
			Details:        string(details),
			Result:         "success",
			OccurredAt:     time.Now().UTC().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		// SQLite does not implement SELECT ... FOR UPDATE and reports a
		// concurrent writer as a lock error instead. Surface the same stable
		// lifecycle conflict contract used by the CAS miss; callers can reload
		// and re-derive available actions instead of treating contention as a
		// server failure.
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked") {
			return nil, ErrLifecycleStateChanged
		}
		return nil, err
	}
	if mutation.To == models.UserStatusOffboarded || mutation.To == models.UserStatusSuspended {
		finalized, finalizeErr := lifecycle.FinalizeTransitions(mutation.OrganizationID, sessionOutcomes, "terminated", "user_access_revoked", mutation.Reason, mutation.ActorID, mutation.ActorType)
		for _, outcome := range finalized {
			result.SessionCleanupFailures = append(result.SessionCleanupFailures, outcome.CleanupFailures...)
		}
		if finalizeErr != nil {
			return result, fmt.Errorf("%w: %v", ErrLifecycleCleanup, finalizeErr)
		}
	}

	result.User.Status = mutation.To
	result.User.LifecycleEpoch++
	if mutation.To == models.UserStatusOffboarded {
		today := time.Now().UTC().Format("2006-01-02")
		result.User.OffboardingDate = &today
	}
	return result, nil
}

func revokedAccessCount(result *UserLifecycleResult) int64 {
	return result.ClosedSessions + result.RevokedLeases + result.RevokedHarnesses +
		result.RevokedDevices + result.RevokedEnrollments + result.RevokedEntitlements +
		result.RevokedProjectMemberships + result.RevokedConsoleAccess
}

func isLifecycleAdminRole(role string) bool {
	switch role {
	case "owner", "admin", "super_admin":
		return true
	default:
		return false
	}
}

// protectLastAdministrator links a managed User to console credentials by
// normalized (organization,email), the relationship already used by token
// issuance. Credentials whose linked user is not active do not count.
func protectLastAdministrator(tx *gorm.DB, target *models.User) error {
	lastAdministrators, err := lastOrganizationAdministratorIDs(tx, []string{target.OrganizationID}, true)
	if err != nil {
		return err
	}
	if lastAdministrators[target.ID] {
		return ErrLifecycleLastAdmin
	}
	return nil
}

// LastOrganizationAdministratorIDs returns managed users whose lifecycle
// mutation would remove the final usable console administrator for their
// organization. It evaluates a set of organizations in two queries so list
// projections can use the same rule as the transactional mutation guard.
func LastOrganizationAdministratorIDs(db *gorm.DB, organizationIDs []string) (map[string]bool, error) {
	return lastOrganizationAdministratorIDs(db, organizationIDs, false)
}

func lastOrganizationAdministratorIDs(db *gorm.DB, organizationIDs []string, lock bool) (map[string]bool, error) {
	result := map[string]bool{}
	if len(organizationIDs) == 0 {
		return result, nil
	}
	var credentials []AdminCredentials
	credentialsQuery := db.Where("organization_id IN ? AND role IN ?", organizationIDs, []string{"owner", "admin", "super_admin"})
	if lock {
		credentialsQuery = credentialsQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := credentialsQuery.Find(&credentials).Error; err != nil {
		return nil, err
	}
	linkedUserIDs := make([]string, 0, len(credentials))
	legacyEmails := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		if credential.UserID != "" {
			linkedUserIDs = append(linkedUserIDs, credential.UserID)
		} else if credential.Email != "" {
			legacyEmails = append(legacyEmails, NormalizeEmail(credential.Email))
		}
	}
	var users []models.User
	userQuery := db.Where("organization_id IN ?", organizationIDs)
	switch {
	case len(linkedUserIDs) > 0 && len(legacyEmails) > 0:
		userQuery = userQuery.Where("id IN ? OR email IN ?", linkedUserIDs, legacyEmails)
	case len(linkedUserIDs) > 0:
		userQuery = userQuery.Where("id IN ?", linkedUserIDs)
	case len(legacyEmails) > 0:
		userQuery = userQuery.Where("email IN ?", legacyEmails)
	default:
		return result, nil
	}
	if err := userQuery.Find(&users).Error; err != nil {
		return nil, err
	}
	usersByID := make(map[string]models.User, len(users))
	usersByOrgEmail := make(map[string]models.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
		usersByOrgEmail[user.OrganizationID+"\x00"+NormalizeEmail(user.Email)] = user
	}
	adminUserIDsByOrg := map[string]map[string]struct{}{}
	activePrincipalsByOrg := map[string]int{}
	for _, credential := range credentials {
		if !isLifecycleAdminRole(credential.Role) {
			continue
		}
		linked, found := usersByID[credential.UserID]
		if !found && credential.UserID == "" {
			linked, found = usersByOrgEmail[credential.OrganizationID+"\x00"+NormalizeEmail(credential.Email)]
		}
		if !found {
			// Local/bootstrap console credentials without a managed-user row
			// remain a usable administrator and therefore prevent a false
			// last-admin denial for a different managed user.
			activePrincipalsByOrg[credential.OrganizationID]++
			continue
		}
		if adminUserIDsByOrg[credential.OrganizationID] == nil {
			adminUserIDsByOrg[credential.OrganizationID] = map[string]struct{}{}
		}
		if _, alreadyCounted := adminUserIDsByOrg[credential.OrganizationID][linked.ID]; !alreadyCounted {
			adminUserIDsByOrg[credential.OrganizationID][linked.ID] = struct{}{}
			if linked.Status == models.UserStatusActive {
				activePrincipalsByOrg[credential.OrganizationID]++
			}
		}
	}
	for orgID, userIDs := range adminUserIDsByOrg {
		for userID := range userIDs {
			linked := usersByID[userID]
			others := activePrincipalsByOrg[orgID]
			if linked.Status == models.UserStatusActive {
				others--
			}
			if others == 0 {
				result[userID] = true
			}
		}
	}
	return result, nil
}

func revokeUserAccess(tx *gorm.DB, orgID, userID string, result *UserLifecycleResult, lifecycle *sessionlifecycle.Service, sessionOutcomes *[]sessionlifecycle.Outcome, mutation UserLifecycleMutation) error {
	if err := revokeUserRuntimeAccess(tx, orgID, userID, result, lifecycle, sessionOutcomes, mutation); err != nil {
		return err
	}

	entitlements := tx.Where("organization_id = ? AND user_id = ?", orgID, userID).Delete(&models.UserRole{})
	if entitlements.Error != nil {
		return entitlements.Error
	}
	result.RevokedEntitlements = entitlements.RowsAffected

	projectMemberships := tx.Where("organization_id = ? AND user_id = ?", orgID, userID).Delete(&models.ProjectMember{})
	if projectMemberships.Error != nil {
		return projectMemberships.Error
	}
	result.RevokedProjectMemberships = projectMemberships.RowsAffected

	var target models.User
	if err := tx.Select("email").Where("id = ? AND organization_id = ?", userID, orgID).First(&target).Error; err != nil {
		return err
	}
	console := tx.Where("organization_id = ? AND (user_id = ? OR (user_id = '' AND email = ?))", orgID, userID, NormalizeEmail(target.Email)).Delete(&AdminCredentials{})
	if console.Error != nil {
		return console.Error
	}
	result.RevokedConsoleAccess = console.RowsAffected

	enrollments := tx.Model(&models.EnrollmentCode{}).
		Where("organization_id = ? AND user_id = ? AND used = ?", orgID, userID, false).
		Updates(map[string]interface{}{"used": true, "used_by": "revoked:user_offboarded"})
	if enrollments.Error != nil {
		return enrollments.Error
	}
	result.RevokedEnrollments = enrollments.RowsAffected

	var devices []models.Device
	if err := tx.Where("organization_id = ? AND user_id = ?", orgID, userID).Find(&devices).Error; err != nil {
		return err
	}
	deviceIDs := make(map[string]bool, len(devices))
	for _, device := range devices {
		deviceIDs[device.ID] = true
	}
	devicesResult := tx.Model(&models.Device{}).
		Where("organization_id = ? AND user_id = ? AND status != ?", orgID, userID, "revoked").
		Update("status", "revoked")
	if devicesResult.Error != nil {
		return devicesResult.Error
	}
	result.RevokedDevices = devicesResult.RowsAffected

	var harnesses []models.Harness
	harnessQuery := tx.Where("organization_id = ? AND allowed_users LIKE ?", orgID, "%\""+userID+"\"%")
	if len(deviceIDs) > 0 {
		ids := make([]string, 0, len(deviceIDs))
		for id := range deviceIDs {
			ids = append(ids, id)
		}
		harnessQuery = tx.Where("organization_id = ? AND (allowed_users LIKE ? OR device_id IN ?)", orgID, "%\""+userID+"\"%", ids)
	}
	if err := harnessQuery.Find(&harnesses).Error; err != nil {
		return err
	}
	for i := range harnesses {
		harness := &harnesses[i]
		var allowed []string
		if harness.AllowedUsers != "" {
			if err := json.Unmarshal([]byte(harness.AllowedUsers), &allowed); err != nil {
				// A malformed candidate cannot be proven not to contain the
				// departing user. Quarantine only that candidate; unrelated
				// malformed rows were not selected by the targeted query.
				if err := tx.Model(&models.Harness{}).Where("id = ? AND organization_id = ?", harness.ID, orgID).
					Updates(map[string]interface{}{"status": "revoked", "allowed_users": "[]", "revocation_reason": "malformed user binding during offboarding"}).Error; err != nil {
					return err
				}
				result.RevokedHarnesses++
				continue
			}
		}
		kept := make([]string, 0, len(allowed))
		removed := false
		for _, allowedUserID := range allowed {
			if allowedUserID == userID {
				removed = true
				continue
			}
			kept = append(kept, allowedUserID)
		}
		ownedDevice := deviceIDs[harness.DeviceID]
		if !removed && !ownedDevice {
			continue
		}
		encoded, err := json.Marshal(kept)
		if err != nil {
			return err
		}
		harnessUpdates := map[string]interface{}{"allowed_users": string(encoded)}
		if ownedDevice || (len(kept) == 0 && harness.DeviceID == "") {
			harnessUpdates["status"] = "revoked"
			harnessUpdates["revocation_reason"] = "user offboarded"
			result.RevokedHarnesses++
		}
		if err := tx.Model(&models.Harness{}).
			Where("id = ? AND organization_id = ?", harness.ID, orgID).
			Updates(harnessUpdates).Error; err != nil {
			return err
		}
	}

	checks := []struct {
		model interface{}
		where string
		args  []interface{}
	}{
		{&models.Session{}, "organization_id = ? AND user_id = ? AND status NOT IN ?", []interface{}{orgID, userID, models.SessionTerminalStatuses()}},
		{&models.CapabilityLease{}, "organization_id = ? AND user_id = ? AND status = ?", []interface{}{orgID, userID, "active"}},
		{&models.UserRole{}, "organization_id = ? AND user_id = ?", []interface{}{orgID, userID}},
		{&models.ProjectMember{}, "organization_id = ? AND user_id = ?", []interface{}{orgID, userID}},
		{&models.EnrollmentCode{}, "organization_id = ? AND user_id = ? AND used = ?", []interface{}{orgID, userID, false}},
		{&models.Device{}, "organization_id = ? AND user_id = ? AND status != ?", []interface{}{orgID, userID, "revoked"}},
		{&AdminCredentials{}, "organization_id = ? AND (user_id = ? OR (user_id = '' AND email = ?))", []interface{}{orgID, userID, NormalizeEmail(target.Email)}},
	}
	for _, check := range checks {
		var count int64
		if err := tx.Model(check.model).Where(check.where, check.args...).Count(&count).Error; err != nil {
			return err
		}
		result.RemainingAccess += count
	}
	var remainingHarnesses int64
	if err := tx.Model(&models.Harness{}).
		Where("organization_id = ? AND status != ? AND allowed_users LIKE ?", orgID, "revoked", "%\""+userID+"\"%").
		Count(&remainingHarnesses).Error; err != nil {
		return err
	}
	result.RemainingAccess += remainingHarnesses
	var remainingDeviceHarnesses int64
	if err := tx.Model(&models.Harness{}).
		Joins("JOIN devices ON devices.id = harnesses.device_id AND devices.organization_id = harnesses.organization_id").
		Where("harnesses.organization_id = ? AND harnesses.status != ? AND devices.user_id = ?", orgID, "revoked", userID).
		Count(&remainingDeviceHarnesses).Error; err != nil {
		return err
	}
	result.RemainingAccess += remainingDeviceHarnesses
	return nil
}

func revokeUserRuntimeAccess(tx *gorm.DB, orgID, userID string, result *UserLifecycleResult, lifecycle *sessionlifecycle.Service, sessionOutcomes *[]sessionlifecycle.Outcome, mutation UserLifecycleMutation) error {
	outcomes, err := lifecycle.TransitionScopeInTransaction(tx, sessionlifecycle.Scope{OrganizationID: orgID, UserID: userID, ForceTerminal: true, ActorType: mutation.ActorType}, "terminated", "user_access_revoked", mutation.Reason, mutation.ActorID)
	if err != nil {
		return err
	}
	for _, outcome := range outcomes {
		if outcome.Result != sessionlifecycle.ResultUpdated {
			return fmt.Errorf("identity: session %s could not be terminated (%s)", outcome.RequestedID, outcome.Result)
		}
	}
	*sessionOutcomes = append(*sessionOutcomes, outcomes...)
	result.ClosedSessions += int64(len(outcomes))

	leases := tx.Model(&models.CapabilityLease{}).
		Where("organization_id = ? AND user_id = ? AND status = ?", orgID, userID, "active").
		Update("status", "revoked")
	if leases.Error != nil {
		return leases.Error
	}
	result.RevokedLeases = leases.RowsAffected
	return nil
}
