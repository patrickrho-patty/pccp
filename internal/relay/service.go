package relay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/catalog"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/events"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/metering"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"github.com/patrickrho-patty/pccp/internal/provenance"
	"github.com/patrickrho-patty/pccp/internal/realtime"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
	"github.com/patrickrho-patty/pccp/internal/security"
	"github.com/patrickrho-patty/pccp/internal/telemetry"
	"github.com/patrickrho-patty/pccp/internal/tools"
	"gorm.io/gorm"
)

// Service implements the DARI Relay data plane.
// The Relay authenticates peers, validates capability leases, binds policy
// epochs, performs governance checks, routes to enrolled PIA, and emits
// evidence receipts.
type Service struct {
	db         *gorm.DB
	provenance *provenance.Service
	security   *security.Service
	metering   *telemetry.Service
	realtime   *realtime.Service
	identity   *identity.Service
	policy     *policy.Service
	catalog    *catalog.Service
	tools      *tools.Service
	forwarder  inferenceForwarder
	cpURL      string
	relayID    string
	httpClient *http.Client

	// In-flight exchanges
	mu        sync.RWMutex
	exchanges map[string]*Exchange
	// listeners receive revocation propagation (Task 6).
	listeners []*DARIListener
	// decisionLog is a bounded ring of recently issued F.6 decisions
	// (exchanges are removed at close; the log preserves the signed
	// decisions for operational inspection).
	decisionLog []*dari.DecisionEnvelope
	// hotState caches governance resolution per harness (Task 15).
	hotState *HotStateCache
	// grantRevocations backs VerifySessionGrant's chain checks.
	grantRevocations revokedGrants
	// exchangeGate bounds governed-exchange concurrency (Task 15).
	exchangeGate *ConcurrencyGate
	// fairSched queues overload requests per-account (org) with
	// weighted priority (§10C.7) instead of shedding them.
	fairSched *scheduler.Service
	// spine is the durable governed-event record (PRD §39).
	spine *events.Service
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

// ensureDevBootstrapEnrollmentUser completes the explicitly enabled local
// bootstrap identity chain. Production leaves unknown actors unresolved so
// EnrollHarness continues to fail closed. FirstOrCreate never reactivates an
// existing suspended or offboarded user.
func (s *Service) ensureDevBootstrapEnrollmentUser(orgID, userID string) error {
	if !devBootstrap() {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		org := &models.Organization{
			Base: models.Base{ID: orgID}, Name: orgID, Slug: orgID,
			Profile: "enterprise", Status: "active", MaxUserSeats: 50, MaxHarnessSeats: 100,
		}
		if err := tx.Where("id = ?", orgID).FirstOrCreate(org).Error; err != nil {
			return err
		}
		user := &models.User{
			AuditBase: models.AuditBase{
				Base:           models.Base{ID: userID},
				OrganizationID: orgID,
				Classification: "internal",
			},
			Email:      userID + "@bootstrap.invalid",
			Name:       userID,
			Status:     models.UserStatusActive,
			AuthMethod: "local",
			Locale:     "ko-KR",
			Timezone:   "Asia/Seoul",
		}
		return tx.Where("organization_id = ? AND id = ?", orgID, userID).FirstOrCreate(user).Error
	})
}

// Exchange tracks a governed exchange in progress.
type Exchange struct {
	ID              string             `json:"id"`
	SessionID       string             `json:"session_id"`
	OrganizationID  string             `json:"organization_id"`
	UserID          string             `json:"user_id"`
	HarnessID       string             `json:"harness_id"`
	LeaseID         string             `json:"lease_id"`
	PolicyEpochID   string             `json:"policy_epoch_id"`
	State           dari.ExchangeState `json:"state"`
	ModelPackageID  string             `json:"model_package_id"`
	EndpointID      string             `json:"endpoint_id"`
	EndpointLeaseID string             `json:"endpoint_lease_id,omitempty"`
	Verdict         dari.VerdictResult `json:"verdict"`
	OpenedAt        time.Time          `json:"opened_at"`
	EvidenceChain   []string           `json:"evidence_chain,omitempty"`
	// Decision is the F.6 signed Authorization Decision issued for
	// this exchange (Task 9): immutable, bound to the exchange, the
	// leaf grant, and the policy checkpoint digests.
	Decision *dari.DecisionEnvelope `json:"-"`
	// GrantDigest is the signed-object digest of the governing grant.
	GrantDigest dari.Digest `json:"grant_digest,omitempty"`
	// Trace is the §10.2 stage trace recorded on the live path.
	Trace *PipelineTrace `json:"-"`
}

// RecordStage appends one stage outcome to the exchange's trace.
func (ex *Exchange) RecordStage(stage string, ok bool, note string) {
	if ex == nil {
		return
	}
	if ex.Trace == nil {
		ex.Trace = &PipelineTrace{}
	}
	ex.Trace.Record(stage, ok, note)
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
		metering:   telemetry.New(db),
		realtime:   realtime.New(db),
		identity:   identitySvc,
		policy:     policySvc,
		catalog:    catalogSvc,
		tools:      tools.New(db),
		cpURL:      cpURL,
		relayID:    relayID,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		exchanges:  make(map[string]*Exchange),
	}
	s.forwarder = s.defaultForwarder
	if err := s.realtime.SetSharedBusSecret(os.Getenv("PCCP_CP_TOKEN")); err != nil {
		return nil, fmt.Errorf("relay: configure realtime bus: %w", err)
	}
	s.hotState = NewHotStateCache(30 * time.Second)
	s.grantRevocations.m = map[dari.Digest]bool{}
	s.exchangeGate = NewConcurrencyGate(128)
	s.fairSched = scheduler.New()
	if spine, serr := events.New(db); serr == nil {
		s.spine = spine
	} else {
		log.Printf("relay: durable event spine unavailable: %v", serr)
	}
	return s, nil
}

// OnModelPublished is invoked by the catalog/publish paths after a
// model package becomes published: hot-state entries may reference the
// old catalog, and connected sessions must receive the delta push.
func (s *Service) OnModelPublished(packageID ...string) {
	if s.hotState != nil {
		s.hotState.InvalidateAll()
	}
	s.mu.RLock()
	listeners := append([]*DARIListener(nil), s.listeners...)
	s.mu.RUnlock()
	for _, pl := range listeners {
		pl.BroadcastCatalogDelta()
	}
}

// HotState exposes the governance hot-state cache (ops/telemetry).
func (s *Service) HotState() *HotStateCache { return s.hotState }

// ExchangeGate exposes the backpressure gate (ops/telemetry).
func (s *Service) ExchangeGate() *ConcurrencyGate { return s.exchangeGate }

// Identity exposes the identity service (CA + revocations) so the
// DARI listener can build its trust bundle and the binary can wire
// issuer keys at startup.
func (s *Service) Identity() *identity.Service { return s.identity }

// Policy exposes the policy service (epochs + capability leases).
func (s *Service) Policy() *policy.Service { return s.policy }

