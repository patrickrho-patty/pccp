package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/publiccloud"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const publicAccountSyncBodyLimit = 256 << 10
const publicAccountSyncSchemaVersion = "pccp.public-account-sync.v1"

type publicAccountSyncIdentity struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

type publicAccountSyncRequest struct {
	SchemaVersion string `json:"schema_version"`
	SourceEventID string `json:"source_event_id"`
	AccountID     string `json:"account_id"`
	Revision      uint64 `json:"revision"`
	Profile       struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
	} `json:"profile"`
	Identities   []publicAccountSyncIdentity `json:"identities"`
	Subscription struct {
		Plan      string  `json:"plan"`
		Status    string  `json:"status"`
		ExpiresAt *string `json:"expires_at"`
	} `json:"subscription"`
}

func (s *Server) handleInternalPublicAccountSync(w http.ResponseWriter, r *http.Request) {
	if !requireAccountsService(w, r) {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, publicAccountSyncBodyLimit))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "account sync body exceeds 256 KiB")
		return
	}
	hmacKey := strings.TrimSpace(os.Getenv("PCCP_SYNC_HMAC_KEY"))
	if hmacKey == "" {
		writeError(w, http.StatusServiceUnavailable, "account sync integrity key is not configured")
		return
	}
	expected := hmac.New(sha256.New, []byte(hmacKey))
	_, _ = expected.Write(raw)
	provided, err := hex.DecodeString(strings.TrimSpace(r.Header.Get("X-Patty-Sync-Signature")))
	if err != nil || !hmac.Equal(provided, expected.Sum(nil)) {
		writeError(w, http.StatusUnauthorized, "account sync signature is invalid")
		return
	}
	var req publicAccountSyncRequest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid account sync: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "account sync must contain exactly one JSON object")
		return
	}
	if err := validatePublicAccountSync(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	digest := sha256.Sum256(raw)
	account, replay, err := s.syncPublicAccount(req, hex.EncodeToString(digest[:]))
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	status := "synchronized"
	if replay {
		status = "replayed"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"account_id": account.AuthorityID, "status": status})
}

func validatePublicAccountSync(req publicAccountSyncRequest) error {
	if req.SchemaVersion != publicAccountSyncSchemaVersion {
		return fmt.Errorf("unsupported account sync schema_version")
	}
	if strings.TrimSpace(req.SourceEventID) == "" || strings.TrimSpace(req.AccountID) == "" {
		return fmt.Errorf("source_event_id and account_id are required")
	}
	if !strings.HasPrefix(req.AccountID, "patty-account:") || strings.TrimPrefix(req.AccountID, "patty-account:") == "" {
		return fmt.Errorf("account_id must be a canonical Patty account identifier")
	}
	for _, digit := range strings.TrimPrefix(req.AccountID, "patty-account:") {
		if digit < '0' || digit > '9' {
			return fmt.Errorf("account_id must be a canonical Patty account identifier")
		}
	}
	if len(req.SourceEventID) > 128 || len(req.AccountID) > 64 || len(req.Profile.Email) > 255 || len(req.Profile.DisplayName) > 255 {
		return fmt.Errorf("account sync identifier or profile field is too long")
	}
	if len(req.Identities) > 20 {
		return fmt.Errorf("identities must contain 0..20 entries")
	}
	seen := map[string]bool{}
	previous := ""
	for _, external := range req.Identities {
		normalized := identity.NormalizeExternalIdentity(external.Issuer, external.Subject)
		if normalized.Issuer == "" || normalized.Subject == "" || len(normalized.Issuer) > 512 || len(normalized.Subject) > 255 {
			return fmt.Errorf("each identity requires a bounded issuer and subject")
		}
		key := normalized.Issuer + "\x00" + normalized.Subject
		if previous != "" && key < previous {
			return fmt.Errorf("identities must be sorted by issuer and subject")
		}
		if seen[key] {
			return fmt.Errorf("duplicate immutable identity")
		}
		seen[key] = true
		previous = key
	}
	status := strings.TrimSpace(req.Subscription.Status)
	switch status {
	case "active", "canceling", "past_due", "lapsed", "suspended":
	default:
		return fmt.Errorf("unsupported subscription status")
	}
	switch strings.TrimSpace(req.Subscription.Plan) {
	case "free", "developer", "pro", "team", "enterprise":
	default:
		return fmt.Errorf("unsupported subscription plan")
	}
	if req.Revision == 0 {
		return fmt.Errorf("revision must be a positive authority revision")
	}
	if req.Subscription.ExpiresAt != nil {
		if _, err := time.Parse(time.RFC3339, *req.Subscription.ExpiresAt); err != nil {
			return fmt.Errorf("subscription expires_at must be RFC3339")
		}
	}
	return nil
}

