package relay

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/paper"
)

// PaperALPN is the ALPN identifier for the PAPER protocol.
const PaperALPN = "paper/1"

// PaperListener accepts native PAPER protocol connections (QUIC/TCP with CBOR framing).
// Per the README guardrail: "No HTTP/REST/WebSocket for protocol traffic."
// The HTTP API in server.go is for admin/control-plane operations only;
// the PAPER wire protocol is for Harness/PIA peer connections.
type PaperListener struct {
	svc               *Service
	tlsConfig         *tls.Config
	authenticator     *PeerAuthenticator
	mu                sync.Mutex
	conns             map[string]credentialConnection
	credentialSerials map[string]string // connID → authenticated credential serial
	sessions          map[string]string // connID → working sessionID (from SESSION_OPEN)
	credentials       map[string]*paper.PeerCredential // connID → verified credential (org/peer binding)
	sessionEpochs     map[string]string // connID → policy epoch bound at session setup
}

type credentialConnection interface {
	Close() error
}

// NewPaperListener creates a native PAPER protocol listener.
func NewPaperListener(svc *Service, tlsConfig *tls.Config, trust ...TrustBundle) *PaperListener {
	if tlsConfig == nil {
		tlsConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: []string{PaperALPN}}
		cert, err := generateListenerCert()
		if err == nil {
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
	}
	if tlsConfig.NextProtos == nil {
		tlsConfig.NextProtos = []string{PaperALPN}
	}
	bundle := TrustBundle{}
	if len(trust) > 0 {
		bundle = trust[0]
	}
	return &PaperListener{
		svc:               svc,
		tlsConfig:         tlsConfig,
		authenticator:     NewPeerAuthenticator(bundle),
		conns:             make(map[string]credentialConnection),
		credentialSerials: make(map[string]string),
		sessions:          make(map[string]string),
		credentials:       make(map[string]*paper.PeerCredential),
		sessionEpochs:     make(map[string]string),
	}
}

func generateListenerCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"PAPER Relay"}},
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

// ListenTCP starts listening for PAPER connections over TLS/TCP (PAPER §7.2).
func (pl *PaperListener) ListenTCP(ctx context.Context, addr string) error {
	listener, err := paper.ListenTCP(addr, pl.tlsConfig)
	if err != nil {
		return fmt.Errorf("relay: paper TCP listen: %w", err)
	}
	defer listener.Close()

	log.Printf("relay: PAPER TCP/TLS listening on %s", addr)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		netConn, err := listener.Accept()
		if err != nil {
			log.Printf("relay: paper accept error: %v", err)
			continue
		}

		go pl.handleConn(ctx, netConn)
	}
}

