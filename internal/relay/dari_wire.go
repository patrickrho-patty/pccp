package relay

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// This file is the cross-repo wire contract for the DARI extension
// messages the relay exchanges with the harness connector
// (`patty-code-pccp/internal/provenancewire` + `internal/dariproto`).
//
// The two repos cannot import each other, so the CBOR field numbers
// below MUST stay in lockstep with the connector's envelope structs.
// The cross-repo conformance suites (internal/lease_conformance,
// internal/provenance_conformance) pin the byte formats; this layer
// pins the transport wiring.
//
// Canonical extension registry (relay ↔ connector):
//
//	0x0210 LEASE_ISSUE        relay→conn  body: wireLease
//	0x0211 LEASE_REVOKE       relay→conn  body: {lease_id}
//	0x0212 LEASE_RENEW        relay→conn  body: wireLease
//	0x0307 EVIDENCE_RECEIPT   relay→conn  body: wireEvidenceReceipt
//	0x0308 EVIDENCE_RECEIPT_ACK conn→relay body: wireReceiptAck
//	0x0700 PROVENANCE_SPAN    conn→relay  body: wireSpanEnvelope
//	0x0701 PROVENANCE_CHANGESET conn→relay body: wireChangeSetEnvelope
//	0x0702 PROVENANCE_COMMIT_BIND conn→relay body: wireCommitBindingEnvelope
//	0x0703 ACTION_ENVELOPE    conn→relay  body: wireActionEnvelope
//	0x0800 CHAT_MESSAGE       relay→conn  (comms)
//	0x0B00 BROADCAST          relay→conn  (comms)
//	0x0B01 ADMIN_DIRECTIVE    relay→conn  body: connector admin.Command CBOR
//	0x0B02 ADMIN_COMMAND_RESULT conn→relay
//	0x0D01 CATALOG_SNAPSHOT   relay→conn  body: wireCatalogSnapshot
//	0x0D10 POLICY_EPOCH       relay→conn  body: wirePolicyEpoch

// ---------------------------------------------------------------------------
// Lease (relay → connector). Mirrors dariproto.Lease field numbers.
// ---------------------------------------------------------------------------

type wireLease struct {
	Version            uint16              `cbor:"1,keyasint"`
	Issuer             string              `cbor:"2,keyasint"`
	LeaseID            string              `cbor:"3,keyasint"`
	SubjectPeerID      string              `cbor:"4,keyasint"`
	UserID             string              `cbor:"5,keyasint"`
	SessionID          string              `cbor:"6,keyasint,omitempty"`
	PolicyEpochID      string              `cbor:"7,keyasint"`
	AllowedModels      []string            `cbor:"8,keyasint,omitempty"`
	RepositoryScope    []map[string]string `cbor:"9,keyasint,omitempty"`
	FilePathReadScope  []string            `cbor:"10,keyasint,omitempty"`
	FilePathWriteScope []string            `cbor:"11,keyasint,omitempty"`
	ToolClasses        []string            `cbor:"12,keyasint,omitempty"`
	TokenBudget        int64               `cbor:"13,keyasint,omitempty"`
	NotBeforeUnixMs    int64               `cbor:"14,keyasint"`
	NotAfterUnixMs     int64               `cbor:"15,keyasint"`
	LeaseSequence      uint64              `cbor:"16,keyasint"`
	IssuedAtUnixMs     int64               `cbor:"17,keyasint,omitempty"`
	Status             string              `cbor:"18,keyasint,omitempty"`
	Signature          string              `cbor:"19,keyasint,omitempty"`
}

