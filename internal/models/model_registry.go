package models

// ModelPackage is a signed Patty Model Package (PMP) — PRD §9.3, DARI §40.2.
type ModelPackage struct {
	Base
	PackageID string `gorm:"type:varchar(64);uniqueIndex;not null" json:"package_id"` // e.g. pmp_01J...
	ModelID   string `gorm:"type:varchar(128);index;not null" json:"model_id"`        // e.g. patty-kocoder-35b
	Name      string `gorm:"type:varchar(255);not null" json:"name"`                  // Patty-KoCoder-v1
	NameKo    string `gorm:"type:varchar(255)" json:"name_ko"`
	Family    string `gorm:"type:varchar(64)" json:"family"`  // coder, chat, etc.
	Version   string `gorm:"type:varchar(64)" json:"version"` // e.g. 2026.08.01
	Release   string `gorm:"type:varchar(64)" json:"release,omitempty"`
	// Artifact references (content-addressed)
	WeightsMerkleRoot  string `gorm:"type:varchar(128)" json:"weights_merkle_root,omitempty"`
	WeightsShardsJSON  string `gorm:"type:text" json:"weights_shards,omitempty"` // JSON array of {name, sha256}
	TokenizerDigest    string `gorm:"type:varchar(128)" json:"tokenizer_digest,omitempty"`
	ConfigDigest       string `gorm:"type:varchar(128)" json:"config_digest,omitempty"`
	ChatTemplateDigest string `gorm:"type:varchar(128)" json:"chat_template_digest,omitempty"`
	AdaptersJSON       string `gorm:"type:text" json:"adapters,omitempty"` // JSON array
	// Quantization
	QuantType        string `gorm:"type:varchar(32)" json:"quant_type,omitempty"`
	QuantCalibDigest string `gorm:"type:varchar(128)" json:"quant_calib_digest,omitempty"`
	// Serving
	ServingEnginesJSON string `gorm:"type:text" json:"serving_engines,omitempty"` // JSON: [{engine, minVersion}]
	ContainerDigest    string `gorm:"type:varchar(128)" json:"container_digest,omitempty"`
	// Capabilities and classification
	CapabilitiesJSON   string `gorm:"type:text" json:"capabilities,omitempty"` // JSON: ["code", "korean", "tool_use"]
	EntitlementClass   string `gorm:"type:varchar(64)" json:"entitlement_class,omitempty"`
	MinAssuranceLevel  string `gorm:"type:varchar(8);default:'L1'" json:"minimum_endpoint_assurance"`
	AllowedDataClasses string `gorm:"type:text" json:"allowed_data_classes,omitempty"` // JSON array
	ContextWindow      int    `json:"context_window,omitempty"`
	// Signature
	ManifestDigest string `gorm:"type:varchar(128)" json:"manifest_digest"` // digest of the full manifest
	SignatureKeyID string `gorm:"type:varchar(128)" json:"signature_key_id,omitempty"`
	Signature      string `gorm:"type:text" json:"signature,omitempty"` // COSE-Sign1 (hex)
	// State
	State       string `gorm:"type:varchar(32);default:'draft'" json:"state"` // draft, published, deprecated, recalled
	PublishedAt string `gorm:"type:timestamp" json:"published_at,omitempty"`
	Expiry      string `gorm:"type:timestamp" json:"expiry,omitempty"`
}

// InferenceEndpoint is an enrolled model serving endpoint (PRD §9.4, DARI §40.3).
type InferenceEndpoint struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	EndpointID     string `gorm:"type:varchar(64);uniqueIndex;not null" json:"endpoint_id"` // DARI endpoint ID
	Name           string `gorm:"type:varchar(255)" json:"name"`
	// PIA identity
	PIAPeerID      string `gorm:"type:varchar(64);index" json:"pia_peer_id"`
	PIABuildDigest string `gorm:"type:varchar(128)" json:"pia_build_digest,omitempty"`
	// Model
	ModelPackageID string `gorm:"type:varchar(64);index;not null" json:"model_package_id"`
	// Serving
	ServingEngine    string `gorm:"type:varchar(32);default:'vllm'" json:"serving_engine"`
	ServingEngineVer string `gorm:"type:varchar(64)" json:"serving_engine_version,omitempty"`
	ServingURL       string `gorm:"type:varchar(512)" json:"serving_url,omitempty"` // PIA-internal only
	// Node identity
	NodeIdentity     string `gorm:"type:varchar(512)" json:"node_identity,omitempty"` // SPIFFE ID
	WorkloadIdentity string `gorm:"type:varchar(512)" json:"workload_identity,omitempty"`
	GPUIDs           string `gorm:"type:text" json:"gpu_ids,omitempty"` // JSON array
	// Assurance
	AssuranceLevel string `gorm:"type:varchar(8);default:'L1'" json:"assurance_level"`
	// State
	Status          string `gorm:"type:varchar(32);default:'pending'" json:"status"` // pending, enrolled, active, revoked, quarantined
	CapacityClass   string `gorm:"type:varchar(32);default:'standard'" json:"capacity_class"`
	AllowedOrgsJSON string `gorm:"type:text" json:"allowed_organizations,omitempty"` // JSON array
	PublicKey       string `gorm:"type:text" json:"public_key"`                      // PIA Ed25519 public key (hex)
	EnrolledAt      string `gorm:"type:timestamp" json:"enrolled_at,omitempty"`
	LastAttestation string `gorm:"type:timestamp" json:"last_attestation,omitempty"`
}

