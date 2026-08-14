// Package sglangadapter provides a reusable adapter for connecting SGLang
// inference engines to the DARI protocol via a PIA.
//
// Per DARI Appendix C: "PIA adapter examples for vLLM and SGLang."
package sglangadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SGLangClient connects to an SGLang serving engine.
type SGLangClient struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *SGLangClient {
	return &SGLangClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 300 * time.Second},
	}
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []ChatChoice   `json:"choices"`
	Usage   map[string]int `json:"usage"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

func (c *SGLangClient) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	bodyJSON, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(bodyJSON))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sglang: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sglang: error %d: %s", resp.StatusCode, string(body))
	}

	var result ChatResponse
	json.NewDecoder(resp.Body).Decode(&result)
	return &result, nil
}

func (c *SGLangClient) HealthCheck(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sglang: health returned %d", resp.StatusCode)
	}
	return nil
}
