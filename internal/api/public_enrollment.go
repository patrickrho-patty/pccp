package api

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sso"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const publicEnrollmentGrantLifetime = 10 * time.Minute

type publicEnrollmentHTTPError struct {
	status int
	err    error
}

func (e *publicEnrollmentHTTPError) Error() string { return e.err.Error() }
func (e *publicEnrollmentHTTPError) Unwrap() error { return e.err }

func (s *Server) handlePublicHarnessEnrollmentGrant(w http.ResponseWriter, r *http.Request) {
	release, allowed := s.ext().SSO.BeginPublicRequest(publicSSORateKey(r, "public-harness-enrollment"))
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "public enrollment request rate exceeded")
		return
	}
	defer release()
	if s.publicTokenVerifier == nil {
		writeError(w, http.StatusServiceUnavailable, "public OIDC enrollment is not configured")
		return
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")) == "" {
		writeError(w, http.StatusUnauthorized, "first-party access token required")
		return
	}
	claims, err := s.publicTokenVerifier.VerifyAccessToken(r.Context(), strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")))
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if claims == nil || !claims.EmailVerified || strings.TrimSpace(claims.Subject) == "" || strings.TrimSpace(claims.Issuer) == "" {
		writeError(w, http.StatusUnauthorized, "verified first-party identity required")
		return
	}
	if !claims.HasScope("harness-enroll") {
		writeError(w, http.StatusForbidden, "harness-enroll scope required")
		return
	}
	var req publicHarnessEnrollmentGrantRequestV1
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.HarnessID) == "" {
		writeError(w, http.StatusBadRequest, "harness_id and public_key_hex are required")
		return
	}
	publicKey, err := hex.DecodeString(strings.TrimSpace(req.PublicKeyHex))
	if err != nil || len(publicKey) != 32 {
		writeError(w, http.StatusBadRequest, "public_key_hex must be an Ed25519 public key")
		return
	}
	if strings.TrimSpace(req.Organization) != "" {
		s.handleEnterpriseHarnessEnrollmentGrant(w, claims, req)
		return
	}

	var account models.Account
	var user models.User
	var subscription models.Subscription
	var enrollmentCode string
	expiresAt := time.Now().UTC().Add(publicEnrollmentGrantLifetime)
	err = s.db.Transaction(func(tx *gorm.DB) error {
		issuer, subject := identity.NormalizeIssuer(claims.Issuer), identity.NormalizeExternalSubject(claims.Subject)
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Joins("JOIN account_external_identities ON account_external_identities.account_id = accounts.id").
			Where("account_external_identities.issuer = ? AND account_external_identities.subject = ?", issuer, subject).First(&account).Error
		if errors.Is(lookup, gorm.ErrRecordNotFound) {
			return &publicEnrollmentHTTPError{status: http.StatusNotFound, err: fmt.Errorf("no Patty subscription account matches this verified identity")}
		}
		if lookup != nil {
			return lookup
		}
		effective, err := effectivePublicSubscriptionWithDB(tx, &account, time.Now().UTC())
		if err != nil {
			return err
		}
		subscription = *effective
		seatLimit := subscription.MaxHarnesses
		if seatLimit <= 0 {
			seatLimit = account.MaxHarnesses
		}
		if seatLimit <= 0 {
			return &publicEnrollmentHTTPError{status: http.StatusPaymentRequired, err: fmt.Errorf("Harness seat limit is unavailable for this plan")}
		}

		org := models.Organization{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&org, "id = ?", account.ID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			org = models.Organization{
				Base: models.Base{ID: account.ID}, Name: account.DisplayName, Slug: "public-" + account.ID,
				Profile: "public", Type: "public", Status: "active", MaxUserSeats: 1, MaxHarnessSeats: seatLimit,
				PlanTier: subscription.Plan,
			}
			if strings.TrimSpace(org.Name) == "" {
				org.Name = account.Email
			}
			if err := tx.Create(&org).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			updates := map[string]interface{}{}
			if org.MaxHarnessSeats != seatLimit {
				updates["max_harness_seats"] = seatLimit
				org.MaxHarnessSeats = seatLimit
			}
			if org.PlanTier != subscription.Plan {
				updates["plan_tier"] = subscription.Plan
				org.PlanTier = subscription.Plan
			}
			if len(updates) > 0 {
				if err := tx.Model(&org).Updates(updates).Error; err != nil {
					return err
				}
			}
		}
		if err := identity.RequireHarnessSeatWithDB(tx, org); err != nil {
			if errors.Is(err, identity.ErrHarnessSeatLimit) {
				return &publicEnrollmentHTTPError{status: http.StatusPaymentRequired, err: err}
			}
			return err
		}

		canonical := identity.PublicAccountIdentity(account.AuthorityID)
		resolved, resolveErr := identity.FindUserByExternalIdentity(tx.Clauses(clause.Locking{Strength: "UPDATE"}), account.ID, identity.PublicAccountAuthMethod, canonical)
		if errors.Is(resolveErr, gorm.ErrRecordNotFound) {
			user = models.User{
				AuditBase: models.AuditBase{OrganizationID: account.ID}, Email: identity.NormalizeEmail(claims.Email),
				Name: account.DisplayName, Status: models.UserStatusActive, AuthMethod: identity.PublicAccountAuthMethod,
				ExternalID: canonical.Subject, ExternalIssuer: canonical.Issuer, ExternalIssuerVerified: true,
				Locale: account.Locale, Timezone: account.Timezone,
			}
			admitted, _, err := identity.AdmitExternalUserWithDB(tx, &user, false)
			if err != nil {
				return err
			}
			user = *admitted
		} else if resolveErr != nil {
			return resolveErr
		} else {
			user = *resolved
		}
		if user.Status != models.UserStatusActive {
			return &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("public account user is not active")}
		}
		enrollmentCode, err = s.identity.GenerateBoundEnrollmentCodeWithDB(tx, account.ID, user.ID, req.HarnessID, req.PublicKeyHex, publicEnrollmentGrantLifetime)
		if err != nil {
			return err
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: account.ID, EventType: "cp.harness.public_enrollment_grant_issued", ActorID: user.ID, ActorType: "user",
			Action: "issue_public_enrollment_grant", ResourceType: "harness", ResourceID: req.HarnessID,
			Details: fmt.Sprintf("plan: %s", subscription.Plan), Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		var httpErr *publicEnrollmentHTTPError
		if errors.As(err, &httpErr) {
			writeError(w, httpErr.status, httpErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, publicHarnessEnrollmentGrantResponseV1{
		EnrollmentCode: enrollmentCode, OrganizationID: account.ID, UserID: user.ID,
		Plan: subscription.Plan, ExpiresAt: expiresAt.Format(time.RFC3339),
	})
}

func (s *Server) handleEnterpriseHarnessEnrollmentGrant(w http.ResponseWriter, claims *sso.FirstPartyAccessClaims, req publicHarnessEnrollmentGrantRequestV1) {
	var org models.Organization
	var user models.User
	var code string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		selector := strings.TrimSpace(req.Organization)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? OR slug = ?", selector, selector).First(&org).Error; err != nil {
			return &publicEnrollmentHTTPError{status: http.StatusNotFound, err: fmt.Errorf("enterprise organization not found")}
		}
		if org.Status != "active" || org.Profile == "public" {
			return &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("enterprise organization is not eligible for enrollment")}
		}
		target := identity.NormalizeExternalIdentity(claims.Issuer, claims.Subject)
		linked, err := identity.FindActiveUserByLinkedTargetIdentity(tx, org.ID, target)
		if err != nil {
			return &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("verified identity is not a member of this organization")}
		}
		user = *linked
		if user.Status != models.UserStatusActive {
			return &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("enterprise user is not active")}
		}
		if err := identity.RequireHarnessSeatWithDB(tx, org); err != nil {
			if errors.Is(err, identity.ErrHarnessSeatLimit) {
				return &publicEnrollmentHTTPError{status: http.StatusPaymentRequired, err: err}
			}
			return err
		}
		code, err = s.identity.GenerateBoundEnrollmentCodeWithDB(tx, org.ID, user.ID, req.HarnessID, req.PublicKeyHex, publicEnrollmentGrantLifetime)
		if err != nil {
			return err
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: org.ID, EventType: "cp.harness.enterprise_enrollment_grant_issued", ActorID: user.ID, ActorType: "user",
			Action: "issue_enterprise_enrollment_grant", ResourceType: "harness", ResourceID: req.HarnessID,
			Details: fmt.Sprintf("organization: %s", org.Slug), Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		var httpErr *publicEnrollmentHTTPError
		if errors.As(err, &httpErr) {
			writeError(w, httpErr.status, httpErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, publicHarnessEnrollmentGrantResponseV1{
		EnrollmentCode: code, OrganizationID: org.ID, UserID: user.ID,
		Plan: org.PlanTier, ExpiresAt: time.Now().UTC().Add(publicEnrollmentGrantLifetime).Format(time.RFC3339),
	})
}

// handlePublicHarnessEnrollment exposes the existing one-time-code redemption
// path without granting a console session. The code remains the authority and
// is checked atomically against tenant, user, Harness ID, and public key.
func (s *Server) handlePublicHarnessEnrollment(w http.ResponseWriter, r *http.Request) {
	var wireRequest publicHarnessEnrollmentRequestV1
	if err := decodeJSON(r, &wireRequest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req := identity.EnrollHarnessRequest{
		OrganizationID: wireRequest.OrganizationID, UserID: wireRequest.UserID, EnrollmentCode: wireRequest.EnrollmentCode,
		HarnessID: wireRequest.HarnessID, PublicKeyHex: wireRequest.PublicKeyHex, BinaryVersion: wireRequest.BinaryVersion,
		BinaryHash: wireRequest.BinaryHash, ExtensionVersion: wireRequest.ExtensionVersion, CLIVersion: wireRequest.CLIVersion,
		DeviceHostname: wireRequest.DeviceHostname, DeviceOS: wireRequest.DeviceOS, DeviceOSVersion: wireRequest.DeviceOSVersion,
		DeviceArch: wireRequest.DeviceArch,
	}
	if strings.TrimSpace(req.EnrollmentCode) == "" || strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.UserID) == "" {
		writeError(w, http.StatusBadRequest, "organization_id, user_id, and enrollment_code are required")
		return
	}
	_, credential, err := s.enrollHarnessOperation(req, req.UserID)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, identity.ErrHarnessSeatLimit):
			status = http.StatusPaymentRequired
		case errors.Is(err, identity.ErrUserNotActive), errors.Is(err, identity.ErrUserNotFound), errors.Is(err, identity.ErrEnrollmentPolicyDenied), errors.Is(err, identity.ErrEnrollmentCodeBinding):
			status = http.StatusForbidden
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, publicHarnessEnrollmentResponseV1{Credential: publicHarnessCredentialV1{SignedCredential: credential.SignedCredential}})
}

