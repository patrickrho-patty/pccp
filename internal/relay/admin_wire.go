package relay

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// admin_wire.go builds the E2/E5 wire bodies the relay pushes to
// connected harnesses. The layouts mirror the connector's decode
// types field-for-field (patty-code-pccp internal/comms.Message and
// internal/admin.Command); the cross-repo conformance suite pins the
// admin signing bytes.

// wireBroadcastMessage is the JSON body riding MsgBroadcast. The
// connector decodes it into a comms.Message and delivers it to the
// session inbox.
type wireBroadcastMessage struct {
	MessageID  string `json:"message_id"`
	Type       string `json:"type"`
	SenderID   string `json:"sender_id"`
	Body       string `json:"body"`
	Severity   string `json:"severity"`
	IssuedAtMs int64  `json:"issued_at_ms"`
}

// BuildBroadcastMessage encodes a governed broadcast push (E2).
func BuildBroadcastMessage(messageID, senderID, body, severity string, now time.Time) []byte {
	if messageID == "" {
		messageID = fmt.Sprintf("bcast-%d", now.UnixNano())
	}
	out, _ := json.Marshal(wireBroadcastMessage{
		MessageID:  messageID,
		Type:       "BROADCAST",
		SenderID:   senderID,
		Body:       body,
		Severity:   severity,
		IssuedAtMs: now.UnixMilli(),
	})
	return out
}

// wireAdminCommand mirrors the connector's admin.Command JSON (the
// connector type carries no json tags, so field names are exact).
type wireAdminCommand struct {
	CommandID      string
	CommandType    string
	OrganizationID string
	Target         string
	Reason         string
	IssuedBy       string
	IssuedAt       int64
	NotAfter       int64
	Payload        []byte
	Signature      []byte
}

// adminSigningBytes mirrors the connector's admin.Command.SigningBytes
// canonical layout. The cross-repo conformance test pins this format;
// changing either side breaks directive verification.
func adminSigningBytes(c wireAdminCommand) []byte {
	notAfterStr := ""
	if c.NotAfter > 0 {
		notAfterStr = fmt.Sprintf("%d", c.NotAfter)
	}
	return []byte(fmt.Sprintf("admin|%s|%s|%s|%s|%s|%s|%d|%d|%s|%s",
		c.CommandID, c.CommandType, c.OrganizationID, c.Target,
		c.IssuedBy, c.Reason, c.IssuedAt, c.NotAfter, notAfterStr,
		string(c.Payload)))
}

// BuildAdminDirective creates and signs an admin directive with the
// policy issuer's key (the same key whose public half rides AUTH_ACK,
// so the connector's dispatcher verifies under it).
func (s *Service) BuildAdminDirective(orgID, cmdType, target, reason, issuedBy string, payload []byte, notAfterMs int64, now time.Time) ([]byte, error) {
	priv := s.policy.SigningPrivateKey()
	cmd := wireAdminCommand{
		CommandID:      fmt.Sprintf("cmd-%d", now.UnixNano()),
		CommandType:    cmdType,
		OrganizationID: orgID,
		Target:         target,
		Reason:         reason,
		IssuedBy:       issuedBy,
		IssuedAt:       now.UnixMilli(),
		NotAfter:       notAfterMs,
		Payload:        payload,
	}
	cmd.Signature = ed25519.Sign(priv, adminSigningBytes(cmd))
	return json.Marshal(cmd)
}

// BroadcastToOrg fans a governed broadcast out to every attached DARI
// listener's online sessions for the org (E2). Returns the number of
// connections reached.
func (s *Service) BroadcastToOrg(orgID string, body []byte) int {
	s.mu.RLock()
	listeners := append([]*DARIListener(nil), s.listeners...)
	s.mu.RUnlock()
	sent := 0
	for _, pl := range listeners {
		sent += pl.Broadcast(orgID, dari.MsgBroadcast, body)
	}
	return sent
}

// DeliverDirectiveToHarness pushes a signed admin directive to the
// target harness's connections (E5).
func (s *Service) DeliverDirectiveToHarness(harnessID string, body []byte) int {
	s.mu.RLock()
	listeners := append([]*DARIListener(nil), s.listeners...)
	s.mu.RUnlock()
	sent := 0
	for _, pl := range listeners {
		sent += pl.DeliverAdminDirective(harnessID, body)
	}
	return sent
}

// DeliverSovereignAdvisoryToAll pushes a signed offline advisory to
// every connected session (E3).
func (s *Service) DeliverSovereignAdvisoryToAll(body []byte) int {
	s.mu.RLock()
	listeners := append([]*DARIListener(nil), s.listeners...)
	s.mu.RUnlock()
	sent := 0
	for _, pl := range listeners {
		sent += pl.DeliverSovereignAdvisory(body)
	}
	return sent
}
