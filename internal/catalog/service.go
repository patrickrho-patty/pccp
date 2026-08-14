package catalog

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"gorm.io/gorm"
)

// Service implements the Server-Authoritative Model Catalog (PCCP v2 §10A).
// The Harness does NOT own the model list. PCCP is the sole authority.
type Service struct {
	db         *gorm.DB
	signingKey ed25519.PrivateKey
}

// New creates a new model catalog service.
func New(db *gorm.DB) (*Service, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: generate signing key: %w", err)
	}
	return &Service{db: db, signingKey: priv}, nil
}

// RegisterCatalogModel creates a new Catalog Model (§10A.2).
// This is the stable, user-facing model identity.
func (s *Service) RegisterCatalogModel(cm *models.CatalogModel) error {
	if cm.CatalogModelID == "" {
		return fmt.Errorf("catalog: catalog_model_id required")
	}
	if cm.CapabilitiesJSON == "" {
		cm.CapabilitiesJSON = defaultCapabilitiesJSON()
	}
	if cm.Status == "" {
		cm.Status = "active"
	}
	if cm.Availability == "" {
		cm.Availability = "available"
	}
	if cm.AnnouncedAt == "" {
		cm.AnnouncedAt = time.Now().Format(time.RFC3339)
	}
	return s.db.Create(cm).Error
}

// GetEffectiveCatalog returns the model catalog for a specific user/account context (§10A.3).
// Two users may receive different catalogs based on subscription, org policy, etc.
func (s *Service) GetEffectiveCatalog(accountID, orgID, subscriptionPlan string) ([]models.ModelDescriptor, error) {
	var catalogModels []models.CatalogModel

	// Get all active catalog models
	query := s.db.Where("status = 'active' AND availability IN ('available', 'degraded')")
	if orgID != "" {
		// Enterprise: include org-specific + global models
		query = query.Where("organization_id = ? OR organization_id = ''", orgID)
	} else {
		// Public: only global models
		query = query.Where("organization_id = ''")
	}
	query.Order("default_rank DESC").Find(&catalogModels)

	var descriptors []models.ModelDescriptor
	for _, cm := range catalogModels {
		desc := s.toDescriptor(cm)
		// Filter by entitlement class
		if subscriptionPlan != "" && !s.isModelAllowedForPlan(desc.Entitlement.Class, subscriptionPlan) {
			continue
		}
		descriptors = append(descriptors, desc)
	}
	return descriptors, nil
}

// GenerateCatalogEpoch creates a new catalog epoch for a specific scope (§10A.5).
func (s *Service) GenerateCatalogEpoch(accountID, orgID, entitlementRevision string) (*models.CatalogEpoch, error) {
	// Get the effective catalog
	descriptors, err := s.GetEffectiveCatalog(accountID, orgID, "")
	if err != nil {
		return nil, err
	}

	// Serialize models
	modelsJSON, _ := json.Marshal(descriptors)

	// Compute scope digest
	scopeData := fmt.Sprintf("%s|%s|%s", accountID, orgID, entitlementRevision)
	scopeDigest := hashString(scopeData)

	// Get next epoch number
	var maxEpoch models.CatalogEpoch
	s.db.Where("scope_digest = ?", scopeDigest).Order("epoch_number DESC").First(&maxEpoch)
	nextNum := uint64(1)
	if maxEpoch.ID != "" {
		nextNum = maxEpoch.EpochNumber + 1
	}

	epoch := &models.CatalogEpoch{
		EpochID:             dari.GenerateID("catalog_epoch"),
		EpochNumber:         nextNum,
		GeneratedAt:         time.Now().Format(time.RFC3339),
		ScopeDigest:         scopeDigest,
		EntitlementRevision: entitlementRevision,
		ModelsJSON:          string(modelsJSON),
		MinValiditySecs:     300, // 5 minutes
		Status:              "active",
	}

	// Sign
	sig := ed25519.Sign(s.signingKey, []byte(epoch.ModelsJSON))
	epoch.CPSignature = hex.EncodeToString(sig)

	// Invalidate previous epochs for this scope
	s.db.Model(&models.CatalogEpoch{}).
		Where("scope_digest = ? AND status = 'active'", scopeDigest).
		Update("status", "superseded")

	if err := s.db.Create(epoch).Error; err != nil {
		return nil, fmt.Errorf("catalog: create epoch: %w", err)
	}
	return epoch, nil
}

