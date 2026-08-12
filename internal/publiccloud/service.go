package publiccloud

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/paper"
	"gorm.io/gorm"
)

// Service implements Patty Public Cloud operations (PCCP v2 §10C).
// Handles subscriptions, fair use, capacity, account integrity, and SRE.
type Service struct {
	db         *gorm.DB
	signingKey ed25519.PrivateKey
	mu         sync.Mutex
	// In-memory active slots tracking (production: sharded by account ID)
	activeSlots map[string]*SlotTracker // accountID → tracker
}

// SlotTracker tracks active work slots for an account.
type SlotTracker struct {
	AccountID      string
	Interactive    int
	Subagent       int
	HeavyContext   int
	Background     int
	LastActivity   time.Time
}

// New creates a new public cloud service.
func New(db *gorm.DB) (*Service, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("publiccloud: generate signing key: %w", err)
	}
	return &Service{
		db:          db,
		signingKey:  priv,
		activeSlots: make(map[string]*SlotTracker),
	}, nil
}

// CreateAccount creates a new Public Cloud account (§8.2).
func (s *Service) CreateAccount(email, displayName, displayNameKo, plan string) (*models.Account, error) {
	account := &models.Account{
		Email:         email,
		DisplayName:   displayName,
		DisplayNameKo: displayNameKo,
		Profile:       "public",
		SubscriptionStatus: "none",
		Locale:         "ko-KR",
		Timezone:       "Asia/Seoul",
		MaxHarnesses:       3,
		MaxActiveHarnesses: 2,
		NormalWorkSlots:    5,
		HeavyWorkSlots:     2,
		BackgroundSlots:    2,
	}
	if err := s.db.Create(account).Error; err != nil {
		return nil, fmt.Errorf("publiccloud: create account: %w", err)
	}

	// Create subscription if plan specified
	if plan != "" {
		s.CreateSubscription(account.ID, plan)
	}

	return account, nil
}

// CreateSubscription creates or updates a subscription (§10C.2).
func (s *Service) CreateSubscription(accountID, plan string) (*models.Subscription, error) {
	// Plan defaults
	planConfig := getPlanConfig(plan)

	now := time.Now()
	sub := &models.Subscription{
		AccountID:           accountID,
		Plan:                plan,
		Status:              "active",
		StartedAt:           now.Format(time.RFC3339),
		ExpiresAt:           now.AddDate(1, 0, 0).Format(time.RFC3339), // 1 year
		AllowedModelClasses: planConfig.AllowedModels,
		MaxHarnesses:        planConfig.MaxHarnesses,
		MaxActiveHarnesses:  planConfig.MaxActiveHarnesses,
		NormalWorkSlots:     planConfig.NormalSlots,
		HeavyWorkSlots:      planConfig.HeavySlots,
		Revision:            paper.GenerateID("sub_rev"),
	}
	if err := s.db.Create(sub).Error; err != nil {
		return nil, fmt.Errorf("publiccloud: create subscription: %w", err)
	}

	// Update account
	s.db.Model(&models.Account{}).Where("id = ?", accountID).Updates(map[string]interface{}{
		"subscription_status":  "active",
		"subscription_plan":     plan,
		"subscription_expiry":   sub.ExpiresAt,
		"max_harnesses":         planConfig.MaxHarnesses,
		"max_active_harnesses":  planConfig.MaxActiveHarnesses,
		"normal_work_slots":     planConfig.NormalSlots,
		"heavy_work_slots":      planConfig.HeavySlots,
	})

	return sub, nil
}

