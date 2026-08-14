package relay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// streaming.go implements F1 (harness latency plan): governed token
// streaming. The relay forwards the governed request to the PIA with
// stream=true, relays every SSE delta to the harness as an
// AI_TOKEN_CHUNK record, and completes with the authoritative
// AI_COMPLETE (usage, finish reason). Governance (lease, epoch, DLP,
// metering, receipts) is unchanged — streaming is transport-only.

// DeltaSender is invoked once per streamed token delta.
type DeltaSender func(text string)

// GovernInferenceStream is GovernInference with token streaming. The
// governance pipeline (authorize → DLP → forward → meter → receipt) is
// identical; the forwarder streams and each delta is handed to onDelta
// as it arrives.
func (s *Service) GovernInferenceStream(ctx context.Context, req GovernRequest, onDelta DeltaSender) (*InferenceResponse, *models.EvidenceReceipt, error) {
	return s.GovernInference(ctx, req, onDelta)
}

// streamForwarder performs the SSE-streaming PIA request. It returns
// the assembled InferenceResponse; onDelta receives each content
// delta. Falls back to the non-streaming forwarder when the PIA
// rejects streaming (no "stream" support → non-SSE body).
func (s *Service) streamForwarder(ctx context.Context, req InferenceRequest, endpointLeaseID string, onDelta DeltaSender) (*InferenceResponse, error) {
	piaURL := piaAPIBase()
	if piaURL == "" {
		// No external PIA configured: use the injected test forwarder.
		return s.forwarder(ctx, req, endpointLeaseID)
	}

	payload := map[string]interface{}{
		"model":       req.Model,
		"messages":    req.Messages,
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
		"exchange_id": req.ExchangeID,
		"stream":      true,
	}
	bodyJSON, _ := json.Marshal(payload)
	httpReq, err := s.preparePIARequest(ctx, piaURL, bodyJSON, endpointLeaseID, req.ExchangeID)
	if err != nil {
		return nil, fmt.Errorf("relay: PIA stream request build failed: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("relay: PIA stream failed: %w", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("relay: PIA error (%d): %s", resp.StatusCode, string(body))
	}
	if !strings.Contains(ct, "text/event-stream") {
		// Non-streaming body: decode as a normal completion (some PIAs
		// ignore stream=true).
		var inferenceResp InferenceResponse
		if err := json.NewDecoder(resp.Body).Decode(&inferenceResp); err != nil {
			return nil, fmt.Errorf("relay: PIA decode: %w", err)
		}
		if onDelta != nil {
			if content := firstChoiceContent(inferenceResp); content != "" {
				onDelta(content)
			}
		}
		return &inferenceResp, nil
	}

	return consumeSSE(resp.Body, onDelta)
}

// consumeSSE parses an OpenAI-compatible SSE stream, invoking onDelta
// per content delta and assembling the final response.
func consumeSSE(r io.Reader, onDelta DeltaSender) (*InferenceResponse, error) {
	out := &InferenceResponse{
		Choices: []map[string]interface{}{{"message": map[string]interface{}{"role": "assistant"}}},
		Usage:   map[string]int{},
	}
	message := out.Choices[0]["message"].(map[string]interface{})
	var content strings.Builder
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var frame struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			continue
		}
		if frame.ID != "" {
			out.ID = frame.ID
		}
		if frame.Model != "" {
			out.Model = frame.Model
		}
		for _, choice := range frame.Choices {
			if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
				if onDelta != nil {
					onDelta(choice.Delta.Content)
				}
			}
			if choice.FinishReason != nil {
				message["finish_reason"] = *choice.FinishReason
			}
		}
		if frame.Usage != nil {
			out.Usage["prompt_tokens"] = frame.Usage.PromptTokens
			out.Usage["completion_tokens"] = frame.Usage.CompletionTokens
			out.Usage["total_tokens"] = frame.Usage.TotalTokens
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("relay: PIA stream read: %w", err)
	}
	message["content"] = content.String()
	if message["finish_reason"] == nil && content.Len() > 0 {
		message["finish_reason"] = "stop"
	}
	if out.Usage["total_tokens"] == 0 {
		out.Usage["prompt_tokens"] = 0
		out.Usage["completion_tokens"] = len(content.String()) / 4 // conservative estimate
		out.Usage["total_tokens"] = out.Usage["prompt_tokens"] + out.Usage["completion_tokens"]
	}
	return out, nil
}

// firstChoiceContent extracts choice[0] content from a response.
func firstChoiceContent(resp InferenceResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	if msg, ok := resp.Choices[0]["message"].(map[string]interface{}); ok {
		c, _ := msg["content"].(string)
		return c
	}
	c, _ := resp.Choices[0]["text"].(string)
	return c
}

var _ = bytes.MinRead
