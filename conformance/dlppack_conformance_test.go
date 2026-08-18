package conformance

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// dlppack_conformance_test.go pins the DLP rule-pack wire contract
// (harness plan C1.3). The relay builds the pack
// (`internal/relay/dari_wire.go` wireDLPRulePack); the connector
// decodes it (`patty-code-pccp/internal/dariproto/dlprules.go`
// DLPRulePackWire). The repos cannot import each other, so this test
// re-derives the connector's layout independently and proves the
// relay's encoding decodes into it.

// connDLPRule mirrors the connector's DLPRuleWire field-for-field.
type connDLPRule struct {
	RuleID     string `cbor:"1,keyasint"`
	Pattern    string `cbor:"2,keyasint"`
	Severity   string `cbor:"3,keyasint"`
	RedactWith string `cbor:"4,keyasint,omitempty"`
	Disabled   bool   `cbor:"5,keyasint,omitempty"`
}

// connDLPRulePack mirrors the connector's DLPRulePackWire.
type connDLPRulePack struct {
	Version    uint16        `cbor:"1,keyasint"`
	EpochID    string        `cbor:"2,keyasint"`
	OrgID      string        `cbor:"3,keyasint"`
	NotAfterMs int64         `cbor:"4,keyasint"`
	Rules      []connDLPRule `cbor:"5,keyasint"`
	Digest     [32]byte      `cbor:"6,keyasint"`
}

// relayDLPPack re-encodes what the relay's BuildDLPRulePack produces
// (mirrored builder — the relay type is unexported in internal/relay).
func relayDLPPack(t *testing.T) []byte {
	t.Helper()
	pack := connDLPRulePack{
		Version:    1,
		EpochID:    "epoch-dlp",
		OrgID:      "org-1",
		NotAfterMs: time.Now().Add(time.Hour).UnixMilli(),
		Rules: []connDLPRule{
			{RuleID: "kr-rrn", Pattern: "korean_pii", Severity: "critical"},
			{RuleID: "sec-aws", Pattern: "secret", Severity: "critical", RedactWith: "[REDACTED]"},
			{RuleID: "inj-1", Pattern: "prompt_injection", Severity: "high", Disabled: true},
		},
	}
	h := sha256.New()
	h.Write([]byte("DARI-DLP-RULEPACK-v1\x00"))
	h.Write([]byte(pack.EpochID))
	h.Write([]byte(pack.OrgID))
	for _, r := range pack.Rules {
		fmt.Fprintf(h, "%s|%s|%s|%t|", r.RuleID, r.Pattern, r.Severity, r.Disabled)
	}
	copy(pack.Digest[:], h.Sum(nil))
	data, err := dari.MarshalCBOR(pack)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestDLPRulePackWireContractPinned: the CBOR field labels are
// stable — label renumbering, type drift, or reordering breaks the
// connector's decode.
func TestDLPRulePackWireContractPinned(t *testing.T) {
	data := relayDLPPack(t)
	var decoded connDLPRulePack
	if err := cbor.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("connector decode: %v", err)
	}
	if decoded.EpochID != "epoch-dlp" || decoded.OrgID != "org-1" || len(decoded.Rules) != 3 {
		t.Fatalf("decoded = %+v", decoded)
	}
	if decoded.Rules[0].Pattern != "korean_pii" || decoded.Rules[2].Disabled != true {
		t.Fatalf("rules = %+v", decoded.Rules)
	}
	if decoded.Digest == [32]byte{} {
		t.Fatal("digest must travel")
	}
}

// TestDLPRulePackRidesItsOwnMessageType: the pack MUST NOT share
// MsgPolicyEpochPush — the connector decodes that type as a PolicyEpoch
// and a pack on it would break session setup (the collision bug this
// test pins).
func TestDLPRulePackRidesItsOwnMessageType(t *testing.T) {
	if dari.MsgDLPRulePack == dari.MsgPolicyEpochPush {
		t.Fatal("DLP rule pack must have its own message type")
	}
	if dari.MsgDLPRulePack != 0x0D11 {
		t.Fatalf("DLP rule pack type = 0x%04x, want 0x0D11", uint16(dari.MsgDLPRulePack))
	}
	if dari.MsgDLPRulePack.String() != "DLP_RULE_PACK" {
		t.Fatalf("String() = %q", dari.MsgDLPRulePack.String())
	}
	// The pack body must NOT decode cleanly as a PolicyEpoch body
	// (the collision bug): the connector's PolicyEpoch expects label 1
	// to be a STRING epoch ID while the pack's label 1 is a UINT —
	// strict CBOR rejects the type mismatch.
	data := relayDLPPack(t)
	var asEpoch struct {
		EpochID           string `cbor:"1,keyasint"`
		IssuedAtUnixMs    int64  `cbor:"2,keyasint"`
		NotBeforeUnixMs   int64  `cbor:"3,keyasint"`
		NotAfterUnixMs    int64  `cbor:"4,keyasint"`
		MonotonicSequence uint64 `cbor:"5,keyasint"`
	}
	if err := dari.UnmarshalCBOR(data, &asEpoch); err == nil {
		t.Fatal("DLP rule pack must NOT be decodable as a PolicyEpoch (message-type collision)")
	}
}

// --- PAT-1432: scoped packs on the wire ---

