package relay

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// DARIALPN is the canonical ALPN identifier for the DARI protocol
// (legacy ALPN accepted via dari.LegacyPaper1ALPN).
const DARIALPN = dari.DARIProtocol

// DARIListener accepts native DARI protocol connections (QUIC/TCP with CBOR framing).
// Per the README guardrail: "No HTTP/REST/WebSocket for protocol traffic."
// The HTTP API in server.go is for admin/control-plane operations only;
// the DARI wire protocol is for Harness/PIA peer connections.
// connState is one authenticated connection's full state. A single
// map + one forget path replaces six parallel maps mutated in
// lockstep (partial-cleanup bugs become impossible).
type connState struct {
	conn      credentialConnection
	serial    string
	sessionID string
	cred      *dari.PeerCredential
	epoch     string
	grant     *dari.GrantEnvelope
}

type DARIListener struct {
	svc           *Service
	tlsConfig     *tls.Config
	authenticator *PeerAuthenticator
	mu            sync.Mutex
	conns         map[string]*connState
}

type credentialConnection interface {
	Close() error
}

// NewDARIListener creates a native DARI protocol listener.
func NewDARIListener(svc *Service, tlsConfig *tls.Config, trust ...TrustBundle) *DARIListener {
	if tlsConfig == nil {
		tlsConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: dari.DARIProtocols()}
		cert, err := generateListenerCert()
		if err == nil {
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
	}
	if tlsConfig.NextProtos == nil {
		tlsConfig.NextProtos = dari.DARIProtocols()
	}
	bundle := TrustBundle{}
	if len(trust) > 0 {
		bundle = trust[0]
	}
	return &DARIListener{
		svc:           svc,
		tlsConfig:     tlsConfig,
		authenticator: NewPeerAuthenticator(bundle),
		conns:         make(map[string]*connState),
	}
}

func generateListenerCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"DARI Relay"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost", "relay"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}

// ListenTCP starts listening for DARI connections over TLS/TCP (DARI §7.2).
func (pl *DARIListener) ListenTCP(ctx context.Context, addr string) error {
	listener, err := dari.ListenTCP(addr, pl.tlsConfig)
	if err != nil {
		return fmt.Errorf("relay: dari TCP listen: %w", err)
	}
	defer listener.Close()

	log.Printf("relay: DARI TCP/TLS listening on %s", addr)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		netConn, err := listener.Accept()
		if err != nil {
			log.Printf("relay: dari accept error: %v", err)
			continue
		}

		go pl.handleConn(ctx, netConn)
	}
}

