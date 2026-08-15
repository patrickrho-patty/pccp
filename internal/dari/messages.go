package dari

import (
	"fmt"
)

// MessageType is a DARI message type registry value (DARI §13).
type MessageType uint16

// Core / connection (0x0000–0x00FF)
const (
	MsgHello    MessageType = 0x0001
	MsgHelloAck MessageType = 0x0002
	MsgPing     MessageType = 0x0003
	MsgPong     MessageType = 0x0004
	MsgDrain    MessageType = 0x0005
	MsgClose    MessageType = 0x0006
)

// Authentication / identity (0x0100–0x01FF)
const (
	MsgAuthChallenge MessageType = 0x0100
	MsgAuthProof     MessageType = 0x0101
	MsgUserBind      MessageType = 0x0102
	MsgUserBindAck   MessageType = 0x0103
	MsgCapabilities  MessageType = 0x0104
	MsgAuthAck       MessageType = 0x0105
)

// Sessions / capability leases (0x0200–0x02FF)
const (
	MsgSessionOpen  MessageType = 0x0200
	MsgSessionGrant MessageType = 0x0201
	MsgSessionClose MessageType = 0x0202
	MsgLeaseIssue   MessageType = 0x0210
	MsgLeaseRevoke  MessageType = 0x0211
	MsgLeaseRenew   MessageType = 0x0212
)

// Governance / approvals (0x0300–0x03FF)
const (
	MsgExchangeOpen    MessageType = 0x0300
	MsgExchangeAction  MessageType = 0x0301
	MsgExchangeClose   MessageType = 0x0302
	MsgGovernanceEnv   MessageType = 0x0303
	MsgRelayVerdict    MessageType = 0x0304
	MsgApprovalRequest MessageType = 0x0305
	MsgApprovalResult  MessageType = 0x0306
	MsgEvidenceReceipt MessageType = 0x0307
	// MsgEvidenceReceiptAck is the connector's tamper-evidence ack for
	// a received receipt (cross-repo registry; see relay/paper_wire.go).
	MsgEvidenceReceiptAck MessageType = 0x0308
)

// AI inference (0x0400–0x04FF)
const (
	MsgAIOpen           MessageType = 0x0400
	MsgInferenceRequest MessageType = 0x0401
	MsgAITokenChunk     MessageType = 0x0402
	MsgAIComplete       MessageType = 0x0403
	MsgPIAEnroll        MessageType = 0x0410
	MsgEndpointRegister MessageType = 0x0411
	MsgEndpointLease    MessageType = 0x0412
)

// Context / repository (0x0500–0x05FF)
const (
	MsgContextManifest MessageType = 0x0500
	MsgContextDecision MessageType = 0x0501
	MsgRepoBaseline    MessageType = 0x0510
)

// Tools / runtime (0x0600–0x06FF)
const (
	MsgToolProposal   MessageType = 0x0600
	MsgToolAuthorize  MessageType = 0x0601
	MsgRuntimeExecute MessageType = 0x0602
	MsgToolResult     MessageType = 0x0603
)

// Provenance / evidence (0x0700–0x07FF)
const (
	MsgProvenanceNode MessageType = 0x0700
	MsgChangeSet      MessageType = 0x0701
	MsgCommitBind     MessageType = 0x0702
	// MsgActionEnvelope carries a connector-built action record for
	// the audit stream (cross-repo registry; see relay/paper_wire.go).
	MsgActionEnvelope MessageType = 0x0703
	MsgChangeSetNack  MessageType = 0x0704
)

// Chat / presence (0x0800–0x08FF)
const (
	MsgChatMessage MessageType = 0x0800
	MsgPresence    MessageType = 0x0801
)

// Files (0x0A00–0x0AFF)
const (
	MsgFileOffer  MessageType = 0x0A00
	MsgFileChunk  MessageType = 0x0A01
	MsgFileCommit MessageType = 0x0A02
)

// Broadcast / administration (0x0B00–0x0BFF)
const (
	MsgBroadcast      MessageType = 0x0B00
	MsgAdminDirective MessageType = 0x0B01
	// MsgAdminCommandResult is the connector's execution-evidence
	// reply to an admin directive (cross-repo registry).
	MsgAdminCommandResult MessageType = 0x0B02
	MsgSovereignAdvisory  MessageType = 0x0B03
)

// Session-governance extensions (0x0D00–0x0DFF) — the extension
// registry shared with the harness connector (relay/paper_wire.go).
// The 0x0D0x catalog block lives in models.go; 0x0D10 is the
// policy-epoch push bound to a session setup.
const (
	MsgPolicyEpochPush MessageType = 0x0D10
	MsgDLPRulePack     MessageType = 0x0D11
	MsgGovernanceState MessageType = 0x0D12
)

// Telemetry / metering (0x0C00–0x0CFF)
const (
	MsgTelemetry  MessageType = 0x0C00
	MsgMeterUsage MessageType = 0x0C01
)

