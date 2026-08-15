// TestMessageStringRegistryComplete pins the registry: every named
// message type must render its name (no UNKNOWN drift).
package dari

import "testing"

func TestMessageStringRegistryComplete(t *testing.T) {
	named := []MessageType{
		MsgHello, MsgHelloAck, MsgPing, MsgPong, MsgDrain, MsgClose,
		MsgAuthChallenge, MsgAuthProof, MsgUserBind, MsgUserBindAck, MsgCapabilities, MsgAuthAck,
		MsgSessionOpen, MsgSessionGrant, MsgSessionClose, MsgLeaseIssue, MsgLeaseRevoke, MsgLeaseRenew,
		MsgExchangeOpen, MsgExchangeAction, MsgExchangeClose, MsgGovernanceEnv, MsgRelayVerdict,
		MsgApprovalRequest, MsgApprovalResult, MsgEvidenceReceipt, MsgEvidenceReceiptAck,
		MsgAIOpen, MsgInferenceRequest, MsgAITokenChunk, MsgAIComplete,
		MsgPIAEnroll, MsgEndpointRegister, MsgEndpointLease,
		MsgContextManifest, MsgContextDecision, MsgRepoBaseline,
		MsgToolProposal, MsgToolAuthorize, MsgRuntimeExecute, MsgToolResult,
		MsgProvenanceNode, MsgChangeSet, MsgCommitBind, MsgActionEnvelope,
		MsgChatMessage, MsgPresence,
		MsgFileOffer, MsgFileChunk, MsgFileCommit,
		MsgBroadcast, MsgAdminDirective, MsgAdminCommandResult,
		MsgTelemetry, MsgMeterUsage,
		MsgModelCatalogRequest, MsgModelCatalogSnapshot, MsgModelCatalogDelta,
		MsgModelAnnounce, MsgModelWithdraw, MsgModelDefaultChanged,
		MsgModelAvailability, MsgModelCapabilityChanged, MsgModelUpgradeRequired,
		MsgCatalogAck, MsgPolicyEpochPush, MsgDLPRulePack, MsgGovernanceState, MsgSovereignAdvisory, MsgChangeSetNack,
	}
	for _, m := range named {
		if s := m.String(); s == "UNKNOWN" {
			t.Errorf("message 0x%04x renders UNKNOWN — String() registry incomplete", uint16(m))
		}
	}
}
