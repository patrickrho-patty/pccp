package dari

// DARI AI Semantic v2 (PCCP v2 §10B).
// Provider-neutral AI semantic IR that is a functional superset of
// OpenAI Responses / Anthropic Messages / AI SDK.
// The Harness never speaks provider wire protocols.

// ContentPartType identifies a content part in an input/output item.
type ContentPartType string

const (
	ContentText       ContentPartType = "text"
	ContentImage      ContentPartType = "image"
	ContentAudio      ContentPartType = "audio"
	ContentFile       ContentPartType = "file"
	ContentPDF        ContentPartType = "pdf"
	ContentStructured ContentPartType = "structured"
	ContentRef        ContentPartType = "reference" // repo/context/file reference
)

// InputRole identifies the role of an input item.
type InputRole string

const (
	RoleUser       InputRole = "user"
	RoleSystem     InputRole = "system"
	RoleDeveloper  InputRole = "developer" // organization instruction
	RoleAssistant  InputRole = "assistant"
	RoleTool       InputRole = "tool"
)

// AIContentPart is a single content part within an input or output item.
type AIContentPart struct {
	Type     ContentPartType `cbor:"1,keyasint"`
	Text     string          `cbor:"2,keyasint,omitempty"`
	MimeType string          `cbor:"3,keyasint,omitempty"`
	Data     []byte          `cbor:"4,keyasint,omitempty"` // inline for small content
	RefID    string          `cbor:"5,keyasint,omitempty"` // reference to large content
	URL      string          `cbor:"6,keyasint,omitempty"`
	Schema   string          `cbor:"7,keyasint,omitempty"` // for structured content
}

// AIInputItem is an ordered input item in an AI exchange.
type AIInputItem struct {
	Role       InputRole       `cbor:"1,keyasint"`
	Parts      []AIContentPart `cbor:"2,keyasint"`
	ToolCallID string          `cbor:"3,keyasint,omitempty"` // for tool results
	ToolName   string          `cbor:"4,keyasint,omitempty"`
}

// ToolPlacement identifies where a tool executes (§10B.4, §10B.8).
type ToolPlacement string

const (
	ToolPlaceHarness    ToolPlacement = "HARNESS"
	ToolPlaceRuntime    ToolPlacement = "RUNTIME"
	ToolPlaceRelay      ToolPlacement = "RELAY_SERVICE"
	ToolPlacePIA        ToolPlacement = "PIA_SERVER"
	ToolPlaceMCP        ToolPlacement = "MCP"
	ToolPlaceExternal   ToolPlacement = "EXTERNAL_APPROVED_SERVICE"
)

// ToolChoiceMode defines how the model selects tools (§10B.5).
type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceNone     ToolChoiceMode = "none"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceSpecific ToolChoiceMode = "specific"
	ToolChoiceSubset   ToolChoiceMode = "subset"
)

// ToolDescriptorV2 describes a tool with full v2 semantics (§10B.4).
type ToolDescriptorV2 struct {
	ToolID           string         `cbor:"1,keyasint"`
	Name             string         `cbor:"2,keyasint"`
	Description      string         `cbor:"3,keyasint"`
	InputSchema      string         `cbor:"4,keyasint"`    // JSON Schema
	OutputSchema     string         `cbor:"5,keyasint,omitempty"`
	StrictSchema     bool           `cbor:"6,keyasint"`
	Placement        ToolPlacement  `cbor:"7,keyasint"`
	RiskClass        string         `cbor:"8,keyasint"`    // low, medium, high, critical
	ApprovalPolicy   string         `cbor:"9,keyasint"`    // auto, required, dual
	StreamingArgs    bool           `cbor:"10,keyasint"`
	MultimodalResult bool           `cbor:"11,keyasint"`
	IdempotencyClass string         `cbor:"12,keyasint"`
	Timeout          uint32         `cbor:"13,keyasint,omitempty"` // seconds
	ProvenanceReq    bool           `cbor:"14,keyasint"`
	ToolClass        string         `cbor:"15,keyasint"` // §10B.10: shell, apply_patch, file_read, etc.
}

// AIRequestV2 is the full DARI AI request with v2 semantics.
type AIRequestV2 struct {
	CatalogModelID string         `cbor:"1,keyasint"`
	CatalogEpoch   string         `cbor:"2,keyasint"`
	Inputs         []AIInputItem  `cbor:"3,keyasint"`
	// Tools
	Tools       []ToolDescriptorV2 `cbor:"4,keyasint,omitempty"`
	ToolChoice  ToolChoiceMode     `cbor:"5,keyasint,omitempty"`
	// Parameters
	MaxOutputTokens int            `cbor:"6,keyasint,omitempty"`
	Temperature     float64        `cbor:"7,keyasint,omitempty"`
	StopSequences   []string       `cbor:"8,keyasint,omitempty"`
	// Structured output
	OutputSchema    string         `cbor:"9,keyasint,omitempty"`
	StrictOutput    bool           `cbor:"10,keyasint,omitempty"`
	// Reasoning
	ReasoningEffort string         `cbor:"11,keyasint,omitempty"` // low, medium, high
	// Cache
	CacheDirective string         `cbor:"12,keyasint,omitempty"` // auto, write, read-only, none
	// Context
	ContextWindowLimit int        `cbor:"13,keyasint,omitempty"`
	CompactionPolicy   string     `cbor:"14,keyasint,omitempty"`
	// Continuation
	ParentExchangeID string       `cbor:"15,keyasint,omitempty"`
	// Background
	Background     bool           `cbor:"16,keyasint,omitempty"`
	// Provenance
	ProvenanceParents []string    `cbor:"17,keyasint,omitempty"`
}

