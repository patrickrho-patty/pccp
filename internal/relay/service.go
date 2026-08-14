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
	"strings"
	"os"
	"time"

	"github.com/patrickrho-patty/pccp/internal/catalog"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/paper"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"github.com/patrickrho-patty/pccp/internal/provenance"
	"github.com/patrickrho-patty/pccp/internal/realtime"
	"github.com/patrickrho-patty/pccp/internal/security"
	"github.com/patrickrho-patty/pccp/internal/workintel"
	"gorm.io/gorm"
)

// Service implements the PAPER Relay data plane.
// The Relay authenticates peers, validates capability leases, binds policy
// epochs, performs governance checks, routes to enrolled PIA, and emits
// evidence receipts.
type Service struct {
	db         *gorm.DB
	provenance *provenance.Service
	security   *security.Service
	workintel  *workintel.Service
	realtime   *realtime.Service
	identity   *identity.Service
	policy     *policy.Service
	catalog    *catalog.Service
	forwarder  inferenceForwarder
	cpURL      string
	relayID    string
	httpClient *http.Client

	// In-flight exchanges
	mu        sync.RWMutex
	exchanges map[string]*Exchange
}

// inferenceForwarder forwards a governed inference request to a PIA.
// Injected so the governed flow is testable without a live PIA/vLLM.
type inferenceForwarder func(ctx context.Context, req InferenceRequest, endpointLeaseID string) (*InferenceResponse, error)

// governResolution carries artifacts resolved during authorize so later
// stages (RouteInference) can reuse them instead of re-querying.
type governResolution struct {
	EndpointID string
	EpLeaseID  string
}

// SetForwarder overrides the PIA forwarder (for tests).
func (s *Service) SetForwarder(f inferenceForwarder) { s.forwarder = f }

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
	EndpointLeaseID string                `json:"endpoint_lease_id,omitempty"`
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
	identitySvc, err := identity.New(db)
	if err != nil {
		return nil, fmt.Errorf("relay: init identity: %w", err)
	}
	policySvc, err := policy.New(db)
	if err != nil {
		return nil, fmt.Errorf("relay: init policy: %w", err)
	}
	catalogSvc, err := catalog.New(db)
	if err != nil {
		return nil, fmt.Errorf("relay: init catalog: %w", err)
	}

	s := &Service{
		db:         db,
		provenance: provSvc,
		security:   security.New(db),
		workintel:  workintel.New(db),
		realtime:   realtime.New(),
		identity:   identitySvc,
		policy:     policySvc,
		catalog:    catalogSvc,
		cpURL:      cpURL,
		relayID:    relayID,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		exchanges:  make(map[string]*Exchange),
	}
	s.forwarder = s.defaultForwarder
	return s, nil
}

// Identity exposes the identity service (CA + revocations) so the
// PAPER listener can build its trust bundle and the binary can wire
// issuer keys at startup.
func (s *Service) Identity() *identity.Service { return s.identity }

// Policy exposes the policy service (epochs + capability leases).
func (s *Service) Policy() *policy.Service { return s.policy }

// Catalog exposes the model-catalog service.
func (s *Service) Catalog() *catalog.Service { return s.catalog }

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

	// Authorize: validate lease + policy epoch + model + endpoint (resolved once).
	var resolution governResolution
	verdict, err := s.authorize(ctx, req, &resolution)
	if err != nil {
		exchange.State = paper.ExchangeDenied
		exchange.Verdict = paper.VerdictDeny
		s.recordExchange(exchange)
		return exchange, paper.VerdictDeny, fmt.Errorf("relay: authorization failed: %w", err)
	}
	exchange.Verdict = verdict
	exchange.EndpointID = resolution.EndpointID
	exchange.EndpointLeaseID = resolution.EpLeaseID

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