func buildWireLease(lease *models.CapabilityLease, issuerID string) (*wireLease, error) {
	if lease == nil {
		return nil, fmt.Errorf("relay: nil lease")
	}
	scope, err := parseLeaseScope(lease)
	if err != nil {
		return nil, err
	}
	nb, err := time.Parse(time.RFC3339, lease.NotBefore)
	if err != nil {
		return nil, fmt.Errorf("relay: lease not-before %q: %w", lease.NotBefore, err)
	}
	na, err := time.Parse(time.RFC3339, lease.NotAfter)
	if err != nil {
		return nil, fmt.Errorf("relay: lease not-after %q: %w", lease.NotAfter, err)
	}
	ia, _ := time.Parse(time.RFC3339, lease.IssuedAt)
	return &wireLease{
		Version:            1,
		Issuer:             issuerID,
		LeaseID:            lease.LeaseID,
		SubjectPeerID:      lease.SubjectPeerID,
		UserID:             lease.UserID,
		SessionID:          lease.SessionID,
		PolicyEpochID:      lease.PolicyEpochID,
		AllowedModels:      scope.AllowedModels,
		RepositoryScope:    scope.RepoScope,
		FilePathReadScope:  scope.ReadPaths,
		FilePathWriteScope: scope.WritePaths,
		ToolClasses:        scope.Tools,
		TokenBudget:        lease.TokenBudget,
		NotBeforeUnixMs:    nb.UnixMilli(),
		NotAfterUnixMs:     na.UnixMilli(),
		LeaseSequence:      uint64(lease.LeaseSequence),
		IssuedAtUnixMs:     ia.UnixMilli(),
		Status:             lease.Status,
		Signature:          lease.CPSignature,
	}, nil
}

// ---------------------------------------------------------------------------
// Policy epoch (relay → connector). Mirrors dariproto.PolicyEpoch.
// ---------------------------------------------------------------------------

type wirePolicyEpoch struct {
	EpochID           string `cbor:"1,keyasint"`
	IssuedAtUnixMs    int64  `cbor:"2,keyasint"`
	NotBeforeUnixMs   int64  `cbor:"3,keyasint"`
	NotAfterUnixMs    int64  `cbor:"4,keyasint"`
	MonotonicSequence uint64 `cbor:"5,keyasint"`
	// IssuerKeyThumbprint mirrors the connector's field 6: the SHA-256
	// of the issuer's public key bytes.
	IssuerKeyThumbprint [32]byte `cbor:"6,keyasint"`
	// Digest mirrors the connector's field 7: the policy content
	// digest (empty when the epoch carries no content body).
	Digest [32]byte `cbor:"7,keyasint,omitempty"`
}

func buildWirePolicyEpoch(epoch *models.PolicyEpoch, issuerPub ed25519.PublicKey) (*wirePolicyEpoch, error) {
	if epoch == nil {
		return nil, fmt.Errorf("relay: nil policy epoch")
	}
	created := epoch.CreatedAt.UnixMilli()
	// The model carries no explicit validity window; the relay grants a
	// 24h binding window after which the connector re-binds on connect.
	thumb := sha256.Sum256(issuerPub)
	return &wirePolicyEpoch{
		EpochID:             epoch.EpochID,
		IssuedAtUnixMs:      created,
		NotBeforeUnixMs:     created,
		NotAfterUnixMs:      created + 24*int64(time.Hour/time.Millisecond),
		MonotonicSequence:   epoch.EpochNumber,
		IssuerKeyThumbprint: thumb,
	}, nil
}

// ---------------------------------------------------------------------------
// Catalog snapshot (relay → connector). Mirrors dariproto.CatalogSnapshot
// + CatalogEntry field numbers.
// ---------------------------------------------------------------------------

type wireCatalogEntry struct {
	ModelID            string   `cbor:"1,keyasint"`
	DisplayName        string   `cbor:"2,keyasint"`
	Version            string   `cbor:"3,keyasint"`
	ModelPackageDigest [32]byte `cbor:"4,keyasint"`
	EndpointDigest     [32]byte `cbor:"5,keyasint"`
	Capabilities       []string `cbor:"6,keyasint,omitempty"`
	TokenLimit         uint32   `cbor:"7,keyasint,omitempty"`
	ContextWindow      uint32   `cbor:"8,keyasint,omitempty"`
	ModeTags           []string `cbor:"9,keyasint,omitempty"`
	PolicyEpochID      string   `cbor:"10,keyasint"`
	ActiveUntilUnixMs  int64    `cbor:"11,keyasint,omitempty"`
}

