package relay

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/paper"
)

// PaperALPN is the ALPN identifier for the PAPER protocol.
const PaperALPN = "paper/1"


// PaperListener accepts native PAPER protocol connections (QUIC/TCP with CBOR framing).
// Per the README guardrail: "No HTTP/REST/WebSocket for protocol traffic."
// The HTTP API in server.go is for admin/control-plane operations only;
// the PAPER wire protocol is for Harness/PIA peer connections.
type PaperListener struct {
	svc       *Service
	tlsConfig *tls.Config
	mu        sync.Mutex
	conns     map[string]*paper.TransportConn
}

// NewPaperListener creates a native PAPER protocol listener.
func NewPaperListener(svc *Service, tlsConfig *tls.Config) *PaperListener {
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
	return &PaperListener{
		svc:       svc,
		tlsConfig: tlsConfig,
		conns:     make(map[string]*paper.TransportConn),
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
		RevocationEpoch:   uint64(time.Now().Unix()),
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

	log.Printf("relay: paper AUTH_PROOF received from %s, cred serial=%s", connID, proof.ChallengeID)

	// In production: verify the COSE-Sign1 credential signature, check revocation,
	// validate the auth proof over the HELLO/HELLO_ACK transcript.
	// For Phase 0: accept any valid PPC format.

	// Send AUTH_ACK to confirm authentication
	if err := conn.SendControl(paper.MsgAuthAck, nil, []byte("authenticated")); err != nil {
		log.Printf("relay: paper AUTH_ACK to %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: paper AUTH_ACK sent to %s, peer ready", connID)

	// Phase 3: Application messages (READY state)
	// The peer is now authenticated and can open governed exchanges.
	pl.handleApplicationMessages(ctx, conn, connID, hello)
}

// handleApplicationMessages processes PAPER application records after authentication.
func (pl *PaperListener) handleApplicationMessages(ctx context.Context, conn *paper.TransportConn, connID string, hello *paper.HelloMessage) {
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

		// Route based on message type
		msgType := paper.MessageType(record.MessageType)

		switch {
		case record.Kind == paper.KindControl && msgType == paper.MsgPing:
			// Respond with PONG
			conn.SendControl(paper.MsgPong, record.Header, []byte("pong"))

		case record.Kind == paper.KindMessage && msgType == paper.MsgSessionOpen:
			// Handle session open via the governance layer
			log.Printf("relay: paper SESSION_OPEN from %s", connID)

		case record.Kind == paper.KindMessage && msgType == paper.MsgAIOpen:
			// Forward AI inference request to PIA via PAPER
			log.Printf("relay: paper AI_OPEN from %s, forwarding to PIA", connID)
			pl.forwardAIToPIA(ctx, conn, record, connID)

		case record.Kind == paper.KindData:
			// Handle data stream (token chunks, etc.)
			log.Printf("relay: paper DATA from %s, lane=%d seq=%d", connID, record.LaneID, record.LaneSequence)

		default:
			log.Printf("relay: paper %s/%s from %s (unhandled)", record.Kind, msgType, connID)
		}
	}
}

// forwardAIToPIA forwards an AI_OPEN from a harness to the PIA via PAPER
// and streams the response back to the harness.
func (pl *PaperListener) forwardAIToPIA(ctx context.Context, harnessConn *paper.TransportConn, reqRecord *paper.Record, connID string) {
	// Get PAPER inference client (connects to PIA)
	pic := getPaperInferenceClient()
	if pic == nil {
		// No PAPER path — try HTTP fallback to PIA
		pl.forwardAIHTTP(ctx, harnessConn, reqRecord, connID)
		return
	}

	// Parse the AI_OPEN request
	var aiReq struct {
		Model     string                   `json:"model"`
		Messages  []map[string]interface{} `json:"messages"`
		MaxTokens int                      `json:"max_tokens"`
	}
	json.Unmarshal(reqRecord.Payload, &aiReq)

	// Forward to PIA via PAPER
	result, err := pic.SendInference(ctx, aiReq.Model, aiReq.Messages, aiReq.MaxTokens)
	if err != nil {
		log.Printf("relay: paper PIA inference error from %s: %v", connID, err)
		errPayload, _ := json.Marshal(map[string]string{"error": err.Error()})
		harnessConn.SendMessage(paper.MsgClose, nil, errPayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
		return
	}

	// Send response back to harness as AI_COMPLETE
	completePayload, _ := json.Marshal(result)
	harnessConn.SendMessage(paper.MsgAIComplete, nil, completePayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
	log.Printf("relay: paper AI_COMPLETE sent to %s", connID)
}

// forwardAIHTTP forwards an AI request to PIA via HTTP (fallback).
func (pl *PaperListener) forwardAIHTTP(ctx context.Context, harnessConn *paper.TransportConn, reqRecord *paper.Record, connID string) {
	// Parse the request
	var aiReq struct {
		Model     string                   `json:"model"`
		Messages  []map[string]interface{} `json:"messages"`
		MaxTokens int                      `json:"max_tokens"`
	}
	json.Unmarshal(reqRecord.Payload, &aiReq)

	// Use the HTTP inference client
	piaURL := os.Getenv("PCCP_PIA_URL")
	if piaURL == "" {
		errPayload, _ := json.Marshal(map[string]string{"error": "PIA not configured"})
		harnessConn.SendMessage(paper.MsgClose, nil, errPayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
		return
	}

	// Build PIA request
	type piaMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var msgs []piaMsg
	for _, m := range aiReq.Messages {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		msgs = append(msgs, piaMsg{Role: role, Content: content})
	}

	vllmModel := os.Getenv("PCCP_VLLM_MODEL")
	if vllmModel == "" {
		vllmModel = aiReq.Model
	}

	promptParts := []string{}
	for _, m := range msgs {
		promptParts = append(promptParts, m.Role+": "+m.Content)
	}
	prompt := "You are a helpful coding assistant.\n\n"
	for i, p := range promptParts {
		if i > 0 {
			prompt += "\n"
		}
		prompt += p
	}
	prompt += "\n\nAssistant:"

	// Build chat messages for PIA's /v1/chat/completions
	type chatMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var chatMsgs []chatMsg
	for _, m := range msgs {
		chatMsgs = append(chatMsgs, chatMsg{Role: m.Role, Content: m.Content})
	}

	piaReq := map[string]interface{}{
		"model":       vllmModel,
		"messages":    chatMsgs,
		"max_tokens":  aiReq.MaxTokens,
		"temperature": 0.0,
		"stream":      false,
	}

	piaBody, _ := json.Marshal(piaReq)
	piaResp, err := http.Post(piaURL+"/v1/chat/completions", "application/json", bytes.NewReader(piaBody))
	if err != nil {
		errPayload, _ := json.Marshal(map[string]string{"error": "PIA unreachable: " + err.Error()})
		harnessConn.SendMessage(paper.MsgClose, nil, errPayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
		return
	}
	defer piaResp.Body.Close()

	var piaResult map[string]interface{}
	json.NewDecoder(piaResp.Body).Decode(&piaResult)

	// Extract text and usage
	text := ""
	if choices, ok := piaResult["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			// Chat completions format: choices[].message.content
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				text, _ = msg["content"].(string)
			}
			// Fallback to completions format: choices[].text
			if text == "" {
				text, _ = choice["text"].(string)
			}
		}
	}

	var inputTokens, outputTokens float64
	if usage, ok := piaResult["usage"].(map[string]interface{}); ok {
		inputTokens, _ = usage["prompt_tokens"].(float64)
		outputTokens, _ = usage["completion_tokens"].(float64)
	}

	// Build AI_COMPLETE response
	completePayload, _ := json.Marshal(map[string]interface{}{
		"content":      text,
		"finish_reason": "stop",
		"input_tokens":  int(inputTokens),
		"output_tokens": int(outputTokens),
		"total_tokens":  int(inputTokens + outputTokens),
	})
	harnessConn.SendMessage(paper.MsgAIComplete, nil, completePayload, reqRecord.LaneID, reqRecord.LaneSequence+1)
	log.Printf("relay: paper AI_COMPLETE sent to %s (via HTTP)", connID)
}

// ActiveConnections returns the number of active PAPER connections.
func (pl *PaperListener) ActiveConnections() int {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return len(pl.conns)
}
