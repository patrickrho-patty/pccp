package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func requireAccountsService(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("PCCP_ACCOUNTS_SERVICE_TOKEN"))
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if expected == "" || len(expected) != len(provided) || subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 {
		writeError(w, http.StatusUnauthorized, "accounts service authentication required")
		return false
	}
	return true
}

func (s *Server) publicAccountByAuthority(accountID string, issuers, subjects []string) (*models.Account, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var account models.Account
	if err := s.db.Where("authority_id = ?", accountID).First(&account).Error; err == nil {
		return &account, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if len(issuers) == 0 || len(issuers) != len(subjects) {
		return nil, gorm.ErrRecordNotFound
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		conditions := make([]string, 0, len(issuers))
		args := make([]interface{}, 0, len(issuers)*2)
		for i := range issuers {
			issuer := identity.NormalizeIssuer(issuers[i])
			subject := identity.NormalizeExternalSubject(subjects[i])
			if issuer == "" || subject == "" {
				continue
			}
			conditions = append(conditions, "(account_external_identities.issuer = ? AND account_external_identities.subject = ?)")
			args = append(args, issuer, subject)
		}
		if len(conditions) == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Joins("JOIN account_external_identities ON account_external_identities.account_id = accounts.id").
			Where(strings.Join(conditions, " OR "), args...).First(&account).Error; err != nil {
			return err
		}
		if account.AuthorityID != "" && account.AuthorityID != accountID {
			return fmt.Errorf("public account authority is already bound")
		}
		if account.AuthorityID == accountID {
			return nil
		}
		result := tx.Model(&models.Account{}).Where("id = ? AND authority_id = ''", account.ID).Update("authority_id", accountID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("public account authority binding changed")
		}
		account.AuthorityID = accountID
		return nil
	})
	return &account, err
}

func publicAccountAuthorityRequest(r *http.Request) (string, []string, []string) {
	query := r.URL.Query()
	return query.Get("account_id"), query["issuer"], query["subject"]
}

func (s *Server) handleInternalPublicHarnesses(w http.ResponseWriter, r *http.Request) {
	if !requireAccountsService(w, r) {
		return
	}
	accountID, issuers, subjects := publicAccountAuthorityRequest(r)
	account, err := s.publicAccountByAuthority(accountID, issuers, subjects)
	if err != nil {
		writeError(w, http.StatusNotFound, "public account not found")
		return
	}
	var harnesses []models.Harness
	limit := 100
	if requested, parseErr := strconv.Atoi(r.URL.Query().Get("limit")); parseErr == nil && requested > 0 {
		if requested < limit {
			limit = requested
		}
	}
	offset := 0
	if requested, parseErr := strconv.Atoi(r.URL.Query().Get("cursor")); parseErr == nil && requested > 0 {
		offset = requested
	}
	if err := s.db.Where("organization_id = ?", account.ID).Order("created_at DESC, id DESC").Offset(offset).Limit(limit + 1).Find(&harnesses).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := len(harnesses) > limit
	if hasMore {
		harnesses = harnesses[:limit]
	}
	var subscription models.Subscription
	_ = s.db.Where("account_id = ?", account.ID).Order("created_at DESC").First(&subscription).Error
	type harnessView struct {
		ID               string `json:"id"`
		HarnessID        string `json:"harness_id"`
		Hostname         string `json:"hostname,omitempty"`
		OS               string `json:"os,omitempty"`
		Arch             string `json:"arch,omitempty"`
		BinaryVersion    string `json:"binary_version,omitempty"`
		Status           string `json:"status"`
		EnrolledAt       string `json:"enrolled_at,omitempty"`
		LastHeartbeat    string `json:"last_heartbeat,omitempty"`
		RevocationReason string `json:"revocation_reason,omitempty"`
	}
	views := make([]harnessView, 0, len(harnesses))
	deviceIDs := make([]string, 0, len(harnesses))
	for _, harness := range harnesses {
		if harness.DeviceID != "" {
			deviceIDs = append(deviceIDs, harness.DeviceID)
		}
	}
	devicesByID := make(map[string]models.Device, len(deviceIDs))
	if len(deviceIDs) > 0 {
		var devices []models.Device
		if err := s.db.Where("organization_id = ? AND id IN ?", account.ID, deviceIDs).Find(&devices).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, device := range devices {
			devicesByID[device.ID] = device
		}
	}
	for _, harness := range harnesses {
		device := devicesByID[harness.DeviceID]
		views = append(views, harnessView{
			ID: harness.ID, HarnessID: harness.HarnessID, Hostname: device.Hostname, OS: device.OS, Arch: device.Arch,
			BinaryVersion: harness.BinaryVersion, Status: harness.Status, EnrolledAt: harness.EnrolledAt,
			LastHeartbeat: harness.LastHeartbeat, RevocationReason: harness.RevocationReason,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"harnesses": views,
		"plan":      map[string]interface{}{"name": subscription.Plan, "status": subscription.Status, "max_harnesses": subscription.MaxHarnesses},
		"has_more":  hasMore,
		"next_cursor": func() interface{} {
			if hasMore {
				return strconv.Itoa(offset + limit)
			}
			return nil
		}(),
	})
}

func (s *Server) handleInternalPublicHarnessRevoke(w http.ResponseWriter, r *http.Request) {
	if !requireAccountsService(w, r) {
		return
	}
	accountID, issuers, subjects := publicAccountAuthorityRequest(r)
	account, err := s.publicAccountByAuthority(accountID, issuers, subjects)
	if err != nil {
		writeError(w, http.StatusNotFound, "public account not found")
		return
	}
	harnessID := chi.URLParam(r, "id")
	var harness models.Harness
	if err := s.db.Where("organization_id = ? AND (id = ? OR harness_id = ?)", account.ID, harnessID, harnessID).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "Harness not found")
		return
	}
	if err := s.identity.RevokeHarnessByActor(account.ID, harness.HarnessID, "revoked by account owner", "accounts-worker"); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "revoked", "revoked_at": time.Now().UTC().Format(time.RFC3339)})
}