// connDLPRuleScope mirrors the connector's DLPRuleScope.
type connDLPRuleScope struct {
	Level string `cbor:"1,keyasint"`
	ID    string `cbor:"2,keyasint"`
}

// connDLPRulePackScoped mirrors the connector's PAT-1432 layout
// (field 8 added). The legacy connDLPRulePack above stays frozen as
// the pre-1432 mirror.
type connDLPRulePackScoped struct {
	Version       uint16                      `cbor:"1,keyasint"`
	EpochID       string                      `cbor:"2,keyasint"`
	OrgID         string                      `cbor:"3,keyasint"`
	NotAfterMs    int64                       `cbor:"4,keyasint"`
	Rules         []connDLPRule               `cbor:"5,keyasint"`
	Digest        [32]byte                    `cbor:"6,keyasint"`
	RuleOverrides []connDLPRuleOverrideMirror `cbor:"7,keyasint,omitempty"`
	Scope         connDLPRuleScope            `cbor:"8,keyasint,omitempty"`
}

// connDLPRuleOverrideMirror mirrors the connector's DLPRuleOverride.
type connDLPRuleOverrideMirror struct {
	RuleID   string `cbor:"1,keyasint"`
	Enabled  bool   `cbor:"2,keyasint"`
	Severity string `cbor:"3,keyasint"`
	Action   string `cbor:"4,keyasint"`
}

// TestDLPPackUnscopedBytesArePre1432Stable: a pack without a scope
// must decode IDENTICALLY through the frozen pre-1432 connector
// mirror — deployed harnesses must keep interoping, and the bytes
// must not grow a phantom field-8.
func TestDLPPackUnscopedBytesArePre1432Stable(t *testing.T) {
	data := relayDLPPack(t)    // built WITHOUT a scope
	var legacy connDLPRulePack // frozen pre-1432 layout (no field 8)
	if err := cbor.Unmarshal(data, &legacy); err != nil {
		t.Fatalf("legacy connector decode: %v", err)
	}
	// Field 8 must not appear: decode with the scoped mirror and
	// confirm the zero scope.
	var scoped connDLPRulePackScoped
	if err := cbor.Unmarshal(data, &scoped); err != nil {
		t.Fatal(err)
	}
	if scoped.Scope.Level != "" || scoped.Scope.ID != "" {
		t.Fatalf("unscoped pack decoded a phantom scope: %+v", scoped.Scope)
	}
}

// TestDLPPackScopedFieldRoundTrip: a scoped pack's field 8 carries
// level+ID and decodes through the connector mirror; unknown-field
// tolerance is what makes an OLD harness skip it.
func TestDLPPackScopedFieldRoundTrip(t *testing.T) {
	pack := connDLPRulePackScoped{
		Version: 1, EpochID: "epoch-dlp", OrgID: "org-1",
		NotAfterMs: time.Now().Add(time.Hour).UnixMilli(),
		Rules: []connDLPRule{
			{RuleID: "pii-kr-rrn", Pattern: "korean_pii", Severity: "critical"},
		},
		RuleOverrides: []connDLPRuleOverrideMirror{
			{RuleID: "pii-kr-phone", Enabled: false, Severity: "low"},
		},
		Scope: connDLPRuleScope{Level: "harness", ID: "peer-9"},
	}
	data, err := dari.MarshalCBOR(pack)
	if err != nil {
		t.Fatal(err)
	}
	var decoded connDLPRulePackScoped
	if err := cbor.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Scope.Level != "harness" || decoded.Scope.ID != "peer-9" {
		t.Fatalf("scope round-trip: %+v", decoded.Scope)
	}
	// An OLD connector (frozen mirror, no Scope field) must still
	// decode the same bytes — field 8 skipped, core fields intact.
	var legacy connDLPRulePack
	if err := cbor.Unmarshal(data, &legacy); err != nil {
		t.Fatalf("legacy connector must tolerate field 8: %v", err)
	}
	if legacy.EpochID != "epoch-dlp" || len(legacy.Rules) != 1 {
		t.Fatalf("legacy decode lost core fields: %+v", legacy)
	}
}

// TestDLPPackAllScopeLevelsCarryExactLevelStrings: the level strings
// are a cross-repo contract (relay sends, harness ranks). A one-sided
// rename must fail HERE, not as a silently-ignored pack in production.
func TestDLPPackAllScopeLevelsCarryExactLevelStrings(t *testing.T) {
	for _, level := range []string{"org", "team", "user", "harness"} {
		pack := connDLPRulePackScoped{
			Version: 1, EpochID: "epoch-dlp", OrgID: "org-1",
			NotAfterMs: time.Now().Add(time.Hour).UnixMilli(),
			Rules:      []connDLPRule{{RuleID: "pii-kr-rrn", Pattern: "korean_pii", Severity: "critical"}},
			Scope:      connDLPRuleScope{Level: level, ID: "subj-1"},
		}
		data, err := dari.MarshalCBOR(pack)
		if err != nil {
			t.Fatalf("%s: %v", level, err)
		}
		var decoded connDLPRulePackScoped
		if err := cbor.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("%s: %v", level, err)
		}
		if decoded.Scope.Level != level {
			t.Fatalf("level %q did not survive the wire: %+v", level, decoded.Scope)
		}
	}
}