// FinishReason is the normalized completion reason (§10B.18).
type FinishReason string

const (
	FinishCompleted      FinishReason = "COMPLETED"
	FinishMaxOutput      FinishReason = "MAX_OUTPUT"
	FinishStopSequence   FinishReason = "STOP_SEQUENCE"
	FinishToolCall       FinishReason = "TOOL_CALL"
	FinishPauseContinue  FinishReason = "PAUSE_CONTINUE"
	FinishRefusal        FinishReason = "REFUSAL"
	FinishContextLimit   FinishReason = "CONTEXT_LIMIT"
	FinishCancelled      FinishReason = "CANCELLED"
	FinishTimeout        FinishReason = "TIMEOUT"
	FinishSafetyBlock    FinishReason = "SAFETY_BLOCK"
	FinishError          FinishReason = "ERROR"
	FinishModelUnavailable FinishReason = "MODEL_UNAVAILABLE"
)

// UsageV2 is the normalized usage accounting (§10B.19).
type UsageV2 struct {
	InputTokens     uint64 `cbor:"1,keyasint"`
	OutputTokens    uint64 `cbor:"2,keyasint"`
	CacheReadTokens uint64 `cbor:"3,keyasint,omitempty"`
	CacheWriteTokens uint64 `cbor:"4,keyasint,omitempty"`
	ReasoningTokens uint64 `cbor:"5,keyasint,omitempty"`
	TotalTokens     uint64 `cbor:"6,keyasint"`
}

// AICompleteV2 is the completion message with v2 semantics.
type AICompleteV2 struct {
	FinishReason   FinishReason   `cbor:"1,keyasint"`
	Usage          UsageV2        `cbor:"2,keyasint"`
	CatalogModelID string         `cbor:"3,keyasint"`
	ModelPackageID string         `cbor:"4,keyasint,omitempty"` // exact PMP (diagnostics/provenance)
	EndpointID     string         `cbor:"5,keyasint,omitempty"`
	NativeReason   string         `cbor:"6,keyasint,omitempty"` // provider-specific for diagnostics
}

// Streaming event types (§10B.20)
type StreamingEventType string

const (
	StreamAccepted           StreamingEventType = "AI_ACCEPTED"
	StreamOutputItemStarted  StreamingEventType = "OUTPUT_ITEM_STARTED"
	StreamTextDelta          StreamingEventType = "TEXT_DELTA"
	StreamReasoningDelta     StreamingEventType = "REASONING_SUMMARY_DELTA"
	StreamToolCallStarted    StreamingEventType = "TOOL_CALL_STARTED"
	StreamToolArgDelta       StreamingEventType = "TOOL_ARGUMENT_DELTA"
	StreamToolCallReady      StreamingEventType = "TOOL_CALL_READY"
	StreamToolResult         StreamingEventType = "TOOL_RESULT"
	StreamOutputItemCompleted StreamingEventType = "OUTPUT_ITEM_COMPLETED"
	StreamUsageUpdate        StreamingEventType = "USAGE_UPDATE"
	StreamAICompleted        StreamingEventType = "AI_COMPLETED"
)

// AIStreamEvent is a single streaming event.
type AIStreamEvent struct {
	EventType  StreamingEventType `cbor:"1,keyasint"`
	ItemIndex  int                `cbor:"2,keyasint,omitempty"`
	TextDelta  string             `cbor:"3,keyasint,omitempty"`
	ToolCallID string             `cbor:"4,keyasint,omitempty"`
	ToolName   string             `cbor:"5,keyasint,omitempty"`
	ToolArgs   string             `cbor:"6,keyasint,omitempty"` // partial or complete JSON
	Usage      *UsageV2           `cbor:"7,keyasint,omitempty"`
	RefusalText string            `cbor:"8,keyasint,omitempty"`
}

// ToolCallV2 represents a model-proposed tool call with v2 semantics.
type ToolCallV2 struct {
	CallID    string `cbor:"1,keyasint"`
	ToolID    string `cbor:"2,keyasint"`
	ToolName  string `cbor:"3,keyasint"`
	Arguments string `cbor:"4,keyasint"` // JSON arguments
	Placement ToolPlacement `cbor:"5,keyasint"`
	Approved  bool   `cbor:"6,keyasint"` // filled after policy check
}

// ToolResultV2 represents a tool execution result.
type ToolResultV2 struct {
	CallID    string         `cbor:"1,keyasint"`
	Parts     []AIContentPart `cbor:"2,keyasint"`
	IsError   bool           `cbor:"3,keyasint"`
	ErrorMsg  string         `cbor:"4,keyasint,omitempty"`
}

// Citation represents a source citation in output (§10B.17).
type Citation struct {
	URL       string `cbor:"1,keyasint,omitempty"`
	Title     string `cbor:"2,keyasint,omitempty"`
	FileRef   string `cbor:"3,keyasint,omitempty"`
	StartPos  int    `cbor:"4,keyasint,omitempty"`
	EndPos    int    `cbor:"5,keyasint,omitempty"`
}

// Built-in coding tool classes (§10B.10)
const (
	ToolClassShell       = "shell"
	ToolClassApplyPatch  = "apply_patch"
	ToolClassFileRead    = "file_read"
	ToolClassFileSearch  = "file_search"
	ToolClassCodeSearch  = "code_search"
	ToolClassCodeExec    = "code_execution"
	ToolClassGit         = "git"
	ToolClassTest        = "test"
	ToolClassPackage     = "package"
	ToolClassNetworkFetch = "network_fetch"
	ToolClassWebSearch   = "web_search"
	ToolClassMCP         = "mcp"
	ToolClassCustomFunc  = "custom_function"
)