// handleConn processes a single DARI peer connection through the
// full handshake: preface → HELLO → AUTH → READY.
func (pl *DARIListener) handleConn(ctx context.Context, netConn net.Conn) {
	defer netConn.Close()

	config := dari.DefaultTransportConfig()

	// Accept and verify the DARI preface
	conn, err := dari.AcceptTCP(netConn, config)
	if err != nil {
		log.Printf("relay: dari preface failed from %s: %v", netConn.RemoteAddr(), err)
		return
	}

	connID := fmt.Sprintf("paper-%s-%d", netConn.RemoteAddr(), time.Now().UnixNano())
	pl.mu.Lock()
	pl.conns[connID] = &connState{conn: conn}
	pl.mu.Unlock()
	defer pl.forgetConnection(connID)

	log.Printf("relay: dari connection from %s (id=%s)", netConn.RemoteAddr(), connID)

	// Phase 1: HELLO / HELLO_ACK
	hello, err := conn.AcceptHandshake()
	if err != nil {
		log.Printf("relay: dari HELLO from %s failed: %v", connID, err)
		return
	}

	log.Printf("relay: dari HELLO from peer profile=%s (id=%s)", hello.PeerProfile, connID)

	// Build HELLO_ACK
	serverNonce := make([]byte, 32)
	ack := &dari.HelloAckMessage{
		CoreVersion:   1,
		CryptoProfile: "DARI-BASE-1",
		ServerNonce:   serverNonce,
		ResourceLimits: map[string]uint64{
			"max_sessions":    100,
			"max_exchanges":   1000,
			"max_payload_len": uint64(dari.MaxPayloadLen),
		},
		// D5: deployment-wide harness floor (operator-set during
		// coordinated upgrades). A connector below the floor refuses
		// to continue the handshake; per-org floors additionally
		// refuse at session setup.
		MinHarnessVersion: os.Getenv("PCCP_MIN_HARNESS_VERSION"),
	}

	if err := conn.SendHelloAck(ack); err != nil {
		log.Printf("relay: dari HELLO_ACK to %s failed: %v", connID, err)
		return
	}

	// Phase 2: AUTH_CHALLENGE / AUTH_PROOF
	challenge := &dari.AuthChallengeMessage{
		ServerNonce:       serverNonce,
		ChallengeID:       []byte(connID),
		CredentialIssuers: []string{"pccp-ca"},
		RevocationEpoch:   pl.authenticator.RevocationEpoch(),
		AuthDeadlineMs:    uint64(time.Now().Add(30 * time.Second).UnixMilli()),
	}

	if err := conn.AuthChallenge(challenge); err != nil {
		log.Printf("relay: dari AUTH_CHALLENGE to %s failed: %v", connID, err)
		return
	}

	proof, err := conn.RecvAuthProof()
	if err != nil {
		log.Printf("relay: dari AUTH_PROOF from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: dari AUTH_PROOF received from %s", connID)

	helloCBOR, err := dari.CanonicalHelloCBOR(hello)
	if err != nil {
		log.Printf("relay: dari encode HELLO transcript for %s failed: %v", connID, err)
		return
	}
	ackCBOR, err := dari.CanonicalAckCBOR(ack)
	if err != nil {
		log.Printf("relay: dari encode HELLO_ACK transcript for %s failed: %v", connID, err)
		return
	}
	credentialDigest := dari.ComputeObjectDigest(dari.ObjTypePeerCredential, proof.Credential)
	transcript := dari.AuthContext(
		helloCBOR,
		ackCBOR,
		hello.ClientNonce,
		challenge.ServerNonce,
		[]byte("tcp-exporter"),
		credentialDigest.Bytes(),
	)
	if os.Getenv("DARI_DEBUG_AUTH") == "1" {
		log.Printf("relay: AUTH_DEBUG helloCBOR=%x\nackCBOR=%x\nclientNonce=%x serverNonce=%x credDigest=%x transcript=%x challengeID=%x epoch=%d",
			helloCBOR, ackCBOR, hello.ClientNonce, challenge.ServerNonce, credentialDigest.Bytes(), transcript.Bytes(), proof.ChallengeID, dari.DecodeRevocationEpochOrZero(proof.RevocationEvidence))
	}
	credential, err := pl.authenticator.VerifyPeerProof(ctx, transcript.Bytes(), proof)
	if err != nil {
		log.Printf("relay: dari rejecting peer proof %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": "authentication failed"})
		conn.SendMessage(dari.MsgClose, nil, errPayload, 0, 1)
		return
	}
	if err := validatePeerProfile(hello.PeerProfile, credential); err != nil {
		log.Printf("relay: dari rejecting peer profile %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": "authentication failed"})
		conn.SendMessage(dari.MsgClose, nil, errPayload, 0, 1)
		return
	}

	// Derive the live identity only from the issuer-verified credential.
	harnessID := credential.SubjectPeerID
	if _, err := pl.svc.AuthorizePeer(harnessID); err != nil {
		log.Printf("relay: dari rejecting peer %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": "unauthorized: " + err.Error()})
		conn.SendMessage(dari.MsgClose, nil, errPayload, 0, 1)
		return
	}
	if !pl.trackAuthenticatedConnection(connID, credential.Serial, conn) {
		log.Printf("relay: dari credential %s revoked during authentication", credential.Serial)
		return
	}
	pl.mu.Lock()
	if state := pl.conns[connID]; state != nil {
		state.cred = credential
	}
	pl.mu.Unlock()

	if err := conn.SendControl(dari.MsgAuthAck, nil, pl.authAckPayload()); err != nil {
		log.Printf("relay: dari AUTH_ACK to %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: dari AUTH_ACK sent to %s, peer=%s ready (governed)", connID, harnessID)

	// Phase 3: Application messages (READY state) — every AI request is governed.
	pl.handleApplicationMessages(ctx, conn, connID, hello, harnessID)
}

