package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ValidateActiveHarnessUserBinding proves that a human user and authenticated
// harness belong to the same tenant and that the harness is explicitly bound
// either to that user or to one of the user's active devices. Callers should
// run this inside the transaction that issues the resulting authority.
func ValidateActiveHarnessUserBinding(db *gorm.DB, orgID, harnessID, userID string) error {
	if _, err := LockActiveUser(db, orgID, userID); err != nil {
		return err
	}
	var harness models.Harness
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("organization_id = ? AND harness_id = ? AND status IN ?", orgID, harnessID, models.HarnessPermittedStatuses()).First(&harness).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: enrolled harness not found in organization", ErrHarnessUserBinding)
		}
		return err
	}
	allowed, err := decodeHarnessUsers(harness.AllowedUsers)
	if err != nil {
		return fmt.Errorf("%w: malformed harness user list: %v", ErrHarnessUserBinding, err)
	}
	for _, allowedUserID := range allowed {
		if allowedUserID == userID {
			return nil
		}
	}
	if harness.DeviceID != "" {
		var deviceCount int64
		if err := db.Model(&models.Device{}).
			Where("organization_id = ? AND id = ? AND user_id = ? AND status != ?", orgID, harness.DeviceID, userID, "revoked").
			Count(&deviceCount).Error; err != nil {
			return err
		}
		if deviceCount == 1 {
			return nil
		}
	}
	return fmt.Errorf("%w: user is not bound to harness", ErrHarnessUserBinding)
}

// GrantUserHarness serializes the JSON binding update with both the user's
// lifecycle row and the harness row, preventing lost updates and post-offboard
// grants. The relationship mutation and its audit event commit together.
func (s *Service) GrantUserHarness(orgID, userID, harnessID, actorID string) (*models.Harness, error) {
	var harness models.Harness
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := LockMutableUser(tx, orgID, userID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND organization_id = ?", harnessID, orgID).First(&harness).Error; err != nil {
			return err
		}
		allowed, err := decodeHarnessUsers(harness.AllowedUsers)
		if err != nil {
			return fmt.Errorf("identity: decode harness %s allowed users: %w", harness.ID, err)
		}
		for _, id := range allowed {
			if id == userID {
				return nil
			}
		}
		allowed = append(allowed, userID)
		raw, err := json.Marshal(allowed)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.Harness{}).Where("id = ? AND organization_id = ?", harness.ID, orgID).
			Update("allowed_users", string(raw)).Error; err != nil {
			return err
		}
		harness.AllowedUsers = string(raw)
		return s.recordAuditWithDB(tx, orgID, "cp.user.harness_granted", "admin", actorID, "user", userID,
			fmt.Sprintf(`{"harness_id":"%s"}`, harness.ID))
	})
	return &harness, err
}

// RevokeUserHarness removes one explicit binding under the same lock order as
// GrantUserHarness and lifecycle offboarding.
func (s *Service) RevokeUserHarness(orgID, userID, harnessID, actorID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := LockMutableUser(tx, orgID, userID); err != nil {
			return err
		}
		var harness models.Harness
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND organization_id = ?", harnessID, orgID).First(&harness).Error; err != nil {
			return err
		}
		allowed, err := decodeHarnessUsers(harness.AllowedUsers)
		if err != nil {
			return fmt.Errorf("identity: decode harness %s allowed users: %w", harness.ID, err)
		}
		kept := make([]string, 0, len(allowed))
		for _, id := range allowed {
			if id != userID {
				kept = append(kept, id)
			}
		}
		raw, err := json.Marshal(kept)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.Harness{}).Where("id = ? AND organization_id = ?", harness.ID, orgID).
			Update("allowed_users", string(raw)).Error; err != nil {
			return err
		}
		return s.recordAuditWithDB(tx, orgID, "cp.user.harness_revoked", "admin", actorID, "user", userID,
			fmt.Sprintf(`{"harness_id":"%s","at":"%s"}`, harness.ID, time.Now().UTC().Format(time.RFC3339)))
	})
}

func decodeHarnessUsers(raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	var users []string
	if err := json.Unmarshal([]byte(raw), &users); err != nil {
		return nil, err
	}
	return users, nil
}
