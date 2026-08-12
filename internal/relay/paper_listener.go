package relay

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
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
	return &PaperListener{
		svc:       svc,
		tlsConfig: tlsConfig,
		conns:     make(map[string]*paper.TransportConn),
	}
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
			// Handle AI inference request via the governance layer
			log.Printf("relay: paper AI_OPEN from %s", connID)

		case record.Kind == paper.KindData:
			// Handle data stream (token chunks, etc.)
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
