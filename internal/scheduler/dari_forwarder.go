package scheduler

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// dari_forwarder.go is the production Forwarder: it dials a worker's PIA
// over DARI (the address from the signed capability card) and performs a
// governed inference round trip. The scheduler never speaks HTTP to
// engines — the card's DariAddr is the only route (spec §13.1).

// DARIForwarder implements Forwarder over the DARI transport.
type DARIForwarder struct {
	tlsConfig *tls.Config
	timeout   time.Duration
}

// NewDARIForwarder builds a forwarder with the given TLS config (dev
// self-signed default when nil) and round-trip timeout.
func NewDARIForwarder(tlsConfig *tls.Config, timeout time.Duration) *DARIForwarder {
	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS13,
			NextProtos:         dari.DARIProtocols(),
		}
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &DARIForwarder{tlsConfig: tlsConfig, timeout: timeout}
}

// Send performs one inference round trip to workerAddr.
func (f *DARIForwarder) Send(workerAddr string, payload InferencePayload) (InferenceResult, error) {
	conn, err := dari.DialTCP(workerAddr, f.tlsConfig, dari.DefaultTransportConfig())
	if err != nil {
		return InferenceResult{}, fmt.Errorf("scheduler: dial worker %s: %w", workerAddr, err)
	}
	defer conn.Close()

	hello := &dari.HelloMessage{
		CoreVersions:       []uint8{1},
		PeerProfile:        dari.ProfileRelay,
		TransportFeatures:  []string{"tcp-tls"},
		Extensions:         map[string]uint8{"dari.ai/1": 1},
		CryptoProfiles:     []string{"DARI-BASE-1"},
		ClientNonce:        make([]byte, 32),
		ImplementationName: "pccp-scheduler",
	}
	if _, err := conn.Handshake(hello); err != nil {
		return InferenceResult{}, fmt.Errorf("scheduler: worker handshake: %w", err)
	}
	challenge, err := conn.RecvAuthChallenge()
	if err != nil {
		return InferenceResult{}, fmt.Errorf("scheduler: worker auth challenge: %w", err)
	}
	proof := &dari.AuthProofMessage{
		Credential:   []byte("scheduler-dispatch"),
		Signature:    []byte("scheduler"),
		KeyAlgorithm: dari.COSEAlgEdDSA,
		ChallengeID:  challenge.ChallengeID,
	}
	if err := conn.AuthProof(proof); err != nil {
		return InferenceResult{}, fmt.Errorf("scheduler: worker auth proof: %w", err)
	}

	requestBody := map[string]interface{}{
		"model":       payload.Model,
		"messages":    json.RawMessage(payload.Messages),
		"max_tokens":  payload.MaxTokens,
		"temperature": payload.Temperature,
	}
	reqJSON, err := json.Marshal(requestBody)
	if err != nil {
		return InferenceResult{}, fmt.Errorf("scheduler: marshal request: %w", err)
	}
	if err := conn.SendMessage(dari.MsgAIOpen, nil, reqJSON, 1, 1); err != nil {
		return InferenceResult{}, fmt.Errorf("scheduler: send AI_OPEN: %w", err)
	}

	deadline := time.Now().Add(f.timeout)
	for time.Now().Before(deadline) {
		record, err := conn.RecvRecord()
		if err != nil {
			return InferenceResult{}, fmt.Errorf("scheduler: recv from worker: %w", err)
		}
		switch dari.MessageType(record.MessageType) {
		case dari.MsgAIComplete:
			// The PIA replies with its OpenAI-compatible response shape
			// (id/model/choices/usage); normalize it into the scheduler's
			// InferenceResult.
			var raw struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(record.Payload, &raw); err != nil {
				// Fall back: the worker may already speak InferenceResult.
				var result InferenceResult
				if err2 := json.Unmarshal(record.Payload, &result); err2 != nil {
					return InferenceResult{}, fmt.Errorf("scheduler: decode completion: %w", err)
				}
				return result, nil
			}
			finish := ""
			if len(raw.Choices) > 0 {
				finish = raw.Choices[0].FinishReason
			}
			result := InferenceResult{
				Finish: finish,
				Usage: map[string]int{
					"prompt_tokens":     raw.Usage.PromptTokens,
					"completion_tokens": raw.Usage.CompletionTokens,
					"total_tokens":      raw.Usage.TotalTokens,
				},
			}
			for _, c := range raw.Choices {
				result.Text += c.Message.Content
			}
			return result, nil
		case dari.MsgAITokenChunk:
			continue // streaming deltas handled by the streaming path
		case dari.MsgClose:
			var errMsg map[string]string
			json.Unmarshal(record.Payload, &errMsg)
			return InferenceResult{}, fmt.Errorf("scheduler: worker error: %s", errMsg["error"])
		case dari.MsgPing:
			conn.SendControl(dari.MsgPong, nil, []byte("pong"))
		}
	}
	return InferenceResult{}, fmt.Errorf("scheduler: worker %s timeout", workerAddr)
}