type wireCatalogSnapshot struct {
	Version             uint64             `cbor:"1,keyasint"`
	EpochID             string             `cbor:"2,keyasint"`
	IssuedAtUnixMs      int64              `cbor:"3,keyasint"`
	NotAfterUnixMs      int64              `cbor:"4,keyasint"`
	IssuedSequence      uint64             `cbor:"5,keyasint"`
	IssuerKeyThumbprint [32]byte           `cbor:"6,keyasint"`
	Digest              [32]byte           `cbor:"7,keyasint"`
	Entries             []wireCatalogEntry `cbor:"8,keyasint"`
}

// catalogDomain mirrors the connector's dariproto.CatalogDomain.
const catalogDomain = "DARI-CATALOG-v1\x00"

// wireCatalogDigest mirrors the connector's CatalogDigest — the
// snapshot's Digest field MUST equal this recomputation or the
// connector rejects the push.
func wireCatalogDigest(snap *wireCatalogSnapshot) [32]byte {
	h := sha256.New()
	h.Write([]byte(catalogDomain))
	var b8 [8]byte
	binary.BigEndian.PutUint64(b8[:], snap.Version)
	h.Write(b8[:])
	h.Write([]byte(snap.EpochID))
	binary.BigEndian.PutUint64(b8[:], snap.IssuedSequence)
	h.Write(b8[:])
	h.Write(snap.IssuerKeyThumbprint[:])
	binary.BigEndian.PutUint64(b8[:], uint64(snap.NotAfterUnixMs))
	h.Write(b8[:])
	for i := range snap.Entries {
		h.Write([]byte(snap.Entries[i].ModelID))
		h.Write([]byte(snap.Entries[i].Version))
		h.Write(snap.Entries[i].ModelPackageDigest[:])
		h.Write(snap.Entries[i].EndpointDigest[:])
	}
	var d [32]byte
	copy(d[:], h.Sum(nil))
	return d
}

func buildWireCatalogSnapshot(epochID string, issuerThumbprint [32]byte, descriptors []models.ModelDescriptor, now time.Time) *wireCatalogSnapshot {
	snap := &wireCatalogSnapshot{
		Version:             1,
		EpochID:             epochID,
		IssuedAtUnixMs:      now.UnixMilli(),
		NotAfterUnixMs:      now.Add(24 * time.Hour).UnixMilli(),
		IssuedSequence:      uint64(now.UnixMilli()), // monotonic per issuance
		IssuerKeyThumbprint: issuerThumbprint,
		Entries:             make([]wireCatalogEntry, 0, len(descriptors)),
	}
	for _, d := range descriptors {
		entry := wireCatalogEntry{
			ModelID:           d.CatalogModelID,
			DisplayName:       d.DisplayName,
			Version:           d.ReleaseChannel,
			PolicyEpochID:     epochID,
			ActiveUntilUnixMs: now.Add(24 * time.Hour).UnixMilli(),
		}
		if d.Limits.MaxOutputTokens > 0 {
			entry.TokenLimit = uint32(d.Limits.MaxOutputTokens)
		}
		if d.Limits.MaxInputTokens > 0 {
			entry.ContextWindow = uint32(d.Limits.MaxInputTokens)
		}
		if d.Capabilities.Input.Text {
			entry.Capabilities = append(entry.Capabilities, "text")
		}
		if d.Capabilities.Input.Image {
			entry.Capabilities = append(entry.Capabilities, "vision")
		}
		if d.Capabilities.Tools.ClientTools || d.Capabilities.Tools.MCP {
			entry.Capabilities = append(entry.Capabilities, "tools")
		}
		if d.Capabilities.Streaming {
			entry.Capabilities = append(entry.Capabilities, "streaming")
		}
		snap.Entries = append(snap.Entries, entry)
	}
	return snap
}