// GetSubscription returns the active subscription for an account.
func (s *Service) GetSubscription(accountID string) (*models.Subscription, error) {
	var sub models.Subscription
	if err := s.db.Where("account_id = ? AND status = 'active'", accountID).
		Order("created_at DESC").First(&sub).Error; err != nil {
		return nil, fmt.Errorf("publiccloud: no active subscription")
	}

	// Check expiry
	if sub.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339, sub.ExpiresAt)
		if err == nil && time.Now().After(expiry) {
			// Check grace period (7 days)
			graceEnd := expiry.AddDate(0, 0, 7)
			if time.Now().After(graceEnd) {
				s.db.Model(&sub).Update("status", "expired")
				s.db.Model(&models.Account{}).Where("id = ?", accountID).
					Update("subscription_status", "expired")
				return nil, fmt.Errorf("publiccloud: subscription expired")
			}
			s.db.Model(&models.Account{}).Where("id = ?", accountID).
				Update("subscription_status", "grace")
		}
	}

	return &sub, nil
}

// CheckHarnessLimit verifies if a new Harness can be enrolled (§10C.3, §8.5).
func (s *Service) CheckHarnessLimit(accountID string) (bool, error) {
	sub, err := s.GetSubscription(accountID)
	if err != nil {
		return false, err
	}

	var count int64
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status IN ('active','enrolled')", accountID).Count(&count)

	return int(count) < sub.MaxHarnesses, nil
}

// IssueCapacityLease creates an Account Capacity Lease (§10C.5).
// This allows Relays to admit work locally without DB round trips.
func (s *Service) IssueCapacityLease(accountID string) (*models.AccountCapacityLease, error) {
	sub, err := s.GetSubscription(accountID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	lease := &models.AccountCapacityLease{
		AccountID:           accountID,
		EntitlementRevision: sub.Revision,
		ActiveAgentSlots:    sub.NormalWorkSlots,
		HeavySlots:          sub.HeavyWorkSlots,
		BackgroundSlots:     2,
		PriorityWeight:      getPlanPriority(sub.Plan),
		ValidUntil:          now.Add(5 * time.Minute).Format(time.RFC3339), // 5-minute TTL
		Status:              "active",
	}

	// Sign
	leaseData := fmt.Sprintf("%s|%s|%d|%d|%s", lease.AccountID, lease.EntitlementRevision,
		lease.ActiveAgentSlots, lease.HeavySlots, lease.ValidUntil)
	sig := ed25519.Sign(s.signingKey, []byte(leaseData))
	lease.CPSignature = hex.EncodeToString(sig)

	if err := s.db.Create(lease).Error; err != nil {
		return nil, fmt.Errorf("publiccloud: create capacity lease: %w", err)
	}
	return lease, nil
}

// ValidateCapacityLease checks if a capacity lease is valid (§10C.6).
func (s *Service) ValidateCapacityLease(leaseID, accountID string) (*models.AccountCapacityLease, error) {
	var lease models.AccountCapacityLease
	if err := s.db.Where("lease_id = ? AND account_id = ? AND status = 'active'",
		leaseID, accountID).First(&lease).Error; err != nil {
		// Fallback: find by account ID (latest active)
		if err2 := s.db.Where("account_id = ? AND status = 'active'",
			accountID).Order("created_at DESC").First(&lease).Error; err2 != nil {
			return nil, fmt.Errorf("publiccloud: no valid capacity lease")
		}
	}

	validUntil, _ := time.Parse(time.RFC3339, lease.ValidUntil)
	if time.Now().After(validUntil) {
		s.db.Model(&lease).Update("status", "expired")
		return nil, fmt.Errorf("publiccloud: capacity lease expired")
	}

	return &lease, nil
}

// AcquireWorkSlot attempts to acquire a work slot (§10C.3).
type WorkSlotClass string

const (
	SlotInteractive  WorkSlotClass = "INTERACTIVE"
	SlotSubagent     WorkSlotClass = "SUBAGENT"
	SlotHeavyContext WorkSlotClass = "HEAVY_CONTEXT"
	SlotBackground   WorkSlotClass = "BACKGROUND"
)

// AcquireWorkSlot checks if an agent work slot is available.
func (s *Service) AcquireWorkSlot(accountID string, class WorkSlotClass) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tracker, ok := s.activeSlots[accountID]
	if !ok {
		tracker = &SlotTracker{AccountID: accountID}
		s.activeSlots[accountID] = tracker
	}

	sub, err := s.GetSubscription(accountID)
	if err != nil {
		return false, err
	}

	totalSlots := tracker.Interactive + tracker.Subagent + tracker.HeavyContext + tracker.Background
	if totalSlots >= sub.NormalWorkSlots+sub.HeavyWorkSlots {
		return false, nil // At capacity
	}

	switch class {
	case SlotInteractive:
		tracker.Interactive++
	case SlotSubagent:
		tracker.Subagent++
	case SlotHeavyContext:
		if tracker.HeavyContext >= sub.HeavyWorkSlots {
			return false, nil
		}
		tracker.HeavyContext++
	case SlotBackground:
		tracker.Background++
	}
	tracker.LastActivity = time.Now()

	return true, nil
}