// EndpointAttestation is a signed attestation envelope (PRD §9.5).
type EndpointAttestation struct {
	Base
	EndpointID     string `gorm:"type:varchar(64);index;not null" json:"endpoint_id"`
	OrganizationID string `gorm:"type:varchar(64);index" json:"organization_id"`
	Nonce          string `gorm:"type:varchar(255);not null" json:"nonce"`
	// Measurements
	ModelPackageID         string `gorm:"type:varchar(64)" json:"model_package_id"`
	ModelManifestDigest    string `gorm:"type:varchar(128)" json:"model_manifest_digest,omitempty"`
	ModelMerkleRoot        string `gorm:"type:varchar(128)" json:"model_merkle_root,omitempty"`
	TokenizerDigest        string `gorm:"type:varchar(128)" json:"tokenizer_digest,omitempty"`
	RuntimeConfigDigest    string `gorm:"type:varchar(128)" json:"runtime_config_digest,omitempty"`
	PIABuildDigest         string `gorm:"type:varchar(128)" json:"pia_build_digest,omitempty"`
	ServingContainerDigest string `gorm:"type:varchar(128)" json:"serving_container_digest,omitempty"`
	NodeAttestation        string `gorm:"type:text" json:"node_attestation,omitempty"` // opaque attestation evidence
	GPUAttestation         string `gorm:"type:text" json:"gpu_attestation,omitempty"`
	// Signature
	Signature    string `gorm:"type:text" json:"signature"` // PIA signature over attestation
	KeyAlgorithm string `gorm:"type:varchar(32);default:'ed25519'" json:"key_algorithm"`
	// Validation result
	Verified          bool   `gorm:"default:false" json:"verified"`
	VerifiedAt        string `gorm:"type:timestamp" json:"verified_at,omitempty"`
	VerificationError string `gorm:"type:text" json:"verification_error,omitempty"`
	Timestamp         string `gorm:"type:timestamp" json:"timestamp"`
}

// EndpointLease is a short-lived authorization to route to an endpoint (PRD §9.4 A.5, DARI §40.4).
type EndpointLease struct {
	Base
	EndpointID     string `gorm:"type:varchar(64);index;not null" json:"endpoint_id"`
	OrganizationID string `gorm:"type:varchar(64);index" json:"organization_id"`
	ModelPackageID string `gorm:"type:varchar(64);not null" json:"model_package_id"`
	LeaseID        string `gorm:"type:varchar(64);uniqueIndex;not null" json:"lease_id"`
	// Bound identity
	PIAPeerID    string `gorm:"type:varchar(64)" json:"pia_peer_id"`
	PIAPublicKey string `gorm:"type:text" json:"pia_public_key,omitempty"`
	HostNodeID   string `gorm:"type:varchar(255)" json:"host_node_id,omitempty"`
	// Scope
	PermittedOrgsJSON  string `gorm:"type:text" json:"permitted_organizations,omitempty"` // JSON array
	AllowedDataClasses string `gorm:"type:text" json:"allowed_data_classes,omitempty"`    // JSON array
	CapacityClass      string `gorm:"type:varchar(32)" json:"capacity_class,omitempty"`
	RoutingZones       string `gorm:"type:text" json:"routing_zones,omitempty"` // JSON array
	// Validity
	NotBefore string `gorm:"type:timestamp;not null" json:"not_before"`
	NotAfter  string `gorm:"type:timestamp;not null" json:"not_after"`
	// Attestation reference
	AttestationID string `gorm:"type:varchar(64)" json:"attestation_id,omitempty"`
	NonceRef      string `gorm:"type:varchar(255)" json:"nonce_ref,omitempty"`
	// Signature
	CPSignature     string `gorm:"type:text" json:"cp_signature"` // CP signature over lease body
	RevocationEpoch uint64 `json:"revocation_epoch,omitempty"`
	Status          string `gorm:"type:varchar(32);default:'active'" json:"status"` // active, expired, revoked
	IssuedAt        string `gorm:"type:timestamp" json:"issued_at"`
}

