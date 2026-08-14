package dari

import (
	"errors"
	"sort"
)

// HelloMessage is the HELLO message sent by the initiating peer (DARI §15.1).
type HelloMessage struct {
	CoreVersions          []uint8            `cbor:"1,keyasint"`
	PeerProfile           PeerProfile        `cbor:"2,keyasint"`
	TransportFeatures     []string           `cbor:"3,keyasint"`
	Extensions            map[string]uint8   `cbor:"4,keyasint"`
	EncodingProfiles      []string           `cbor:"5,keyasint"`
	CryptoProfiles        []string           `cbor:"6,keyasint"`
	ClientNonce           []byte             `cbor:"7,keyasint"`
	CredentialHint        []byte             `cbor:"8,keyasint,omitempty"`
	ImplementationName    string             `cbor:"9,keyasint,omitempty"`
	ImplementationVersion string             `cbor:"10,keyasint,omitempty"`
}

// HelloAckMessage is the HELLO_ACK from the Relay (DARI §15.2).
type HelloAckMessage struct {
	CoreVersion          uint8            `cbor:"1,keyasint"`
	ExtensionVersions    map[string]uint8 `cbor:"2,keyasint"`
	CryptoProfile        string           `cbor:"3,keyasint"`
	ServerNonce          []byte           `cbor:"4,keyasint"`
	RelayCredential      []byte           `cbor:"5,keyasint"` // COSE-Sign1 PPC
	AuthChallenge        []byte           `cbor:"6,keyasint"` // AUTH_CHALLENGE payload
	MinHarnessVersion    string           `cbor:"7,keyasint,omitempty"`
	ResourceLimits       map[string]uint64 `cbor:"8,keyasint,omitempty"`
}

// AuthChallengeMessage is the AUTH_CHALLENGE (DARI §18.1).
type AuthChallengeMessage struct {
	ServerNonce          []byte `cbor:"1,keyasint"`
	ChallengeID          []byte `cbor:"2,keyasint"`
	CredentialIssuers    []string `cbor:"3,keyasint"`
	RevocationEpoch      uint64 `cbor:"4,keyasint"`
	AuthDeadlineMs       uint64 `cbor:"5,keyasint"`
}

// AuthProofMessage is the AUTH_PROOF (DARI §18.2).
type AuthProofMessage struct {
	Credential           []byte `cbor:"1,keyasint"` // COSE-Sign1 PPC
	Signature            []byte `cbor:"2,keyasint"`
	KeyAlgorithm         COSEAlgorithm `cbor:"3,keyasint"`
	ChallengeID          []byte `cbor:"4,keyasint"`
	RevocationEvidence   []byte `cbor:"5,keyasint,omitempty"`
}

// UserBindMessage binds a user identity to the connection (DARI §19).
type UserBindMessage struct {
	UserAssertion        []byte `cbor:"1,keyasint"` // SSO assertion or token
	Organization         string `cbor:"2,keyasint"`
	ClaimedUserID        string `cbor:"3,keyasint"`
	HarnessID            string `cbor:"4,keyasint"`
	RequestedPersona     string `cbor:"5,keyasint,omitempty"`
	AuthAssurance        string `cbor:"6,keyasint,omitempty"`
}

// UserBindAckMessage acknowledges a user binding (DARI §19).
type UserBindAckMessage struct {
	CanonicalUserID      string `cbor:"1,keyasint"`
	Organization         string `cbor:"2,keyasint"`
	GroupRoleRefs        []string `cbor:"3,keyasint"`
	BindingExpiryMs      uint64 `cbor:"4,keyasint"`
	UserPolicyEpoch      string `cbor:"5,keyasint"`
	ReAuthDeadlineMs     uint64 `cbor:"6,keyasint,omitempty"`
}

// CapabilitiesMessage advertises supported extensions (DARI §20).
type CapabilitiesMessage struct {
	Extensions []string `cbor:"1,keyasint"`
}

// SessionOpenMessage requests a working session (DARI §21).
type SessionOpenMessage struct {
	Organization      string   `cbor:"1,keyasint"`
	UserBinding       string   `cbor:"2,keyasint"`
	Project           string   `cbor:"3,keyasint"`
	Repository        string   `cbor:"4,keyasint,omitempty"`
	Branch            string   `cbor:"5,keyasint,omitempty"`
	TaskPurpose       string   `cbor:"6,keyasint,omitempty"`
	RequestedExtensions []string `cbor:"7,keyasint"`
	RequestedModel    string   `cbor:"8,keyasint"`
	RequestedTools    []string `cbor:"9,keyasint"`
	RetentionProfile  string   `cbor:"10,keyasint,omitempty"`
	SessionNonce      []byte   `cbor:"11,keyasint"`
}