// Provenance exposes the provenance service (receipt signer identity).
func (s *Service) Provenance() *provenance.Service { return s.provenance }

// Catalog exposes the model-catalog service.
func (s *Service) Catalog() *catalog.Service { return s.catalog }

// SetAlertKeyProvider wires the relay's finding-delivery service. The relay
// owns a distinct security.Service from the API process.
func (s *Service) SetAlertKeyProvider(provider keymgmt.KeyProvider) {
	s.security.SetAlertKeyProvider(provider)
}

func (s *Service) StartAlertDeliveryWorker(ctx context.Context) {
	s.security.StartAlertDeliveryWorker(ctx)
}

func (s *Service) SetAlertHTTPClient(client security.HTTPDoer) {
	s.security.SetAlertHTTPClient(client)
}

func (s *Service) ProcessAlertDeliveries(ctx context.Context, limit int) (int, error) {
	return s.security.ProcessAlertDeliveries(ctx, limit)
}

// OpenExchange starts a governed exchange for an AI inference request.
type OpenExchangeRequest struct {
	OrganizationID string `json:"organization_id"`
	SessionID      string `json:"session_id"`
	UserID         string `json:"user_id"`
	HarnessID      string `json:"harness_id"`
	LeaseID        string `json:"lease_id"`
	PolicyEpochID  string `json:"policy_epoch_id"`
	ModelPackageID string `json:"model_package_id"`
	ProjectID      string `json:"project_id,omitempty"`
	RepositoryID   string `json:"repository_id,omitempty"`
	Branch         string `json:"branch,omitempty"`
	Purpose        string `json:"purpose,omitempty"`
}

// OpenExchange creates and authorizes a new governed exchange.
func (s *Service) OpenExchange(ctx context.Context, req OpenExchangeRequest) (*Exchange, dari.VerdictResult, error) {
	exchangeID := dari.GenerateID("exch")
	exchange := &Exchange{
		ID:             exchangeID,
		SessionID:      req.SessionID,
		OrganizationID: req.OrganizationID,
		UserID:         req.UserID,
		HarnessID:      req.HarnessID,
		LeaseID:        req.LeaseID,
		PolicyEpochID:  req.PolicyEpochID,
		State:          dari.ExchangeCreated,
		ModelPackageID: req.ModelPackageID,
		OpenedAt:       time.Now(),
	}

	// Authorize: validate lease + policy epoch + model + endpoint (resolved once).
	var resolution governResolution
	verdict, err := s.authorize(ctx, req, &resolution)
	if err != nil {
		exchange.State = dari.ExchangeDenied
		exchange.Verdict = dari.VerdictDeny
		s.issueDecision(exchange, req, dari.DecisionDeny, "authorization_failed")
		s.recordExchange(exchange)
		return exchange, dari.VerdictDeny, fmt.Errorf("relay: authorization failed: %w", err)
	}
	exchange.Verdict = verdict
	if verdict == dari.VerdictAllow {
		// F.6: an unqualified ALLOW for a plain authorized exchange.
		s.issueDecision(exchange, req, dari.DecisionAllow, "")
	}
	exchange.EndpointID = resolution.EndpointID
	exchange.EndpointLeaseID = resolution.EpLeaseID

	if verdict == dari.VerdictDeny {
		exchange.State = dari.ExchangeDenied
		s.recordExchange(exchange)
		return exchange, verdict, fmt.Errorf("relay: exchange denied")
	}

	exchange.State = dari.ExchangeAuthorized
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
func (s *Service) authorize(ctx context.Context, req OpenExchangeRequest, res *governResolution) (dari.VerdictResult, error) {
	if req.OrganizationID == "" || req.SessionID == "" || req.UserID == "" || req.HarnessID == "" ||
		req.LeaseID == "" || req.PolicyEpochID == "" || req.ModelPackageID == "" {
		return dari.VerdictDeny, fmt.Errorf("complete exchange authority context is required")
	}

	// 1. Validate the capability lease
	var lease models.CapabilityLease
	if err := s.db.Where("lease_id = ? AND organization_id = ?", req.LeaseID, req.OrganizationID).First(&lease).Error; err != nil {
		return dari.VerdictDeny, fmt.Errorf("capability lease not found")
	}
	if lease.Status != "active" {
		return dari.VerdictDeny, fmt.Errorf("lease status is %s", lease.Status)
	}
	if lease.SubjectPeerID != req.HarnessID || lease.SessionID != req.SessionID || lease.UserID != req.UserID || lease.PolicyEpochID != req.PolicyEpochID {
		return dari.VerdictDeny, fmt.Errorf("capability lease authority binding mismatch")
	}
	now := time.Now()
	notBefore, beforeErr := time.Parse(time.RFC3339, lease.NotBefore)
	notAfter, afterErr := time.Parse(time.RFC3339, lease.NotAfter)
	if beforeErr != nil || afterErr != nil || now.Before(notBefore) || !now.Before(notAfter) {
		return dari.VerdictDeny, fmt.Errorf("lease expired")
	}
	if !capabilityLeaseAllowsModel(&lease, req.ModelPackageID) {
		return dari.VerdictDeny, fmt.Errorf("capability lease does not allow model %s", req.ModelPackageID)
	}

	var harness models.Harness
	if err := s.db.Where("organization_id = ? AND harness_id = ?", req.OrganizationID, req.HarnessID).First(&harness).Error; err != nil {
		return dari.VerdictDeny, fmt.Errorf("harness is not enrolled for organization")
	}
	if !models.HarnessStatusPermitted(harness.Status) {
		return dari.VerdictDeny, fmt.Errorf("harness status is %s", harness.Status)
	}
	if restriction, err := models.HarnessAdmissionRestriction(s.db, req.OrganizationID, req.HarnessID); err != nil {
		return dari.VerdictDeny, fmt.Errorf("fleet desired state unavailable: %w", err)
	} else if restriction != nil {
		return dari.VerdictDeny, fmt.Errorf("harness admission blocked by %s", restriction.Action)
	}
	var session models.Session
	if err := s.db.Where("organization_id = ? AND session_id = ? AND harness_id = ? AND user_id = ?",
		req.OrganizationID, req.SessionID, req.HarnessID, req.UserID).First(&session).Error; err != nil {
		return dari.VerdictDeny, fmt.Errorf("session authority binding mismatch")
	}
	if !models.SessionIsLive(session.Status) {
		return dari.VerdictDeny, fmt.Errorf("session status is %s", session.Status)
	}
	if locked, err := models.ActiveSecurityLockdown(s.db, req.OrganizationID, session.ProjectID); err != nil {
		return dari.VerdictDeny, fmt.Errorf("security lockdown state unavailable: %w", err)
	} else if locked {
		return dari.VerdictDeny, fmt.Errorf("security lockdown is active")
	}
	var user models.User
	if err := s.db.Where("organization_id = ? AND id = ? AND status = ?", req.OrganizationID, req.UserID, models.UserStatusActive).First(&user).Error; err != nil {
		return dari.VerdictDeny, fmt.Errorf("user is not active for organization")
	}

	// 2. Validate model is allowed under policy epoch
	var epoch models.PolicyEpoch
	if err := s.db.Where("organization_id = ? AND epoch_id = ?", req.OrganizationID, req.PolicyEpochID).First(&epoch).Error; err != nil {
		return dari.VerdictDeny, fmt.Errorf("policy epoch not found")
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
		return dari.VerdictDeny, fmt.Errorf("model %s not allowed under policy epoch %s", req.ModelPackageID, req.PolicyEpochID)
	}

	// 3. Check for model recall
	var pkg models.ModelPackage
	if err := s.db.Where("package_id = ?", req.ModelPackageID).First(&pkg).Error; err != nil {
		return dari.VerdictDeny, fmt.Errorf("model package not found")
	}
	if pkg.State == "recalled" {
		return dari.VerdictDeny, fmt.Errorf("model %s has been recalled", req.ModelPackageID)
	}

	// 4. Find a valid endpoint with lease
	var endpoint models.InferenceEndpoint
	var epLease models.EndpointLease
	err := s.db.Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
		req.OrganizationID, req.ModelPackageID).First(&endpoint).Error
	if err != nil {
		return dari.VerdictDeny, fmt.Errorf("no active endpoint for model %s", req.ModelPackageID)
	}

	// Check for valid endpoint lease
	err = s.db.Where("organization_id = ? AND endpoint_id = ? AND model_package_id = ? AND status = 'active' AND not_after > ?",
		req.OrganizationID, endpoint.EndpointID, req.ModelPackageID, now.Format(time.RFC3339)).
		Order("issued_at DESC").First(&epLease).Error
	if err != nil {
		return dari.VerdictDeny, fmt.Errorf("no valid endpoint lease for endpoint %s", endpoint.EndpointID)
	}

	res.EndpointID = endpoint.EndpointID
	res.EpLeaseID = epLease.LeaseID
	return dari.VerdictAllow, nil
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
	// Program carries optional WS3 agent-program scheduling metadata
	// (opaque, bounded identifiers only — the relay validates and signs
	// it; the harness never self-asserts priority through it).
	Program *scheduler.ProgramMeta `json:"program,omitempty"`
}

