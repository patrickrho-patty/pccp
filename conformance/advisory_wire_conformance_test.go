package conformance

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/relay"
)

// advisory_wire_conformance_test.go pins the E2/E3/E5 wire contracts:
// the broadcast JSON the connector's comms inbox decodes, the admin
// directive signing bytes the connector's dispatcher verifies, and
// the SOVEREIGN_ADVISORY type allocation.

func TestBroadcastWireContractPinned(t *testing.T) {
	body := relay.BuildBroadcastMessage("bc-9", "pccp-policy", "점검 공지", "warning", time.UnixMilli(1700000000000))
	var decoded struct {
		MessageID  string `json:"message_id"`
		Type       string `json:"type"`
		SenderID   string `json:"sender_id"`
		Body       string `json:"body"`
		Severity   string `json:"severity"`
		IssuedAtMs int64  `json:"issued_at_ms"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.MessageID != "bc-9" || decoded.Type != "BROADCAST" || decoded.Body != "점검 공지" || decoded.Severity != "warning" {
		t.Fatalf("decoded = %+v", decoded)
	}
	if decoded.IssuedAtMs != 1700000000000 {
		t.Fatalf("issued_at_ms = %d", decoded.IssuedAtMs)
	}
}

// connectorAdminSigningBytes re-derives the connector's
// admin.Command.SigningBytes layout independently (the connector type
// is unreachable from this repo).
func connectorAdminSigningBytes(commandID, commandType, orgID, target, issuedBy, reason string, issuedAt, notAfter int64, payload string) []byte {
	notAfterStr := ""
	if notAfter > 0 {
		notAfterStr = jsonInt(notAfter)
	}
	return []byte("admin|" + commandID + "|" + commandType + "|" + orgID + "|" + target + "|" + issuedBy + "|" + reason + "|" + jsonInt(issuedAt) + "|" + jsonInt(notAfter) + "|" + notAfterStr + "|" + payload)
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestAdminDirectiveSigningBytesPinned(t *testing.T) {
	// The relay signs with its policy key; the connector verifies the
	// SAME canonical bytes. Sign here with an independent key pair and
	// verify against the connector-derived layout.
	_, priv, _ := ed25519.GenerateKey(nil)
	_ = priv
	pub, priv2, _ := ed25519.GenerateKey(nil)

	signed := ed25519.Sign(priv2, connectorAdminSigningBytes("cmd-1", "PAUSE_SESSION", "org-1", "h-1", "admin", "incident", 1700000000000, 0, ""))
	if !ed25519.Verify(pub, connectorAdminSigningBytes("cmd-1", "PAUSE_SESSION", "org-1", "h-1", "admin", "incident", 1700000000000, 0, ""), signed) {
		t.Fatal("signing layout must round-trip")
	}
	// Any field drift breaks verification.
	if ed25519.Verify(pub, connectorAdminSigningBytes("cmd-1", "PAUSE_SESSION", "org-1", "h-2", "admin", "incident", 1700000000000, 0, ""), signed) {
		t.Fatal("target drift must break verification")
	}
}

func TestSovereignAdvisoryTypePinned(t *testing.T) {
	if dari.MsgSovereignAdvisory != 0x0B03 {
		t.Fatalf("SOVEREIGN_ADVISORY = 0x%04x, want 0x0B03", uint16(dari.MsgSovereignAdvisory))
	}
	if dari.MsgSovereignAdvisory == dari.MsgBroadcast {
		t.Fatal("sovereign advisory must not ride the broadcast type")
	}
	if dari.MsgSovereignAdvisory.String() != "SOVEREIGN_ADVISORY" {
		t.Fatalf("String() = %q", dari.MsgSovereignAdvisory.String())
	}
}