// authorize performs the governance checks for an exchange and resolves the
// serving endpoint + lease (carried out via *res so RouteInference reuses).
func (s *Service) authorize(ctx context.Context, req OpenExchangeRequest, res *governResolution) (paper.VerdictResult, error) {
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

	res.EndpointID = endpoint.EndpointID
	res.EpLeaseID = epLease.LeaseID
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

	// Endpoint + lease were resolved once during authorize (OpenExchange).
	if exchange.EndpointID == "" {
		return nil, fmt.Errorf("relay: exchange %s has no resolved endpoint", req.ExchangeID)
	}

	// Forward to PIA via the injectable forwarder — PAPER transport when
	// PCCP_PIA_PAPER_ADDR is set, HTTP fallback otherwise (dev/legacy).
	inferenceResp, err := s.forwarder(ctx, req, exchange.EndpointLeaseID)
	if err != nil {
		return nil, fmt.Errorf("relay: inference forward failed: %w", err)
	}

	// Meter usage (§10.2 stage 13 / §29.13) — every governed request is accounted.
	s.recordUsage(exchange, req, inferenceResp)

	// Record the inference action
	s.provenance.RecordAction(provenance.RecordActionRequest{
		OrganizationID: req.OrganizationID,
		SessionID:      req.SessionID,
		ExchangeID:     req.ExchangeID,
		UserID:         exchange.UserID,
		HarnessID:      exchange.HarnessID,
		ModelPackageID: req.ModelPackageID,
		EndpointID:     exchange.EndpointID,
		PolicyEpochID:  exchange.PolicyEpochID,
		LeaseID:        exchange.LeaseID,
		ActionType:     "ai.inference",
		VerdictResult:  string(paper.VerdictAllow),
	})

	// Update exchange with completion
	s.mu.Lock()
	exchange.State = paper.ExchangeCompleted
	s.mu.Unlock()

	return inferenceResp, nil
}