// SessionGrantMessage grants a working session (DARI §21).
type SessionGrantMessage struct {
	SessionID         string   `cbor:"1,keyasint"`
	PolicyEpoch       string   `cbor:"2,keyasint"`
	CapabilityLease   []byte   `cbor:"3,keyasint"` // COSE-Sign1 lease
	RetentionSummary  string   `cbor:"4,keyasint,omitempty"`
	AllowedModels     []string `cbor:"5,keyasint"`
	ProtectionProfile uint8    `cbor:"6,keyasint"`
	SessionTTL        uint32   `cbor:"7,keyasint"`
	IdleTTL           uint32   `cbor:"8,keyasint"`
	ResumptionPolicy  string   `cbor:"9,keyasint,omitempty"`
}

// GovernanceEnvelope carries policy context for a governed exchange (DARI §26).
type GovernanceEnvelope struct {
	LeaseID              string   `cbor:"1,keyasint"`
	PolicyEpoch          string   `cbor:"2,keyasint"`
	Organization         string   `cbor:"3,keyasint"`
	UserID               string   `cbor:"4,keyasint"`
	HarnessID            string   `cbor:"5,keyasint"`
	SessionID            string   `cbor:"6,keyasint"`
	ProjectID            string   `cbor:"7,keyasint,omitempty"`
	RepositoryID         string   `cbor:"8,keyasint,omitempty"`
	Branch               string   `cbor:"9,keyasint,omitempty"`
	Classification       string   `cbor:"10,keyasint"`
	Purpose              string   `cbor:"11,keyasint"`
	RequestedCapabilities []string `cbor:"12,keyasint"`
	ModelAuthorization   string   `cbor:"13,keyasint,omitempty"`
	ToolAuthorization    string   `cbor:"14,keyasint,omitempty"`
	ProtectionProfile    uint8    `cbor:"15,keyasint"`
	ApprovalRequirements []string `cbor:"16,keyasint"`
	RetentionProfile     string   `cbor:"17,keyasint,omitempty"`
}

// RelayVerdictMessage carries a structured relay verdict (DARI §27).
type RelayVerdictMessage struct {
	VerdictID        string         `cbor:"1,keyasint"`
	ExchangeID       string         `cbor:"2,keyasint"`
	RelayID          string         `cbor:"3,keyasint"`
	PolicyEpoch      string         `cbor:"4,keyasint"`
	RuleIDs          []string       `cbor:"5,keyasint"`
	ReasonCodes      []string       `cbor:"6,keyasint"`
	Transformations  []string       `cbor:"7,keyasint,omitempty"`
	Obligations      []string       `cbor:"8,keyasint,omitempty"`
	VerdictTime      uint64         `cbor:"9,keyasint"`
	EvidenceDigest   []byte         `cbor:"10,keyasint,omitempty"`
	AuthMode         string         `cbor:"11,keyasint"`
	Result           VerdictResult  `cbor:"12,keyasint"`
}

// ProvenanceNodeMessage carries a provenance spine node (DARI §33).
type ProvenanceNodeMessage struct {
	NodeType        string   `cbor:"1,keyasint"`
	ActorPeerID     string   `cbor:"2,keyasint"`
	SessionID       string   `cbor:"3,keyasint"`
	ExchangeID      string   `cbor:"4,keyasint"`
	PolicyEpoch     string   `cbor:"5,keyasint,omitempty"`
	ObjectRefs      []string `cbor:"6,keyasint,omitempty"`
	CausalParents   []string `cbor:"7,keyasint,omitempty"`
	Result          string   `cbor:"8,keyasint"`
	Summary         string   `cbor:"9,keyasint,omitempty"`
	NodeDigest      []byte   `cbor:"10,keyasint"`
}

// EvidenceReceiptMessage carries an evidence receipt (DARI §34).
type EvidenceReceiptMessage struct {
	ExchangeID        string `cbor:"1,keyasint"`
	FinalState        string `cbor:"2,keyasint"`
	FirstEventSeq     uint64 `cbor:"3,keyasint"`
	LastEventSeq      uint64 `cbor:"4,keyasint"`
	ChainRoot         []byte `cbor:"5,keyasint"`
	ProvenanceRoot    []byte `cbor:"6,keyasint,omitempty"`
	PolicyEpoch       string `cbor:"7,keyasint"`
	LeaseDigest       []byte `cbor:"8,keyasint,omitempty"`
	RelayIdentity     string `cbor:"9,keyasint"`
	ModelPackageID    string `cbor:"10,keyasint,omitempty"`
	EndpointID        string `cbor:"11,keyasint,omitempty"`
	KeyAlgorithm      string `cbor:"12,keyasint"`
	Signature         []byte `cbor:"13,keyasint"`
	RedactionManifest []byte `cbor:"14,keyasint,omitempty"`
}

// AIOpenMessage requests an AI inference exchange (DARI §39.1).
type AIOpenMessage struct {
	TaskReference     string   `cbor:"1,keyasint"`
	RequestedModel    string   `cbor:"2,keyasint"`
	InferenceMode     string   `cbor:"3,keyasint"`
	MaxInputTokens    uint32   `cbor:"4,keyasint"`
	MaxOutputTokens   uint32   `cbor:"5,keyasint"`
	ContextManifestRef []byte  `cbor:"6,keyasint,omitempty"`
	ResponseFormat    string   `cbor:"7,keyasint,omitempty"`
	PermittedTools    []string `cbor:"8,keyasint,omitempty"`
	ModelParameters   map[string]interface{} `cbor:"9,keyasint,omitempty"`
	ProvenanceParents []string `cbor:"10,keyasint,omitempty"`
}

