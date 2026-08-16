package pia

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements the Patty Inference Agent.
// It stands between the Relay and the actual serving engine (vLLM/SGLang/mock),
// verifying model packages, holding endpoint leases, and proxying inference requests.
type Service struct {
	db          *gorm.DB
	peerID      string
	privKey     ed25519.PrivateKey
	pubKey      ed25519.PublicKey
	pubKeyHex   string
	servingURL  string
	servingType string
	assureLevel string
	modelPkgID  string
	vllmAdapter *VLLMAdapter

	mu          sync.RWMutex
	endpointID  string
	lease       *models.EndpointLease
	cpURL       string
	httpClient  *http.Client
	attestTimer *time.Timer
}

// Config holds PIA runtime configuration.
type Config struct {
	PeerID          string `json:"peer_id"`
	ServingURL      string `json:"serving_url"`
	ServingType     string `json:"serving_type"` // vllm, sglang, mock
	AssuranceLevel  string `json:"assurance_level"`
	ModelPackageID  string `json:"model_package_id"`
	ControlPlaneURL string `json:"control_plane_url"`
}

// New creates a new PIA service.
func New(db *gorm.DB, cfg Config) (*Service, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("pia: generate key: %w", err)
	}

	var vllm *VLLMAdapter
	if cfg.ServingType == "vllm" {
		vllm = NewVLLMAdapter(cfg.ServingURL, "")
	}

	s := &Service{
		db:          db,
		peerID:      cfg.PeerID,
		privKey:     priv,
		pubKey:      pub,
		pubKeyHex:   hex.EncodeToString(pub),
		servingURL:  cfg.ServingURL,
		servingType: cfg.ServingType,
		assureLevel: cfg.AssuranceLevel,
		modelPkgID:  cfg.ModelPackageID,
		vllmAdapter: vllm,
		cpURL:       cfg.ControlPlaneURL,
		httpClient:  &http.Client{Timeout: 120 * time.Second},
	}

	return s, nil
}

// PublicKeyHex returns the PIA's Ed25519 public key in hex.
func (s *Service) PublicKeyHex() string {
	return s.pubKeyHex
}

// PeerID returns the PIA's peer identifier.
func (s *Service) PeerID() string {
	return s.peerID
}

// EnrollWithControlPlane registers the PIA with the control plane.
func (s *Service) EnrollWithControlPlane(ctx context.Context, orgID, modelPackageID string) error {
	reqBody := map[string]interface{}{
		"organization_id":        orgID,
		"pia_peer_id":            s.peerID,
		"model_package_id":       modelPackageID,
		"serving_engine":         s.servingType,
		"serving_engine_version": "0.6.0",
		"public_key_hex":         s.pubKeyHex,
		"node_identity":          fmt.Sprintf("spiffe://patty.local/node/%s", s.peerID),
		"assurance_level":        s.assureLevel,
	}

	bodyJSON, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", s.cpURL+"/api/endpoints/enroll", bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("pia: create enroll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	s.setServiceAuth(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pia: enroll request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pia: enroll failed (%d): %s", resp.StatusCode, string(body))
	}

	var result models.InferenceEndpoint
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("pia: decode enroll response: %w", err)
	}

	s.mu.Lock()
	s.endpointID = result.EndpointID
	s.mu.Unlock()
	// Attest the endpoint so lease issuance has a fresh measurement
	// record (registry gate: no attestation, no lease). The digest
	// covers what THIS PIA can actually measure about itself.
	if aerr := s.attestSelf(ctx, orgID, modelPackageID); aerr != nil {
		log.Printf("pia: self-attestation failed (leases will be refused): %v", aerr)
	}

	log.Printf("pia: enrolled as endpoint %s", result.EndpointID)
	return nil
}

// RequestLease requests a new endpoint lease from the control plane.
func (s *Service) RequestLease(ctx context.Context) error {
	s.mu.RLock()
	epID := s.endpointID
	s.mu.RUnlock()
	if epID == "" {
		return fmt.Errorf("pia: not enrolled, cannot request lease")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.cpURL+"/api/endpoints/"+epID+"/lease", nil)
	if err != nil {
		return fmt.Errorf("pia: create lease request: %w", err)
	}
	s.setServiceAuth(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pia: lease request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pia: lease request failed (%d): %s", resp.StatusCode, string(body))
	}

	var lease models.EndpointLease
	if err := json.NewDecoder(resp.Body).Decode(&lease); err != nil {
		return fmt.Errorf("pia: decode lease: %w", err)
	}

	s.mu.Lock()
	s.lease = &lease
	s.mu.Unlock()

	log.Printf("pia: received lease %s (valid until %s)", lease.LeaseID, lease.NotAfter)
	return nil
}