// ReleaseWorkSlot releases a previously acquired work slot.
func (s *Service) ReleaseWorkSlot(accountID string, class WorkSlotClass) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tracker, ok := s.activeSlots[accountID]
	if !ok {
		return
	}
	switch class {
	case SlotInteractive:
		if tracker.Interactive > 0 { tracker.Interactive-- }
	case SlotSubagent:
		if tracker.Subagent > 0 { tracker.Subagent-- }
	case SlotHeavyContext:
		if tracker.HeavyContext > 0 { tracker.HeavyContext-- }
	case SlotBackground:
		if tracker.Background > 0 { tracker.Background-- }
	}
}

// SetAccountIntegrityState updates the account integrity risk state (§10C.9-10).
// This is SEPARATE from Trust & Safety and Platform Security.
func (s *Service) SetAccountIntegrityState(accountID, state, reason string) error {
	return s.db.Model(&models.Account{}).Where("id = ?", accountID).
		Update("account_integrity_state", state).Error
}

// SetTrustSafetyState updates the Trust & Safety state (§10C.11).
func (s *Service) SetTrustSafetyState(accountID, state, reason string) error {
	return s.db.Model(&models.Account{}).Where("id = ?", accountID).
		Update("trust_safety_state", state).Error
}

// SetCapacityState updates the capacity/fairness state (§10C.6).
func (s *Service) SetCapacityState(accountID, state string) error {
	return s.db.Model(&models.Account{}).Where("id = ?", accountID).
		Update("capacity_state", state).Error
}

// GetAccount returns the account by ID.
func (s *Service) GetAccount(accountID string) (*models.Account, error) {
	var account models.Account
	if err := s.db.Where("id = ?", accountID).First(&account).Error; err != nil {
		return nil, fmt.Errorf("publiccloud: account not found")
	}
	return &account, nil
}

// GetAccountByEmail returns the account by email.
func (s *Service) GetAccountByEmail(email string) (*models.Account, error) {
	var account models.Account
	if err := s.db.Where("email = ?", email).First(&account).Error; err != nil {
		return nil, fmt.Errorf("publiccloud: account not found")
	}
	return &account, nil
}

// PlanConfig holds plan-specific defaults.
type PlanConfig struct {
	Name             string
	AllowedModels    string // JSON array
	MaxHarnesses     int
	MaxActiveHarnesses int
	NormalSlots      int
	HeavySlots       int
}

func getPlanConfig(plan string) PlanConfig {
	switch plan {
	case "developer":
		return PlanConfig{plan, `["patty-code-standard"]`, 2, 2, 5, 1}
	case "pro":
		return PlanConfig{plan, `["patty-code-standard","patty-code-pro"]`, 3, 2, 5, 2}
	case "team":
		return PlanConfig{plan, `["patty-code-standard","patty-code-pro"]`, 3, 3, 8, 3}
	case "enterprise":
		return PlanConfig{plan, `["patty-code-standard","patty-code-pro"]`, 10, 5, 10, 5}
	default: // free
		return PlanConfig{"free", `[]`, 1, 1, 1, 0}
	}
}

func getPlanPriority(plan string) int {
	switch plan {
	case "enterprise":
		return 100
	case "team":
		return 50
	case "pro":
		return 30
	case "developer":
		return 10
	default:
		return 1
	}
}
