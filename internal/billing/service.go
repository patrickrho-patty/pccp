package billing

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/paper"
	"gorm.io/gorm"
)

// Service implements Usage, Entitlements, Billing, and Chargeback (PRD §29).
type Service struct {
	db         *gorm.DB
	signingKey ed25519.PrivateKey
	mu         sync.RWMutex
	entitlements map[string]*Entitlement // orgID → entitlement
}

// Entitlement constrains what an organization can use (PRD §29.2).
type Entitlement struct {
	ID                string   `json:"id"`
	OrganizationID    string   `json:"organization_id"`
	Plan              string   `json:"plan"` // enterprise, government, public
	// Model constraints
	AllowedModelFamilies []string `json:"allowed_model_families"`
	ContextLimit         int      `json:"context_limit"`
	// Quotas
	RequestQuotaPerDay   int64    `json:"request_quota_per_day"`
	TokenQuotaPerDay     int64    `json:"token_quota_per_day"`
	ConcurrentSessions   int      `json:"concurrent_sessions"`
	MaxHarnesses         int      `json:"max_harnesses"`
	// Features
	WorkIntelligenceEnabled bool   `json:"work_intelligence_enabled"`
	AdvancedSecurityEnabled bool   `json:"advanced_security_enabled"`
	CloudGPUPoolAccess     bool   `json:"cloud_gpu_pool_access"`
	SupportTier            string `json:"support_tier"`
	// Retention
	RetentionDays         int      `json:"retention_days"`
	// Validity
	ValidFrom             string   `json:"valid_from"`
	ValidUntil            string   `json:"valid_until"`
	GracePeriodDays       int      `json:"grace_period_days"`
	// Signing
	CPSignature           string   `json:"cp_signature"`
	Status                string   `json:"status"` // active, suspended, expired
}

// New creates a new billing service.
func New(db *gorm.DB) (*Service, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("billing: generate signing key: %w", err)
	}
	return &Service{
		db:            db,
		signingKey:    priv,
		entitlements:  make(map[string]*Entitlement),
	}, nil
}

