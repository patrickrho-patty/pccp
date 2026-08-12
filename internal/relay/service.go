package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"os"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/paper"
	"github.com/patrickrho-patty/pccp/internal/security"
	"github.com/patrickrho-patty/pccp/internal/provenance"
	"gorm.io/gorm"
)

// Service implements the PAPER Relay data plane.
// The Relay authenticates peers, validates capability leases, binds policy
// epochs, performs governance checks, routes to enrolled PIA, and emits
// evidence receipts.
type Service struct {
	db           *gorm.DB
	provenance   *provenance.Service
	security     *security.Service
	cpURL        string
	relayID      string
	httpClient   *http.Client

	// In-flight exchanges
	mu        sync.RWMutex
	exchanges map[string]*Exchange
}

// Exchange tracks a governed exchange in progress.
type Exchange struct {
	ID             string                 `json:"id"`
	SessionID      string                 `json:"session_id"`
	OrganizationID string                 `json:"organization_id"`
	UserID         string                 `json:"user_id"`
	HarnessID      string                 `json:"harness_id"`
	LeaseID        string                 `json:"lease_id"`
	PolicyEpochID  string                 `json:"policy_epoch_id"`
	State          paper.ExchangeState    `json:"state"`
	ModelPackageID string                 `json:"model_package_id"`
	EndpointID     string                 `json:"endpoint_id"`
	Verdict        paper.VerdictResult    `json:"verdict"`
	OpenedAt       time.Time              `json:"opened_at"`
	EvidenceChain  []string               `json:"evidence_chain,omitempty"`
}

// New creates a new Relay service.
func New(db *gorm.DB, cpURL, relayID string) (*Service, error) {
	provSvc, err := provenance.New(db, relayID)
	if err != nil {
		return nil, fmt.Errorf("relay: init provenance: %w", err)
	}

	return &Service{
		db:         db,
		provenance: provSvc,
		security:   security.New(db),
		cpURL:      cpURL,
		relayID:    relayID,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		exchanges:  make(map[string]*Exchange),
	}, nil
}

// OpenExchange starts a governed exchange for an AI inference request.
type OpenExchangeRequest struct {
	OrganizationID  string `json:"organization_id"`
	SessionID       string `json:"session_id"`
	UserID          string `json:"user_id"`
	HarnessID       string `json:"harness_id"`
	LeaseID         string `json:"lease_id"`
	PolicyEpochID   string `json:"policy_epoch_id"`
	ModelPackageID  string `json:"model_package_id"`
	ProjectID       string `json:"project_id,omitempty"`
	RepositoryID    string `json:"repository_id,omitempty"`
	Branch          string `json:"branch,omitempty"`
	Purpose         string `json:"purpose,omitempty"`
}

// OpenExchange creates and authorizes a new governed exchange.
func (s *Service) OpenExchange(ctx context.Context, req OpenExchangeRequest) (*Exchange, paper.VerdictResult, error) {
	exchangeID := paper.GenerateID("exch")
	exchange := &Exchange{
		ID:             exchangeID,
		SessionID:      req.SessionID,
		OrganizationID: req.OrganizationID,
		UserID:         req.UserID,
		HarnessID:      req.HarnessID,
		LeaseID:        req.LeaseID,
		PolicyEpochID:  req.PolicyEpochID,
		State:          paper.ExchangeCreated,
		ModelPackageID: req.ModelPackageID,
		OpenedAt:       time.Now(),
	}

	// Authorize: validate the capability lease
	verdict, err := s.authorize(ctx, req)
	if err != nil {
		exchange.State = paper.ExchangeDenied
		exchange.Verdict = paper.VerdictDeny
		s.recordExchange(exchange)
		return exchange, paper.VerdictDeny, fmt.Errorf("relay: authorization failed: %w", err)
	}
	exchange.Verdict = verdict

	if verdict == paper.VerdictDeny {
		exchange.State = paper.ExchangeDenied
		s.recordExchange(exchange)
		return exchange, verdict, fmt.Errorf("relay: exchange denied")
	}

	exchange.State = paper.ExchangeAuthorized
	s.recordExchange(exchange)

	// Record the action
	s.provenance.RecordAction(provenance.RecordActionRequest{
		OrganizationID: req.OrganizationID,
		SessionID:      req.SessionID,
		ExchangeID:     exchangeID,
		UserID:         req.UserID,
		HarnessID:      req.HarnessID,
		ModelPackageID: req.ModelPackageID,
		ProjectID:      req.ProjectID,
		RepositoryID:   req.RepositoryID,
		Branch:         req.Branch,
		PolicyEpochID:  req.PolicyEpochID,
		LeaseID:        req.LeaseID,
		ActionType:     "exchange.opened",
		VerdictResult:  string(verdict),
	})

	return exchange, verdict, nil
}