// RouteInference finds a valid endpoint and forwards the request to PIA.
func (s *Service) RouteInference(ctx context.Context, req InferenceRequest) (*InferenceResponse, error) {
	s.mu.RLock()
	exchange, ok := s.exchanges[req.ExchangeID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("relay: exchange %s not found", req.ExchangeID)
	}
	if exchange.State != dari.ExchangeAuthorized && exchange.State != dari.ExchangeActive {
		return nil, fmt.Errorf("relay: exchange state is %s, not authorized", exchange.State)
	}
	if err := validateInferenceBinding(exchange, req); err != nil {
		return nil, err
	}

	// Update exchange state
	s.mu.Lock()
	exchange.State = dari.ExchangeActive
	s.mu.Unlock()

	// Endpoint + lease were resolved once during authorize (OpenExchange).
	if exchange.EndpointID == "" {
		return nil, fmt.Errorf("relay: exchange %s has no resolved endpoint", req.ExchangeID)
	}

	// Forward to PIA via the injectable forwarder — DARI transport when
	// PCCP_PIA_DARI_ADDR is set, HTTP fallback otherwise (dev/legacy).
	return s.routeViaForwarder(ctx, exchange, req, func(ictx context.Context, ireq InferenceRequest) (*InferenceResponse, error) {
		return s.forwarder(ictx, ireq, exchange.EndpointLeaseID)
	})
}

// RouteInferenceStream is RouteInference with token streaming: every
// PIA delta is handed to onDelta (F1). The exchange bookkeeping,
// metering, and action recording are identical.
func (s *Service) RouteInferenceStream(ctx context.Context, req InferenceRequest, onDelta DeltaSender) (*InferenceResponse, error) {
	s.mu.RLock()
	exchange, ok := s.exchanges[req.ExchangeID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("relay: exchange %s not found", req.ExchangeID)
	}
	if exchange.State != dari.ExchangeAuthorized && exchange.State != dari.ExchangeActive {
		return nil, fmt.Errorf("relay: exchange state is %s, not authorized", exchange.State)
	}
	if err := validateInferenceBinding(exchange, req); err != nil {
		return nil, err
	}

	s.mu.Lock()
	exchange.State = dari.ExchangeActive
	s.mu.Unlock()
	if exchange.EndpointID == "" {
		return nil, fmt.Errorf("relay: exchange %s has no resolved endpoint", req.ExchangeID)
	}

	return s.routeViaForwarder(ctx, exchange, req, func(ictx context.Context, ireq InferenceRequest) (*InferenceResponse, error) {
		return s.streamForwarder(ictx, ireq, exchange.EndpointLeaseID, onDelta)
	})
}

func validateInferenceBinding(exchange *Exchange, req InferenceRequest) error {
	if exchange.OrganizationID != req.OrganizationID {
		return fmt.Errorf("relay: inference organization does not match authorized exchange")
	}
	if exchange.SessionID != req.SessionID {
		return fmt.Errorf("relay: inference session does not match authorized exchange")
	}
	if exchange.ModelPackageID != req.ModelPackageID {
		return fmt.Errorf("relay: inference model package does not match authorized exchange")
	}
	return nil
}

// routeViaForwarder runs the post-forward bookkeeping (meter, action
// record, completion) shared by the streaming and buffered paths.
func (s *Service) routeViaForwarder(ctx context.Context, exchange *Exchange, req InferenceRequest, fwd func(context.Context, InferenceRequest) (*InferenceResponse, error)) (*InferenceResponse, error) {
	inferenceResp, err := fwd(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("relay: inference forward failed: %w", err)
	}

	// Meter usage (§10.2 stage 13 / §29.13) — every governed request is accounted.
	if err := s.recordUsageContext(ctx, exchange, inferenceResp); err != nil {
		return nil, fmt.Errorf("relay: record metering: %w", err)
	}

	// Record the inference action
	s.provenance.RecordAction(provenance.RecordActionRequest{
		OrganizationID: exchange.OrganizationID,
		SessionID:      exchange.SessionID,
		ExchangeID:     exchange.ID,
		UserID:         exchange.UserID,
		HarnessID:      exchange.HarnessID,
		ModelPackageID: exchange.ModelPackageID,
		EndpointID:     exchange.EndpointID,
		PolicyEpochID:  exchange.PolicyEpochID,
		LeaseID:        exchange.LeaseID,
		ActionType:     "ai.inference",
		VerdictResult:  string(dari.VerdictAllow),
	})

	// Update exchange with completion
	s.mu.Lock()
	exchange.State = dari.ExchangeCompleted
	s.mu.Unlock()

	return inferenceResp, nil
}

// schedGatewayBase resolves the scheduler's unified-gateway base URL. When
// set, the governed path routes THROUGH the scheduler (S2): the relay stays
// the governance gate, the scheduler owns admission/queue/dispatch.
func schedGatewayBase() string {
	return strings.TrimRight(os.Getenv("PCCP_SCHED_GATEWAY_ADDR"), "/")
}

