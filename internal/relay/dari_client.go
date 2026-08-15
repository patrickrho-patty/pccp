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

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// DARIInferenceClient connects to a PIA via DARI protocol and sends
// inference requests as DARI AI_OPEN records (§9.2, §38.1).
// This REPLACES the HTTP /v1/chat/completions path.
type DARIInferenceClient struct {
	mu        sync.Mutex
	conn      *dari.TransportConn
	piaAddr   string
	tlsConfig *tls.Config
}

// NewDARIInferenceClient creates a DARI client for PIA communication.
func NewDARIInferenceClient(piaAddr string) *DARIInferenceClient {
	return &DARIInferenceClient{
		piaAddr:   piaAddr,
		tlsConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: []string{dari.DARIProtocol}},
	}
}

// ensureConnected establishes a DARI connection to the PIA if not already connected.
func (c *DARIInferenceClient) ensureConnected(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		// Test if connection is still alive with a ping
		if err := c.conn.SendControl(dari.MsgPing, nil, []byte("ping")); err != nil {
			c.conn.Close()
			c.conn = nil
		} else {
			return nil
		}
	}

	// Dial PIA via DARI TLS/TCP
	conn, err := dari.DialTCP(c.piaAddr, c.tlsConfig, dari.DefaultTransportConfig())
	if err != nil {
		return fmt.Errorf("paper-client: dial PIA %s: %w", c.piaAddr, err)
	}

	// Perform DARI handshake
	hello := &dari.HelloMessage{
		CoreVersions:       []uint8{1},
		PeerProfile:        dari.ProfileRelay,
		TransportFeatures:  []string{"tcp-tls"},
		Extensions:         map[string]uint8{"dari.ai/1": 1, "dari.model-supply/1": 1},
		CryptoProfiles:     []string{"DARI-BASE-1"},
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
	proof := &dari.AuthProofMessage{
		Credential:   []byte("relay-credential"),
		Signature:    []byte("relay-signature"),
		KeyAlgorithm: dari.COSEAlgEdDSA,
		ChallengeID:  challenge.ChallengeID,
	}

	if err := conn.AuthProof(proof); err != nil {
		conn.Close()
		return fmt.Errorf("paper-client: auth proof: %w", err)
	}

	c.conn = conn
	log.Printf("dari-client: connected to PIA at %s via DARI", c.piaAddr)
	return nil
}

// DARIInferenceResult holds the result of a DARI inference request.
type DARIInferenceResult struct {
	ID      string                   `json:"id"`
	Model   string                   `json:"model"`
	Choices []map[string]interface{} `json:"choices"`
	Usage   map[string]int           `json:"usage"`
}

// SendInference sends an AI inference request to the PIA via DARI protocol.
// This replaces the HTTP POST /v1/chat/completions path.
func (c *DARIInferenceClient) SendInference(ctx context.Context, model string, messages []map[string]interface{}, maxTokens int) (*DARIInferenceResult, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Build the DARI AI request payload
	// Per §10B: use DARI AI Semantic IR, not OpenAI format
	requestBody := map[string]interface{}{
		"model":      model,
		"messages":   messages,
		"max_tokens": maxTokens,
	}

	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("paper-client: marshal request: %w", err)
	}

	// Send AI_OPEN as a DARI MESSAGE record on lane 1
	if err := c.conn.SendMessage(dari.MsgAIOpen, nil, payload, 1, 1); err != nil {
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

		msgType := dari.MessageType(record.MessageType)

		switch msgType {
		case dari.MsgAIComplete:
			// Decode the AI_COMPLETE response
			var result DARIInferenceResult
			if err := json.Unmarshal(record.Payload, &result); err != nil {
				return nil, fmt.Errorf("paper-client: decode AI_COMPLETE: %w", err)
			}
			return &result, nil

		case dari.MsgAITokenChunk:
			// Streaming token chunk — for non-streaming mode, accumulate or skip
			// In a streaming implementation, these would be forwarded to the Harness
			continue

		case dari.MsgClose:
			var errMsg map[string]string
			json.Unmarshal(record.Payload, &errMsg)
			return nil, fmt.Errorf("paper-client: PIA error: %s", errMsg["error"])

		case dari.MsgPing:
			c.conn.SendControl(dari.MsgPong, nil, []byte("pong"))
			continue

		default:
			// Unknown message — skip
			continue
		}
	}

	return nil, fmt.Errorf("paper-client: timeout waiting for AI_COMPLETE")
}

// Close closes the DARI connection.
func (c *DARIInferenceClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// GetDARIInferenceClient returns a DARI inference client for the configured PIA.
// If PCCP_PIA_DARI_ADDR is not set, returns nil (caller should use HTTP fallback).
var (
	globalDARIWireClient *DARIInferenceClient
	globalDARIOnce       sync.Once
)

func getDARIInferenceClient() *DARIInferenceClient {
	globalDARIOnce.Do(func() {
		addr := os.Getenv("PCCP_PIA_DARI_ADDR")
		if addr != "" {
			globalDARIWireClient = NewDARIInferenceClient(addr)
			log.Printf("paper-client: will use DARI transport to PIA at %s", addr)
		}
	})
	return globalDARIWireClient
}
