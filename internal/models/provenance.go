package models

// ActionEnvelope is a signed record of a governed action (PRD §37.2, Appendix H item 3).
type ActionEnvelope struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ActionID       string `gorm:"type:varchar(64);uniqueIndex" json:"action_id"`
	SessionID      string `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	ExchangeID     string `gorm:"type:varchar(64);index" json:"exchange_id,omitempty"`
	// Actor chain
	UserID         string `gorm:"type:varchar(64);index" json:"user_id,omitempty"`
	HarnessID      string `gorm:"type:varchar(64);index" json:"harness_id,omitempty"`
	ModelPackageID string `gorm:"type:varchar(64)" json:"model_package_id,omitempty"`
	EndpointID     string `gorm:"type:varchar(64)" json:"endpoint_id,omitempty"`
	// Context
	ProjectID      string `gorm:"type:varchar(64)" json:"project_id,omitempty"`
	RepositoryID   string `gorm:"type:varchar(64)" json:"repository_id,omitempty"`
	Branch         string `gorm:"type:varchar(255)" json:"branch,omitempty"`
	PolicyEpochID  string `gorm:"type:varchar(64)" json:"policy_epoch_id,omitempty"`
	LeaseID        string `gorm:"type:varchar(64)" json:"lease_id,omitempty"`
	// Action details
	ActionType     string `gorm:"type:varchar(64);not null;index" json:"action_type"` // ai_inference, file_write, tool_use, etc.
	ActionPayload  string `gorm:"type:text" json:"action_payload,omitempty"` // JSON details
	VerdictResult  string `gorm:"type:varchar(64)" json:"verdict_result,omitempty"`
	Classification string `gorm:"type:varchar(32);default:'internal'" json:"classification"`
	// Signature
	EnvelopeDigest string `gorm:"type:varchar(128)" json:"envelope_digest"` // content-addressed digest
	CPSignature    string `gorm:"type:text" json:"cp_signature"` // CP signature
	OccurredAt     string `gorm:"type:timestamp" json:"occurred_at"`
}

// ChangeSet is a code patch with provenance (PRD §18.4, Appendix B).
type ChangeSet struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	SessionID      string `gorm:"type:varchar(64);index;not null" json:"session_id"`
	ExchangeID     string `gorm:"type:varchar(64);index" json:"exchange_id,omitempty"`
	RepositoryID   string `gorm:"type:varchar(64);index;not null" json:"repository_id"`
	Branch         string `gorm:"type:varchar(255);not null" json:"branch"`
	BaselineID     string `gorm:"type:varchar(64)" json:"baseline_id,omitempty"`
	// Provenance chain
	UserID         string `gorm:"type:varchar(64)" json:"user_id,omitempty"`
	HarnessID      string `gorm:"type:varchar(64)" json:"user_harness_id,omitempty"`
	ModelPackageID string `gorm:"type:varchar(64)" json:"model_package_id,omitempty"`
	EndpointID     string `gorm:"type:varchar(64)" json:"endpoint_id,omitempty"`
	// Changes
	FilesChanged   string `gorm:"type:text" json:"files_changed,omitempty"` // JSON array of file paths
	DiffSummary    string `gorm:"type:text" json:"diff_summary,omitempty"`
	DiffDigest     string `gorm:"type:varchar(128)" json:"diff_digest,omitempty"` // content-addressed
	LinesAdded     int    `json:"lines_added,omitempty"`
	LinesRemoved   int    `json:"lines_removed,omitempty"`
	// Attribution
	AttributionState string `gorm:"type:varchar(32);default:'AI_GENERATED'" json:"attribution_state"`
	// PRD §19.3: AI_GENERATED, AI_THEN_HUMAN_EDITED, HUMAN_THEN_AI_ASSISTED, HUMAN_WRITTEN
	Confidence     float64 `json:"confidence,omitempty"`
	ChangeSetDigest string `gorm:"type:varchar(128)" json:"change_set_digest"`
	Status         string `gorm:"type:varchar(32);default:'pending'" json:"status"` // pending, committed, rejected
}

// ProvenanceSpan maps code to origin (PRD §19, Appendix B.1).
type ProvenanceSpan struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	RepositoryID   string `gorm:"type:varchar(64);index;not null" json:"repository_id"`
	ChangeSetID    string `gorm:"type:varchar(64);index" json:"change_set_id,omitempty"`
	// Code location
	FilePath       string `gorm:"type:varchar(512);not null" json:"file_path"`
	CommitSHA      string `gorm:"type:varchar(64)" json:"commit_sha,omitempty"`
	SymbolLang     string `gorm:"type:varchar(32)" json:"symbol_language,omitempty"`
	SymbolName     string `gorm:"type:varchar(512)" json:"symbol_qualified_name,omitempty"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	// Fingerprints (PRD §19.4)
	ASTFingerprint string `gorm:"type:varchar(128)" json:"ast_fingerprint,omitempty"`
	SemanticFingerprint string `gorm:"type:varchar(128)" json:"semantic_fingerprint,omitempty"`
	// Attribution
	AttributionState string `gorm:"type:varchar(32);not null" json:"attribution_state"`
	Confidence     float64 `json:"confidence,omitempty"`
	// Origin
	SessionID      string `gorm:"type:varchar(64)" json:"session_id,omitempty"`
	UserID         string `gorm:"type:varchar(64)" json:"user_id,omitempty"`
	HarnessID      string `gorm:"type:varchar(64)" json:"harness_id,omitempty"`
	ModelPackageID string `gorm:"type:varchar(64)" json:"model_package_id,omitempty"`
	EndpointID     string `gorm:"type:varchar(64)" json:"endpoint_id,omitempty"`
	// References
	ContextRefsJSON string `gorm:"type:text" json:"context_refs,omitempty"` // JSON array
	ToolCallRefsJSON string `gorm:"type:text" json:"tool_call_refs,omitempty"`
	PolicyDecisionRefsJSON string `gorm:"type:text" json:"policy_decision_refs,omitempty"`
	ParentSpanRefs string `gorm:"type:text" json:"parent_span_refs,omitempty"` // JSON array
	// Evidence
	EvidenceBundleID string `gorm:"type:varchar(64)" json:"evidence_bundle_id,omitempty"`
	SpanDigest     string `gorm:"type:varchar(128)" json:"span_digest"` // content-addressed
}

