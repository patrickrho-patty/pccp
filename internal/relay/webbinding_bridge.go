package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/webbinding"
)

// webbinding_bridge.go is the relay's dari.web/1 carrier bridge: the
// executor serving EFFECT_STATUS and the governed envelope handler.

// webEffectExecutor is the relay-owned effect executor serving
// dari.web/1 EFFECT_STATUS queries with SIGNED status responses
// (F.10: a reconnecting caller queries status; it never re-executes).
type webEffectExecutor struct {
	executor *dari.EffectExecutor
	priv     ed25519.PrivateKey
}

// NewWebBindingServer builds the dari.web/1 carrier over this relay's
// governed path: every inbound envelope routes through GovernInference
// with the session's grant — the browser path never bypasses governance.
func (s *Service) NewWebBindingServer(allowedOrigins []string) (*webbinding.Server, error) {
	store, err := webbinding.NewSessionStore("")
	if err != nil {
		return nil, err
	}
	fx := &webEffectExecutor{
		executor: dari.NewEffectExecutor(s.relayID, s.policy.SigningPrivateKey()),
		priv:     s.policy.SigningPrivateKey(),
	}
	return webbinding.NewServer(store, allowedOrigins, func(sessionID string, envelope []byte) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Effect-status queries route to the durable executor view with
		// a SIGNED response (never fabricated).
		if opID, ok := strings.CutPrefix(string(envelope), "EFFECT_STATUS:"); ok {
			return fx.status(opID)
		}
		// Parse the canonical AI_OPEN envelope and govern it (shared
		// parser — defaulting matches the native listener).
		model, msgs, maxTokens, err := parseAIOpen(envelope)
		if err != nil {
			return nil, fmt.Errorf("webbinding: not an AI_OPEN envelope: %w", err)
		}
		resp, _, err := s.GovernInference(ctx, GovernRequest{
			SessionID: "web-" + sessionID,
			Model:     model,
			Messages:  msgs,
			MaxTokens: maxTokens,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp)
	})
}

// status returns the signed F.10 status response for an operation
// (ABSENT for unknown operations — honest, never fabricated). The
// response is shape-validated and kernel-signed.
func (w *webEffectExecutor) status(opID string) ([]byte, error) {
	state, result, ok := w.executor.Status(opID)
	if !ok {
		state = dari.EffectStateAbsent
	}
	body := &dari.EffectStatusBody{
		Version:     1,
		OperationID: opID,
		Kind:        2,
		State:       &state,
	}
	if result != nil && result.Body != nil {
		pd := result.Body.PrepareDigest
		body.PrepareDigest = &pd
	}
	env, err := dari.SignEffectStatus(body, w.priv)
	if err != nil {
		return nil, err
	}
	return env.COSEBytes, nil
}

// parseAIOpen is the single AI_OPEN payload parser shared by the
// native listener and the web carrier (defaulting cannot diverge).
func parseAIOpen(payload []byte) (model string, msgs []map[string]string, maxTokens int, err error) {
	var aiReq struct {
		Model     string                   `json:"model"`
		Messages  []map[string]interface{} `json:"messages"`
		MaxTokens int                      `json:"max_tokens"`
	}
	if err := json.Unmarshal(payload, &aiReq); err != nil {
		return "", nil, 0, fmt.Errorf("decode: %w", err)
	}
	msgs = make([]map[string]string, 0, len(aiReq.Messages))
	for _, m := range aiReq.Messages {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		msgs = append(msgs, map[string]string{"role": role, "content": content})
	}
	if aiReq.MaxTokens <= 0 {
		aiReq.MaxTokens = 4096
	}
	return aiReq.Model, msgs, aiReq.MaxTokens, nil
}
