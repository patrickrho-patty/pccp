package paper

// Model catalog message types (PCCP v2 §10A.4 — paper.models/1 extension).
// These extend the PAPER protocol to support server-authoritative model discovery.
const (
	MsgModelCatalogRequest   MessageType = 0x0D00
	MsgModelCatalogSnapshot  MessageType = 0x0D01
	MsgModelCatalogDelta     MessageType = 0x0D02
	MsgModelAnnounce         MessageType = 0x0D03
	MsgModelWithdraw         MessageType = 0x0D04
	MsgModelDefaultChanged   MessageType = 0x0D05
	MsgModelAvailability     MessageType = 0x0D06
	MsgModelCapabilityChanged MessageType = 0x0D07
	MsgModelUpgradeRequired  MessageType = 0x0D08
	MsgCatalogAck            MessageType = 0x0D09
)

// ModelCatalogRequestMessage requests the current effective model catalog.
type ModelCatalogRequestMessage struct {
	CatalogEpoch  string `cbor:"1,keyasint,omitempty"` // empty = request fresh
	ClientVersion string `cbor:"2,keyasint,omitempty"`
}

// ModelCatalogSnapshotMessage delivers the full effective catalog to the Harness.
// Per §10A.1: "PCCP is the authority" — the Harness renders models from this snapshot.
type ModelCatalogSnapshotMessage struct {
	CatalogEpoch   string            `cbor:"1,keyasint"`
	GeneratedAt    uint64            `cbor:"2,keyasint"`
	ValiditySecs   uint32            `cbor:"3,keyasint"`
	Models         []ModelDescriptorCBOR `cbor:"4,keyasint"`
	DefaultModelID string            `cbor:"5,keyasint,omitempty"`
	CPSignature    []byte            `cbor:"6,keyasint"`
}

// ModelDescriptorCBOR is the wire-format model descriptor sent to Harness.
type ModelDescriptorCBOR struct {
	CatalogModelID string `cbor:"1,keyasint"`
	DisplayName    string `cbor:"2,keyasint"`
	DisplayNameKo  string `cbor:"3,keyasint,omitempty"`
	Family         string `cbor:"4,keyasint"`
	Availability   string `cbor:"5,keyasint"`
	DefaultRank    int    `cbor:"6,keyasint"`
	// Capabilities (compact)
	SupportsText     bool `cbor:"10,keyasint"`
	SupportsImage    bool `cbor:"11,keyasint"`
	SupportsTools    bool `cbor:"12,keyasint"`
	SupportsParallel bool `cbor:"13,keyasint"`
	SupportsMCP      bool `cbor:"14,keyasint"`
	SupportsReasoning bool `cbor:"15,keyasint"`
	SupportsStreaming bool `cbor:"16,keyasint"`
	SupportsCache    bool `cbor:"17,keyasint"`
	// Limits
	MaxInputTokens  int `cbor:"20,keyasint"`
	MaxOutputTokens int `cbor:"21,keyasint"`
	// Entitlement
	EntitlementClass string `cbor:"30,keyasint"`
	EntitlementLabel string `cbor:"31,keyasint"`
	// Client requirements
	MinPaperAIVersion int `cbor:"40,keyasint"`
}

// ModelAnnounceMessage announces a newly available model.
type ModelAnnounceMessage struct {
	CatalogModelID string `cbor:"1,keyasint"`
	CatalogEpoch   string `cbor:"2,keyasint"`
	Reason         string `cbor:"3,keyasint,omitempty"`
}

// ModelWithdrawMessage withdraws a model from the catalog.
// Per §10A.8: the Harness must stop opening new exchanges against this model.
type ModelWithdrawMessage struct {
	CatalogModelID string `cbor:"1,keyasint"`
	CatalogEpoch   string `cbor:"2,keyasint"`
	Action         string `cbor:"3,keyasint"` // finish_current, stop_immediately, migrate, select_replacement
	ReplacementID  string `cbor:"4,keyasint,omitempty"`
	Reason         string `cbor:"5,keyasint,omitempty"`
	ReasonKo       string `cbor:"6,keyasint,omitempty"`
}