// defaultForwarder is the production forwarder: the S2 scheduler gateway
// when configured (governance → queue → late-bound dispatch to PIA), DARI
// transport to PIA otherwise, HTTP fallback last (dev/legacy only).
func (s *Service) defaultForwarder(ctx context.Context, req InferenceRequest, endpointLeaseID string) (*InferenceResponse, error) {
	var inferenceResp InferenceResponse

	// S2: the scheduler gateway is the preferred next hop. The relay
	// remains the only governance gate; admission/queue/dispatch live
	// in the scheduler (fail-closed — scheduler down = request rejected,
	// never bypassed).
	if schedURL := schedGatewayBase(); schedURL != "" {
		piaReq := map[string]interface{}{
			"model":       req.Model,
			"messages":    req.Messages,
			"max_tokens":  req.MaxTokens,
			"temperature": req.Temperature,
			"exchange_id": req.ExchangeID,
		}
		bodyJSON, _ := json.Marshal(piaReq)
		httpReq, err := http.NewRequestWithContext(ctx, "POST", schedURL+"/v1/chat/completions", bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, fmt.Errorf("relay: scheduler request build failed: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Tenant-ID", req.OrganizationID)
		httpReq.Header.Set("X-Exchange-ID", req.ExchangeID)
		envUserID := ""
		s.mu.RLock()
		if ex, ok := s.exchanges[req.ExchangeID]; ok {
			envUserID = ex.UserID
		}
		s.mu.RUnlock()
		if env, err := s.signTrafficEnvelope(req.OrganizationID, envUserID, req.ExchangeID, req.Program); err == nil {
			raw, _ := json.Marshal(env)
			httpReq.Header.Set("X-Traffic-Envelope", string(raw))
		}
		resp, err := s.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("relay: scheduler gateway failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("relay: scheduler gateway error (%d): %s", resp.StatusCode, string(body))
		}
		json.NewDecoder(resp.Body).Decode(&inferenceResp)
		return &inferenceResp, nil
	}

	dariClient := getDARIInferenceClient()
	if dariClient != nil {
		interfaceMsgs := make([]map[string]interface{}, len(req.Messages))
		for i, m := range req.Messages {
			interfaceMsgs[i] = map[string]interface{}{"role": m["role"], "content": m["content"]}
		}
		result, err := dariClient.SendInference(ctx, req.Model, interfaceMsgs, req.MaxTokens)
		if err != nil {
			return nil, fmt.Errorf("relay: DARI inference failed: %w", err)
		}
		inferenceResp.ID = result.ID
		inferenceResp.Model = result.Model
		inferenceResp.Choices = result.Choices
		inferenceResp.Usage = result.Usage
	} else {
		piaURL := piaAPIBase()
		if piaURL == "" {
			// No PIA configured: fail closed. There is NO mock
			// inference fallback — a governed exchange either reaches
			// a real inference endpoint or errors (T15).
			return nil, fmt.Errorf("relay: no PIA endpoint configured (PCCP_PIA_URL / YOLO_AUTO_ENDPOINT) — refusing to fabricate inference")
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

	// F.9 on the live path: the receipt's chain root is the LINEAR
	// CHAIN ROOT over the exchange's actual evidence events (an empty
	// chain yields the deterministic chain-start root bound to the
	// exchange identity) — never a fabricated random identifier
	// (Task 10 Step 3 audit finding).
	exchange.RecordStage(StageMeter, true, "")
	exchange.RecordStage(StageEvidence, true, exchange.ID)
	// The durable event spine (PRD §39): every closed governed
	// exchange emits its ordered event record — telemetry and audit
	// reconstruction consume this, not in-memory rings.
	s.emitSpine(exchange, "exchange_closed", map[string]any{
		"state": string(exchange.State), "evidence_events": len(exchange.EvidenceChain),
	})
	if exchange.Trace != nil {
		for _, st := range exchange.Trace.Stages() {
			s.emitSpine(exchange, "pipeline_stage", map[string]any{
				"stage": st.Stage, "ok": st.OK, "note": st.Detail,
			})
		}
	}
	root := s.evidenceChainRoot(exchange)
	receipt, err := s.provenance.IssueEvidenceReceipt(provenance.IssueReceiptRequest{
		OrganizationID: exchange.OrganizationID,
		ExchangeID:     exchange.ID,
		SessionID:      exchange.SessionID,
		FinalState:     string(exchange.State),
		FirstEventSeq:  0,
		LastEventSeq:   uint64(len(exchange.EvidenceChain)),
		ChainRoot:      hex.EncodeToString(root[:]),
		PolicyEpochID:  exchange.PolicyEpochID,
		ModelPackageID: exchange.ModelPackageID,
		EndpointID:     exchange.EndpointID,
	})
	if err != nil {
		// Fail closed: an exchange whose receipt cannot be issued must
		// not silently complete (F.8 receipt truthfulness).
		log.Printf("relay: issue receipt failed for %s: %v", exchangeID, err)
		s.mu.Lock()
		exchange.State = dari.ExchangeFailed
		delete(s.exchanges, exchangeID)
		s.mu.Unlock()
		return nil, fmt.Errorf("relay: evidence receipt issuance failed: %w", err)
	}

	s.recordExchangeProvenance(exchange)

	s.mu.Lock()
	exchange.State = dari.ExchangeCompleted
	delete(s.exchanges, exchangeID)
	s.mu.Unlock()

	return receipt, nil
}

// evidenceChainRoot computes the exchange's F.9 linear chain root:
// R_0 = EvidenceChainStart(exchangeDigest) and one domain-hashed step
// per recorded evidence event (sequence i+1, canonical bytes = the
// recorded reference). An empty chain still yields the deterministic
// start root bound to the exchange identity — never a random ID.
func (s *Service) evidenceChainRoot(ex *Exchange) dari.Digest {
	h := sha256.New()
	h.Write([]byte("DARI-EXCHANGE-IDENTITY-v1\x00"))
	for _, part := range []string{ex.ID, ex.SessionID, ex.OrganizationID, ex.HarnessID, ex.PolicyEpochID, ex.ModelPackageID, ex.EndpointID} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	var exDigest dari.Digest
	copy(exDigest[:], h.Sum(nil))

	events := make([]dari.EventCommitment, 0, len(ex.EvidenceChain))
	for i, ref := range ex.EvidenceChain {
		events = append(events, dari.EventCommitment{
			Sequence:  uint64(i + 1),
			Type:      dari.ObjTypeGovernedExchange, // opaque recorded-ref class
			Canonical: []byte(ref),
		})
	}
	return dari.LinearChainRoot(exDigest, events)
}

// recordExchangeProvenance records a ChangeSet + ProvenanceSpan for an exchange,
// establishing AI_GENERATED attribution lineage (§19).
func (s *Service) recordExchangeProvenance(ex *Exchange) {
	cs, csErr := s.provenance.CreateChangeSet(provenance.CreateChangeSetRequest{
		OrganizationID:   ex.OrganizationID,
		SessionID:        ex.SessionID,
		ExchangeID:       ex.ID,
		UserID:           ex.UserID,
		HarnessID:        ex.HarnessID,
		ModelPackageID:   ex.ModelPackageID,
		EndpointID:       ex.EndpointID,
		AttributionState: "AI_GENERATED",
		Confidence:       1.0,
	})
	if csErr != nil {
		log.Printf("relay: warning: create changeset failed: %v", csErr)
		return
	}
	if _, spanErr := s.provenance.CreateProvenanceSpan(provenance.CreateSpanRequest{
		OrganizationID:   ex.OrganizationID,
		ChangeSetID:      cs.ID,
		SessionID:        ex.SessionID,
		UserID:           ex.UserID,
		HarnessID:        ex.HarnessID,
		ModelPackageID:   ex.ModelPackageID,
		EndpointID:       ex.EndpointID,
		AttributionState: "AI_GENERATED",
		Confidence:       1.0,
	}); spanErr != nil {
		log.Printf("relay: warning: create provenance span failed: %v", spanErr)
	}
}

// GovernRequest is a live-path inference request resolved from the authenticated peer.
type GovernRequest struct {
	HarnessID string
	SessionID string
	Model     string // model_id (resolved to a ModelPackage)
	Messages  []map[string]string
	MaxTokens int
	// Grant is the DARI Authorization Grant presented for the
	// exchange (Task 7). When nil, the compatibility-window adapter
	// derives a non-delegable view from the persisted legacy lease.
	Grant *dari.GrantEnvelope
}

// GovernInference is the live-path entry point: it resolves governance context
// for an authenticated harness (org, active lease, active policy epoch, model
// package) from the control-plane DB, then runs the full governed flow
// (authorize → forward → meter → evidence). It fails closed if any required
// governance artifact is missing or invalid. (MISSING_ITEMS Domain 1–2 P0 / Harness A)
func (s *Service) GovernInference(ctx context.Context, req GovernRequest, stream ...DeltaSender) (*InferenceResponse, *models.EvidenceReceipt, error) {
	var onDelta DeltaSender
	if len(stream) > 0 {
		onDelta = stream[0]
	}
	// Task 15 backpressure + fair admission: bounded exchange
	// concurrency; on saturation requests queue in the per-account
	// fair scheduler (bounded wait, weighted priority §10C.7) instead
	// of shedding immediately. The queue drains as slots release.
	if !s.admitGoverned(req.HarnessID, req.SessionID) {
		return nil, nil, ErrLoadShed
	}
	defer s.releaseGoverned(req.HarnessID)
	// 1. Resolve org + standing from the enrolled harness (hot-state
	// snapshot re-resolves the full chain on the cold path below).
	var harness models.Harness
	if err := s.db.Where("harness_id = ?", req.HarnessID).First(&harness).Error; err != nil {
		return nil, nil, fmt.Errorf("relay: harness %s not enrolled: %w", req.HarnessID, err)
	}
	orgID := harness.OrganizationID

	// Defense in depth: reject harnesses not in good standing even if a stale
	// lease exists. (Connect-time gate is Service.AuthorizePeer.)
	if !models.HarnessStatusPermitted(harness.Status) {
		return nil, nil, fmt.Errorf("relay: harness %s is %s", req.HarnessID, harness.Status)
	}
	if restriction, err := models.HarnessAdmissionRestriction(s.db, orgID, req.HarnessID); err != nil {
		return nil, nil, fmt.Errorf("relay: fleet desired state unavailable: %w", err)
	} else if restriction != nil {
		return nil, nil, fmt.Errorf("relay: harness admission blocked by %s", restriction.Action)
	}

	// Session-status enforcement (web/02 B3): a session the control
	// plane closed/paused/terminated must not keep exchanging, even
	// on an otherwise healthy connection with a live lease.
	if req.SessionID == "" {
		return nil, nil, fmt.Errorf("relay: authenticated session is required")
	}
	var session models.Session
	if err := s.db.Where("organization_id = ? AND session_id = ?", orgID, req.SessionID).First(&session).Error; err != nil {
		return nil, nil, fmt.Errorf("relay: session %s is not registered for organization: %w", req.SessionID, err)
	}
	if session.HarnessID != req.HarnessID {
		return nil, nil, fmt.Errorf("relay: session %s is not bound to harness %s", req.SessionID, req.HarnessID)
	}
	switch session.Status {
	case "closed", "terminated":
		s.denyWithoutExchange(req, orgID, "session_"+session.Status)
		return nil, nil, fmt.Errorf("relay: session %s is %s — inference refused", req.SessionID, session.Status)
	case "paused", "idle":
		s.denyWithoutExchange(req, orgID, "session_"+session.Status)
		return nil, nil, fmt.Errorf("relay: session %s is %s — resume the session before inference", req.SessionID, session.Status)
	}
	if !models.SessionIsLive(session.Status) {
		s.denyWithoutExchange(req, orgID, "session_"+session.Status)
		return nil, nil, fmt.Errorf("relay: session %s is %s — inference requires an active session", req.SessionID, session.Status)
	}
	if locked, err := models.ActiveSecurityLockdown(s.db, orgID, session.ProjectID); err != nil {
		return nil, nil, fmt.Errorf("relay: security lockdown state unavailable: %w", err)
	} else if locked {
		s.denyWithoutExchange(req, orgID, "security_lockdown")
		return nil, nil, fmt.Errorf("relay: security lockdown is active")
	}

	// User standing gate (web/01 B2): a suspended/offboarded user must
	// not keep exchanging through any harness, even with a live lease.
	if session.UserID != "" {
		var user models.User
		if err := s.db.Where("organization_id = ? AND id = ?", orgID, session.UserID).First(&user).Error; err != nil {
			return nil, nil, fmt.Errorf("relay: session user %s is not registered: %w", session.UserID, err)
		}
		if user.Status != "active" {
			s.denyWithoutExchange(req, orgID, "user_"+user.Status)
			return nil, nil, fmt.Errorf("relay: user %s is %s — inference refused", session.UserID, user.Status)
		}
	}

	// Governed exchanges prove liveness only after the session and user
	// bindings above have passed.
	s.recordHeartbeat(req.HarnessID)
	s.db.Model(&models.Session{}).
		Where("organization_id = ? AND session_id = ?", orgID, req.SessionID).
		Update("last_activity_at", time.Now().Format(time.RFC3339))

	// 2. Resolve the harness + lease (fail-closed) through the
	// hot-state snapshot keyed by the complete authority context: the full chain
	// (harness→lease→epoch→package→endpoint→endpoint-lease) resolves
	// once and is reused per request while fresh.
	revEpoch, _ := s.identity.RevocationSnapshot()
	activeEpoch, epochErr := s.Policy().GetActiveEpoch(orgID)
	if epochErr != nil {
		return nil, nil, epochErr
	}
	cacheKey := GovCacheKey(orgID, req.HarnessID, session.UserID, req.SessionID, req.Model, activeEpoch.EpochID)
	snap, cerr := s.hotState.Get(cacheKey, time.Now(), revEpoch)
	if cerr != nil || snap == nil {
		resolved, rerr := s.ResolveGovernanceSnapshot(req.HarnessID, req.SessionID, req.Model)
		if rerr != nil {
			reason := "governance_resolution_failed"
			if strings.Contains(rerr.Error(), "not in registry") {
				reason = "model_not_registered"
			} else if strings.Contains(rerr.Error(), "no active capability lease") {
				reason = "no_active_lease"
			}
			s.denyWithoutExchange(req, harness.OrganizationID, reason)
			return nil, nil, rerr
		}
		s.hotState.Put(cacheKey, resolved, revEpoch)
		snap = resolved
	}
	harness, lease := snap.Harness, snap.Lease
	// The snapshot's harness row already resolved org+status above; a
	// cached snapshot is only served while revocation-fresh.
	orgID = harness.OrganizationID
	notAfter, _ := time.Parse(time.RFC3339, lease.NotAfter)
	if !notAfter.IsZero() && time.Now().After(notAfter) {
		s.hotState.Invalidate(cacheKey)
		return nil, nil, fmt.Errorf("relay: capability lease expired for harness %s", req.HarnessID)
	}

	// Legacy-lease admissibility (compat window): the persisted lease
	// must at least be viewable as a non-delegable grant; the grant
	// itself is verified against the resolved model below.
	if _, gerr := DecodeLegacyCapabilityLease(&lease); gerr != nil {
		return nil, nil, fmt.Errorf("relay: legacy lease not convertible to an authorization grant: %w", gerr)
	}

	// 3. The snapshot resolved the package; cache hits reuse it and
	// re-check recall from the carried row (reject recalled).
	var pkg models.ModelPackage
	if snap.Package.PackageID != "" {
		pkg = snap.Package
	} else if err := s.db.Where("model_id = ?", req.Model).First(&pkg).Error; err != nil {
		s.denyWithoutExchange(req, orgID, "model_not_registered")
		return nil, nil, fmt.Errorf("relay: model %s not in registry: %w", req.Model, err)
	}
	if pkg.State == "recalled" {
		s.denyWithoutExchange(req, orgID, "model_recalled")
		s.hotState.Invalidate(cacheKey)
		return nil, nil, fmt.Errorf("relay: model %s has been recalled", req.Model)
	}

	// Authorization grant (Task 7): a presented DARI grant is the
	// authority for the exchange — verified ONCE under the policy
	// issuer, bound to harness/session/audience, and authorized for
	// either the model_id or the resolved package id.
	if req.Grant != nil {
		if err := s.VerifySessionGrantFor(req.Grant, req.HarnessID, req.SessionID,
			[]string{req.Model, pkg.PackageID}, time.Now().UnixMilli()); err != nil {
			return nil, nil, fmt.Errorf("relay: authorization grant rejected: %w", err)
		}
	}

	// 4. Authorize → forward → meter via the governed exchange flow.
	// F.4: bind the verified grant's signed-object digest to the
	// exchange so the F.6 decision's LeafGrantDigest is real.
	ex, verdict, err := s.OpenExchange(ctx, OpenExchangeRequest{
		OrganizationID: orgID,
		SessionID:      req.SessionID,
		UserID:         lease.UserID,
		HarnessID:      req.HarnessID,
		LeaseID:        lease.LeaseID,
		PolicyEpochID:  lease.PolicyEpochID,
		ModelPackageID: pkg.PackageID,
	})
	if err != nil || verdict != dari.VerdictAllow {
		return nil, nil, fmt.Errorf("relay: exchange denied for harness %s (verdict=%s): %w", req.HarnessID, verdict, err)
	}
	if req.Grant != nil {
		ex.GrantDigest = req.Grant.SignedDigest
	}
	appendEvidence(ex, "exchange_open|"+ex.ID)

	// §10.2/DARI: record the REAL admission trace on the exchange —
	// the same stages EnforceStages pins, run against the snapshot
	// this request actually resolved (hot-state), not a parallel
	// re-resolution.
	ex.Trace, _ = s.enforceStagesForSnapshot(&PipelineTrace{}, ex, snap, req)

	// Inline DLP/PII/injection scan (§16) — populates Security findings from real
	// traffic. Blocks on DENY (critical/high); logs REQUIRE_REVIEW (medium).
	var combined strings.Builder
	for _, m := range req.Messages {
		combined.WriteString(m["content"])
		combined.WriteByte('\n')
	}
	combinedText := combined.String()
	secResult := s.security.CheckContext(orgID, combinedText)
	appendEvidence(ex, "dlp_scan|"+secResult.Verdict)
	ex.RecordStage(StageDLPScan, secResult.Verdict != "DENY", fmt.Sprintf("%d findings", len(secResult.Findings)))
	if len(secResult.Findings) > 0 {
		for _, f := range secResult.Findings {
			persisted, err := s.security.RecordFinding(orgID, req.SessionID, ex.ID, f)
			if err != nil {
				_, _ = s.CloseExchange(ctx, ex.ID)
				return nil, nil, fmt.Errorf("relay: security finding persistence failed")
			}
			s.realtime.NotifySecurityFinding(orgID, persisted.ID, persisted.Severity, persisted.TitleKo, persisted.Status)
		}
		if secResult.Verdict == "DENY" {
			s.CloseExchange(ctx, ex.ID)
			return nil, nil, fmt.Errorf("relay: security verdict DENY — request blocked (%d findings)", len(secResult.Findings))
		}
	}

	infReq := InferenceRequest{
		ExchangeID:     ex.ID,
		OrganizationID: orgID,
		SessionID:      req.SessionID,
		ModelPackageID: pkg.PackageID,
		Model:          req.Model,
		Messages:       req.Messages,
		MaxTokens:      req.MaxTokens,
	}
	var resp *InferenceResponse
	if onDelta != nil {
		resp, err = s.RouteInferenceStream(ctx, infReq, onDelta)
	} else {
		resp, err = s.RouteInference(ctx, infReq)
	}
	if err != nil {
		// Failed/denied exchanges are removed from the in-flight map —
		// only exchanges that reach CloseExchange persist there.
		s.mu.Lock()
		delete(s.exchanges, ex.ID)
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("relay: governed inference failed: %w", err)
	}

	// Inline response inspection (security C4, §16.5): the model's
	// OUTPUT is scanned before it leaves the relay — exfiltration in
	// responses is recorded as findings and DENY-class output is
	// blocked.
	if resp != nil {
		var responseText strings.Builder
		for _, choice := range resp.Choices {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					responseText.WriteString(content)
					responseText.WriteByte('\n')
				}
			}
			if content, ok := choice["text"].(string); ok {
				responseText.WriteString(content)
				responseText.WriteByte('\n')
			}
		}
		if responseText.Len() > 0 {
			secResp := s.security.CheckContext(orgID, responseText.String())
			if len(secResp.Findings) > 0 {
				appendEvidence(ex, "dlp_response_scan|"+secResp.Verdict)
				for _, f := range secResp.Findings {
					f.Direction = "response"
					persisted, err := s.security.RecordFinding(orgID, req.SessionID, ex.ID, f)
					if err != nil {
						_, _ = s.CloseExchange(ctx, ex.ID)
						return nil, nil, fmt.Errorf("relay: security finding persistence failed")
					}
					s.realtime.NotifySecurityFinding(orgID, persisted.ID, persisted.Severity, persisted.TitleKo, persisted.Status)
				}
				if secResp.Verdict == "DENY" {
					s.CloseExchange(ctx, ex.ID)
					return nil, nil, fmt.Errorf("relay: response security verdict DENY — model output blocked (%d findings)", len(secResp.Findings))
				}
			}
		}
	}

	appendEvidence(ex, "forward|"+pkg.PackageID)
	ex.RecordStage(StageForward, true, pkg.PackageID)
	tokenUsage := canonicalTokenUsage{}
	if resp != nil && resp.Usage != nil {
		tokenUsage, _ = extractCanonicalTokenUsage(resp.Usage)
		if tokenUsage.InputReported || tokenUsage.OutputReported {
			ex.RecordStage(StageTokenize, true, fmt.Sprintf("in=%d out=%d", tokenUsage.Input, tokenUsage.Output))
		}
	}

	// Conversation record (web/02 inspector): the session's prompt/
	// response history persists per exchange — prompt text as the
	// DLP-redacted form (the scanner's redaction policy applied), so
	// the inspector never renders what policy masked.
	promptText := combinedText
	if redacted, _, exists := redactIfConfigured(s.security, orgID, promptText); exists {
		promptText = redacted
	}
	respText := ""
	if resp != nil && len(resp.Choices) > 0 {
		if content, ok := resp.Choices[0]["message"].(map[string]any)["content"].(string); ok {
			respText = content
		}
	}
	inTok, outTok := int(tokenUsage.Input), int(tokenUsage.Output)
	s.db.Create(&models.PromptExchange{
		SessionID: ex.SessionID, ExchangeID: ex.ID,
		PromptText: promptText, ResponseText: respText,
		ModelPackageID: ex.ModelPackageID, EndpointID: ex.EndpointID,
		InputTokens: inTok, OutputTokens: outTok,
		VerdictResult: string(ex.Verdict), PolicyEpochID: ex.PolicyEpochID,
	})

	// 5. Evidence receipt.
	receipt, err := s.CloseExchange(ctx, ex.ID)
	if err != nil {
		log.Printf("relay: warning: close exchange %s: %v", ex.ID, err)
	}
	// F.6: carry the signed decision so the transport pushes the
	// RELAY_VERDICT to the connector (verify + aggregate there).
	if ex.Decision != nil && ex.Decision.COSEBytes != nil {
		resp.DecisionCOSE = ex.Decision.COSEBytes
	}
	return resp, receipt, nil
}

// emitSpine writes one durable governed event for an exchange. The
// spine is best-effort per event: a spine write failure logs but never
// fails the exchange (the receipt chain is the authoritative record).
func (s *Service) emitSpine(ex *Exchange, eventType string, payload map[string]any) {
	if s.spine == nil || ex == nil {
		return
	}
	payload["exchange_id"] = ex.ID
	if _, err := s.spine.Emit(events.EmitRequest{
		EventType:      "cp.exchange." + eventType,
		OrganizationID: ex.OrganizationID,
		SessionID:      ex.SessionID,
		HarnessID:      ex.HarnessID,
		ActorType:      "relay",
		Payload:        payload,
	}); err != nil {
		log.Printf("relay: spine emit %s for %s failed: %v", eventType, ex.ID, err)
	}
}

// appendEvidence records one evidence event on the exchange's chain.
// The receipt's F.9 root commits to these refs (decision/scan/forward/
// meter), making the chain non-empty on every governed exchange.
func appendEvidence(ex *Exchange, ref string) {
	if ex == nil {
		return
	}
	ex.EvidenceChain = append(ex.EvidenceChain, ref)
}

// InferenceResponse mirrors the PIA/OpenAI completion response.
type InferenceResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []map[string]interface{} `json:"choices"`
	Usage   map[string]int           `json:"usage"`
	// DecisionCOSE carries the signed F.6 Authorization Decision (the
	// relay→connector RELAY_VERDICT payload); never serialized onward.
	DecisionCOSE []byte `json:"-"`
}