func validatePeerProfile(negotiated dari.PeerProfile, credential *dari.PeerCredential) error {
	if credential == nil || credential.PeerProfile != negotiated {
		return fmt.Errorf("credential profile does not match negotiated profile %q", negotiated)
	}
	return nil
}

func (pl *DARIListener) trackAuthenticatedConnection(connID, serial string, conn credentialConnection) bool {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if pl.authenticator.isRevoked(serial) {
		_ = conn.Close()
		delete(pl.conns, connID)
		return false
	}
	if state := pl.conns[connID]; state != nil {
		state.conn = conn
		state.serial = serial
	} else {
		pl.conns[connID] = &connState{conn: conn, serial: serial}
	}
	return true
}

// forgetConnection removes every trace of a connection (single
// cleanup path for all six formerly-parallel maps).
func (pl *DARIListener) forgetConnection(connID string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	delete(pl.conns, connID)
}

// RevokeCredential advances the revocation view and immediately closes every
// active transport authenticated with the revoked serial.
func (pl *DARIListener) RevokeCredential(serial string, epoch uint64) {
	pl.authenticator.Revoke(serial, epoch)

	pl.mu.Lock()
	toClose := make([]credentialConnection, 0)
	for connID, state := range pl.conns {
		if state.serial != serial {
			continue
		}
		if state.conn != nil {
			toClose = append(toClose, state.conn)
		}
		delete(pl.conns, connID)
	}
	pl.mu.Unlock()

	for _, conn := range toClose {
		// Graceful revocation notice first (the connector's reader can
		// surface WHY the transport died + discard its lease), then the
		// hard close.
		if tc, ok := conn.(*dari.TransportConn); ok {
			payload, _ := json.Marshal(map[string]string{"reason": "credential_revoked"})
			_ = tc.SendMessage(dari.MsgLeaseRevoke, nil, payload, 0, 1)
		}
		_ = conn.Close()
	}
}