func publicAccountSubscriptionStatus(wire string) string {
	switch wire {
	case "active", "canceling":
		return "active"
	case "past_due":
		return "grace"
	case "lapsed":
		return "expired"
	default:
		return wire
	}
}

func (s *Server) syncPublicAccount(req publicAccountSyncRequest, payloadDigest string) (*models.Account, bool, error) {
	var account models.Account
	var replay bool
	err := s.db.Transaction(func(tx *gorm.DB) error {
		event := models.PublicAccountSyncEvent{SourceEventID: req.SourceEventID, AccountID: req.AccountID, PayloadDigest: payloadDigest, Revision: req.Revision}
		claimed := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_event_id"}}, DoNothing: true}).Create(&event)
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected == 0 {
			var persisted models.PublicAccountSyncEvent
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_event_id = ?", req.SourceEventID).First(&persisted).Error; err != nil {
				return err
			}
			if persisted.PayloadDigest != payloadDigest {
				return fmt.Errorf("source_event_id was reused with a different payload")
			}
			replay = true
			return tx.Where("authority_id = ?", req.AccountID).First(&account).Error
		}
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("authority_id = ?", strings.TrimSpace(req.AccountID)).First(&account).Error
		if errors.Is(lookup, gorm.ErrRecordNotFound) {
			first := identity.ExternalIdentity{}
			if len(req.Identities) > 0 {
				first = identity.NormalizeExternalIdentity(req.Identities[0].Issuer, req.Identities[0].Subject)
			}
			account = models.Account{
				Email: identity.NormalizeEmail(req.Profile.Email), DisplayName: strings.TrimSpace(req.Profile.DisplayName), Profile: "public",
				AuthorityID: strings.TrimSpace(req.AccountID), OAuthProvider: first.Issuer, OAuthSubject: first.Subject,
				AccountIntegrityState: "normal", TrustSafetyState: "normal", PlatformSecurityState: "normal", CapacityState: "normal",
				Locale: "ko-KR", Timezone: "Asia/Seoul",
			}
			if err := tx.Create(&account).Error; err != nil {
				return err
			}
		} else if lookup != nil {
			return lookup
		} else {
			updates := map[string]interface{}{}
			if email := identity.NormalizeEmail(req.Profile.Email); account.Email != email {
				updates["email"] = email
			}
			if name := strings.TrimSpace(req.Profile.DisplayName); account.DisplayName != name {
				updates["display_name"] = name
			}
			if len(updates) > 0 {
				if err := tx.Model(&account).Updates(updates).Error; err != nil {
					return err
				}
			}
		}

		var subscription models.Subscription
		subLookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ? AND payment_provider = ?", account.ID, "accounts-authority").First(&subscription).Error
		if subLookup == nil && subscription.AuthorityRevision > 0 {
			if subscription.AuthorityRevision >= req.Revision {
				return fmt.Errorf("stale account subscription sync")
			}
		} else if subLookup != nil && !errors.Is(subLookup, gorm.ErrRecordNotFound) {
			return subLookup
		}

		cfg := publiccloud.PlanConfigFor(strings.TrimSpace(req.Subscription.Plan))
		status := publicAccountSubscriptionStatus(req.Subscription.Status)
		expiresAt := ""
		if req.Subscription.ExpiresAt != nil {
			expiresAt = *req.Subscription.ExpiresAt
		}
		accountUpdates := map[string]interface{}{}
		if account.SubscriptionStatus != status {
			accountUpdates["subscription_status"] = status
		}
		if account.SubscriptionPlan != cfg.Name {
			accountUpdates["subscription_plan"] = cfg.Name
		}
		if account.SubscriptionExpiry != expiresAt {
			accountUpdates["subscription_expiry"] = expiresAt
		}
		if account.MaxHarnesses != cfg.MaxHarnesses {
			accountUpdates["max_harnesses"] = cfg.MaxHarnesses
		}
		if account.MaxActiveHarnesses != cfg.MaxActiveHarnesses {
			accountUpdates["max_active_harnesses"] = cfg.MaxActiveHarnesses
		}
		if account.NormalWorkSlots != cfg.NormalSlots {
			accountUpdates["normal_work_slots"] = cfg.NormalSlots
		}
		if account.HeavyWorkSlots != cfg.HeavySlots {
			accountUpdates["heavy_work_slots"] = cfg.HeavySlots
		}
		if len(accountUpdates) > 0 {
			if err := tx.Model(&account).Updates(accountUpdates).Error; err != nil {
				return err
			}
		}
		if errors.Is(subLookup, gorm.ErrRecordNotFound) {
			subscription = models.Subscription{AccountID: account.ID, Plan: cfg.Name, Status: status, ExpiresAt: expiresAt,
				AllowedModelClasses: cfg.AllowedModels, MaxHarnesses: cfg.MaxHarnesses, MaxActiveHarnesses: cfg.MaxActiveHarnesses,
				NormalWorkSlots: cfg.NormalSlots, HeavyWorkSlots: cfg.HeavySlots, Revision: fmt.Sprintf("%d", req.Revision), AuthorityRevision: req.Revision,
				PaymentProvider: "accounts-authority", PaymentID: req.SourceEventID}
			subscription.StartedAt = time.Now().UTC().Format(time.RFC3339)
			if err := tx.Create(&subscription).Error; err != nil {
				return err
			}
		} else {
			subscriptionUpdates := map[string]interface{}{"revision": fmt.Sprintf("%d", req.Revision), "authority_revision": req.Revision, "payment_id": req.SourceEventID}
			if subscription.Plan != cfg.Name {
				subscriptionUpdates["plan"] = cfg.Name
			}
			if subscription.Status != status {
				subscriptionUpdates["status"] = status
			}
			if subscription.ExpiresAt != expiresAt {
				subscriptionUpdates["expires_at"] = expiresAt
			}
			if subscription.AllowedModelClasses != cfg.AllowedModels {
				subscriptionUpdates["allowed_model_classes"] = cfg.AllowedModels
			}
			if subscription.MaxHarnesses != cfg.MaxHarnesses {
				subscriptionUpdates["max_harnesses"] = cfg.MaxHarnesses
			}
			if subscription.MaxActiveHarnesses != cfg.MaxActiveHarnesses {
				subscriptionUpdates["max_active_harnesses"] = cfg.MaxActiveHarnesses
			}
			if subscription.NormalWorkSlots != cfg.NormalSlots {
				subscriptionUpdates["normal_work_slots"] = cfg.NormalSlots
			}
			if subscription.HeavyWorkSlots != cfg.HeavySlots {
				subscriptionUpdates["heavy_work_slots"] = cfg.HeavySlots
			}
			if err := tx.Model(&subscription).Updates(subscriptionUpdates).Error; err != nil {
				return err
			}
		}

		var existing []models.AccountExternalIdentity
		if err := tx.Where("account_id = ?", account.ID).Find(&existing).Error; err != nil {
			return err
		}
		wanted := make(map[string]publicAccountSyncIdentity, len(req.Identities))
		for _, supplied := range req.Identities {
			external := identity.NormalizeExternalIdentity(supplied.Issuer, supplied.Subject)
			wanted[external.Issuer+"\x00"+external.Subject] = publicAccountSyncIdentity{Issuer: external.Issuer, Subject: external.Subject}
		}
		for _, bound := range existing {
			key := bound.Issuer + "\x00" + bound.Subject
			if _, ok := wanted[key]; ok {
				delete(wanted, key)
				continue
			}
			if err := tx.Unscoped().Delete(&bound).Error; err != nil {
				return err
			}
		}
		missing := make([]models.AccountExternalIdentity, 0, len(wanted))
		for _, supplied := range wanted {
			external := identity.NormalizeExternalIdentity(supplied.Issuer, supplied.Subject)
			missing = append(missing, models.AccountExternalIdentity{AccountID: account.ID, Issuer: external.Issuer, Subject: external.Subject})
		}
		if len(missing) > 0 {
			if err := tx.Create(&missing).Error; err != nil {
				return fmt.Errorf("immutable identity is already bound to another account: %w", err)
			}
		}
		return nil
	})
	return &account, replay, err
}