// authorize performs the governance checks for an exchange.
func (s *Service) authorize(ctx context.Context, req OpenExchangeRequest) (paper.VerdictResult, error) {
	// 1. Validate the capability lease
	var lease models.CapabilityLease
	if err := s.db.Where("lease_id = ? AND organization_id = ?", req.LeaseID, req.OrganizationID).First(&lease).Error; err != nil {
		return paper.VerdictDeny, fmt.Errorf("capability lease not found")
	}
	if lease.Status != "active" {
		return paper.VerdictDeny, fmt.Errorf("lease status is %s", lease.Status)
	}
	notAfter, _ := time.Parse(time.RFC3339, lease.NotAfter)
	if time.Now().After(notAfter) {
		return paper.VerdictDeny, fmt.Errorf("lease expired")
	}

	// 2. Validate model is allowed under policy epoch
	var epoch models.PolicyEpoch
	if err := s.db.Where("epoch_id = ?", req.PolicyEpochID).First(&epoch).Error; err != nil {
		return paper.VerdictDeny, fmt.Errorf("policy epoch not found")
	}

	var allowedModels []string
	json.Unmarshal([]byte(epoch.AllowedModelsJSON), &allowedModels)
	modelAllowed := false
	for _, m := range allowedModels {
		if m == req.ModelPackageID {
			modelAllowed = true
			break
		}
	}
	if !modelAllowed {
		return paper.VerdictDeny, fmt.Errorf("model %s not allowed under policy epoch %s", req.ModelPackageID, req.PolicyEpochID)
	}

	// 3. Check for model recall
	var pkg models.ModelPackage
	if err := s.db.Where("package_id = ?", req.ModelPackageID).First(&pkg).Error; err != nil {
		return paper.VerdictDeny, fmt.Errorf("model package not found")
	}
	if pkg.State == "recalled" {
		return paper.VerdictDeny, fmt.Errorf("model %s has been recalled", req.ModelPackageID)
	}

	// 4. Find a valid endpoint with lease
	var endpoint models.InferenceEndpoint
	var epLease models.EndpointLease
	err := s.db.Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
		req.OrganizationID, req.ModelPackageID).First(&endpoint).Error
	if err != nil {
		return paper.VerdictDeny, fmt.Errorf("no active endpoint for model %s", req.ModelPackageID)
	}

	// Check for valid endpoint lease
	err = s.db.Where("endpoint_id = ? AND status = 'active' AND not_after > ?",
		endpoint.EndpointID, time.Now().Format(time.RFC3339)).
		Order("issued_at DESC").First(&epLease).Error
	if err != nil {
		return paper.VerdictDeny, fmt.Errorf("no valid endpoint lease for endpoint %s", endpoint.EndpointID)
	}

	// All checks passed
	return paper.VerdictAllow, nil
}

// RouteInference routes an inference request to a PIA endpoint.
type InferenceRequest struct {
	ExchangeID     string              `json:"exchange_id"`
	OrganizationID string              `json:"organization_id"`
	SessionID      string              `json:"session_id"`
	ModelPackageID string              `json:"model_package_id"`
	Model          string              `json:"model"`
	Messages       []map[string]string `json:"messages"`
	MaxTokens      int                 `json:"max_tokens,omitempty"`
	Temperature    float64             `json:"temperature,omitempty"`
}