// handleConn processes a single PAPER peer connection through the
// full handshake: preface → HELLO → AUTH → READY.
func (pl *PaperListener) handleConn(ctx context.Context, netConn net.Conn) {
	defer netConn.Close()

	config := paper.DefaultTransportConfig()

	// Accept and verify the PAPER preface
	conn, err := paper.AcceptTCP(netConn, config)
	if err != nil {
		log.Printf("relay: paper preface failed from %s: %v", netConn.RemoteAddr(), err)
		return
	}

	connID := fmt.Sprintf("paper-%s-%d", netConn.RemoteAddr(), time.Now().UnixNano())
	pl.mu.Lock()
	pl.conns[connID] = conn
	pl.mu.Unlock()
	defer func() {
		pl.mu.Lock()
		delete(pl.conns, connID)
		delete(pl.credentialSerials, connID)
		delete(pl.sessions, connID)
		delete(pl.credentials, connID)
		delete(pl.sessionEpochs, connID)
		pl.mu.Unlock()
	}()

	log.Printf("relay: paper connection from %s (id=%s)", netConn.RemoteAddr(), connID)

	// Phase 1: HELLO / HELLO_ACK
	hello, err := conn.AcceptHandshake()
	if err != nil {
		log.Printf("relay: paper HELLO from %s failed: %v", connID, err)
		return
	}

	log.Printf("relay: paper HELLO from peer profile=%s (id=%s)", hello.PeerProfile, connID)

	// Build HELLO_ACK
	serverNonce := make([]byte, 32)
	ack := &paper.HelloAckMessage{
		CoreVersion:   1,
		CryptoProfile: "PAPER-BASE-1",
		ServerNonce:   serverNonce,
		ResourceLimits: map[string]uint64{
			"max_sessions":    100,
			"max_exchanges":   1000,
			"max_payload_len": uint64(paper.MaxPayloadLen),
		},
	}

	if err := conn.SendHelloAck(ack); err != nil {
		log.Printf("relay: paper HELLO_ACK to %s failed: %v", connID, err)
		return
	}

	// Phase 2: AUTH_CHALLENGE / AUTH_PROOF
	challenge := &paper.AuthChallengeMessage{
		ServerNonce:       serverNonce,
		ChallengeID:       []byte(connID),
		CredentialIssuers: []string{"pccp-ca"},
		RevocationEpoch:   pl.authenticator.RevocationEpoch(),
		AuthDeadlineMs:    uint64(time.Now().Add(30 * time.Second).UnixMilli()),
	}

	if err := conn.AuthChallenge(challenge); err != nil {
		log.Printf("relay: paper AUTH_CHALLENGE to %s failed: %v", connID, err)
		return
	}

	proof, err := conn.RecvAuthProof()
	if err != nil {
		log.Printf("relay: paper AUTH_PROOF from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: paper AUTH_PROOF received from %s", connID)

	helloCBOR, err := paper.CanonicalHelloCBOR(hello)
	if err != nil {
		log.Printf("relay: paper encode HELLO transcript for %s failed: %v", connID, err)
		return
	}
	ackCBOR, err := paper.CanonicalAckCBOR(ack)
	if err != nil {
		log.Printf("relay: paper encode HELLO_ACK transcript for %s failed: %v", connID, err)
		return
	}
	credentialDigest := paper.ComputeObjectDigest(paper.ObjTypePeerCredential, proof.Credential)
	transcript := paper.AuthContext(
		helloCBOR,
		ackCBOR,
		hello.ClientNonce,
		challenge.ServerNonce,
		[]byte("tcp-exporter"),
		credentialDigest.Bytes(),
	)
	if os.Getenv("PAPER_DEBUG_AUTH") == "1" {
		log.Printf("relay: AUTH_DEBUG helloCBOR=%x\nackCBOR=%x\nclientNonce=%x serverNonce=%x credDigest=%x transcript=%x challengeID=%x epoch=%d",
			helloCBOR, ackCBOR, hello.ClientNonce, challenge.ServerNonce, credentialDigest.Bytes(), transcript.Bytes(), proof.ChallengeID, paper.DecodeRevocationEpochOrZero(proof.RevocationEvidence))
	}
	credential, err := pl.authenticator.VerifyPeerProof(ctx, transcript.Bytes(), proof)
	if err != nil {
		log.Printf("relay: paper rejecting peer proof %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": "authentication failed"})
		conn.SendMessage(paper.MsgClose, nil, errPayload, 0, 1)
		return
	}
	if err := validatePeerProfile(hello.PeerProfile, credential); err != nil {
		log.Printf("relay: paper rejecting peer profile %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": "authentication failed"})
		conn.SendMessage(paper.MsgClose, nil, errPayload, 0, 1)
		return
	}

	// Derive the live identity only from the issuer-verified credential.
	harnessID := credential.SubjectPeerID
	if _, err := pl.svc.AuthorizePeer(harnessID); err != nil {
		log.Printf("relay: paper rejecting peer %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": "unauthorized: " + err.Error()})
		conn.SendMessage(paper.MsgClose, nil, errPayload, 0, 1)
		return
	}
	if !pl.trackAuthenticatedConnection(connID, credential.Serial, conn) {
		log.Printf("relay: paper credential %s revoked during authentication", credential.Serial)
		return
	}
	pl.mu.Lock()
	pl.credentials[connID] = credential
	pl.mu.Unlock()

	if err := conn.SendControl(paper.MsgAuthAck, nil, pl.authAckPayload()); err != nil {
		log.Printf("relay: paper AUTH_ACK to %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: paper AUTH_ACK sent to %s, peer=%s ready (governed)", connID, harnessID)

	// Phase 3: Application messages (READY state) — every AI request is governed.
	pl.handleApplicationMessages(ctx, conn, connID, hello, harnessID)
}