// handleApplicationMessages processes DARI application records after authentication.
// Every AI request is governed (Service.GovernInference); there is no ungoverned
// forward path.
func (pl *DARIListener) handleApplicationMessages(ctx context.Context, conn *dari.TransportConn, connID string, hello *dari.HelloMessage, harnessID string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := conn.RecvRecord()
		if err != nil {
			if ctx.Err() != nil {
				return // shutdown
			}
			log.Printf("relay: dari recv from %s ended: %v", connID, err)
			return
		}

		msgType := dari.MessageType(record.MessageType)

		switch {
		case record.Kind == dari.KindControl && msgType == dari.MsgPing:
			conn.SendControl(dari.MsgPong, record.Header, []byte("pong"))

		case record.Kind == dari.KindMessage && msgType == dari.MsgSessionOpen:
			var so struct {
				SessionID string `json:"session_id"`
				UserID    string `json:"user_id"`
				Model     string `json:"model"`
			}
			json.Unmarshal(record.Payload, &so)
			if so.SessionID != "" {
				pl.setSession(connID, so.SessionID)
			}
			log.Printf("relay: dari SESSION_OPEN from %s session=%s", connID, so.SessionID)
			pl.setupSession(ctx, conn, connID, so, hello.ImplementationVersion)

		case record.Kind == dari.KindMessage && msgType == dari.MsgAIOpen:
			log.Printf("relay: dari AI_OPEN from %s, governing", connID)
			pl.governAIOpen(ctx, conn, record, connID, harnessID)

		case record.Kind == dari.KindMessage && msgType == dari.MsgChangeSet:
			pl.ingestChangeSet(conn, connID, harnessID, record)

		case record.Kind == dari.KindMessage && msgType == dari.MsgProvenanceNode:
			pl.ingestSpan(conn, connID, harnessID, record)

		case record.Kind == dari.KindMessage && msgType == dari.MsgCommitBind:
			pl.ingestCommitBinding(conn, connID, harnessID, record)

		case record.Kind == dari.KindMessage && msgType == dari.MsgActionEnvelope:
			pl.ingestActionEnvelope(conn, connID, harnessID, record)

		case record.Kind == dari.KindMessage && msgType == dari.MsgEvidenceReceiptAck:
			pl.ingestReceiptAck(conn, connID, record)

		case record.Kind == dari.KindData:
			log.Printf("relay: dari DATA from %s, lane=%d seq=%d", connID, record.LaneID, record.LaneSequence)

		default:
			log.Printf("relay: dari %s/%s from %s (unhandled)", record.Kind, msgType, connID)
		}
	}
}

// BroadcastCatalogDelta pushes a fresh epoch-bound catalog snapshot to
// every connected session (Task 15: catalog push on publish). Sessions
// on other epochs are skipped — the connector applies deltas only
// against its bound epoch.
func (pl *DARIListener) BroadcastCatalogDelta() {
	pl.mu.Lock()
	type target struct {
		conn  *dari.TransportConn
		epoch string
	}
	var targets []target
	for _, state := range pl.conns {
		if conn, ok := state.conn.(*dari.TransportConn); ok && state.epoch != "" {
			targets = append(targets, target{conn, state.epoch})
		}
	}
	pl.mu.Unlock()

	if len(targets) == 0 {
		return
	}
	// Resolve + digest the catalog ONCE for the whole broadcast; only
	// the epoch binding (and thus the digest) varies per target.
	descriptors, err := pl.svc.Catalog().GetEffectiveCatalog("", "", "")
	if err != nil {
		log.Printf("relay: catalog delta resolve failed: %v", err)
		return
	}
	thumb := sha256.Sum256(pl.svc.Policy().SigningPublicKey())
	encoded := map[string][]byte{}
	for _, t := range targets {
		body, cached := encoded[t.epoch]
		if !cached {
			snap := buildWireCatalogSnapshot(t.epoch, thumb, descriptors, time.Now())
			snap.Digest = wireCatalogDigest(snap)
			body, err = encodeWire(snap)
			if err != nil {
				continue
			}
			encoded[t.epoch] = body
		}
		if err := t.conn.SendMessage(dari.MsgModelCatalogDelta, nil, body, 0, 2); err != nil {
			log.Printf("relay: catalog delta to %s epoch failed: %v", t.epoch, err)
		}
	}
}

// Broadcast pushes a governed broadcast (E2) to every connected
// session of an org. envelope must be pre-encoded by the caller (the
// comms service signs it).
func (pl *DARIListener) Broadcast(orgID string, messageType dari.MessageType, body []byte) int {
	pl.mu.Lock()
	var targets []*connState
	for _, state := range pl.conns {
		if state.cred != nil && state.cred.Organization == orgID {
			if conn, ok := state.conn.(*dari.TransportConn); ok {
				targets = append(targets, state)
				_ = conn // sent below under the map copy
			}
		}
	}
	// Copy transports out of the lock.
	conns := make([]*dari.TransportConn, 0, len(targets))
	for _, state := range targets {
		if conn, ok := state.conn.(*dari.TransportConn); ok {
			conns = append(conns, conn)
		}
	}
	pl.mu.Unlock()
	sent := 0
	for _, conn := range conns {
		if err := conn.SendMessage(messageType, nil, body, 0, 1); err == nil {
			sent++
		}
	}
	return sent
}