// CommitBinding links a git commit to provenance (PRD §18.6, PAPER §43).
type CommitBinding struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index" json:"organization_id"`
	RepositoryID   string `gorm:"type:varchar(64);index;not null" json:"repository_id"`
	CommitSHA      string `gorm:"type:varchar(64);index;not null" json:"commit_sha"`
	ChangeSetID    string `gorm:"type:varchar(64);index" json:"change_set_id"`
	SessionID      string `gorm:"type:varchar(64)" json:"session_id,omitempty"`
	Branch         string `gorm:"type:varchar(255)" json:"branch,omitempty"`
	BoundAt        string `gorm:"type:timestamp" json:"bound_at"`
	BindingDigest  string `gorm:"type:varchar(128)" json:"binding_digest"`
}

// AuditEvent is an immutable audit log entry (PRD §40).
type AuditEvent struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	EventType      string `gorm:"type:varchar(64);not null;index" json:"event_type"`
	ActorID        string `gorm:"type:varchar(64)" json:"actor_id,omitempty"`
	ActorType      string `gorm:"type:varchar(32)" json:"actor_type,omitempty"` // user, admin, system, harness
	Action         string `gorm:"type:varchar(128)" json:"action"`
	ResourceType   string `gorm:"type:varchar(64)" json:"resource_type,omitempty"`
	ResourceID     string `gorm:"type:varchar(64)" json:"resource_id,omitempty"`
	Details        string `gorm:"type:text" json:"details,omitempty"` // JSON
	IPAddress      string `gorm:"type:varchar(64)" json:"ip_address,omitempty"`
	UserAgent      string `gorm:"type:varchar(512)" json:"user_agent,omitempty"`
	Result         string `gorm:"type:varchar(32);default:'success'" json:"result"` // success, failure, denied
	LegalHold      bool   `gorm:"default:false" json:"legal_hold"`
	EventDigest    string `gorm:"type:varchar(128)" json:"event_digest"`
	OccurredAt     string `gorm:"type:timestamp" json:"occurred_at"`
}

// EvidenceReceipt is a COSE-signed proof of an exchange (PAPER §34).
type EvidenceReceipt struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ExchangeID     string `gorm:"type:varchar(64);uniqueIndex;not null" json:"exchange_id"`
	SessionID      string `gorm:"type:varchar(64);index" json:"session_id"`
	// Chain
	FinalState     string `gorm:"type:varchar(32)" json:"final_state"`
	FirstEventSeq  uint64 `json:"first_event_seq"`
	LastEventSeq   uint64 `json:"last_event_seq"`
	ChainRoot      string `gorm:"type:varchar(128)" json:"chain_root"` // final Rn
	ProvenanceRoot string `gorm:"type:varchar(128)" json:"provenance_root,omitempty"`
	// Policy and identity
	PolicyEpochID  string `gorm:"type:varchar(64)" json:"policy_epoch_id"`
	LeaseDigest    string `gorm:"type:varchar(128)" json:"lease_digest,omitempty"`
	RelayIdentity  string `gorm:"type:varchar(128)" json:"relay_identity"`
	ModelPackageID string `gorm:"type:varchar(64)" json:"model_package_id,omitempty"`
	EndpointID     string `gorm:"type:varchar(64)" json:"endpoint_id,omitempty"`
	KeyAlgorithm   string `gorm:"type:varchar(32);default:'ed25519'" json:"key_algorithm"`
	// Signature
	Signature      string `gorm:"type:text" json:"signature"` // COSE-Sign1
	RedactionManifest string `gorm:"type:text" json:"redaction_manifest,omitempty"`
	IssuedAt       string `gorm:"type:timestamp" json:"issued_at"`
	// AcknowledgedAt records (RFC3339) when the harness confirmed
	// receipt of this evidence receipt over PAPER; empty until acked.
	AcknowledgedAt string `gorm:"type:timestamp" json:"acknowledged_at,omitempty"`
}