// ---------------------------------------------------------------------------
// Evidence receipt (relay → connector). Mirrors provenancewire.
// EvidenceReceiptEnvelope field numbers.
// ---------------------------------------------------------------------------

type wireEvidenceReceipt struct {
	ReceiptID         string   `cbor:"1,keyasint"`
	ExchangeID        string   `cbor:"2,keyasint"`
	SessionID         string   `cbor:"3,keyasint,omitempty"`
	OrganizationID    string   `cbor:"4,keyasint,omitempty"`
	FinalState        string   `cbor:"5,keyasint,omitempty"`
	FirstEventSeq     uint64   `cbor:"6,keyasint"`
	LastEventSeq      uint64   `cbor:"7,keyasint"`
	ChainRoot         [32]byte `cbor:"8,keyasint"`
	ProvenanceRoot    [32]byte `cbor:"9,keyasint,omitempty"`
	PolicyEpochID     string   `cbor:"10,keyasint,omitempty"`
	LeaseDigest       [32]byte `cbor:"11,keyasint,omitempty"`
	RelayIdentity     string   `cbor:"12,keyasint,omitempty"`
	ModelPackageID    string   `cbor:"13,keyasint,omitempty"`
	EndpointID        string   `cbor:"14,keyasint,omitempty"`
	KeyAlgorithm      string   `cbor:"15,keyasint"`
	Signature         string   `cbor:"16,keyasint"`
	RedactionManifest string   `cbor:"17,keyasint,omitempty"`
	IssuedAtUnixMs    int64    `cbor:"18,keyasint"`
	Acknowledged      bool     `cbor:"19,keyasint,omitempty"`
}

func buildWireEvidenceReceipt(r *models.EvidenceReceipt) *wireEvidenceReceipt {
	out := &wireEvidenceReceipt{
		// The relay's receipt identity is the ExchangeID (unique); the
		// connector's envelope carries it as both ReceiptID and
		// ExchangeID so the store can key on either.
		ReceiptID:      "rcpt-" + r.ExchangeID,
		ExchangeID:     r.ExchangeID,
		SessionID:      r.SessionID,
		OrganizationID: r.OrganizationID,
		FinalState:     r.FinalState,
		FirstEventSeq:  r.FirstEventSeq,
		LastEventSeq:   r.LastEventSeq,
		PolicyEpochID:  r.PolicyEpochID,
		RelayIdentity:  r.RelayIdentity,
		ModelPackageID: r.ModelPackageID,
		EndpointID:     r.EndpointID,
		KeyAlgorithm:   r.KeyAlgorithm,
		Signature:      r.Signature,
	}
	if ia, err := time.Parse(time.RFC3339, r.IssuedAt); err == nil {
		out.IssuedAtUnixMs = ia.UnixMilli()
	} else {
		out.IssuedAtUnixMs = time.Now().UnixMilli()
	}
	copy(out.ChainRoot[:], digestBytes(r.ChainRoot))
	copy(out.ProvenanceRoot[:], digestBytes(r.ProvenanceRoot))
	copy(out.LeaseDigest[:], digestBytes(r.LeaseDigest))
	return out
}

