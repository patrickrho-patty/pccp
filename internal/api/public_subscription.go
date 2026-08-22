package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const publicSubscriptionGracePeriod = 7 * 24 * time.Hour

func effectivePublicSubscriptionWithDB(tx *gorm.DB, account *models.Account, now time.Time) (*models.Subscription, error) {
	if account == nil || account.ID == "" {
		return nil, &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("public account is unavailable")}
	}
	if account.AccountIntegrityState == "restricted" || account.TrustSafetyState == "restricted" || account.PlatformSecurityState == "blocked" {
		return nil, &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("account is not eligible for Harness credentials")}
	}
	var subscription models.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("account_id = ? AND status IN ?", account.ID, []string{"active", "grace"}).
		Order("created_at DESC").First(&subscription).Error; err != nil {
		return nil, &publicEnrollmentHTTPError{status: http.StatusPaymentRequired, err: fmt.Errorf("an active coding plan is required")}
	}
	expiresAt := strings.TrimSpace(subscription.ExpiresAt)
	if expiresAt == "" {
		return nil, &publicEnrollmentHTTPError{status: http.StatusPaymentRequired, err: fmt.Errorf("coding plan expiry is unavailable")}
	}
	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return nil, &publicEnrollmentHTTPError{status: http.StatusPaymentRequired, err: fmt.Errorf("coding plan expiry is invalid")}
	}
	deadline := expiry
	if subscription.Status == "grace" {
		deadline = expiry.Add(publicSubscriptionGracePeriod)
	}
	if !now.Before(deadline) {
		return nil, &publicEnrollmentHTTPError{status: http.StatusPaymentRequired, err: fmt.Errorf("the coding plan has ended")}
	}
	return &subscription, nil
}