// HasValidLease checks if the PIA currently holds a valid lease.
// It checks in-memory state first, then falls back to the database.
func (s *Service) HasValidLease() bool {
	s.mu.RLock()
	lease := s.lease
	s.mu.RUnlock()
	if lease != nil && lease.Status == "active" {
		notAfter, err := time.Parse(time.RFC3339, lease.NotAfter)
		if err == nil && time.Now().Before(notAfter) {
			return true
		}
	}
	// Check database for a valid lease. A nil DB means no lease state
	// exists at all — fail closed, never panic.
	s.mu.RLock()
	epID := s.endpointID
	s.mu.RUnlock()
	if epID == "" {
		if s.db == nil {
			return false
		}
		// Discover endpoint_id from DB by matching pia_peer_id
		var ep models.InferenceEndpoint
		if err := s.db.Where("pia_peer_id = ?", s.peerID).First(&ep).Error; err == nil {
			s.mu.Lock()
			s.endpointID = ep.EndpointID
			s.mu.Unlock()
			epID = ep.EndpointID
		}
	}
	if epID == "" {
		return false
	}
	if s.db == nil {
		return false
	}
	var dbLease models.EndpointLease
	err := s.db.Where("endpoint_id = ? AND status = 'active' AND not_after > ?",
		epID, time.Now().Format(time.RFC3339)).
		Order("issued_at DESC").First(&dbLease).Error
	if err == nil {
		s.mu.Lock()
		s.lease = &dbLease
		s.mu.Unlock()
		return true
	}
	return false
}

// InferenceRequest is the DARI inference request payload.
type InferenceRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
	ExchangeID  string    `json:"exchange_id,omitempty"`
}

// Message is a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// InferenceResponse is the OpenAI-compatible completion response.
type InferenceResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice is a completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage contains token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// HandleInference processes an inference request by proxying to the local serving engine.
func (s *Service) HandleInference(ctx context.Context, req InferenceRequest) (*InferenceResponse, error) {
	// In direct-proxy mode, skip lease check
	directMode := os.Getenv("PCCP_PIA_DIRECT") == "1"
	if !directMode && !s.HasValidLease() {
		if err := s.RequestLease(ctx); err != nil {
			return nil, fmt.Errorf("pia: no valid lease: %w", err)
		}
	}

	switch s.servingType {
	case "mock":
		return s.handleMockInference(ctx, req)
	default:
		return s.proxyToEngine(ctx, req)
	}
}

// proxyToEngine forwards the request to the actual serving engine (vLLM/SGLang).
func (s *Service) proxyToEngine(ctx context.Context, req InferenceRequest) (*InferenceResponse, error) {
	// Use the vLLM adapter if available
	if s.vllmAdapter != nil {
		return s.vllmAdapter.ChatCompletion(ctx, req)
	}
	bodyJSON, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.servingURL+"/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("pia: create engine request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("pia: engine request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pia: engine error (%d): %s", resp.StatusCode, string(body))
	}

	var result InferenceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("pia: decode engine response: %w", err)
	}
	return &result, nil
}