// String returns the symbolic name of a message type.
func (mt MessageType) String() string {
	switch mt {
	case MsgHello:
		return "HELLO"
	case MsgHelloAck:
		return "HELLO_ACK"
	case MsgPing:
		return "PING"
	case MsgPong:
		return "PONG"
	case MsgDrain:
		return "DRAIN"
	case MsgClose:
		return "CLOSE"
	case MsgAuthChallenge:
		return "AUTH_CHALLENGE"
	case MsgAuthProof:
		return "AUTH_PROOF"
	case MsgUserBind:
		return "USER_BIND"
	case MsgUserBindAck:
		return "USER_BIND_ACK"
	case MsgCapabilities:
		return "CAPABILITIES"
	case MsgSessionOpen:
		return "SESSION_OPEN"
	case MsgSessionGrant:
		return "SESSION_GRANT"
	case MsgSessionClose:
		return "SESSION_CLOSE"
	case MsgLeaseIssue:
		return "LEASE_ISSUE"
	case MsgLeaseRevoke:
		return "LEASE_REVOKE"
	case MsgLeaseRenew:
		return "LEASE_RENEW"
	case MsgExchangeOpen:
		return "EXCHANGE_OPEN"
	case MsgExchangeAction:
		return "EXCHANGE_ACTION"
	case MsgExchangeClose:
		return "EXCHANGE_CLOSE"
	case MsgGovernanceEnv:
		return "GOVERNANCE_ENV"
	case MsgRelayVerdict:
		return "RELAY_VERDICT"
	case MsgApprovalRequest:
		return "APPROVAL_REQUEST"
	case MsgApprovalResult:
		return "APPROVAL_RESULT"
	case MsgEvidenceReceipt:
		return "EVIDENCE_RECEIPT"
	case MsgAIOpen:
		return "AI_OPEN"
	case MsgInferenceRequest:
		return "INFERENCE_REQUEST"
	case MsgAITokenChunk:
		return "AI_TOKEN_CHUNK"
	case MsgAIComplete:
		return "AI_COMPLETE"
	case MsgPIAEnroll:
		return "PIA_ENROLL"
	case MsgEndpointRegister:
		return "ENDPOINT_REGISTER"
	case MsgEndpointLease:
		return "ENDPOINT_LEASE"
	case MsgContextManifest:
		return "CONTEXT_MANIFEST"
	case MsgContextDecision:
		return "CONTEXT_DECISION"
	case MsgRepoBaseline:
		return "REPO_BASELINE"
	case MsgToolProposal:
		return "TOOL_PROPOSAL"
	case MsgToolAuthorize:
		return "TOOL_AUTHORIZE"
	case MsgRuntimeExecute:
		return "RUNTIME_EXECUTE"
	case MsgToolResult:
		return "TOOL_RESULT"
	case MsgProvenanceNode:
		return "PROVENANCE_NODE"
	case MsgChangeSet:
		return "CHANGE_SET"
	case MsgCommitBind:
		return "COMMIT_BIND"
	case MsgChatMessage:
		return "CHAT_MESSAGE"
	case MsgPresence:
		return "PRESENCE"
	case MsgFileOffer:
		return "FILE_OFFER"
	case MsgFileChunk:
		return "FILE_CHUNK"
	case MsgFileCommit:
		return "FILE_COMMIT"
	case MsgBroadcast:
		return "BROADCAST"
	case MsgAdminDirective:
		return "ADMIN_DIRECTIVE"
	case MsgTelemetry:
		return "TELEMETRY"
	case MsgMeterUsage:
		return "METER_USAGE"
	case MsgEvidenceReceiptAck:
		return "EVIDENCE_RECEIPT_ACK"
	case MsgActionEnvelope:
		return "ACTION_ENVELOPE"
	case MsgAdminCommandResult:
		return "ADMIN_COMMAND_RESULT"
	case MsgSovereignAdvisory:
		return "SOVEREIGN_ADVISORY"
	case MsgChangeSetNack:
		return "CHANGESET_NACK"
	case MsgPolicyEpochPush:
		return "POLICY_EPOCH"
	case MsgDLPRulePack:
		return "DLP_RULE_PACK"
	case MsgGovernanceState:
		return "GOVERNANCE_STATE"
	case MsgModelCatalogRequest:
		return "MODEL_CATALOG_REQUEST"
	case MsgModelCatalogSnapshot:
		return "MODEL_CATALOG_SNAPSHOT"
	case MsgModelCatalogDelta:
		return "MODEL_CATALOG_DELTA"
	case MsgModelAnnounce:
		return "MODEL_ANNOUNCE"
	case MsgModelWithdraw:
		return "MODEL_WITHDRAW"
	case MsgModelDefaultChanged:
		return "MODEL_DEFAULT_CHANGED"
	case MsgModelAvailability:
		return "MODEL_AVAILABILITY"
	case MsgModelCapabilityChanged:
		return "MODEL_CAPABILITY_CHANGED"
	case MsgModelUpgradeRequired:
		return "MODEL_UPGRADE_REQUIRED"
	case MsgCatalogAck:
		return "CATALOG_ACK"
	default:
		return fmt.Sprintf("UNKNOWN(0x%04X)", uint16(mt))
	}
}