// AICompleteMessage signals inference completion (DARI §39.5).
type AICompleteMessage struct {
	CompletionReason  string `cbor:"1,keyasint"`
	InputTokens       uint64 `cbor:"2,keyasint"`
	OutputTokens      uint64 `cbor:"3,keyasint"`
	ModelPackage      string `cbor:"4,keyasint"`
	InferenceEndpoint string `cbor:"5,keyasint"`
	EngineMetadata    map[string]interface{} `cbor:"6,keyasint,omitempty"`
	OutputDigest      []byte `cbor:"7,keyasint"`
	ToolProposals     []string `cbor:"8,keyasint,omitempty"`
	ErrorState        string `cbor:"9,keyasint,omitempty"`
}

// canonicalHelloForAuth renders the HELLO into deterministic CBOR for
// the AUTH transcript hash. Map fields are encoded as key-SORTED
// arrays so both peers compute identical bytes regardless of Go's
// random map iteration order.
type canonicalKV struct {
	Key   string `cbor:"1,keyasint"`
	Value uint8  `cbor:"2,keyasint"`
}

type canonicalHello struct {
	CoreVersions          []uint8        `cbor:"1,keyasint"`
	PeerProfile           PeerProfile    `cbor:"2,keyasint"`
	TransportFeatures     []string       `cbor:"3,keyasint"`
	Extensions            []canonicalKV  `cbor:"4,keyasint"`
	EncodingProfiles      []string       `cbor:"5,keyasint"`
	CryptoProfiles        []string       `cbor:"6,keyasint"`
	ClientNonce           []byte         `cbor:"7,keyasint"`
	CredentialHint        []byte         `cbor:"8,keyasint,omitempty"`
	ImplementationName    string         `cbor:"9,keyasint,omitempty"`
	ImplementationVersion string         `cbor:"10,keyasint,omitempty"`
}

type canonicalAck struct {
	CoreVersion       uint8             `cbor:"1,keyasint"`
	ExtensionVersions []canonicalKV     `cbor:"2,keyasint"`
	CryptoProfile     string            `cbor:"3,keyasint"`
	ServerNonce       []byte            `cbor:"4,keyasint"`
	RelayCredential   []byte            `cbor:"5,keyasint"`
	AuthChallenge     []byte            `cbor:"6,keyasint"`
	MinHarnessVersion string            `cbor:"7,keyasint,omitempty"`
	ResourceLimits    []canonicalLimit  `cbor:"8,keyasint,omitempty"`
}

type canonicalLimit struct {
	Key   string `cbor:"1,keyasint"`
	Value uint64 `cbor:"2,keyasint"`
}

func sortedKVs(m map[string]uint8) []canonicalKV {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]canonicalKV, 0, len(m))
	for _, k := range keys {
		out = append(out, canonicalKV{Key: k, Value: m[k]})
	}
	return out
}

func sortedLimits(m map[string]uint64) []canonicalLimit {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]canonicalLimit, 0, len(m))
	for _, k := range keys {
		out = append(out, canonicalLimit{Key: k, Value: m[k]})
	}
	return out
}

// CanonicalHelloCBOR returns the deterministic CBOR encoding of a HELLO
// used by both peers when computing the AUTH transcript.
func CanonicalHelloCBOR(h *HelloMessage) ([]byte, error) {
	if h == nil {
		return nil, errors.New("dari: nil hello")
	}
	return MarshalCBOR(canonicalHello{
		CoreVersions:          h.CoreVersions,
		PeerProfile:           h.PeerProfile,
		TransportFeatures:     h.TransportFeatures,
		Extensions:            sortedKVs(h.Extensions),
		EncodingProfiles:      h.EncodingProfiles,
		CryptoProfiles:        h.CryptoProfiles,
		ClientNonce:           h.ClientNonce,
		CredentialHint:        h.CredentialHint,
		ImplementationName:    h.ImplementationName,
		ImplementationVersion: h.ImplementationVersion,
	})
}

// CanonicalAckCBOR returns the deterministic CBOR encoding of a
// HELLO_ACK used by both peers when computing the AUTH transcript.
func CanonicalAckCBOR(a *HelloAckMessage) ([]byte, error) {
	if a == nil {
		return nil, errors.New("dari: nil ack")
	}
	return MarshalCBOR(canonicalAck{
		CoreVersion:       a.CoreVersion,
		ExtensionVersions: sortedKVs(a.ExtensionVersions),
		CryptoProfile:     a.CryptoProfile,
		ServerNonce:       a.ServerNonce,
		RelayCredential:   a.RelayCredential,
		AuthChallenge:     a.AuthChallenge,
		MinHarnessVersion: a.MinHarnessVersion,
		ResourceLimits:    sortedLimits(a.ResourceLimits),
	})
}