// defaultForwarder is the production forwarder: PAPER transport when configured,
// HTTP fallback otherwise (dev/legacy only — not v2 compliant).
func (s *Service) defaultForwarder(ctx context.Context, req InferenceRequest, endpointLeaseID string) (*InferenceResponse, error) {
	var inferenceResp InferenceResponse

	paperClient := getPaperInferenceClient()
	if paperClient != nil {
		interfaceMsgs := make([]map[string]interface{}, len(req.Messages))
		for i, m := range req.Messages {
			interfaceMsgs[i] = map[string]interface{}{"role": m["role"], "content": m["content"]}
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
		piaURL := piaAPIBase()
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
		httpReq, err := s.preparePIARequest(ctx, piaURL, bodyJSON, endpointLeaseID, req.ExchangeID)
		if err != nil {
			return nil, fmt.Errorf("relay: PIA request build failed: %w", err)
		}

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
	return &inferenceResp, nil
}

// preparePIARequest builds the outbound PIA request with the relay's
// authentication. A PIA that requires a bearer key (e.g. an
// OpenAI-compatible endpoint fronting vLLM/SGLang, or the yolo-auto
// gateway) reads it from PCCP_PIA_API_KEY / YOLO_AUTO_API_KEY.
func (s *Service) preparePIARequest(ctx context.Context, url string, body []byte, endpointLeaseID, exchangeID string) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Endpoint-Lease", endpointLeaseID)
	httpReq.Header.Set("X-Exchange-ID", exchangeID)
	if key := piaAPIKey(); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	return httpReq, nil
}

// piaAPIKey resolves the PIA bearer credential. The YOLO_AUTO_* pair
// mimics a vLLM/SGLang deployment behind an OpenAI-compatible gateway
// for live-path validation.
func piaAPIKey() string {
	if k := os.Getenv("PCCP_PIA_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("YOLO_AUTO_API_KEY")
}

// piaAPIBase resolves the PIA base URL (OpenAI-compatible), normalized
// to the HOST ROOT (any trailing "/v1" or "/" is stripped — callers
// append "/v1/chat/completions"). Gateways like yolo-auto hand out
// ".../v1" as the base; naively concatenating produced /v1/v1/.
func piaAPIBase() string {
	u := os.Getenv("PCCP_PIA_URL")
	if u == "" {
		u = os.Getenv("YOLO_AUTO_ENDPOINT")
	}
	for strings.HasSuffix(u, "/") {
		u = u[:len(u)-1]
	}
	if strings.HasSuffix(u, "/v1") {
		u = u[:len(u)-3]
	}
	return u
}

// CloseExchange finalizes an exchange and issues an evidence receipt.
func (s *Service) CloseExchange(ctx context.Context, exchangeID string) (*models.EvidenceReceipt, error) {
	s.mu.RLock()
	exchange, ok := s.exchanges[exchangeID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("relay: exchange not found")
	}

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

	s.recordExchangeProvenance(exchange)

	s.mu.Lock()
	exchange.State = paper.ExchangeCompleted
	delete(s.exchanges, exchangeID)
	s.mu.Unlock()

	return receipt, nil
}

// recordExchangeProvenance records a ChangeSet + ProvenanceSpan for an exchange,
// establishing AI_GENERATED attribution lineage (§19).
func (s *Service) recordExchangeProvenance(ex *Exchange) {
	cs, csErr := s.provenance.CreateChangeSet(provenance.CreateChangeSetRequest{
		OrganizationID:  ex.OrganizationID,
		SessionID:       ex.SessionID,
		ExchangeID:      ex.ID,
		UserID:          ex.UserID,
		HarnessID:       ex.HarnessID,
		ModelPackageID:  ex.ModelPackageID,
		EndpointID:      ex.EndpointID,
		AttributionState: "AI_GENERATED",
		Confidence:      1.0,
	})
	if csErr != nil {
		log.Printf("relay: warning: create changeset failed: %v", csErr)
		return
	}
	if _, spanErr := s.provenance.CreateProvenanceSpan(provenance.CreateSpanRequest{
		OrganizationID:  ex.OrganizationID,
		ChangeSetID:     cs.ID,
		SessionID:       ex.SessionID,
		UserID:          ex.UserID,
		HarnessID:       ex.HarnessID,
		ModelPackageID:  ex.ModelPackageID,
		EndpointID:      ex.EndpointID,
		AttributionState: "AI_GENERATED",
		Confidence:      1.0,
	}); spanErr != nil {
		log.Printf("relay: warning: create provenance span failed: %v", spanErr)
	}
}

// GovernRequest is a live-path inference request resolved from the authenticated peer.
type GovernRequest struct {
	HarnessID string
	SessionID string
	Model     string                 // model_id (resolved to a ModelPackage)
	Messages  []map[string]string
	MaxTokens int
}

// GovernInference is the live-path entry point: it resolves governance context
// for an authenticated harness (org, active lease, active policy epoch, model
// package) from the control-plane DB, then runs the full governed flow
// (authorize → forward → meter → evidence). It fails closed if any required
// governance artifact is missing or invalid. (MISSING_ITEMS Domain 1–2 P0 / Harness A)
func (s *Service) GovernInference(ctx context.Context, req GovernRequest) (*InferenceResponse, *models.EvidenceReceipt, error) {
	// 1. Resolve org from the enrolled harness.
	var harness models.Harness
	if err := s.db.Where("harness_id = ?", req.HarnessID).First(&harness).Error; err != nil {
		return nil, nil, fmt.Errorf("relay: harness %s not enrolled: %w", req.HarnessID, err)
	}
	orgID := harness.OrganizationID

	// Defense in depth: reject harnesses not in good standing even if a stale
	// lease exists. (Connect-time gate is Service.AuthorizePeer.)
	if harness.Status == "revoked" || harness.Status == "quarantined" {
		return nil, nil, fmt.Errorf("relay: harness %s is %s", req.HarnessID, harness.Status)
	}

	// 2. Resolve the active capability lease for this harness (fail-closed).
	var lease models.CapabilityLease
	if err := s.db.Where("subject_peer_id = ? AND status = 'active'", req.HarnessID).
		Order("not_after DESC").First(&lease).Error; err != nil {
		return nil, nil, fmt.Errorf("relay: no active capability lease for harness %s: %w", req.HarnessID, err)
	}
	notAfter, _ := time.Parse(time.RFC3339, lease.NotAfter)
	if !notAfter.IsZero() && time.Now().After(notAfter) {
		return nil, nil, fmt.Errorf("relay: capability lease expired for harness %s", req.HarnessID)
	}

	// 3. Resolve model_id → published ModelPackage (reject recalled).
	var pkg models.ModelPackage
	if err := s.db.Where("model_id = ?", req.Model).First(&pkg).Error; err != nil {
		return nil, nil, fmt.Errorf("relay: model %s not in registry: %w", req.Model, err)
	}
	if pkg.State == "recalled" {
		return nil, nil, fmt.Errorf("relay: model %s has been recalled", req.Model)
	}

	// 4. Authorize → forward → meter via the governed exchange flow.
	ex, verdict, err := s.OpenExchange(ctx, OpenExchangeRequest{
		OrganizationID: orgID,
		SessionID:      req.SessionID,
		UserID:         lease.UserID,
		HarnessID:      req.HarnessID,
		LeaseID:        lease.LeaseID,
		PolicyEpochID:  lease.PolicyEpochID,
		ModelPackageID: pkg.PackageID,
	})
	if err != nil || verdict != paper.VerdictAllow {
		return nil, nil, fmt.Errorf("relay: exchange denied for harness %s (verdict=%s): %w", req.HarnessID, verdict, err)
	}

	// Inline DLP/PII/injection scan (§16) — populates Security findings from real
	// traffic. Blocks on DENY (critical/high); logs REQUIRE_REVIEW (medium).
	combinedText := ""
	for _, m := range req.Messages {
		combinedText += m["content"] + "\n"
	}
	if secResult := s.security.CheckContext(orgID, combinedText); len(secResult.Findings) > 0 {
		for _, f := range secResult.Findings {
			s.security.RecordFinding(orgID, req.SessionID, ex.ID, f)
			s.realtime.NotifySecurityFinding(orgID, f.Severity, f.TitleKo)
		}
		if secResult.Verdict == "DENY" {
			s.CloseExchange(ctx, ex.ID)
			return nil, nil, fmt.Errorf("relay: security verdict DENY — request blocked (%d findings)", len(secResult.Findings))
		}
	}

	resp, err := s.RouteInference(ctx, InferenceRequest{
		ExchangeID:     ex.ID,
		OrganizationID: orgID,
		SessionID:      req.SessionID,
		ModelPackageID: pkg.PackageID,
		Model:          req.Model,
		Messages:       req.Messages,
		MaxTokens:      req.MaxTokens,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("relay: governed inference failed: %w", err)
	}

	// 5. Evidence receipt.
	receipt, err := s.CloseExchange(ctx, ex.ID)
	if err != nil {
		log.Printf("relay: warning: close exchange %s: %v", ex.ID, err)
	}
	return resp, receipt, nil
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

// recordUsage meters token usage for a completed governed inference (§10.2 stage 13).
func (s *Service) recordUsage(ex *Exchange, req InferenceRequest, resp *InferenceResponse) {
	if resp == nil || resp.Usage == nil || s.workintel == nil {
		return
	}
	tokensIn := int64(resp.Usage["prompt_tokens"])
	tokensOut := int64(resp.Usage["completion_tokens"])
	if tokensIn > 0 {
		s.workintel.RecordUsage(ex.OrganizationID, ex.UserID, ex.HarnessID, ex.SessionID, req.ModelPackageID, ex.EndpointID, "tokens_in", tokensIn, "tokens")
	}
	if tokensOut > 0 {
		s.workintel.RecordUsage(ex.OrganizationID, ex.UserID, ex.HarnessID, ex.SessionID, req.ModelPackageID, ex.EndpointID, "tokens_out", tokensOut, "tokens")
	}
}

// AuthorizePeer is the connect-time gate: an enrolled harness in good standing
// is allowed; unknown/revoked/quarantined harnesses are rejected. This ties
// fleet revocation/quarantine (web/09-fleet) to the live PAPER path — a revoked
// harness can no longer authenticate. Full PPC signature verification is a
// follow-up once the harness presents real signed credentials (Harness A).
func (s *Service) AuthorizePeer(harnessID string) (string, error) {
	var harness models.Harness
	if err := s.db.Where("harness_id = ?", harnessID).First(&harness).Error; err != nil {
		return "", fmt.Errorf("relay: unknown harness %s: %w", harnessID, err)
	}
	switch harness.Status {
	case "revoked":
		return "", fmt.Errorf("relay: harness %s is revoked", harnessID)
	case "quarantined":
		return "", fmt.Errorf("relay: harness %s is quarantined", harnessID)
	}
	return harness.OrganizationID, nil
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