// SetEntitlement sets an organization's entitlement.
func (s *Service) SetEntitlement(ent Entitlement) (*Entitlement, error) {
	if ent.ID == "" {
		ent.ID = paper.GenerateID("ent")
	}
	if ent.Status == "" {
		ent.Status = "active"
	}
	if ent.ValidFrom == "" {
		ent.ValidFrom = time.Now().Format(time.RFC3339)
	}

	// Sign the entitlement
	entData := fmt.Sprintf("%s|%s|%s|%s", ent.ID, ent.OrganizationID, ent.Plan, ent.ValidUntil)
	sig := ed25519.Sign(s.signingKey, []byte(entData))
	ent.CPSignature = fmt.Sprintf("%x", sig)

	s.mu.Lock()
	s.entitlements[ent.OrganizationID] = &ent
	s.mu.Unlock()

	// Record in audit
	details, _ := json.Marshal(ent)
	audit := &models.AuditEvent{
		OrganizationID: ent.OrganizationID,
		EventType:      "cp.entitlement.set",
		ActorType:      "admin",
		Action:         "set_entitlement",
		ResourceType:   "entitlement",
		ResourceID:     ent.ID,
		Details:        string(details),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(audit)

	return &ent, nil
}

// GetEntitlement retrieves an organization's entitlement.
func (s *Service) GetEntitlement(orgID string) *Entitlement {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ent, ok := s.entitlements[orgID]; ok {
		// Check validity
		if ent.ValidUntil != "" {
			until, err := time.Parse(time.RFC3339, ent.ValidUntil)
			if err == nil && time.Now().After(until) {
				// Check grace period
				graceEnd := until.AddDate(0, 0, ent.GracePeriodDays)
				if time.Now().After(graceEnd) {
					ent.Status = "expired"
					return ent
				}
				// In grace period — still active but flagged
			}
		}
		return ent
	}
	// Return default public entitlement
	return defaultEntitlement(orgID)
}

// CheckQuota verifies if a request falls within quota limits.
type QuotaCheck struct {
	Allowed       bool   `json:"allowed"`
	Reason        string `json:"reason"`
	Remaining     int64  `json:"remaining"`
	ResetAt       string `json:"reset_at,omitempty"`
}

// CheckRequestQuota checks if a model request is within daily quota.
func (s *Service) CheckRequestQuota(orgID string, tokenEstimate int64) *QuotaCheck {
	ent := s.GetEntitlement(orgID)

	if ent.Status != "active" && ent.Status != "" {
		return &QuotaCheck{
			Allowed: false,
			Reason:  fmt.Sprintf("entitlement status is %s", ent.Status),
		}
	}

	// Count today's usage
	var todayUsage int64
	s.db.Model(&models.UsageRecord{}).
		Where("organization_id = ? AND occurred_at > ? AND metric_type IN ('tokens_in', 'tokens_out')",
			orgID, time.Now().Format("2006-01-02")).Count(&todayUsage)

	if ent.TokenQuotaPerDay > 0 {
		remaining := ent.TokenQuotaPerDay - todayUsage
		if remaining <= 0 {
			return &QuotaCheck{
				Allowed: false,
				Reason:  "daily token quota exceeded",
				ResetAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			}
		}
		return &QuotaCheck{
			Allowed:   true,
			Remaining: remaining,
			ResetAt:   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		}
	}

	return &QuotaCheck{Allowed: true}
}

// CheckModelAllowed verifies if a model family is allowed under the entitlement.
func (s *Service) CheckModelAllowed(orgID, modelFamily string) bool {
	ent := s.GetEntitlement(orgID)
	if len(ent.AllowedModelFamilies) == 0 {
		return true // no restriction
	}
	for _, fam := range ent.AllowedModelFamilies {
		if fam == modelFamily || fam == "*" {
			return true
		}
	}
	return false
}

// CheckHarnessLimit verifies if a new harness can be enrolled.
func (s *Service) CheckHarnessLimit(orgID string) bool {
	ent := s.GetEntitlement(orgID)
	if ent.MaxHarnesses == 0 {
		return true // unlimited
	}
	var count int64
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status IN ('active','enrolled')", orgID).Count(&count)
	return int(count) < ent.MaxHarnesses
}

// CheckConcurrentSessions verifies if a new session can be opened.
func (s *Service) CheckConcurrentSessions(orgID string) bool {
	ent := s.GetEntitlement(orgID)
	if ent.ConcurrentSessions == 0 {
		return true
	}
	var count int64
	s.db.Model(&models.Session{}).Where("organization_id = ? AND status = 'active'", orgID).Count(&count)
	return int(count) < ent.ConcurrentSessions
}

// Chargeback represents an internal chargeback/showback entry (PRD §29.5).
type Chargeback struct {
	OrganizationID string `json:"organization_id"`
	BusinessUnit   string `json:"business_unit"`
	ProjectID      string `json:"project_id"`
	Period         string `json:"period"`
	TotalTokensIn  int64  `json:"total_tokens_in"`
	TotalTokensOut int64  `json:"total_tokens_out"`
	TotalRequests  int64  `json:"total_requests"`
	EstimatedCostKRW int64 `json:"estimated_cost_krw"`
}

// GenerateChargebackReport generates a chargeback report by business unit.
func (s *Service) GenerateChargebackReport(orgID, period string) ([]Chargeback, error) {
	var sessions []models.Session
	s.db.Where("organization_id = ?", orgID).Find(&sessions)

	buMap := make(map[string]*Chargeback)
	for _, sess := range sessions {
		bu := sess.ProjectID // simplified: chargeback by project
		if bu == "" {
			bu = "unassigned"
		}
		if _, ok := buMap[bu]; !ok {
			buMap[bu] = &Chargeback{
				OrganizationID: orgID,
				ProjectID:      bu,
				Period:         period,
			}
		}

		var usage []models.UsageRecord
		s.db.Where("session_id = ?", sess.SessionID).Find(&usage)
		for _, u := range usage {
			if u.MetricType == "tokens_in" {
				buMap[bu].TotalTokensIn += u.Quantity
			}
			if u.MetricType == "tokens_out" {
				buMap[bu].TotalTokensOut += u.Quantity
			}
			buMap[bu].TotalRequests++
		}

		// Estimate cost (KRW per 1M tokens — simplified)
		buMap[bu].EstimatedCostKRW = (buMap[bu].TotalTokensIn+buMap[bu].TotalTokensOut)/1000000*5000
	}

	var result []Chargeback
	for _, cb := range buMap {
		result = append(result, *cb)
	}
	return result, nil
}

func defaultEntitlement(orgID string) *Entitlement {
	return &Entitlement{
		OrganizationID:        orgID,
		Plan:                  "public",
		ContextLimit:          32768,
		ConcurrentSessions:    3,
		MaxHarnesses:          5,
		WorkIntelligenceEnabled: false,
		AdvancedSecurityEnabled: false,
		SupportTier:           "community",
		RetentionDays:         30,
		Status:                "active",
	}
}
