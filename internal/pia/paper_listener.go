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

	"github.com/patrickrho-patty/pccp/internal/paper"
)

// PaperListener accepts native PAPER protocol connections on the PIA.
// Per v2 §9.2: "PAPER Relay to PIA: PAPER"
type PaperListener struct {
	svc       *Service
	tlsConfig *tls.Config
	mu        sync.Mutex
	conns     map[string]*paper.TransportConn
}

func NewPaperListener(svc *Service) *PaperListener {
	return &PaperListener{
		svc:       svc,
		tlsConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: []string{paper.ALPNProtocol}},
		conns:     make(map[string]*paper.TransportConn),
	}
}

func (pl *PaperListener) ListenTCP(ctx context.Context, addr string) error {
	listener, err := paper.ListenTCP(addr, pl.tlsConfig)
	if err != nil {
		return fmt.Errorf("pia-paper: listen %s: %w", addr, err)
	}
	defer listener.Close()
	log.Printf("pia-paper: PAPER listener on %s", addr)

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

func (pl *PaperListener) handleConn(ctx context.Context, netConn net.Conn) {
	defer netConn.Close()
	conn, err := paper.AcceptTCP(netConn, paper.DefaultTransportConfig())
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
	if hello.PeerProfile != paper.ProfileRelay && hello.PeerProfile != paper.ProfileControl {
		return
	}
	serverNonce := make([]byte, 32)
	conn.SendHelloAck(&paper.HelloAckMessage{CoreVersion: 1, CryptoProfile: "PAPER-BASE-1", ServerNonce: serverNonce})
	conn.RecvAuthProof()
	log.Printf("pia-paper: peer authenticated, ready for AI requests")
	pl.handleAIRequests(ctx, conn, connID)
}

func (pl *PaperListener) handleAIRequests(ctx context.Context, conn *paper.TransportConn, connID string) {
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
		msgType := paper.MessageType(record.MessageType)
		switch msgType {
		case paper.MsgAIOpen:
			var aiReq struct {
				Model     string                   `json:"model"`
				Messages  []map[string]interface{} `json:"messages"`
				MaxTokens int                      `json:"max_tokens"`
			}
			json.Unmarshal(record.Payload, &aiReq)
			vllmModel := os.Getenv("PCCP_VLLM_MODEL")
			if vllmModel == "" { vllmModel = aiReq.Model }
			infReq := InferenceRequest{Model: vllmModel, Messages: convertPaperMessages(aiReq.Messages), MaxTokens: aiReq.MaxTokens, Temperature: 0.7}
			resp, err := pl.svc.HandleInference(ctx, infReq)
			if err != nil {
				errPayload, _ := json.Marshal(map[string]string{"error": err.Error()})
				conn.SendMessage(paper.MsgAIComplete, nil, errPayload, record.LaneID, record.LaneSequence+1)
				continue
			}
			completePayload, _ := json.Marshal(resp)
			conn.SendMessage(paper.MsgAIComplete, nil, completePayload, record.LaneID, record.LaneSequence+1)
		case paper.MsgPing:
			conn.SendControl(paper.MsgPong, nil, []byte("pong"))
		}
	}
}

func (pl *PaperListener) ActiveConnections() int {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return len(pl.conns)
}

func convertPaperMessages(raw []map[string]interface{}) []Message {
	var msgs []Message
	for _, m := range raw {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		msgs = append(msgs, Message{Role: role, Content: content})
	}
	return msgs
}
