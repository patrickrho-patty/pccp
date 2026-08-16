package api

import (
	"net/http"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// portal_extra.go: web/24 — the self-service Account Portal. Access is
// keyed by the account's portal access token (issued once at creation,
// never listed in responses). The portal never exposes transferable
// API credentials (§6.6).

// portalToken extracts the bearer token from the Authorization header,
// falling back to the token query parameter (compat).
func portalToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("token")
}

// handlePortalSelf returns the portal's self-service view for the
// token's account: account + subscription + leases + cases + usage.
func (s *Server) handlePortalSelf(w http.ResponseWriter, r *http.Request) {
	token := portalToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "portal access token required")
		return
	}
	var account models.Account
	if err := s.db.Where("access_token = ?", token).First(&account).Error; err != nil {
		writeError(w, http.StatusUnauthorized, "invalid portal token")
		return
	}
	var sub models.Subscription
	s.db.Where("account_id = ? AND status = 'active'", account.ID).First(&sub)
	var leases []models.AccountCapacityLease
	s.db.Where("account_id = ?", account.ID).Order("created_at DESC").Limit(10).Find(&leases)
	var cases []models.SupportCase
	s.db.Where("account_id = ?", account.ID).Order("created_at DESC").Limit(10).Find(&cases)
	var usage []models.UsageRecord
	s.db.Where("harness_id IN (?)", s.db.Model(&models.Harness{}).Select("harness_id").
		Where("organization_id IN (?)", s.db.Model(&models.Subscription{}).Select("account_id").Where("account_id = ?", account.ID))).
		Limit(50).Find(&usage)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"account":       account,
		"subscription":  sub,
		"leases":        leases,
		"support_cases": cases,
		"usage_records": len(usage),
	})
}

// handlePortalSignOutAll revokes the account's capacity leases.
func (s *Server) handlePortalSignOutAll(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusUnauthorized, "portal access token required")
		return
	}
	var account models.Account
	if err := s.db.Where("access_token = ?", token).First(&account).Error; err != nil {
		writeError(w, http.StatusUnauthorized, "invalid portal token")
		return
	}
	n, err := s.ext().PublicCloud.SignOutAll(account.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "signed_out", "leases_revoked": n})
}

// handlePortalChangePlan upgrades/downgrades the subscription.
func (s *Server) handlePortalChangePlan(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusUnauthorized, "portal access token required")
		return
	}
	var account models.Account
	if err := s.db.Where("access_token = ?", token).First(&account).Error; err != nil {
		writeError(w, http.StatusUnauthorized, "invalid portal token")
		return
	}
	var req struct {
		Plan string `json:"plan"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Plan == "" {
		writeError(w, http.StatusBadRequest, "plan required")
		return
	}
	sub, err := s.ext().PublicCloud.ChangePlan(account.ID, req.Plan)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// handlePortalSupportCase files a support request from the portal.
func (s *Server) handlePortalSupportCase(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusUnauthorized, "portal access token required")
		return
	}
	var account models.Account
	if err := s.db.Where("access_token = ?", token).First(&account).Error; err != nil {
		writeError(w, http.StatusUnauthorized, "invalid portal token")
		return
	}
	var req struct {
		Subject     string `json:"subject"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Subject == "" {
		writeError(w, http.StatusBadRequest, "subject required")
		return
	}
	c := models.SupportCase{
		Base:      models.Base{ID: models.GenerateID("case")},
		AccountID: account.ID, Subject: req.Subject, Description: req.Description,
		Priority: "normal", Status: "open",
	}
	if err := s.db.Create(&c).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// handlePortalRotateToken rotates the portal access key.
func (s *Server) handlePortalRotateToken(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusUnauthorized, "portal access token required")
		return
	}
	var account models.Account
	if err := s.db.Where("access_token = ?", token).First(&account).Error; err != nil {
		writeError(w, http.StatusUnauthorized, "invalid portal token")
		return
	}
	newToken, err := s.ext().PublicCloud.RotatePortalToken(account.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"portal_token": newToken, "note": "이전 토큰은 즉시 무효화됩니다"})
}
