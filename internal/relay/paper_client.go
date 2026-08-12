package relay

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/paper"
)

// PaperInferenceClient connects to a PIA via PAPER protocol and sends
// inference requests as PAPER AI_OPEN records (§9.2, §38.1).
// This REPLACES the HTTP /v1/chat/completions path.
type PaperInferenceClient struct {
	mu        sync.Mutex
	conn      *paper.TransportConn
	piaAddr   string
	tlsConfig *tls.Config
}

// NewPaperInferenceClient creates a PAPER client for PIA communication.
func NewPaperInferenceClient(piaAddr string) *PaperInferenceClient {
	return &PaperInferenceClient{
		piaAddr:   piaAddr,
		tlsConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: []string{paper.ALPNProtocol}},
	}
}

// ensureConnected establishes a PAPER connection to the PIA if not already connected.
func (c *PaperInferenceClient) ensureConnected(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		// Test if connection is still alive with a ping
		if err := c.conn.SendControl(paper.MsgPing, nil, []byte("ping")); err != nil {
			c.conn.Close()
			c.conn = nil
		} else {
			return nil
		}
	}

	// Dial PIA via PAPER TLS/TCP
	conn, err := paper.DialTCP(c.piaAddr, c.tlsConfig, paper.DefaultTransportConfig())
	if err != nil {
		return fmt.Errorf("paper-client: dial PIA %s: %w", c.piaAddr, err)
	}

	// Perform PAPER handshake
	hello := &paper.HelloMessage{
		CoreVersions:       []uint8{1},
		PeerProfile:        paper.ProfileRelay,
		TransportFeatures:  []string{"tcp-tls"},
		Extensions:         map[string]uint8{"paper.ai/1": 1, "paper.models/1": 1},
		CryptoProfiles:     []string{"PAPER-BASE-1"},
		ClientNonce:        make([]byte, 32),
		ImplementationName: "pccp-relay",
	}

	_, err = conn.Handshake(hello)
	if err != nil {
		conn.Close()
		return fmt.Errorf("paper-client: handshake: %w", err)
	}

	// Receive AUTH_CHALLENGE
	challenge, err := conn.RecvAuthChallenge()
	if err != nil {
		conn.Close()
		return fmt.Errorf("paper-client: auth challenge: %w", err)
	}

	// Send AUTH_PROOF (simplified — in production, sign with relay's PPC)
	proof := &paper.AuthProofMessage{
		Credential:   []byte("relay-credential"),
		Signature:    []byte("relay-signature"),
		KeyAlgorithm: paper.COSEAlgEdDSA,
		ChallengeID:  challenge.ChallengeID,
	}

	if err := conn.AuthProof(proof); err != nil {
		conn.Close()
		return fmt.Errorf("paper-client: auth proof: %w", err)
	}

	c.conn = conn
	log.Printf("paper-client: connected to PIA at %s via PAPER", c.piaAddr)
	return nil
}

// PaperInferenceResult holds the result of a PAPER inference request.
type PaperInferenceResult struct {
	ID      string                   `json:"id"`
	Model   string                   `json:"model"`
	Choices []map[string]interface{} `json:"choices"`
	Usage   map[string]int           `json:"usage"`
}

// SendInference sends an AI inference request to the PIA via PAPER protocol.
// This replaces the HTTP POST /v1/chat/completions path.
func (c *PaperInferenceClient) SendInference(ctx context.Context, model string, messages []map[string]interface{}, maxTokens int) (*PaperInferenceResult, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Build the PAPER AI request payload
	// Per §10B: use PAPER AI Semantic IR, not OpenAI format
	requestBody := map[string]interface{}{
		"model":      model,
		"messages":   messages,
		"max_tokens": maxTokens,
	}

	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("paper-client: marshal request: %w", err)
	}

	// Send AI_OPEN as a PAPER MESSAGE record on lane 1
	if err := c.conn.SendMessage(paper.MsgAIOpen, nil, payload, 1, 1); err != nil {
		c.conn.Close()
		c.conn = nil
		return nil, fmt.Errorf("paper-client: send AI_OPEN: %w", err)
	}

	// Set a deadline for the response
	deadline := time.Now().Add(120 * time.Second)

	// Receive response — should be AI_COMPLETE
	for time.Now().Before(deadline) {
		record, err := c.conn.RecvRecord()
		if err != nil {
			c.conn.Close()
			c.conn = nil
			return nil, fmt.Errorf("paper-client: recv response: %w", err)
		}

		msgType := paper.MessageType(record.MessageType)

		switch msgType {
		case paper.MsgAIComplete:
			// Decode the AI_COMPLETE response
			var result PaperInferenceResult
			if err := json.Unmarshal(record.Payload, &result); err != nil {
				return nil, fmt.Errorf("paper-client: decode AI_COMPLETE: %w", err)
			}
			return &result, nil

		case paper.MsgAITokenChunk:
			// Streaming token chunk — for non-streaming mode, accumulate or skip
			// In a streaming implementation, these would be forwarded to the Harness
			continue

		case paper.MsgClose:
			var errMsg map[string]string
			json.Unmarshal(record.Payload, &errMsg)
			return nil, fmt.Errorf("paper-client: PIA error: %s", errMsg["error"])

		case paper.MsgPing:
			c.conn.SendControl(paper.MsgPong, nil, []byte("pong"))
			continue

		default:
			// Unknown message — skip
			continue
		}
	}

	return nil, fmt.Errorf("paper-client: timeout waiting for AI_COMPLETE")
}

// Close closes the PAPER connection.
func (c *PaperInferenceClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// GetPaperInferenceClient returns a PAPER inference client for the configured PIA.
// If PCCP_PIA_PAPER_ADDR is not set, returns nil (caller should use HTTP fallback).
var (
	globalPaperClient *PaperInferenceClient
	globalPaperOnce   sync.Once
)

func getPaperInferenceClient() *PaperInferenceClient {
	globalPaperOnce.Do(func() {
		addr := os.Getenv("PCCP_PIA_PAPER_ADDR")
		if addr != "" {
			globalPaperClient = NewPaperInferenceClient(addr)
			log.Printf("paper-client: will use PAPER transport to PIA at %s", addr)
		}
	})
	return globalPaperClient
}