// ValidateCatalogEpoch checks if a catalog epoch is valid for a request (§10A.11).
func (s *Service) ValidateCatalogEpoch(epochID, catalogModelID string) (*models.CatalogEpoch, *models.CatalogModel, error) {
	var epoch models.CatalogEpoch
	if err := s.db.Where("epoch_id = ? AND status = 'active'", epochID).First(&epoch).Error; err != nil {
		return nil, nil, fmt.Errorf("catalog: epoch not found or inactive")
	}

	// Check if the model is in the epoch's catalog
	var descriptors []models.ModelDescriptor
	json.Unmarshal([]byte(epoch.ModelsJSON), &descriptors)

	found := false
	for _, d := range descriptors {
		if d.CatalogModelID == catalogModelID {
			found = true
			break
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("catalog: model %s not in epoch %s", catalogModelID, epochID)
	}

	// Get the catalog model
	var cm models.CatalogModel
	if err := s.db.Where("catalog_model_id = ? AND status = 'active'", catalogModelID).First(&cm).Error; err != nil {
		return nil, nil, fmt.Errorf("catalog: catalog model not found")
	}

	// Check availability
	if cm.Availability == "withdrawn" {
		return nil, nil, fmt.Errorf("catalog: model %s has been withdrawn", catalogModelID)
	}

	return &epoch, &cm, nil
}

// ResolveToPackage maps a Catalog Model to the current production ModelPackage (§10A.7).
func (s *Service) ResolveToPackage(catalogModelID string) (string, error) {
	var cm models.CatalogModel
	if err := s.db.Where("catalog_model_id = ?", catalogModelID).First(&cm).Error; err != nil {
		return "", fmt.Errorf("catalog: model not found")
	}
	if cm.ProductionPackageID == "" {
		return "", fmt.Errorf("catalog: no production package mapped")
	}
	return cm.ProductionPackageID, nil
}

// WithdrawModel marks a catalog model as withdrawn and generates announcement (§10A.8).
func (s *Service) WithdrawModel(catalogModelID, reason string) error {
	return s.db.Model(&models.CatalogModel{}).
		Where("catalog_model_id = ?", catalogModelID).
		Updates(map[string]interface{}{
			"availability": "withdrawn",
			"status":       "deprecated",
		}).Error
}

// AnnounceModel marks a catalog model as newly available (§10A.8).
func (s *Service) AnnounceModel(catalogModelID string) error {
	return s.db.Model(&models.CatalogModel{}).
		Where("catalog_model_id = ?", catalogModelID).
		Updates(map[string]interface{}{
			"availability": "available",
			"announced_at": time.Now().Format(time.RFC3339),
		}).Error
}

// SeedDefaultCatalog creates the default Patty models (Appendix H.2).
func (s *Service) SeedDefaultCatalog() error {
	defaults := []models.CatalogModel{
		{
			CatalogModelID:   "patty-code-standard",
			DisplayName:      "Patty Code Standard",
			DisplayNameKo:    "패티 코드 스탠다드",
			Description:      "Qwen3 MoE — standard coding model for everyday development",
			DescriptionKo:    "Qwen3 MoE — 일상적인 개발을 위한 표준 코딩 모델",
			Family:           "code",
			ReleaseChannel:   "stable",
			Availability:     "available",
			DefaultRank:      10,
			CapabilitiesJSON: defaultCapabilitiesJSON(),
			MaxInputTokens:   131072,
			MaxOutputTokens:  32768,
			EntitlementClass: "unlimited-developer",
			EntitlementLabel: "Included",
			EntitlementLabelKo: "포함됨",
			MinDARIProtocolVersion: 2,
			ProductionPackageID: "pmp_qwen3_moe_v1",
		},
		{
			CatalogModelID:   "patty-code-pro",
			DisplayName:      "Patty Code Pro",
			DisplayNameKo:    "패티 코드 프로",
			Description:      "Qwen3 MoE with higher reasoning effort for complex tasks",
			DescriptionKo:    "Qwen3 MoE — 복잡한 작업을 위한 심층 추론 고급 모델",
			Family:           "code",
			ReleaseChannel:   "stable",
			Availability:     "available",
			DefaultRank:      20,
			CapabilitiesJSON: proCapabilitiesJSON(),
			MaxInputTokens:   262144,
			MaxOutputTokens:  65536,
			EntitlementClass: "pro",
			EntitlementLabel: "Pro Plan",
			EntitlementLabelKo: "프로 플랜",
			MinDARIProtocolVersion: 2,
			ProductionPackageID: "pmp_kocoder_v1",
		},
	}

	for _, cm := range defaults {
		// Check if already exists
		var existing models.CatalogModel
		if s.db.Where("catalog_model_id = ?", cm.CatalogModelID).First(&existing).Error == nil {
			continue // Already seeded
		}
		if err := s.RegisterCatalogModel(&cm); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) toDescriptor(cm models.CatalogModel) models.ModelDescriptor {
	var caps models.ModelCapabilities
	json.Unmarshal([]byte(cm.CapabilitiesJSON), &caps)

	var extensions []string
	if cm.RequiredExtensions != "" {
		json.Unmarshal([]byte(cm.RequiredExtensions), &extensions)
	}

	return models.ModelDescriptor{
		CatalogModelID: cm.CatalogModelID,
		DisplayName:    cm.DisplayName,
		DisplayNameKo:  cm.DisplayNameKo,
		Description:    cm.Description,
		DescriptionKo:  cm.DescriptionKo,
		Family:         cm.Family,
		ReleaseChannel: cm.ReleaseChannel,
		Availability:   cm.Availability,
		DefaultRank:    cm.DefaultRank,
		Capabilities:   caps,
		Limits: models.ModelLimits{
			MaxInputTokens:     cm.MaxInputTokens,
			MaxOutputTokens:    cm.MaxOutputTokens,
			MaxTools:           cm.MaxTools,
			MaxParallelToolCalls: cm.MaxParallelTools,
		},
		Entitlement: models.ModelEntitlement{
			Class:   cm.EntitlementClass,
			Label:   cm.EntitlementLabel,
			LabelKo: cm.EntitlementLabelKo,
		},
		ClientReqs: models.ModelClientReqs{
			MinHarnessVersion:  cm.MinHarnessVersion,
			MinDARIProtocolVersion:  cm.MinDARIProtocolVersion,
			RequiredExtensions: extensions,
		},
		Lifecycle: models.ModelLifecycle{
			AnnouncedAt:  cm.AnnouncedAt,
			DeprecatedAt: cm.DeprecatedAt,
			RetireAfter:  cm.RetireAfter,
		},
	}
}

func (s *Service) isModelAllowedForPlan(entitlementClass, plan string) bool {
	if entitlementClass == "" || entitlementClass == "free" {
		return true
	}
	planTier := map[string]int{"free": 0, "developer": 1, "pro": 2, "team": 3, "enterprise": 4}
	classTier := map[string]int{"free": 0, "unlimited-developer": 1, "pro": 2, "team": 3, "enterprise": 4}
	return planTier[plan] >= classTier[entitlementClass]
}

func defaultCapabilitiesJSON() string {
	caps := models.ModelCapabilities{
		Input:     models.ContentCapabilities{Text: true, Image: true, File: true, PDF: true},
		Output:    models.ContentCapabilities{Text: true, Structured: true},
		Tools:     models.ToolCapabilities{ClientTools: true, RuntimeTools: true, MCP: true, ParallelCalls: true, StrictSchema: true, Approval: true},
		Reasoning: models.ReasoningCapabilities{Supported: true, EffortLevels: []string{"low", "medium", "high"}},
		Context:   models.ContextCapabilities{Compaction: true, ToolResultClearing: true},
		Cache:     models.CacheCapabilities{PromptCache: true, CacheUsageReporting: true},
		Citations: true,
		Streaming: true,
		Resumable: true,
	}
	b, _ := json.Marshal(caps)
	return string(b)
}

func proCapabilitiesJSON() string {
	caps := models.ModelCapabilities{
		Input:     models.ContentCapabilities{Text: true, Image: true, Audio: false, File: true, PDF: true},
		Output:    models.ContentCapabilities{Text: true, Structured: true},
		Tools:     models.ToolCapabilities{ClientTools: true, RuntimeTools: true, ServerTools: true, MCP: true, ParallelCalls: true, StrictSchema: true, DynamicDiscovery: true, Approval: true},
		Reasoning: models.ReasoningCapabilities{Supported: true, EffortLevels: []string{"low", "medium", "high"}, OpaqueContinuationState: true},
		Context:   models.ContextCapabilities{Compaction: true, ToolResultClearing: true},
		Cache:     models.CacheCapabilities{PromptCache: true, CacheUsageReporting: true},
		Citations: true,
		Streaming: true,
		Resumable: true,
	}
	b, _ := json.Marshal(caps)
	return string(b)
}

func hashString(s string) string {
	// Simple hash for scope digest
	h := make([]byte, 16)
	for i := 0; i < len(s); i++ {
		h[i%16] ^= s[i]
	}
	return hex.EncodeToString(h)
}