// PolicyEpoch identifies the exact effective policy set (DARI §23).
type PolicyEpoch struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	EpochID        string `gorm:"type:varchar(64);uniqueIndex;not null" json:"epoch_id"`
	EpochNumber    uint64 `gorm:"index" json:"epoch_number"`
	// Policy digests (immutable once issued)
	OrgPolicyDigest       string `gorm:"type:varchar(128)" json:"org_policy_digest"`
	ProjectOverlayDigest  string `gorm:"type:varchar(128)" json:"project_overlay_digest,omitempty"`
	ModelPolicyDigest     string `gorm:"type:varchar(128)" json:"model_policy_digest,omitempty"`
	DLPSecurityDigest     string `gorm:"type:varchar(128)" json:"dlp_security_digest,omitempty"`
	ApprovalMatrixDigest  string `gorm:"type:varchar(128)" json:"approval_matrix_digest,omitempty"`
	RetentionPolicyDigest string `gorm:"type:varchar(128)" json:"retention_policy_digest,omitempty"`
	EngineVersion         string `gorm:"type:varchar(32)" json:"engine_version"`
	// Config
	AllowedModelsJSON string `gorm:"type:text" json:"allowed_models,omitempty"`                   // JSON array of model class/package IDs
	TransitionMode    string `gorm:"type:varchar(32);default:'immediate'" json:"transition_mode"` // immediate, finish_then_renew, allow_until_expiry
	EffectiveAt       string `gorm:"type:timestamp" json:"effective_at"`
	SupersededBy      string `gorm:"type:varchar(64)" json:"superseded_by,omitempty"`
	Status            string `gorm:"type:varchar(32);default:'active'" json:"status"`
}

// CapabilityLease is a signed authorization object (DARI §22).
type CapabilityLease struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	LeaseID        string `gorm:"type:varchar(64);uniqueIndex;not null" json:"lease_id"`
	// Bound identity
	SubjectPeerID string `gorm:"type:varchar(64);index;not null" json:"subject_peer_id"` // Harness peer ID
	UserID        string `gorm:"type:varchar(64);index;not null" json:"user_id"`
	SessionID     string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	// Authority
	PolicyEpochID string `gorm:"type:varchar(64);not null" json:"policy_epoch_id"`
	// Scope
	AllowedModelPackages string `gorm:"type:text" json:"allowed_model_packages,omitempty"` // JSON array
	RepositoryScope      string `gorm:"type:text" json:"repository_scope,omitempty"`       // JSON array of repo/branch
	FilePathScope        string `gorm:"type:text" json:"file_path_scope,omitempty"`        // JSON: {read:[], write:[]}
	ToolClasses          string `gorm:"type:text" json:"tool_classes,omitempty"`           // JSON array
	NetworkDestinations  string `gorm:"type:text" json:"network_destinations,omitempty"`   // JSON array
	// Resource budgets
	TokenBudget    int64  `json:"token_budget,omitempty"`
	ContextBudget  int64  `json:"context_budget,omitempty"`
	ResourceBudget string `gorm:"type:text" json:"resource_budget,omitempty"` // JSON
	// Protection
	ProtectionProfile string `gorm:"type:varchar(16);default:'P0'" json:"protection_profile"`
	RequiredApprovals string `gorm:"type:text" json:"required_approvals,omitempty"` // JSON array
	// Validity
	NotBefore     string `gorm:"type:timestamp;not null" json:"not_before"`
	NotAfter      string `gorm:"type:timestamp;not null" json:"not_after"`
	LeaseSequence uint64 `json:"lease_sequence"`
	// Signature
	CPSignature string `gorm:"type:text" json:"cp_signature"`
	IssuedAt    string `gorm:"type:timestamp" json:"issued_at"`
	Status      string `gorm:"type:varchar(32);default:'active'" json:"status"` // active, expired, revoked, renewed
}
