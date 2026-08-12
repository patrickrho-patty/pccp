package models

import "time"

// CatalogModel is the stable, user-facing model identity (PCCP v2 §10A).
// This is distinct from ModelPackage (the signed artifact) and InferenceEndpoint.
// The Harness sees only CatalogModel — never endpoints or provider URLs.
type CatalogModel struct {
	Base
	OrganizationID   string `gorm:"type:varchar(64);index" json:"organization_id,omitempty"` // empty = global/public
	CatalogModelID   string `gorm:"type:varchar(128);uniqueIndex;not null" json:"catalog_model_id"`
	DisplayName      string `gorm:"type:varchar(255);not null" json:"display_name"`
	DisplayNameKo    string `gorm:"type:varchar(255)" json:"display_name_ko"`
	Description      string `gorm:"type:text" json:"description,omitempty"`
	DescriptionKo    string `gorm:"type:text" json:"description_ko,omitempty"`
	Family           string `gorm:"type:varchar(64)" json:"family"` // code, chat, vision
	ReleaseChannel   string `gorm:"type:varchar(32);default:'stable'" json:"release_channel"`
	Availability     string `gorm:"type:varchar(32);default:'available'" json:"availability"` // available, degraded, maintenance, withdrawn
	DefaultRank      int    `json:"default_rank"`
	// Capabilities (JSON — matches §10A.6 ModelDescriptor)
	CapabilitiesJSON string `gorm:"type:text" json:"capabilities"` // JSON: input/output/tools/reasoning/cache/etc
	// Limits
	MaxInputTokens   int    `json:"max_input_tokens"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
	MaxTools         int    `json:"max_tools,omitempty"`
	MaxParallelTools int    `json:"max_parallel_tool_calls,omitempty"`
	// Entitlement
	EntitlementClass string `gorm:"type:varchar(64)" json:"entitlement_class"`
	EntitlementLabel string `gorm:"type:varchar(128)" json:"entitlement_label"`
	EntitlementLabelKo string `gorm:"type:varchar(128)" json:"entitlement_label_ko"`
	// Client requirements
	MinHarnessVersion  string `gorm:"type:varchar(64)" json:"min_harness_version,omitempty"`
	MinPaperAIVersion  int    `gorm:"default:2" json:"min_paper_ai_version"`
	RequiredExtensions string `gorm:"type:text" json:"required_extensions,omitempty"` // JSON array
	// Lifecycle
	AnnouncedAt  string `gorm:"type:timestamp" json:"announced_at,omitempty"`
	DeprecatedAt string `gorm:"type:timestamp" json:"deprecated_at,omitempty"`
	RetireAfter  string `gorm:"type:timestamp" json:"retire_after,omitempty"`
	// Mapping to ModelPackages (current production, canary, rollback)
	ProductionPackageID string `gorm:"type:varchar(64)" json:"production_package_id"`
	CanaryPackageID     string `gorm:"type:varchar(64)" json:"canary_package_id,omitempty"`
	RollbackPackageID   string `gorm:"type:varchar(64)" json:"rollback_package_id,omitempty"`
	Status              string `gorm:"type:varchar(32);default:'active'" json:"status"`
}

// CatalogEpoch identifies the exact effective model catalog (PCCP v2 §10A.5).
// Every AI_OPEN references a catalog_epoch. Relay validates the epoch.
type CatalogEpoch struct {
	Base
	OrganizationID    string `gorm:"type:varchar(64);index" json:"organization_id,omitempty"`
	EpochID           string `gorm:"type:varchar(64);uniqueIndex;not null" json:"epoch_id"`
	EpochNumber       uint64 `json:"epoch_number"`
	GeneratedAt       string `gorm:"type:timestamp" json:"generated_at"`
	// Scope digest — identifies the user/account/org context this catalog applies to
	ScopeDigest       string `gorm:"type:varchar(128)" json:"scope_digest"`
	EntitlementRevision string `gorm:"type:varchar(64)" json:"entitlement_revision"`
	// The catalog content
	ModelsJSON        string `gorm:"type:text" json:"models_json"` // JSON array of CatalogModel summaries
	// Validity
	MinValiditySecs   int    `json:"min_validity_secs"`
	// Signature
	CPSignature       string `gorm:"type:text" json:"cp_signature"`
	Status            string `gorm:"type:varchar(32);default:'active'" json:"status"`
}

// ModelDescriptor is the capability descriptor sent to the Harness (PCCP v2 §10A.6).
// This is the contract the Harness uses to render UI and negotiate capabilities.
type ModelDescriptor struct {
	CatalogModelID string                 `json:"catalog_model_id"`
	DisplayName    string                 `json:"display_name"`
	DisplayNameKo  string                 `json:"display_name_ko,omitempty"`
	Description    string                 `json:"description,omitempty"`
	DescriptionKo  string                 `json:"description_ko,omitempty"`
	Family         string                 `json:"family"`
	ReleaseChannel string                 `json:"release_channel"`
	Availability   string                 `json:"availability"`
	DefaultRank    int                    `json:"default_rank"`
	Capabilities   ModelCapabilities      `json:"capabilities"`
	Limits         ModelLimits            `json:"limits"`
	Entitlement    ModelEntitlement       `json:"entitlement"`
	ClientReqs     ModelClientReqs        `json:"client"`
	Lifecycle      ModelLifecycle         `json:"lifecycle"`
}

type ModelCapabilities struct {
	Input      ContentCapabilities  `json:"input"`
	Output     ContentCapabilities  `json:"output"`
	Tools      ToolCapabilities     `json:"tools"`
	Reasoning  ReasoningCapabilities `json:"reasoning"`
	Context    ContextCapabilities  `json:"context_management"`
	Cache      CacheCapabilities    `json:"cache"`
	Citations  bool                 `json:"citations_sources"`
	Streaming  bool                 `json:"streaming"`
	Resumable  bool                 `json:"resumable_background"`
}

type ContentCapabilities struct {
	Text      bool `json:"text"`
	Image     bool `json:"image"`
	Audio     bool `json:"audio"`
	File      bool `json:"file"`
	PDF       bool `json:"pdf"`
	Structured bool `json:"structured"`
}

type ToolCapabilities struct {
	ClientTools      bool `json:"client_tools"`
	RuntimeTools     bool `json:"runtime_tools"`
	ServerTools      bool `json:"server_tools"`
	MCP              bool `json:"mcp"`
	ParallelCalls    bool `json:"parallel_calls"`
	StrictSchema     bool `json:"strict_schema"`
	DynamicDiscovery bool `json:"dynamic_discovery"`
	Approval         bool `json:"approval"`
}

type ReasoningCapabilities struct {
	Supported              bool     `json:"supported"`
	EffortLevels           []string `json:"effort_levels"`
	OpaqueContinuationState bool     `json:"opaque_continuation_state"`
}

type ContextCapabilities struct {
	Compaction        bool `json:"compaction"`
	ToolResultClearing bool `json:"tool_result_clearing"`
}

type CacheCapabilities struct {
	PromptCache        bool `json:"prompt_cache"`
	CacheUsageReporting bool `json:"cache_usage_reporting"`
}

type ModelLimits struct {
	MaxInputTokens     int `json:"max_input_tokens"`
	MaxOutputTokens    int `json:"max_output_tokens"`
	MaxTools           int `json:"max_tools,omitempty"`
	MaxParallelToolCalls int `json:"max_parallel_tool_calls,omitempty"`
}

type ModelEntitlement struct {
	Class   string `json:"class"`
	Label   string `json:"ui_label"`
	LabelKo string `json:"ui_label_ko,omitempty"`
}

type ModelClientReqs struct {
	MinHarnessVersion  string   `json:"min_harness_version,omitempty"`
	MinPaperAIVersion  int      `json:"min_paper_ai_version"`
	RequiredExtensions []string `json:"required_extensions,omitempty"`
}

type ModelLifecycle struct {
	AnnouncedAt  string `json:"announced_at,omitempty"`
	DeprecatedAt string `json:"deprecated_at,omitempty"`
	RetireAfter  string `json:"retire_after,omitempty"`
}

// Account is the Public Cloud account entity (PCCP v2 §10C, §8.2).
// For Enterprise, this maps to Organization. For Public, it's the subscriber.
type Account struct {
	Base
	Email          string `gorm:"type:varchar(255);uniqueIndex" json:"email"`
	DisplayName    string `gorm:"type:varchar(255)" json:"display_name"`
	DisplayNameKo  string `gorm:"type:varchar(255)" json:"display_name_ko"`
	Profile        string `gorm:"type:varchar(32);default:'public'" json:"profile"` // public, enterprise, government
	// Subscription state
	SubscriptionStatus string `gorm:"type:varchar(32);default:'none'" json:"subscription_status"` // none, active, grace, expired, suspended
	SubscriptionPlan   string `gorm:"type:varchar(64)" json:"subscription_plan"`
	SubscriptionExpiry string `gorm:"type:timestamp" json:"subscription_expiry,omitempty"`
	// Risk states (PCCP v2 §10C — separate dimensions)
	AccountIntegrityState string `gorm:"type:varchar(32);default:'normal'" json:"account_integrity_state"` // normal, flagged, restricted
	TrustSafetyState      string `gorm:"type:varchar(32);default:'normal'" json:"trust_safety_state"` // normal, reviewing, restricted
	PlatformSecurityState string `gorm:"type:varchar(32);default:'normal'" json:"platform_security_state"` // normal, suspicious, blocked
	CapacityState         string `gorm:"type:varchar(32);default:'normal'" json:"capacity_state"` // normal, high_usage, throttled
	// Limits
	MaxHarnesses       int    `gorm:"default:3" json:"max_harnesses"`
	MaxActiveHarnesses int    `gorm:"default:2" json:"max_active_harnesses"`
	NormalWorkSlots    int    `gorm:"default:5" json:"normal_work_slots"`
	HeavyWorkSlots     int    `gorm:"default:2" json:"heavy_work_slots"`
	BackgroundSlots    int    `gorm:"default:2" json:"background_slots"`
	// OAuth
	OAuthSubject    string `gorm:"type:varchar(255);index" json:"oauth_subject,omitempty"`
	OAuthProvider   string `gorm:"type:varchar(64)" json:"oauth_provider,omitempty"`
	Locale          string `gorm:"type:varchar(10);default:'ko-KR'" json:"locale"`
	Timezone        string `gorm:"type:varchar(64);default:'Asia/Seoul'" json:"timezone"`
}

// Subscription represents a Public Cloud subscription (PCCP v2 §10C.2).
type Subscription struct {
	Base
	AccountID       string `gorm:"type:varchar(64);index;not null" json:"account_id"`
	Plan            string `gorm:"type:varchar(64);not null" json:"plan"` // free, developer, pro, team, enterprise
	Status          string `gorm:"type:varchar(32);default:'active'" json:"status"` // active, grace, expired, suspended, cancelled
	// Period
	StartedAt       string `gorm:"type:timestamp" json:"started_at"`
	ExpiresAt       string `gorm:"type:timestamp" json:"expires_at,omitempty"`
	// Payment
	PaymentProvider string `gorm:"type:varchar(64)" json:"payment_provider,omitempty"`
	PaymentID       string `gorm:"type:varchar(255)" json:"payment_id,omitempty"`
	// Entitlements
	AllowedModelClasses string `gorm:"type:text" json:"allowed_model_classes"` // JSON array
	MaxHarnesses        int    `gorm:"default:3" json:"max_harnesses"`
	MaxActiveHarnesses  int    `gorm:"default:2" json:"max_active_harnesses"`
	NormalWorkSlots     int    `gorm:"default:5" json:"normal_work_slots"`
	HeavyWorkSlots      int    `gorm:"default:2" json:"heavy_work_slots"`
	// Revision tracking
	Revision       string `gorm:"type:varchar(64)" json:"revision"`
}

// AccountCapacityLease controls multi-Relay concurrency (PCCP v2 §10C.5).
type AccountCapacityLease struct {
	Base
	AccountID         string `gorm:"type:varchar(64);index;not null" json:"account_id"`
	EntitlementRevision string `gorm:"type:varchar(64)" json:"entitlement_revision"`
	// Slot allocation
	ActiveAgentSlots  int    `json:"active_agent_slots"`
	HeavySlots        int    `json:"heavy_slots"`
	BackgroundSlots   int    `json:"background_slots"`
	BurstCLU          int64  `json:"burst_clu,omitempty"`
	SustainedCLUWindow int64 `json:"sustained_clu_window,omitempty"`
	PriorityWeight    int    `json:"priority_weight"`
	// Validity
	ValidUntil     string `gorm:"type:timestamp;not null" json:"valid_until"`
	// State
	UsedSlots      int    `json:"used_slots"`
	Status         string `gorm:"type:varchar(32);default:'active'" json:"status"`
	// Signature
	CPSignature    string `gorm:"type:text" json:"cp_signature"`
}

// Ensure time import is referenced
var _ = time.Now