// handleMockInference generates a mock response for development/testing.
func (s *Service) handleMockInference(ctx context.Context, req InferenceRequest) (*InferenceResponse, error) {
	// Build a Korean-aware mock response based on the last user message
	var userMsg string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			userMsg = req.Messages[i].Content
			break
		}
	}

	mockResponse := generateMockCodeResponse(userMsg, req.Model)
	promptTokens := len(strings.Fields(userMsg))/4 + 10
	completionTokens := len(strings.Fields(mockResponse))/4 + 10

	return &InferenceResponse{
		ID:      fmt.Sprintf("chatcmpl-mock-%d", time.Now().UnixMilli()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []Choice{
			{
				Index:        0,
				Message:      Message{Role: "assistant", Content: mockResponse},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}, nil
}

// generateMockCodeResponse produces a realistic mock code response for the Phase 0 demo.
func generateMockCodeResponse(prompt, model string) string {
	// Korean-aware mock: detect Korean characters and respond appropriately
	hasKorean := false
	for _, r := range prompt {
		if r >= 0xAC00 && r <= 0xD7A3 {
			hasKorean = true
			break
		}
	}

	if hasKorean {
		return "요청하신 Korean 코딩 작업에 대한 응답입니다 (모델: " + model + ").\n\n" +
			"```go\n" +
			"// AI가 생성한 코드 예시 - 출처: " + model + "\n" +
			"// 세션 프로바이던스 체인에 기록됨\n" +
			"package main\n\n" +
			"import \"fmt\"\n\n" +
			"// RefundProcessor processes payment refunds\n" +
			"type RefundProcessor struct {\n" +
			"\tamount  int64\n" +
			"\tcurrency string\n" +
			"}\n\n" +
			"func (r *RefundProcessor) Process() error {\n" +
			"\tif r.amount <= 0 {\n" +
			"\t\treturn fmt.Errorf(\"invalid refund amount\")\n" +
			"\t}\n" +
			"\tfmt.Printf(\"Processing refund\")\n" +
			"\treturn nil\n" +
			"}\n" +
			"```\n\n" +
			"이 코드는 프로바이던스 추적을 통해 생성자, 하네스, 모델, 엔드포인트 정보와 함께 기록됩니다."
	}

	return fmt.Sprintf(`Here is the code response for your request (model: %s).

`+"```go"+`// AI-generated code — source: %s
// Recorded in the provenance chain
package main

import "fmt"

func ExampleFunction() {
    fmt.Println("Hello from Patty Code")
}
`+"```", model, model)
}

// StartAttestationLoop starts periodic re-attestation.
func (s *Service) StartAttestationLoop(ctx context.Context, interval time.Duration) {
	s.attestTimer = time.NewTimer(interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.attestTimer.C:
				if err := s.RequestLease(ctx); err != nil {
					log.Printf("pia: re-attestation failed: %v", err)
				}
				s.attestTimer.Reset(interval)
			}
		}
	}()
}

// CPURL returns the control plane URL for lease requests.
func (s *Service) CPURL() string {
	return s.cpURL
}

// EndpointID returns the enrolled endpoint ID.
func (s *Service) EndpointID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.endpointID
}

// setServiceAuth attaches the machine credential for control-plane
// calls: PCCP_CP_SERVICE_TOKEN (a console-issued JWT for the service
// principal). Absent token = unauthenticated (CP refuses — honest).
func (s *Service) setServiceAuth(req *http.Request) {
	if tok := os.Getenv("PCCP_CP_SERVICE_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
}

// attestSelf submits the endpoint's measurement envelope to the CP.
func (s *Service) attestSelf(ctx context.Context, orgID, modelPackageID string) error {
	epID := s.EndpointID()
	h := sha256.Sum256([]byte(fmt.Sprintf("pia|%s|%s|%s|%s", s.peerID, s.servingType, "0.6.0", s.servingURL)))
	att := map[string]string{
		"endpoint_id":              epID,
		"organization_id":          orgID,
		"nonce":                    hex.EncodeToString(h[:8]),
		"model_package_id":         modelPackageID,
		"pia_build_digest":         "sha256:" + hex.EncodeToString(h[:]),
		"serving_container_digest": "sha256:" + hex.EncodeToString(h[:]),
	}
	// Sign the canonical measurement bytes with the PIA's key (the CP
	// verifies under the ENROLLED public key).
	sig := ed25519.Sign(s.privKey, registryAttestationBytes(att))
	att["signature"] = hex.EncodeToString(sig)
	body, _ := json.Marshal(att)
	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/endpoints/%s/attest", s.cpURL, epID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	s.setServiceAuth(req)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("attestation (%d): %s", resp.StatusCode, string(b))
	}
	return nil
}

// registryAttestationBytes mirrors registry.AttestationSigningBytes.
func registryAttestationBytes(att map[string]string) []byte {
	get := func(k string) string { return att[k] }
	return []byte(fmt.Sprintf("pia-attest-v1|%s|%s|%s|%s|%s|%s|%s",
		get("endpoint_id"), get("organization_id"), get("nonce"), get("model_package_id"),
		get("pia_build_digest"), get("serving_container_digest"), get("runtime_config_digest")))
}
