package pia

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// DARIListener accepts native DARI protocol connections on the PIA.
// Per v2 §9.2: "DARI Relay to PIA: DARI"
type DARIListener struct {
	svc       *Service
	tlsConfig *tls.Config
	mu        sync.Mutex
	conns     map[string]*dari.TransportConn
}

func NewDARIListener(svc *Service) *DARIListener {
	tlsConfig := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: []string{dari.DARIProtocol}}

	// Generate self-signed cert for DARI TLS if none provided
	cert, err := generateSelfSignedCert()
	if err != nil {
		log.Printf("pia-paper: cert generation failed, DARI listener will use InsecureSkipVerify: %v", err)
	} else {
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return &DARIListener{
		svc:       svc,
		tlsConfig: tlsConfig,
		conns:     make(map[string]*dari.TransportConn),
	}
}

// generateSelfSignedCert delegates to the shared dari dev-cert helper.
func generateSelfSignedCert() (tls.Certificate, error) {
	return dari.DevSelfSignedCert("Patty Code PIA", []string{"localhost", "pia"})
}

// TLSConfig returns the listener's TLS configuration (the S2 scheduler
// forwarder dials workers with the same shape).
func (pl *DARIListener) TLSConfig() *tls.Config {
	return pl.tlsConfig
}

func (pl *DARIListener) ListenTCP(ctx context.Context, addr string) error {
	listener, err := dari.ListenTCP(addr, pl.tlsConfig)
	if err != nil {
		return fmt.Errorf("pia-paper: listen %s: %w", addr, err)
	}
	defer listener.Close()
	log.Printf("pia-paper: DARI listener on %s", addr)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		netConn, err := listener.Accept()
		if err != nil {
			continue
		}
		go pl.handleConn(ctx, netConn)
	}
}

func (pl *DARIListener) handleConn(ctx context.Context, netConn net.Conn) {
	defer netConn.Close()
	conn, err := dari.AcceptTCP(netConn, dari.DefaultTransportConfig())
	if err != nil {
		log.Printf("pia-paper: preface failed: %v", err)
		return
	}
	connID := fmt.Sprintf("paper-%s", netConn.RemoteAddr())
	pl.mu.Lock()
	pl.conns[connID] = conn
	pl.mu.Unlock()
	defer func() {
		pl.mu.Lock()
		delete(pl.conns, connID)
		pl.mu.Unlock()
	}()

	hello, err := conn.AcceptHandshake()
	if err != nil {
		return
	}
	if hello.PeerProfile != dari.ProfileRelay && hello.PeerProfile != dari.ProfileControl {
		return
	}
	serverNonce := make([]byte, 32)
	if err := conn.SendHelloAck(&dari.HelloAckMessage{CoreVersion: 1, CryptoProfile: "DARI-BASE-1", ServerNonce: serverNonce}); err != nil {
		return
	}
	// Full protocol flow (HELLO → HELLO_ACK → AUTH_CHALLENGE →
	// AUTH_PROOF): the DARI clients dialing this listener (relay,
	// scheduler forwarder) expect the challenge record before sending
	// their proof.
	challenge := &dari.AuthChallengeMessage{
		ServerNonce:     serverNonce,
		ChallengeID:     []byte(fmt.Sprintf("pia-%s-%d", netConn.RemoteAddr(), time.Now().UnixNano())),
		RevocationEpoch: 0,
		AuthDeadlineMs:  uint64(time.Now().Add(30 * time.Second).UnixMilli()),
	}
	if err := conn.AuthChallenge(challenge); err != nil {
		return
	}
	if _, err := conn.RecvAuthProof(); err != nil {
		return
	}
	log.Printf("pia-paper: peer authenticated, ready for AI requests")
	pl.handleAIRequests(ctx, conn, connID)
}

func (pl *DARIListener) handleAIRequests(ctx context.Context, conn *dari.TransportConn, connID string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		record, err := conn.RecvRecord()
		if err != nil {
			return
		}
		msgType := dari.MessageType(record.MessageType)
		switch msgType {
		case dari.MsgAIOpen:
			var aiReq struct {
				Model     string                   `json:"model"`
				Messages  []map[string]interface{} `json:"messages"`
				MaxTokens int                      `json:"max_tokens"`
			}
			json.Unmarshal(record.Payload, &aiReq)
			vllmModel := os.Getenv("PCCP_VLLM_MODEL")
			if vllmModel == "" {
				vllmModel = aiReq.Model
			}
			infReq := InferenceRequest{Model: vllmModel, Messages: convertDARIMessages(aiReq.Messages), MaxTokens: aiReq.MaxTokens, Temperature: 0.7}
			resp, err := pl.svc.HandleInference(ctx, infReq)
			if err != nil {
				errPayload, _ := json.Marshal(map[string]string{"error": err.Error()})
				conn.SendMessage(dari.MsgAIComplete, nil, errPayload, record.LaneID, record.LaneSequence+1)
				continue
			}
			completePayload, _ := json.Marshal(resp)
			conn.SendMessage(dari.MsgAIComplete, nil, completePayload, record.LaneID, record.LaneSequence+1)
		case dari.MsgPing:
			conn.SendControl(dari.MsgPong, nil, []byte("pong"))
		}
	}
}

func (pl *DARIListener) ActiveConnections() int {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return len(pl.conns)
}

func convertDARIMessages(raw []map[string]interface{}) []Message {
	var msgs []Message
	for _, m := range raw {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		msgs = append(msgs, Message{Role: role, Content: content})
	}
	return msgs
}