func (s *Server) handlePublicHarnessRenewal(w http.ResponseWriter, r *http.Request) {
	var req publicHarnessRenewalRequestV1
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.HarnessID) == "" || strings.TrimSpace(req.CredentialHash) == "" {
		writeError(w, http.StatusBadRequest, "complete Harness renewal proof is required")
		return
	}
	signedAt, err := time.Parse(time.RFC3339Nano, req.SignedAt)
	if err != nil || time.Since(signedAt).Abs() > 5*time.Minute {
		writeError(w, http.StatusUnauthorized, "Harness renewal proof is outside the allowed time window")
		return
	}
	signature, err := hex.DecodeString(req.SignatureHex)
	if err != nil || len(signature) != ed25519.SignatureSize {
		writeError(w, http.StatusUnauthorized, "Harness renewal signature is invalid")
		return
	}
	message := identity.HarnessRenewalSigningBytes(req.HarnessID, req.CredentialHash, req.SignedAt)
	harness, err := s.identity.VerifyHarnessAuth(req.HarnessID, signature, message)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Harness renewal proof is invalid")
		return
	}
	current, err := hex.DecodeString(harness.CredentialJSON)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Harness credential is invalid")
		return
	}
	digest := sha256.Sum256(current)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), req.CredentialHash) {
		writeError(w, http.StatusConflict, "Harness credential has already changed")
		return
	}
	credential, err := s.identity.RenewHarnessCredentialWithAdmission(req.HarnessID, req.CredentialHash, func(tx *gorm.DB, org *models.Organization, user *models.User, lockedHarness *models.Harness) error {
		if org.Status != "active" {
			return &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("Harness organization is unavailable")}
		}
		var allowedUsers []string
		if err := json.Unmarshal([]byte(lockedHarness.AllowedUsers), &allowedUsers); err != nil || len(allowedUsers) == 0 || strings.TrimSpace(allowedUsers[0]) == "" {
			return &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("Harness user binding is unavailable")}
		}
		if user == nil || user.ID != allowedUsers[0] {
			return &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("Harness user is not active")}
		}
		if _, err := identity.RequireHarnessEntitlementWithDB(tx, *org); err != nil {
			return &publicEnrollmentHTTPError{status: http.StatusForbidden, err: err}
		}
		if org.Profile == "public" {
			var account models.Account
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ?", org.ID).Error; err != nil {
				return &publicEnrollmentHTTPError{status: http.StatusForbidden, err: fmt.Errorf("public account is unavailable")}
			}
			_, err = effectivePublicSubscriptionWithDB(tx, &account, time.Now().UTC())
			return err
		}
		return nil
	})
	if err != nil {
		var httpErr *publicEnrollmentHTTPError
		if errors.As(err, &httpErr) {
			writeError(w, httpErr.status, httpErr.Error())
		} else {
			writeError(w, http.StatusConflict, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, publicHarnessRenewalResponseV1{
		Credential: publicHarnessCredentialV1{SignedCredential: credential.SignedCredential},
	})
}
