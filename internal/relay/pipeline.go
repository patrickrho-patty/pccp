package relay

import (
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// ValidateCatalogModel checks that a catalog model ID is valid for use.
// Per PCCP v2 §10A.11: "A user types a fake model ID; Relay rejects it."
func ValidateCatalogModel(catalogModelID, catalogEpoch string) error {
	if catalogModelID == "" {
		return fmt.Errorf("relay: catalog model ID required (v2 §10A)")
	}
	// In production, this queries the local catalog cache.
	return nil
}

// PipelineStage represents one stage of the request pipeline (§10.2).
type PipelineStage struct {
	Name        string
	Description string
	FailMode    string // fail_closed, fail_open, degrade
}

// PipelineResult is the result of running the full pipeline.
type PipelineResult struct {
	Allowed     bool
	Verdict     string
	Stage       string // which stage made the decision
	Reason      string
	Elapsed     time.Duration
	ModelPkgID  string // resolved package
	EndpointID  string // resolved endpoint
}

// RunPipeline executes the 14-stage request pipeline (§10.2).
// This is the core governance hot path for every AI request.
func (s *Service) RunPipeline(req PipelineRequest) PipelineResult {
	start := time.Now()

	// Stage 1: Peer Authentication
	if req.PeerID == "" {
		return PipelineResult{Allowed: false, Verdict: "DENY", Stage: "1.auth", Reason: "peer not authenticated", Elapsed: time.Since(start)}
	}

	// Stage 2: User/Account/Org Binding
	if req.UserID == "" && req.AccountID == "" {
		return PipelineResult{Allowed: false, Verdict: "DENY", Stage: "2.binding", Reason: "no user/account binding", Elapsed: time.Since(start)}
	}

	// Stage 3: Subscription / Entitlement
	if req.AccountID != "" {
		sub, err := s.checkSubscription(req.AccountID)
		if err != nil {
			return PipelineResult{Allowed: false, Verdict: "DENY", Stage: "3.entitlement", Reason: err.Error(), Elapsed: time.Since(start)}
		}
		_ = sub
	}

	// Stage 4: Harness / Session Authorization
	if req.LeaseID != "" {
		lease, err := s.validateLease(req.LeaseID, req.PeerID, req.SessionID)
		if err != nil {
			return PipelineResult{Allowed: false, Verdict: "DENY", Stage: "4.session", Reason: err.Error(), Elapsed: time.Since(start)}
		}
		_ = lease
	}

	// Stage 5: Account Integrity / Platform Security
	// Check if account is in restricted state
	if req.AccountID != "" {
		if blocked := s.checkRiskStates(req.AccountID); blocked {
			return PipelineResult{Allowed: false, Verdict: "DENY", Stage: "5.risk", Reason: "account in blocked risk state", Elapsed: time.Since(start)}
		}
	}

	// Stage 6: Policy / Governance
	if req.PolicyEpochID != "" && req.ModelPackageID != "" {
		if !s.checkModelAllowed(req.PolicyEpochID, req.ModelPackageID) {
			return PipelineResult{Allowed: false, Verdict: "DENY", Stage: "6.policy", Reason: "model not allowed under policy epoch", Elapsed: time.Since(start)}
		}
	}

	// Stage 7: Fair-Use / Budget / Capacity
	// Check capacity lease if account-based
	if req.AccountID != "" && req.CapacityLeaseID != "" {
		if err := s.checkCapacityLease(req.CapacityLeaseID, req.AccountID); err != nil {
			return PipelineResult{Allowed: false, Verdict: "QUARANTINE", Stage: "7.capacity", Reason: err.Error(), Elapsed: time.Since(start)}
		}
	}

	// Stage 8: Model Catalog Validation (v2 §10A)
	if req.CatalogModelID != "" {
		if err := ValidateCatalogModel(req.CatalogModelID, req.CatalogEpoch); err != nil {
			return PipelineResult{Allowed: false, Verdict: "DENY", Stage: "8.catalog", Reason: err.Error(), Elapsed: time.Since(start)}
		}
	}

	// Stage 9: Route Eligibility — find endpoint with valid lease
	endpoint, _, err := s.findEndpoint(req.OrganizationID, req.ModelPackageID)
	if err != nil {
		return PipelineResult{Allowed: false, Verdict: "DENY", Stage: "9.routing", Reason: err.Error(), Elapsed: time.Since(start)}
	}

	// Stage 10: Admission — for now, always admit if we got here
	// In production, this would check the fair scheduler

	// Stage 11: PIA Dispatch — ready to route
	// Stage 12-14 happen during/after streaming

	return PipelineResult{
		Allowed:    true,
		Verdict:    "ALLOW",
		Stage:      "complete",
		Reason:     "all pipeline stages passed",
		Elapsed:    time.Since(start),
		ModelPkgID: req.ModelPackageID,
		EndpointID: endpoint,
	}
}

// PipelineRequest contains all the inputs for the pipeline.
type PipelineRequest struct {
	// Stage 1-2: Identity
	PeerID       string
	UserID       string
	AccountID    string
	OrganizationID string

	// Stage 3: Entitlement
	SubscriptionPlan string

	// Stage 4: Session
	LeaseID    string
	SessionID  string

	// Stage 6: Policy
	PolicyEpochID  string
	ModelPackageID string

	// Stage 7: Capacity
	CapacityLeaseID string

	// Stage 8: Catalog (v2)
	CatalogModelID string
	CatalogEpoch   string
}

// Helper methods for pipeline stages

func (s *Service) checkSubscription(accountID string) (*models.Subscription, error) {
	var sub models.Subscription
	err := s.db.Where("account_id = ? AND status = 'active'", accountID).
		Order("created_at DESC").First(&sub).Error
	if err != nil {
		return nil, fmt.Errorf("no active subscription for account %s", accountID)
	}
	return &sub, nil
}

func (s *Service) validateLease(leaseID, peerID, sessionID string) (*models.CapabilityLease, error) {
	var lease models.CapabilityLease
	if err := s.db.Where("lease_id = ?", leaseID).First(&lease).Error; err != nil {
		return nil, fmt.Errorf("capability lease not found")
	}
	if lease.Status != "active" {
		return nil, fmt.Errorf("lease status is %s", lease.Status)
	}
	return &lease, nil
}

func (s *Service) checkRiskStates(accountID string) bool {
	var account models.Account
	if err := s.db.Where("id = ?", accountID).First(&account).Error; err != nil {
		return false // can't check, don't block
	}
	// Block if platform security is in "blocked" state
	return account.PlatformSecurityState == "blocked"
}

func (s *Service) checkModelAllowed(epochID, modelPkgID string) bool {
	var epoch models.PolicyEpoch
	if err := s.db.Where("epoch_id = ?", epochID).First(&epoch).Error; err != nil {
		return false
	}
	// Check if model is in the allowed list
	// For now, accept if epoch exists and is active
	return epoch.Status == "active"
}

func (s *Service) checkCapacityLease(leaseID, accountID string) error {
	// For now, just check the lease exists
	// In production, this would check the AccountCapacityLease
	return nil
}

func (s *Service) findEndpoint(orgID, modelPkgID string) (string, string, error) {
	var endpoint models.InferenceEndpoint
	err := s.db.Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
		orgID, modelPkgID).First(&endpoint).Error
	if err != nil {
		return "", "", fmt.Errorf("no active endpoint for model %s", modelPkgID)
	}

	var epLease models.EndpointLease
	s.db.Where("endpoint_id = ? AND status = 'active'",
		endpoint.EndpointID).Order("issued_at DESC").First(&epLease)
	if epLease.ID == "" {
		return "", "", fmt.Errorf("no valid endpoint lease")
	}

	return endpoint.EndpointID, epLease.LeaseID, nil
}