type canonicalTokenUsage struct {
	Input          int64
	Output         int64
	InputReported  bool
	OutputReported bool
}

// extractCanonicalTokenUsage normalizes OpenAI-style and internal usage keys
// exactly once. A present zero is authoritative; fallback keys are consulted
// only when the provider key is absent.
func extractCanonicalTokenUsage(values map[string]int) (canonicalTokenUsage, error) {
	metric := func(primary, fallback string) (int64, bool, error) {
		value, ok := values[primary]
		key := primary
		if !ok {
			value, ok = values[fallback]
			key = fallback
		}
		if !ok {
			return 0, false, nil
		}
		if value < 0 {
			return 0, false, fmt.Errorf("relay: provider reported negative %s", key)
		}
		return int64(value), true, nil
	}
	input, inputReported, err := metric("prompt_tokens", "input_tokens")
	if err != nil {
		return canonicalTokenUsage{}, err
	}
	output, outputReported, err := metric("completion_tokens", "output_tokens")
	if err != nil {
		return canonicalTokenUsage{}, err
	}
	return canonicalTokenUsage{Input: input, Output: output, InputReported: inputReported, OutputReported: outputReported}, nil
}

func modelTokenPrice(pkg models.ModelPackage, input bool) (int64, bool, error) {
	rate, configured, legacy := pkg.PriceOutputMicrosPer1K, pkg.PriceOutputConfigured, pkg.PriceOutputPer1K
	if input {
		rate, configured, legacy = pkg.PriceInputMicrosPer1K, pkg.PriceInputConfigured, pkg.PriceInputPer1K
	}
	rate, configured, err := metering.ResolveKRWPriceMicrosPer1K(rate, configured, legacy)
	if err != nil {
		return 0, false, fmt.Errorf("relay: token price: %w", err)
	}
	return rate, configured, nil
}

