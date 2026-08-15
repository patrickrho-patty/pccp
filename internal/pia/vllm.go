package pia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// VLLMAdapter connects the PIA to a real vLLM serving engine.
// It translates between the PIA's internal inference request format
// and vLLM's OpenAI-compatible API.
type VLLMAdapter struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewVLLMAdapter creates a new vLLM adapter.
func NewVLLMAdapter(baseURL, apiKey string) *VLLMAdapter {
	return &VLLMAdapter{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// vLLMChatRequest is the OpenAI-compatible chat completion request.
type vLLMChatRequest struct {
	Model       string                 `json:"model"`
	Messages    []VLLMMessage          `json:"messages"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Temperature float64                `json:"temperature,omitempty"`
	TopP        float64                `json:"top_p,omitempty"`
	Stream      bool                   `json:"stream,omitempty"`
	Stop        []string               `json:"stop,omitempty"`
	Extra       map[string]interface{} `json:"extra_body,omitempty"`
}

// VLLMMessage is a chat message for vLLM.
type VLLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// vLLMChatResponse is the OpenAI-compatible chat completion response.
type vLLMChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []VLLMChoice `json:"choices"`
	Usage   VLLMUsage    `json:"usage"`
}

type VLLMChoice struct {
	Index        int         `json:"index"`
	Message      VLLMMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type VLLMUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletion sends a chat completion request to vLLM.
func (a *VLLMAdapter) ChatCompletion(ctx context.Context, req InferenceRequest) (*InferenceResponse, error) {
	vllmReq := vLLMChatRequest{
		Model:       req.Model,
		Messages:    make([]VLLMMessage, len(req.Messages)),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
	}
	for i, msg := range req.Messages {
		vllmReq.Messages[i] = VLLMMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	bodyJSON, err := json.Marshal(vllmReq)
	if err != nil {
		return nil, fmt.Errorf("vllm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("vllm: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vllm: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vllm: error %d: %s", resp.StatusCode, string(body))
	}

	var vllmResp vLLMChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&vllmResp); err != nil {
		return nil, fmt.Errorf("vllm: decode response: %w", err)
	}

	// Translate to PIA inference response
	piaResp := &InferenceResponse{
		ID:      vllmResp.ID,
		Object:  vllmResp.Object,
		Created: vllmResp.Created,
		Model:   vllmResp.Model,
		Usage: Usage{
			PromptTokens:     vllmResp.Usage.PromptTokens,
			CompletionTokens: vllmResp.Usage.CompletionTokens,
			TotalTokens:      vllmResp.Usage.TotalTokens,
		},
	}

	for _, choice := range vllmResp.Choices {
		piaResp.Choices = append(piaResp.Choices, Choice{
			Index:        choice.Index,
			Message:      Message{Role: choice.Message.Role, Content: choice.Message.Content},
			FinishReason: choice.FinishReason,
		})
	}

	return piaResp, nil
}

// CheckHealth checks if the vLLM server is healthy and the model is loaded.
func (a *VLLMAdapter) CheckHealth(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", a.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("vllm: create health request: %w", err)
	}

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("vllm: health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vllm: health check returned %d", resp.StatusCode)
	}
	return nil
}

// ListModels queries vLLM for loaded models.
func (a *VLLMAdapter) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", a.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if a.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vllm: models endpoint returned %d", resp.StatusCode)
	}

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

// VerifyModelLoaded checks that a specific model is loaded on the vLLM server.
func (a *VLLMAdapter) VerifyModelLoaded(ctx context.Context, modelID string) error {
	models, err := a.ListModels(ctx)
	if err != nil {
		return err
	}
	for _, m := range models {
		if m == modelID {
			return nil
		}
	}
	return fmt.Errorf("vllm: model %s is not loaded", modelID)
}