// digestBytes strips an optional "sha256:" prefix and hex-decodes.
func digestBytes(s string) []byte {
	for strings.HasPrefix(s, "sha256:") {
		s = s[len("sha256:"):]
	}
	if s == "" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

// ---------------------------------------------------------------------------
// Connector → relay provenance envelopes. Mirrors provenancewire field
// numbers exactly.
// ---------------------------------------------------------------------------

type wireActionEnvelope struct {
	ActionID         string   `cbor:"1,keyasint"`
	OrganizationID   string   `cbor:"2,keyasint"`
	SessionID        string   `cbor:"3,keyasint,omitempty"`
	ExchangeID       string   `cbor:"4,keyasint,omitempty"`
	UserID           string   `cbor:"5,keyasint,omitempty"`
	HarnessID        string   `cbor:"6,keyasint,omitempty"`
	ModelPackageID   string   `cbor:"7,keyasint,omitempty"`
	EndpointID       string   `cbor:"8,keyasint,omitempty"`
	ProjectID        string   `cbor:"9,keyasint,omitempty"`
	RepositoryID     string   `cbor:"10,keyasint,omitempty"`
	Branch           string   `cbor:"11,keyasint,omitempty"`
	PolicyEpochID    string   `cbor:"12,keyasint,omitempty"`
	LeaseID          string   `cbor:"13,keyasint,omitempty"`
	ActionType       string   `cbor:"14,keyasint"`
	ActionPayload    string   `cbor:"15,keyasint,omitempty"`
	VerdictResult    string   `cbor:"16,keyasint,omitempty"`
	Classification   string   `cbor:"17,keyasint,omitempty"`
	EnvelopeDigest   [32]byte `cbor:"18,keyasint"`
	CPSignature      string   `cbor:"19,keyasint,omitempty"`
	OccurredAtUnixMs int64    `cbor:"20,keyasint"`
}

type wireChangeSetEnvelope struct {
	ChangeSetID      string   `cbor:"1,keyasint"`
	OrganizationID   string   `cbor:"2,keyasint"`
	SessionID        string   `cbor:"3,keyasint"`
	ExchangeID       string   `cbor:"4,keyasint,omitempty"`
	RepositoryID     string   `cbor:"5,keyasint"`
	Branch           string   `cbor:"6,keyasint"`
	BaselineID       string   `cbor:"7,keyasint,omitempty"`
	UserID           string   `cbor:"8,keyasint,omitempty"`
	HarnessID        string   `cbor:"9,keyasint,omitempty"`
	ModelPackageID   string   `cbor:"10,keyasint,omitempty"`
	EndpointID       string   `cbor:"11,keyasint,omitempty"`
	FilesChanged     []string `cbor:"12,keyasint,omitempty"`
	DiffSummary      string   `cbor:"13,keyasint,omitempty"`
	DiffDigest       [32]byte `cbor:"14,keyasint"`
	LinesAdded       int      `cbor:"15,keyasint"`
	LinesRemoved     int      `cbor:"16,keyasint"`
	AttributionState string   `cbor:"17,keyasint"`
	Confidence       float64  `cbor:"18,keyasint"`
	ChangeSetDigest  [32]byte `cbor:"19,keyasint"`
	Status           string   `cbor:"20,keyasint,omitempty"`
}

type wireSpanEnvelope struct {
	SpanID              string   `cbor:"1,keyasint"`
	OrganizationID      string   `cbor:"2,keyasint"`
	RepositoryID        string   `cbor:"3,keyasint"`
	ChangeSetID         string   `cbor:"4,keyasint,omitempty"`
	FilePath            string   `cbor:"5,keyasint"`
	CommitSHA           string   `cbor:"6,keyasint,omitempty"`
	SymbolLang          string   `cbor:"7,keyasint,omitempty"`
	SymbolName          string   `cbor:"8,keyasint,omitempty"`
	StartLine           int      `cbor:"9,keyasint"`
	EndLine             int      `cbor:"10,keyasint"`
	ASTFingerprint      [32]byte `cbor:"11,keyasint"`
	SemanticFingerprint [32]byte `cbor:"12,keyasint,omitempty"`
	AttributionState    string   `cbor:"13,keyasint"`
	Confidence          float64  `cbor:"14,keyasint"`
	SessionID           string   `cbor:"15,keyasint,omitempty"`
	UserID              string   `cbor:"16,keyasint,omitempty"`
	HarnessID           string   `cbor:"17,keyasint,omitempty"`
	ModelPackageID      string   `cbor:"18,keyasint,omitempty"`
	EndpointID          string   `cbor:"19,keyasint,omitempty"`
	ContextRefs         []string `cbor:"20,keyasint,omitempty"`
	ParentSpanRefs      []string `cbor:"21,keyasint,omitempty"`
	SpanDigest          [32]byte `cbor:"22,keyasint"`
}

type wireCommitBindingEnvelope struct {
	BindingID      string   `cbor:"1,keyasint"`
	OrganizationID string   `cbor:"2,keyasint,omitempty"`
	RepositoryID   string   `cbor:"3,keyasint"`
	CommitSHA      string   `cbor:"4,keyasint"`
	ChangeSetID    string   `cbor:"5,keyasint"`
	SessionID      string   `cbor:"6,keyasint,omitempty"`
	Branch         string   `cbor:"7,keyasint,omitempty"`
	BoundAtUnixMs  int64    `cbor:"8,keyasint"`
	BindingDigest  [32]byte `cbor:"9,keyasint"`
}

type wireReceiptAck struct {
	ReceiptID     string   `cbor:"1,keyasint"`
	ExchangeID    string   `cbor:"2,keyasint"`
	AckDigest     [32]byte `cbor:"3,keyasint"`
	AckedAtUnixMs int64    `cbor:"4,keyasint"`
}

func encodeWire(v interface{}) ([]byte, error) {
	return cbor.Marshal(v)
}

func decodeWire(data []byte, v interface{}) error {
	return cbor.Unmarshal(data, v)
}

// Ensure the relay's DARI stack is linked in this file's dependency
// graph (AuthContext / digest helpers are shared with the listener).
var _ = dari.MarshalCBOR

// BuildReceiptAckCBOR encodes the connector's evidence-receipt ack for
// the wire (field numbers 1-4). Operational clients (bench, tooling)
// share the exact connector shape.
func BuildReceiptAckCBOR(receiptID, exchangeID string, ackDigest [32]byte, ackedAtUnixMs int64) ([]byte, error) {
	return encodeWire(wireReceiptAck{
		ReceiptID:     receiptID,
		ExchangeID:    exchangeID,
		AckDigest:     ackDigest,
		AckedAtUnixMs: ackedAtUnixMs,
	})
}

// leaseScopeView is the parsed JSON-scope view of a persisted
// capability lease. Shared by grant issuance and wire encoding so
// tolerance policy lives in one place.
type leaseScopeView struct {
	AllowedModels []string            `json:"allowed"`
	Tools         []string            `json:"tools"`
	RepoScope     []map[string]string `json:"repo"`
	ReadPaths     []string            `json:"read"`
	WritePaths    []string            `json:"write"`
}

// parseLeaseScope parses the lease's JSON columns. Unparseable
// allowed-models is an ERROR (callers fail closed); the optional
// scope columns tolerate absence.
func parseLeaseScope(lease *models.CapabilityLease) (*leaseScopeView, error) {
	out := &leaseScopeView{}
	if err := json.Unmarshal([]byte(lease.AllowedModelPackages), &out.AllowedModels); err != nil {
		return nil, fmt.Errorf("relay: lease allowed-models unparseable")
	}
	_ = json.Unmarshal([]byte(lease.ToolClasses), &out.Tools)
	_ = json.Unmarshal([]byte(lease.RepositoryScope), &out.RepoScope)
	var filePathScope struct {
		Read  []string `json:"read"`
		Write []string `json:"write"`
	}
	_ = json.Unmarshal([]byte(lease.FilePathScope), &filePathScope)
	out.ReadPaths, out.WritePaths = filePathScope.Read, filePathScope.Write
	return out, nil
}