// recordUsage meters token usage for a completed governed inference (§10.2 stage 13).
func (s *Service) recordUsage(ex *Exchange, resp *InferenceResponse) error {
	return s.recordUsageContext(context.Background(), ex, resp)
}

func (s *Service) recordUsageContext(parent context.Context, ex *Exchange, resp *InferenceResponse) error {
	if resp == nil || resp.Usage == nil {
		return nil
	}
	usage, err := extractCanonicalTokenUsage(resp.Usage)
	if err != nil {
		return err
	}
	if usage.InputReported || usage.OutputReported {
		appendEvidence(ex, fmt.Sprintf("meter|in=%d out=%d", usage.Input, usage.Output))
	}
	if s.metering == nil || (!usage.InputReported && !usage.OutputReported) {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	// Inference has already completed, so a browser disconnect must not erase
	// its billable record. Detach cancellation while retaining request values,
	// then bound the ownership lookup and insert with a short durable deadline.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	var pkg models.ModelPackage
	if err := s.db.WithContext(persistCtx).Where("package_id = ? OR id = ?", ex.ModelPackageID, ex.ModelPackageID).First(&pkg).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("relay: read model pricing: %w", err)
	}
	now := time.Now().UTC()
	events := make([]telemetry.MeteringEvent, 0, 2)
	appendTokenEvent := func(sequence uint64, metric telemetry.MetricType, quantity int64, reported, input bool) error {
		if !reported {
			return nil
		}
		rate, configured, err := modelTokenPrice(pkg, input)
		if err != nil {
			return err
		}
		event := telemetry.MeteringEvent{
			OrganizationID: ex.OrganizationID, SessionID: ex.SessionID, ExchangeID: ex.ID,
			UserID: ex.UserID, HarnessID: ex.HarnessID, ModelPackageID: ex.ModelPackageID,
			EndpointID: ex.EndpointID, Sequence: sequence, MetricType: metric,
			Quantity: quantity, Unit: "tokens", OccurredAt: now,
			PricingState: models.UsagePricingUnpriced,
		}
		if !configured {
			events = append(events, event)
			return nil
		}
		cost, err := metering.TokenCostMicros(quantity, rate)
		if err != nil {
			return err
		}
		event.CostMicros = cost
		event.Currency = "KRW"
		event.PricingState = models.UsagePricingPriced
		event.AppliedRateMicrosPer1K = rate
		event.AppliedPriceVersion = pkg.PriceVersion
		event.AppliedPriceSource = pkg.PriceSource
		events = append(events, event)
		return nil
	}
	if err := appendTokenEvent(1, telemetry.MetricTokensIn, usage.Input, usage.InputReported, true); err != nil {
		return err
	}
	if err := appendTokenEvent(2, telemetry.MetricTokensOut, usage.Output, usage.OutputReported, false); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	return s.metering.RecordMeteringBatchContext(persistCtx, events)
}