// PeerProfile identifies the role of a DARI peer (DARI §6).
type PeerProfile string

const (
	ProfileRelay     PeerProfile = "RELAY"
	ProfileHarness   PeerProfile = "HARNESS"
	ProfileInference PeerProfile = "INFERENCE"
	ProfileControl   PeerProfile = "CONTROL"
)

// HeaderKey is a numeric label for DARI common header fields (DARI §12).
type HeaderKey int

const (
	HKExchangeID        HeaderKey = 1
	HKSessionID         HeaderKey = 2
	HKMessageID         HeaderKey = 3
	HKParentIDs         HeaderKey = 4
	HKCreatedAtMs       HeaderKey = 5
	HKOrganizationID    HeaderKey = 6
	HKPeerID            HeaderKey = 7
	HKLeaseID           HeaderKey = 8
	HKPolicyEpoch       HeaderKey = 9
	HKProvenanceParents HeaderKey = 10
	HKContentType       HeaderKey = 11
	HKProtectionProfile HeaderKey = 12
	HKIdempotencyKey    HeaderKey = 13
	HKClassification    HeaderKey = 14
	HKCriticalFields    HeaderKey = 15
)

// ProtectionProfile is a DARI payload protection level (DARI §36).
type ProtectionProfile uint8

const (
	ProtectionP0 ProtectionProfile = 0 // Relay Inspectable
	ProtectionP1 ProtectionProfile = 1 // Service Sealed
	ProtectionP2 ProtectionProfile = 2 // Endpoint Sealed
	ProtectionP3 ProtectionProfile = 3 // Group E2EE
)

// ExchangeState is the state of a governed exchange (DARI §25).
type ExchangeState string

const (
	ExchangeCreated         ExchangeState = "CREATED"
	ExchangeAuthorizing     ExchangeState = "AUTHORIZING"
	ExchangeDenied          ExchangeState = "DENIED"
	ExchangeQuarantined     ExchangeState = "QUARANTINED"
	ExchangeWaitingApproval ExchangeState = "WAITING_APPROVAL"
	ExchangeAuthorized      ExchangeState = "AUTHORIZED"
	ExchangeActive          ExchangeState = "ACTIVE"
	ExchangeTerminated      ExchangeState = "TERMINATED"
	ExchangeFailed          ExchangeState = "FAILED"
	ExchangeFinalizing      ExchangeState = "FINALIZING"
	ExchangeCompleted       ExchangeState = "COMPLETED"
)

// VerdictResult is the outcome of a relay enforcement verdict (DARI §27).
type VerdictResult string

const (
	VerdictAllow               VerdictResult = "ALLOW"
	VerdictAllowTransform      VerdictResult = "ALLOW_TRANSFORM"
	VerdictAllowWithObligation VerdictResult = "ALLOW_WITH_OBLIGATION"
	VerdictRequireConfirmation VerdictResult = "REQUIRE_USER_CONFIRMATION"
	VerdictRequireApproval     VerdictResult = "REQUIRE_REVIEWER_APPROVAL"
	VerdictRequireSecurity     VerdictResult = "REQUIRE_SECURITY_APPROVAL"
	VerdictRequireDual         VerdictResult = "REQUIRE_DUAL_APPROVAL"
	VerdictQuarantine          VerdictResult = "QUARANTINE"
	VerdictDeny                VerdictResult = "DENY"
	VerdictTerminateSession    VerdictResult = "TERMINATE_SESSION"
	VerdictIsolateRuntime      VerdictResult = "ISOLATE_RUNTIME"
	VerdictCreateIncident      VerdictResult = "CREATE_INCIDENT"
)

// AssuranceLevel is the endpoint attestation assurance level (PCCP PRD §9.6).
type AssuranceLevel string

const (
	AssuranceL1 AssuranceLevel = "L1" // Software Verified
	AssuranceL2 AssuranceLevel = "L2" // Host Attested
	AssuranceL3 AssuranceLevel = "L3" // Confidential / Hardware Attested
)

// ConnectionState is the DARI connection state machine (DARI §14).
type ConnectionState string

const (
	StateNew               ConnectionState = "NEW"
	StateTransportReady    ConnectionState = "TRANSPORT_READY"
	StateNegotiated        ConnectionState = "NEGOTIATED"
	StatePeerAuthenticated ConnectionState = "PEER_AUTHENTICATED"
	StateIdentityBound     ConnectionState = "IDENTITY_BOUND"
	StateReady             ConnectionState = "READY"
	StateDraining          ConnectionState = "DRAINING"
	StateClosed            ConnectionState = "CLOSED"
)