func validatePeerProfile(negotiated paper.PeerProfile, credential *paper.PeerCredential) error {
	if credential == nil || credential.PeerProfile != negotiated {
		return fmt.Errorf("credential profile does not match negotiated profile %q", negotiated)
	}
	return nil
}

func (pl *PaperListener) trackAuthenticatedConnection(connID, serial string, conn credentialConnection) bool {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if pl.authenticator.isRevoked(serial) {
		_ = conn.Close()
		delete(pl.conns, connID)
		delete(pl.sessions, connID)
		delete(pl.credentials, connID)
		delete(pl.sessionEpochs, connID)
		return false
	}
	pl.conns[connID] = conn
	pl.credentialSerials[connID] = serial
	return true
}

// RevokeCredential advances the revocation view and immediately closes every
// active transport authenticated with the revoked serial.
func (pl *PaperListener) RevokeCredential(serial string, epoch uint64) {
	pl.authenticator.Revoke(serial, epoch)

	pl.mu.Lock()
	toClose := make([]credentialConnection, 0)
	for connID, credentialSerial := range pl.credentialSerials {
		if credentialSerial != serial {
			continue
		}
		if conn := pl.conns[connID]; conn != nil {
			toClose = append(toClose, conn)
		}
		delete(pl.conns, connID)
		delete(pl.credentialSerials, connID)
		delete(pl.sessions, connID)
		delete(pl.credentials, connID)
		delete(pl.sessionEpochs, connID)
	}
	pl.mu.Unlock()

	for _, conn := range toClose {
		_ = conn.Close()
	}
}

// handleApplicationMessages processes PAPER application records after authentication.
// Every AI request is governed (Service.GovernInference); there is no ungoverned
// forward path.
func (pl *PaperListener) handleApplicationMessages(ctx context.Context, conn *paper.TransportConn, connID string, hello *paper.HelloMessage, harnessID string) {
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
			log.Printf("relay: paper recv from %s ended: %v", connID, err)
			return
		}

		msgType := paper.MessageType(record.MessageType)

		switch {
		case record.Kind == paper.KindControl && msgType == paper.MsgPing:
			conn.SendControl(paper.MsgPong, record.Header, []byte("pong"))

		case record.Kind == paper.KindMessage && msgType == paper.MsgSessionOpen:
			var so struct {
				SessionID string `json:"session_id"`
				UserID    string `json:"user_id"`
				Model     string `json:"model"`
			}
			json.Unmarshal(record.Payload, &so)
			if so.SessionID != "" {
				pl.setSession(connID, so.SessionID)
			}
			log.Printf("relay: paper SESSION_OPEN from %s session=%s", connID, so.SessionID)
			pl.setupSession(ctx, conn, connID, so.SessionID, so.UserID, so.Model)

		case record.Kind == paper.KindMessage && msgType == paper.MsgAIOpen:
			log.Printf("relay: paper AI_OPEN from %s, governing", connID)
			pl.governAIOpen(ctx, conn, record, connID, harnessID)

		case record.Kind == paper.KindMessage && msgType == paper.MsgChangeSet:
			pl.ingestChangeSet(conn, connID, harnessID, record)

		case record.Kind == paper.KindMessage && msgType == paper.MsgProvenanceNode:
			pl.ingestSpan(conn, connID, harnessID, record)

		case record.Kind == paper.KindMessage && msgType == paper.MsgCommitBind:
			pl.ingestCommitBinding(conn, connID, harnessID, record)

		case record.Kind == paper.KindMessage && msgType == paper.MsgActionEnvelope:
			pl.ingestActionEnvelope(conn, connID, harnessID, record)

		case record.Kind == paper.KindMessage && msgType == paper.MsgEvidenceReceiptAck:
			pl.ingestReceiptAck(conn, connID, record)

		case record.Kind == paper.KindData:
			log.Printf("relay: paper DATA from %s, lane=%d seq=%d", connID, record.LaneID, record.LaneSequence)

		default:
			log.Printf("relay: paper %s/%s from %s (unhandled)", record.Kind, msgType, connID)
		}
	}
}