// DeliverAdminDirective pushes a signed admin command (E5) to every
// connection of the target harness.
func (pl *DARIListener) DeliverAdminDirective(harnessID string, body []byte) int {
	pl.mu.Lock()
	var conns []*dari.TransportConn
	for _, state := range pl.conns {
		if state.cred != nil && state.cred.SubjectPeerID == harnessID {
			if conn, ok := state.conn.(*dari.TransportConn); ok {
				conns = append(conns, conn)
			}
		}
	}
	pl.mu.Unlock()
	sent := 0
	for _, conn := range conns {
		if err := conn.SendMessage(dari.MsgAdminDirective, nil, body, 0, 1); err == nil {
			sent++
		}
	}
	return sent
}

// DeliverSovereignAdvisory pushes a signed offline advisory (E3) to
// every connected session.
func (pl *DARIListener) DeliverSovereignAdvisory(body []byte) int {
	pl.mu.Lock()
	var conns []*dari.TransportConn
	for _, state := range pl.conns {
		if conn, ok := state.conn.(*dari.TransportConn); ok {
			conns = append(conns, conn)
		}
	}
	pl.mu.Unlock()
	sent := 0
	for _, conn := range conns {
		if err := conn.SendMessage(dari.MsgSovereignAdvisory, nil, body, 0, 2); err == nil {
			sent++
		}
	}
	return sent
}

// ActiveConnections returns the number of active DARI connections.
func (pl *DARIListener) ActiveConnections() int {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return len(pl.conns)
}

// sessionFor returns the working sessionID recorded for a connection.
func (pl *DARIListener) sessionFor(connID string) string {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if state := pl.conns[connID]; state != nil {
		return state.sessionID
	}
	return ""
}

func (pl *DARIListener) setSession(connID, sessionID string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if state := pl.conns[connID]; state != nil {
		state.sessionID = sessionID
	}
}

