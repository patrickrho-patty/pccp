package scheduler

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// classes.go implements spec §13.14: signed traffic classes. Tenant and
// priority metadata arrive in a COSE-signed envelope verified against the
// issuing relay/CP key; clients can never self-assert a class they do not
// own (llm-d's client-header model is the spoofable anti-pattern this
// replaces).

// TrafficEnvelopeDomain is the domain-separation prefix for the traffic
// envelope's canonical signing body. v2 adds the WS3 program metadata.
const TrafficEnvelopeDomain = "DARI-TRAFFIC-ENVELOPE-v2\x00"

// ProgramMeta is the WS3 agent-program scheduling metadata carried by a
// signed envelope: opaque, bounded identifiers and budgets only — never
// conversation text, tool arguments, repository identifiers, or code
// (PAT-1445 WS3 §program model). A client hint is never an authorization
// or a resource guarantee; the signature binds it to the issuer.
type ProgramMeta struct {
	ProgramID  string `json:"program_id"`            // opaque program/task identifier
	TurnSeq    uint32 `json:"turn_seq"`              // session turn sequence
	ParentID   string `json:"parent_id,omitempty"`   // parent/continuation relationship
	ToolPaused bool   `json:"tool_paused,omitempty"` // the request ended waiting on a tool
	TaskSLOMs  uint64 `json:"task_slo_ms,omitempty"` // overall task budget (0 = none)
}

// TrafficEnvelope is a COSE-signed claim: which tenant this request
// belongs to and which traffic class the tenant's issued capabilities
// authorize for it.
type TrafficEnvelope struct {
	RequestID string       `json:"request_id"`
	TenantID  string       `json:"tenant_id"`
	Class     string       `json:"class"`
	ExpiresAt time.Time    `json:"expires_at"`
	Program   *ProgramMeta `json:"program,omitempty"`

	SignatureHex string `json:"signature_hex,omitempty"`
}

// NewTrafficEnvelope builds an unsigned envelope valid for ttl.
func NewTrafficEnvelope(requestID, tenantID, class string, ttl time.Duration) *TrafficEnvelope {
	return &TrafficEnvelope{
		RequestID: requestID,
		TenantID:  tenantID,
		Class:     class,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// SetProgram attaches WS3 program metadata before signing (the signature
// binds it — later mutation invalidates the envelope).
func (e *TrafficEnvelope) SetProgram(m ProgramMeta) { e.Program = &m }

// signingBytes renders the canonical body bound by the signature. Field
// order is pinned; bump the domain on schema changes.
func (e *TrafficEnvelope) signingBytes() []byte {
	dst := make([]byte, 0, 192)
	dst = append(dst, TrafficEnvelopeDomain...)
	dst = lpString(dst, e.RequestID)
	dst = lpString(dst, e.TenantID)
	dst = lpString(dst, e.Class)
	exp, _ := e.ExpiresAt.MarshalText()
	dst = lpString(dst, string(exp))
	if e.Program != nil {
		dst = lpString(dst, e.Program.ProgramID)
		dst = lpU32(dst, e.Program.TurnSeq)
		dst = lpString(dst, e.Program.ParentID)
		if e.Program.ToolPaused {
			dst = append(dst, 1)
		} else {
			dst = append(dst, 0)
		}
		dst = lpU64(dst, e.Program.TaskSLOMs)
	}
	return dst
}

// Sign binds the envelope with the issuing key.
func (e *TrafficEnvelope) Sign(priv ed25519.PrivateKey) error {
	sign1, err := dari.CreateCOSESign1(e.signingBytes(), priv, []byte(e.RequestID))
	if err != nil {
		return fmt.Errorf("scheduler: sign traffic envelope: %w", err)
	}
	encoded, err := dari.EncodeCOSESign1(sign1)
	if err != nil {
		return fmt.Errorf("scheduler: encode traffic envelope: %w", err)
	}
	e.SignatureHex = hex.EncodeToString(encoded)
	return nil
}

// Verify checks the signature and expiry against the issuing public key.
func (e *TrafficEnvelope) Verify(pub ed25519.PublicKey) error {
	if e.SignatureHex == "" {
		return fmt.Errorf("scheduler: traffic envelope has no signature")
	}
	if time.Now().After(e.ExpiresAt) {
		return fmt.Errorf("scheduler: traffic envelope expired at %s", e.ExpiresAt)
	}
	raw, err := hex.DecodeString(e.SignatureHex)
	if err != nil {
		return fmt.Errorf("scheduler: decode traffic envelope: %w", err)
	}
	sign1, err := dari.DecodeCOSESign1(raw)
	if err != nil {
		return fmt.Errorf("scheduler: decode traffic COSE: %w", err)
	}
	if !bytes.Equal(sign1.Payload, e.signingBytes()) {
		return fmt.Errorf("scheduler: traffic envelope payload mismatch")
	}
	return dari.VerifyCOSESign1(sign1, pub)
}

// ClassResolver maps ingress metadata to an authoritative traffic class.
type ClassResolver struct {
	issuerPub ed25519.PublicKey
}

// NewClassResolver builds a resolver trusting the given issuer key.
func NewClassResolver(issuerPub ed25519.PublicKey) *ClassResolver {
	return &ClassResolver{issuerPub: issuerPub}
}

// Resolve returns the verified class from a signed envelope, or the
// lowest class when the envelope is absent or invalid — fail-closed, so
// a client can never claim more than it holds.
func (r *ClassResolver) Resolve(env *TrafficEnvelope) string {
	if env == nil || env.SignatureHex == "" {
		return "batch"
	}
	if err := env.Verify(r.issuerPub); err != nil {
		return "batch"
	}
	return env.Class
}