// ActiveConnections returns the number of active PAPER connections.
func (pl *PaperListener) ActiveConnections() int {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return len(pl.conns)
}

// sessionFor returns the working sessionID recorded for a connection.
func (pl *PaperListener) sessionFor(connID string) string {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.sessions[connID]
}

func (pl *PaperListener) setSession(connID, sessionID string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.sessions[connID] = sessionID
}

// governAIOpen runs an AI_OPEN through the governed flow and sends the
// AI_COMPLETE (or MsgClose on denial/failure) back to the harness. A
// completed governed exchange also issues a signed evidence receipt
// (B3) so the harness retains tamper-evidence for the exchange.
func (pl *PaperListener) governAIOpen(ctx context.Context, conn *paper.TransportConn, reqRecord *paper.Record, connID, harnessID string) {
	greq, err := buildGovernRequest(harnessID, pl.sessionFor(connID), reqRecord.Payload)
	if err != nil {
		log.Printf("relay: invalid AI_OPEN from %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": "invalid AI_OPEN: " + err.Error()})
		conn.SendMessage(paper.MsgClose, nil, errPayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
		return
	}

	resp, receipt, err := pl.svc.GovernInference(ctx, greq)
	if err != nil {
		log.Printf("relay: governed inference denied/failed for %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": err.Error()})
		conn.SendMessage(paper.MsgClose, nil, errPayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
		return
	}

	// Build the AI_COMPLETE payload in the shape the harness decodes.
	content := ""
	if len(resp.Choices) > 0 {
		if msg, ok := resp.Choices[0]["message"].(map[string]interface{}); ok {
			content, _ = msg["content"].(string)
		}
		if content == "" {
			content, _ = resp.Choices[0]["text"].(string)
		}
	}
	inTok := resp.Usage["prompt_tokens"]
	outTok := resp.Usage["completion_tokens"]
	completePayload, _ := json.Marshal(map[string]interface{}{
		"content":       content,
		"finish_reason": "stop",
		"input_tokens":  inTok,
		"output_tokens": outTok,
		"total_tokens":  inTok + outTok,
	})
	// B3: push the evidence receipt BEFORE AI_COMPLETE — the
	// connector's stream reader terminates on AI_COMPLETE, so the
	// receipt must already be in flight for the ack to happen.
	pl.pushEvidenceReceiptMessage(conn, connID, receipt)

	conn.SendMessage(paper.MsgAIComplete, nil, completePayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
	log.Printf("relay: governed AI_COMPLETE sent to %s", connID)
}

// pushEvidenceReceiptMessage encodes and pushes an issued receipt.
func (pl *PaperListener) pushEvidenceReceiptMessage(conn *paper.TransportConn, connID string, receipt *models.EvidenceReceipt) {
	if receipt == nil {
		return
	}
	body, err := encodeWire(buildWireEvidenceReceipt(receipt))
	if err != nil {
		log.Printf("relay: receipt encode for %s failed: %v", connID, err)
		return
	}
	if err := conn.SendMessage(paper.MsgEvidenceReceipt, nil, body, 0, 5); err != nil {
		log.Printf("relay: receipt push to %s failed: %v", connID, err)
	}
}

// buildGovernRequest parses an AI_OPEN payload into a GovernRequest. Pure helper
// (unit-tested) so the governed dispatch is verifiable without a live PAPER conn.
func buildGovernRequest(harnessID, sessionID string, payload []byte) (GovernRequest, error) {
	var aiReq struct {
		Model     string                   `json:"model"`
		Messages  []map[string]interface{} `json:"messages"`
		MaxTokens int                      `json:"max_tokens"`
	}
	if err := json.Unmarshal(payload, &aiReq); err != nil {
		return GovernRequest{}, fmt.Errorf("decode: %w", err)
	}
	msgs := make([]map[string]string, 0, len(aiReq.Messages))
	for _, m := range aiReq.Messages {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		msgs = append(msgs, map[string]string{"role": role, "content": content})
	}
	if aiReq.MaxTokens <= 0 {
		aiReq.MaxTokens = 4096
	}
	return GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: aiReq.Model,
		Messages: msgs, MaxTokens: aiReq.MaxTokens,
	}, nil
}