// governAIOpen runs an AI_OPEN through the governed flow and sends the
// AI_COMPLETE (or MsgClose on denial/failure) back to the harness. A
// completed governed exchange also issues a signed evidence receipt
// (B3) so the harness retains tamper-evidence for the exchange.
func (pl *DARIListener) governAIOpen(ctx context.Context, conn *dari.TransportConn, reqRecord *dari.Record, connID, harnessID string) {
	greq, err := buildGovernRequest(harnessID, pl.sessionFor(connID), reqRecord.Payload)
	if err == nil {
		pl.mu.Lock()
		if state := pl.conns[connID]; state != nil {
			greq.Grant = state.grant
		}
		pl.mu.Unlock()
	}
	if err != nil {
		log.Printf("relay: invalid AI_OPEN from %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": "invalid AI_OPEN: " + err.Error()})
		conn.SendMessage(dari.MsgClose, nil, errPayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
		return
	}

	// F1: governed token streaming — every PIA delta is relayed to the
	// harness as an AI_TOKEN_CHUNK before the final AI_COMPLETE.
	delta := func(text string) {
		chunkPayload, _ := json.Marshal(map[string]string{"text": text})
		if err := conn.SendMessage(dari.MsgAITokenChunk, nil, chunkPayload, reqRecord.LaneID, reqRecord.LaneSequence+1); err != nil {
			log.Printf("relay: token chunk to %s failed: %v", connID, err)
		}
		// Console live view (web/21 B): fan each delta to the org's
		// SSE subscribers (LiveView terminal cards + throughput).
		orgID := pl.orgForPeer(connID)
		pl.svc.realtime.BroadcastToOrg(orgID, "session.chunk", map[string]interface{}{
			"session_id": greq.SessionID,
			"harness_id": harnessID,
			"text":       text,
		})
	}
	resp, receipt, err := pl.svc.GovernInference(ctx, greq, delta)
	if err != nil {
		log.Printf("relay: governed inference denied/failed for %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": err.Error()})
		conn.SendMessage(dari.MsgClose, nil, errPayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
		return
	}
	// The streamed text already reached the harness as AI_TOKEN_CHUNK
	// records; the AI_COMPLETE carries usage + finish only (a non-empty
	// content would duplicate tokens client-side).
	inTok := resp.Usage["prompt_tokens"]
	outTok := resp.Usage["completion_tokens"]
	completePayload, _ := json.Marshal(map[string]interface{}{
		"content":       "",
		"finish_reason": "stop",
		"input_tokens":  inTok,
		"output_tokens": outTok,
		"total_tokens":  inTok + outTok,
	})
	// F.6: push the signed Authorization Decision (RELAY_VERDICT)
	// before completion — the connector verifies it under the AUTH_ACK
	// policy issuer key and refuses the stream on a DENY/expired
	// decision (decision-before-consumption).
	if len(resp.DecisionCOSE) > 0 {
		if err := conn.SendMessage(dari.MsgRelayVerdict, nil, resp.DecisionCOSE, reqRecord.LaneID, reqRecord.LaneSequence+1); err != nil {
			log.Printf("relay: verdict push to %s failed: %v", connID, err)
		}
	}

	// B3: push the evidence receipt BEFORE AI_COMPLETE — the
	// connector's stream reader terminates on AI_COMPLETE, so the
	// receipt must already be in flight for the ack to happen.
	pl.pushEvidenceReceiptMessage(conn, connID, receipt)

	conn.SendMessage(dari.MsgAIComplete, nil, completePayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
	log.Printf("relay: governed AI_COMPLETE sent to %s", connID)
}

// pushEvidenceReceiptMessage encodes and pushes an issued receipt.
func (pl *DARIListener) pushEvidenceReceiptMessage(conn *dari.TransportConn, connID string, receipt *models.EvidenceReceipt) {
	if receipt == nil {
		return
	}
	body, err := encodeWire(buildWireEvidenceReceipt(receipt))
	if err != nil {
		log.Printf("relay: receipt encode for %s failed: %v", connID, err)
		return
	}
	if err := conn.SendMessage(dari.MsgEvidenceReceipt, nil, body, 0, 5); err != nil {
		log.Printf("relay: receipt push to %s failed: %v", connID, err)
	}
}

// buildGovernRequest parses an AI_OPEN payload into a GovernRequest. Pure helper
// (unit-tested) so the governed dispatch is verifiable without a live DARI conn.
func buildGovernRequest(harnessID, sessionID string, payload []byte) (GovernRequest, error) {
	model, msgs, maxTokens, err := parseAIOpen(payload)
	if err != nil {
		return GovernRequest{}, err
	}
	return GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: model,
		Messages: msgs, MaxTokens: maxTokens,
	}, nil
}

// ApplyRevocationSnapshot replaces the listener's revocation view with
// the identity service's authoritative snapshot (epoch + revoked
// serials) and immediately terminates every active transport whose
// serial is revoked. Called whenever the control plane revokes a
// credential (Task 6 Step 3: revocation must reach live listeners, not
// just the database).
func (pl *DARIListener) ApplyRevocationSnapshot(epoch uint64, serials map[string]uint64) {
	pl.mu.Lock()
	snapshot := make(map[string]uint64, len(serials))
	for s, e := range serials {
		snapshot[s] = e
	}
	pl.mu.Unlock()

	for serial := range snapshot {
		pl.RevokeCredential(serial, epoch)
	}
	// Advance the epoch so NEW connections must present fresh
	// revocation evidence at or above the authoritative snapshot.
	pl.authenticator.AdvanceEpoch(epoch)
}

// registerListener is invoked by Service.AttachDARIListener.