// RouteInference finds a valid endpoint and forwards the request to PIA.
func (s *Service) RouteInference(ctx context.Context, req InferenceRequest) (*InferenceResponse, error) {
	s.mu.RLock()
	exchange, ok := s.exchanges[req.ExchangeID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("relay: exchange %s not found", req.ExchangeID)
	}
	if exchange.State != paper.ExchangeAuthorized && exchange.State != paper.ExchangeActive {
		return nil, fmt.Errorf("relay: exchange state is %s, not authorized", exchange.State)
	}

	// Update exchange state
	s.mu.Lock()
	exchange.State = paper.ExchangeActive
	s.mu.Unlock()

	// Find valid endpoint
	var endpoint models.InferenceEndpoint
	var epLease models.EndpointLease
	err := s.db.Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
		req.OrganizationID, req.ModelPackageID).First(&endpoint).Error
	if err != nil {
		return nil, fmt.Errorf("relay: no active endpoint: %w", err)
	}

	err = s.db.Where("endpoint_id = ? AND status = 'active' AND not_after > ?",
		endpoint.EndpointID, time.Now().Format(time.RFC3339)).
		Order("issued_at DESC").First(&epLease).Error
	if err != nil {
		return nil, fmt.Errorf("relay: no valid endpoint lease: %w", err)
	}

	exchange.EndpointID = endpoint.EndpointID

	// Forward to PIA via PAPER protocol (v2 §9.2, §38.1)
	// If PCCP_PIA_PAPER_ADDR is set, use PAPER transport
	// Otherwise fall back to HTTP (legacy/dev mode)
	var inferenceResp InferenceResponse

	paperClient := getPaperInferenceClient()
	if paperClient != nil {
		// Use PAPER transport — this is the v2-compliant path
		// Convert messages to interface{} format
		interfaceMsgs := make([]map[string]interface{}, len(req.Messages))
		for i, m := range req.Messages {
			interfaceMsgs[i] = make(map[string]interface{})
			interfaceMsgs[i]["role"] = m["role"]
			interfaceMsgs[i]["content"] = m["content"]
		}
		result, err := paperClient.SendInference(ctx, req.Model, interfaceMsgs, req.MaxTokens)
		if err != nil {
			return nil, fmt.Errorf("relay: PAPER inference failed: %w", err)
		}
		inferenceResp.ID = result.ID
		inferenceResp.Model = result.Model
		inferenceResp.Choices = result.Choices
		inferenceResp.Usage = result.Usage
	} else {
		// HTTP fallback (dev/legacy only — not v2 compliant)
		piaURL := os.Getenv("PCCP_PIA_URL")
		if piaURL == "" {
			piaURL = "http://localhost:9090"
		}
		piaReq := map[string]interface{}{
			"model":       req.Model,
			"messages":    req.Messages,
			"max_tokens":  req.MaxTokens,
			"temperature": req.Temperature,
			"exchange_id": req.ExchangeID,
		}
		bodyJSON, _ := json.Marshal(piaReq)
		httpReq, _ := http.NewRequestWithContext(ctx, "POST", piaURL+"/v1/chat/completions", bytes.NewReader(bodyJSON))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Endpoint-Lease", epLease.LeaseID)
		httpReq.Header.Set("X-Exchange-ID", req.ExchangeID)

		resp, err := s.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("relay: PIA request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("relay: PIA error (%d): %s", resp.StatusCode, string(body))
		}
		json.NewDecoder(resp.Body).Decode(&inferenceResp)
	}

	// Record the inference action
	s.provenance.RecordAction(provenance.RecordActionRequest{
		OrganizationID: req.OrganizationID,
		SessionID:      req.SessionID,
		ExchangeID:     req.ExchangeID,
		UserID:         exchange.UserID,
		HarnessID:      exchange.HarnessID,
		ModelPackageID: req.ModelPackageID,
		EndpointID:     endpoint.EndpointID,
		PolicyEpochID:  exchange.PolicyEpochID,
		LeaseID:        exchange.LeaseID,
		ActionType:     "ai.inference",
		VerdictResult:  string(paper.VerdictAllow),
	})

	// Update exchange with completion
	s.mu.Lock()
	exchange.State = paper.ExchangeCompleted
	s.mu.Unlock()

	return &inferenceResp, nil
}

// CloseExchange finalizes an exchange and issues an evidence receipt.
func (s *Service) CloseExchange(ctx context.Context, exchangeID string) (*models.EvidenceReceipt, error) {
	s.mu.RLock()
	exchange, ok := s.exchanges[exchangeID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("relay: exchange not found")
	}

	// Issue evidence receipt
	receipt, err := s.provenance.IssueEvidenceReceipt(provenance.IssueReceiptRequest{
		OrganizationID: exchange.OrganizationID,
		ExchangeID:     exchange.ID,
		SessionID:      exchange.SessionID,
		FinalState:     string(exchange.State),
		FirstEventSeq:  0,
		LastEventSeq:   uint64(len(exchange.EvidenceChain)),
		ChainRoot:      paper.GenerateID("chainroot"),
		PolicyEpochID:  exchange.PolicyEpochID,
		ModelPackageID: exchange.ModelPackageID,
		EndpointID:     exchange.EndpointID,
	})
	if err != nil {
		log.Printf("relay: warning: issue receipt failed: %v", err)
	}

	s.mu.Lock()
	exchange.State = paper.ExchangeCompleted
	delete(s.exchanges, exchangeID)
	s.mu.Unlock()

	return receipt, nil
}

// InferenceResponse mirrors the PIA/OpenAI completion response.
type InferenceResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []map[string]interface{} `json:"choices"`
	Usage   map[string]int           `json:"usage"`
}

// Ensure the type is used

func (s *Service) recordExchange(ex *Exchange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exchanges[ex.ID] = ex
}

// RelayID returns the relay identifier.
func (s *Service) RelayID() string {
	return s.relayID
}