// AuthorizePeer is the connect-time gate: an enrolled harness in good standing
// is allowed; unknown/revoked/quarantined harnesses are rejected. This ties
// fleet revocation/quarantine (web/09-fleet) to the live DARI path — a revoked
// harness can no longer authenticate. Full PPC signature verification is a
// follow-up once the harness presents real signed credentials (Harness A).
func (s *Service) AuthorizePeer(harnessID string) (string, error) {
	var harness models.Harness
	if err := s.db.Where("harness_id = ?", harnessID).First(&harness).Error; err != nil {
		return "", fmt.Errorf("relay: unknown harness %s: %w", harnessID, err)
	}
	if !models.HarnessStatusPermitted(harness.Status) {
		return "", fmt.Errorf("relay: harness %s status is %s", harnessID, harness.Status)
	}
	if restriction, err := models.HarnessAdmissionRestriction(s.db, harness.OrganizationID, harnessID); err != nil {
		return "", fmt.Errorf("relay: fleet desired state unavailable: %w", err)
	} else if restriction != nil {
		return "", fmt.Errorf("relay: harness admission blocked by %s", restriction.Action)
	}
	if locked, err := models.ActiveSecurityLockdown(s.db, harness.OrganizationID, ""); err != nil {
		return "", fmt.Errorf("relay: security lockdown state unavailable: %w", err)
	} else if locked {
		return "", fmt.Errorf("relay: organization security lockdown is active")
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

// AttachDARIListener registers a live DARI listener to receive
// revocation propagation. The relay binary calls this at startup so a
// control-plane revoke terminates active governed streams.
func (s *Service) AttachDARIListener(pl *DARIListener) {
	if pl == nil {
		return
	}
	s.mu.Lock()
	s.listeners = append(s.listeners, pl)
	s.mu.Unlock()
}

// RevokeHarness revokes a harness in the identity service AND pushes
// the revocation to every attached listener, terminating its active
// transports (Task 6 Step 3).
func (s *Service) RevokeHarness(orgID, harnessID, reason string) error {
	if err := s.identity.RevokeHarness(orgID, harnessID, reason); err != nil {
		return err
	}
	epoch, serials := s.identity.RevocationSnapshot()
	// Hot-state must never serve a pre-revocation snapshot.
	s.hotState.InvalidateAll()
	s.mu.RLock()
	listeners := append([]*DARIListener(nil), s.listeners...)
	s.mu.RUnlock()
	for _, pl := range listeners {
		pl.ApplyRevocationSnapshot(epoch, serials)
	}
	return nil
}

// securityRulesFor projects the security service's rules for the DLP
// rule-pack push. It carries both class-level toggles (backward compat)
// and per-rule overrides (PAT-1431).
func (s *Service) securityRulesFor(orgID string) []SecurityRuleView {
	rules, err := s.security.ListRules(orgID)
	if err != nil {
		return nil
	}
	disabled := s.security.DisabledRuleIDs(orgID)
	out := make([]SecurityRuleView, 0, len(rules))
	for _, r := range rules {
		// The model row carries the rule CLASS; the detection regexes
		// live in the detection engine. The pack pushes the class so
		// the connector enables the matching built-in lexicon.
		// Per-rule overrides carry the exact enabled/severity/action state.
		out = append(out, SecurityRuleView{
			RuleID: r.RuleID, Pattern: r.Type, Severity: r.Severity,
			Action: r.Action, RedactWith: "[REDACTED]", Disabled: disabled[r.RuleID],
		})
	}
	return out
}

// dlpOverridePacksFor builds the scoped DELTA packs (PAT-1432) for a
// session's subject, ordered team → user → harness (ascending
// specificity; the harness applies them in arrival order and the
// most specific scope wins). Levels with no override rows are
// omitted entirely — no empty packs on the wire.
func (s *Service) dlpOverridePacksFor(orgID, userID, harnessPeerID, epochID string) []*wireDLPRulePack {
	if s.security == nil {
		return nil
	}
	var packs []*wireDLPRulePack
	for _, scope := range s.security.OverridesFor(orgID, userID, harnessPeerID) {
		if pack := BuildScopedDLPOverridePack(epochID, orgID, scope.Level, scope.ScopeID, scope.Overrides, time.Now()); pack != nil {
			packs = append(packs, pack)
		}
	}
	return packs
}

// redactIfConfigured applies the org's DLP redaction to inspector
// text when the scanner carries redaction rules (best effort: a
// scanner failure returns the input unredacted-but-flagged=false).
func redactIfConfigured(sec *security.Service, orgID, text string) (string, bool, bool) {
	if sec == nil || text == "" {
		return text, false, false
	}
	res := sec.CheckContext(orgID, text)
	if len(res.Findings) == 0 {
		return text, false, true
	}
	if r := sec.Redact(orgID, text); r != "" {
		return r, true, true
	}
	return text, false, false
}
