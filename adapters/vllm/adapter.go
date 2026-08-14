// Package vllmadapter provides a reusable adapter for connecting vLLM
// inference engines to the DARI protocol via a PIA (Patty Inference Agent).
//
// This is the reference implementation per DARI Protocol Specification
// Appendix C: "PIA adapter examples for vLLM and SGLang."
//
// Usage:
//
//	adapter := vllmadapter.New("http://localhost:8000", "my-model")
//	pia := vllmadapter.NewPIA("pia-peer-01", adapter, ":9444")
//	pia.Start(ctx)
//
// The adapter handles:
//   - DARI peer authentication (HELLO → AUTH)
//   - AI_OPEN → vLLM /v1/chat/completions translation
//   - AI_COMPLETE response generation
//   - Token usage normalization
//   - Health checking
package vllmadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// VLLMClient connects to a vLLM serving engine via its OpenAI-compatible API.
// Per DARI §38.6: "A PIA adapter MAY call local serving-engine
// OpenAI-compatible APIs... behind PIA, on loopback/private endpoint."
type VLLMClient struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a vLLM adapter client.
// baseURL should be the vLLM endpoint (e.g. http://localhost:8000).
func New(baseURL string) *VLLMClient {
	return &VLLMClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // Long timeout for large models
		},
	}
}

// ChatRequest is the OpenAI-compatible request sent to vLLM.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	TopP        float64       `json:"top_p,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
}

// ChatMessage is a single message in the chat format.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse is the OpenAI-compatible response from vLLM.
type ChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []ChatChoice   `json:"choices"`
	Usage   ChatUsage      `json:"usage"`
}

// ChatChoice is a completion choice.
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatUsage contains token usage.
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletion sends a chat completion request to vLLM and returns the response.
func (c *VLLMClient) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	bodyJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("vllm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("vllm: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vllm: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vllm: error %d: %s", resp.StatusCode, string(body))
	}

	var result ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("vllm: decode response: %w", err)
	}

	return &result, nil
}

// HealthCheck verifies the vLLM server is running.
func (c *VLLMClient) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vllm: health check failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vllm: health check returned %d", resp.StatusCode)
	}
	return nil
}

// ListModels queries vLLM for loaded models.
func (c *VLLMClient) ListModels(ctx context.Context) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v1/models", nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []string
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

// VerifyModelLoaded checks that a specific model is available on vLLM.
func (c *VLLMClient) VerifyModelLoaded(ctx context.Context, modelID string) error {
	models, err := c.ListModels(ctx)
	if err != nil {
		return err
	}
	for _, m := range models {
		if m == modelID {
			return nil
		}
	}
	return fmt.Errorf("vllm: model %s not loaded", modelID)
}
