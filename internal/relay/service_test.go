package relay

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupGovernedTestDB opens an in-memory SQLite DB with all models migrated.
func setupGovernedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:governed_exchange_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}
	return db
}

// TestGovernedExchange_AuthorizesMetersAndEvidences proves the governed
// inference flow (OpenExchange → RouteInference → CloseExchange): a request
// with a valid lease/epoch/endpoint is ALLOWED, a provenance action is
// recorded, usage is METERED, and an evidence receipt is issued.
//
// This is the spine of the product: every AI request must be governed,
// metered, and evidenced end-to-end (MISSING_ITEMS Domain 1–2 P0).
func TestGovernedExchange_AuthorizesMetersAndEvidences(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID      = "org-test-1"
		userID     = "user-test-1"
		harnessID  = "hrn-test-1"
		sessionID  = "ses-test-1"
		leaseID    = "lease-test-1"
		epochID    = "epoch-test-1"
		modelPkgID = "pmp-test-1"
		modelID    = "patty-test"
		endpointID = "ep-test-1"
		epLeaseID  = "eplease-test-1"
	)
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	if err := db.Create(&models.CapabilityLease{
		OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID,
		UserID: userID, SessionID: sessionID, PolicyEpochID: epochID,
		NotBefore: past, NotAfter: future, Status: "active",
	}).Error; err != nil {
		t.Fatalf("seed lease: %v", err)
	}
	allowedJSON, _ := json.Marshal([]string{modelPkgID})
	db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: epochID, AllowedModelsJSON: string(allowedJSON), Status: "active"})
	db.Create(&models.ModelPackage{PackageID: modelPkgID, ModelID: modelID, Name: "Test", State: "published"})
	db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: endpointID, ModelPackageID: modelPkgID, Status: "active"})
	db.Create(&models.EndpointLease{EndpointID: endpointID, OrganizationID: orgID, ModelPackageID: modelPkgID, LeaseID: epLeaseID, NotAfter: future, Status: "active", IssuedAt: past})

	svc, err := New(db, "http://localhost:8080", "relay-test")
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}
	// Inject a fake PIA so the governed flow is exercised without a live model.
	svc.SetForwarder(func(ctx context.Context, req InferenceRequest, endpointLeaseID string) (*InferenceResponse, error) {
		return &InferenceResponse{
			Model: req.Model,
			Usage: map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		}, nil
	})

	ctx := context.Background()

	// 1. Authorize (lease + policy epoch + model-not-recalled + endpoint lease).
	ex, verdict, err := svc.OpenExchange(ctx, OpenExchangeRequest{
		OrganizationID: orgID, SessionID: sessionID, UserID: userID, HarnessID: harnessID,
		LeaseID: leaseID, PolicyEpochID: epochID, ModelPackageID: modelPkgID,
	})
	if err != nil {
		t.Fatalf("OpenExchange: %v", err)
	}
	if verdict != dari.VerdictAllow {
		t.Fatalf("expected allow verdict, got %s", verdict)
	}

	// 2. Route inference (forwarded via the fake PIA).
	resp, err := svc.RouteInference(ctx, InferenceRequest{
		ExchangeID: ex.ID, OrganizationID: orgID, SessionID: sessionID,
		ModelPackageID: modelPkgID, Model: modelID,
		Messages: []map[string]string{{"role": "user", "content": "안녕"}},
	})
	if err != nil {
		t.Fatalf("RouteInference: %v", err)
	}
	if resp == nil || resp.Usage["total_tokens"] != 15 {
		t.Fatalf("unexpected inference response: %+v", resp)
	}

	// 3. Close + evidence receipt.
	receipt, err := svc.CloseExchange(ctx, ex.ID)
	if err != nil {
		t.Fatalf("CloseExchange: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected an evidence receipt to be issued")
	}

	// 4. Provenance action recorded.
	var actCount int64
	db.Model(&models.ActionEnvelope{}).
		Where("exchange_id = ? AND action_type = ?", ex.ID, "ai.inference").Count(&actCount)
	if actCount == 0 {
		t.Error("expected an ai.inference provenance action to be recorded")
	}

	// 5. Metering — the NEW behavior this slice adds (§10.2 stage 13 / §29.13).
	var tokensIn, tokensOut int64
	db.Model(&models.UsageRecord{}).Where("organization_id = ? AND metric_type = ?", orgID, "tokens_in").Count(&tokensIn)
	db.Model(&models.UsageRecord{}).Where("organization_id = ? AND metric_type = ?", orgID, "tokens_out").Count(&tokensOut)
	if tokensIn == 0 || tokensOut == 0 {
		t.Errorf("expected metered usage (tokens_in/tokens_out), got in=%d out=%d", tokensIn, tokensOut)
	}
}

// TestGovernInference_ResolvesFromHarnessID proves the live-path entry point:
// given only an authenticated harness ID + a model, the relay resolves org,
// active lease, active policy epoch, and model package from the DB, then runs
// the full governed flow (authorize → forward → meter → evidence). This is
// what the DARI AI_OPEN path will call (P3).
func TestGovernInference_ResolvesFromHarnessID(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID     = "org-gov-1"
		userID    = "user-gov-1"
		harnessID = "hrn-gov-1"
		sessionID = "ses-gov-1"
		leaseID   = "lease-gov-1"
		epochID   = "epoch-gov-1"
		modelPkg  = "pmp-gov-1"
		modelID   = "patty-gov"
		endpoint  = "ep-gov-1"
		epLease   = "eplease-gov-1"
	)
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	db.Create(&models.Harness{OrganizationID: orgID, HarnessID: harnessID, Status: "enrolled"})
	db.Create(&models.CapabilityLease{
		OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID,
		UserID: userID, SessionID: sessionID, PolicyEpochID: epochID,
		NotBefore: past, NotAfter: future, Status: "active",
	})
	allowedJSON, _ := json.Marshal([]string{modelPkg})
	db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: epochID, AllowedModelsJSON: string(allowedJSON), Status: "active"})
	db.Create(&models.ModelPackage{PackageID: modelPkg, ModelID: modelID, Name: "Gov", State: "published"})
	db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: endpoint, ModelPackageID: modelPkg, Status: "active"})
	db.Create(&models.EndpointLease{EndpointID: endpoint, OrganizationID: orgID, ModelPackageID: modelPkg, LeaseID: epLease, NotAfter: future, Status: "active", IssuedAt: past})

	svc, err := New(db, "http://localhost:8080", "relay-test")
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}
	svc.SetForwarder(func(ctx context.Context, req InferenceRequest, endpointLeaseID string) (*InferenceResponse, error) {
		return &InferenceResponse{Model: req.Model, Usage: map[string]int{"prompt_tokens": 7, "completion_tokens": 3, "total_tokens": 10}}, nil
	})

	resp, receipt, err := svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: modelID,
		Messages: []map[string]string{{"role": "user", "content": "hi"}}, MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("GovernInference: %v", err)
	}
	if resp == nil || resp.Usage["total_tokens"] != 10 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if receipt == nil {
		t.Fatal("expected evidence receipt")
	}

	// Metering + provenance recorded against the resolved org/harness/session.
	var usage, acts int64
	db.Model(&models.UsageRecord{}).Where("organization_id = ? AND harness_id = ?", orgID, harnessID).Count(&usage)
	db.Model(&models.ActionEnvelope{}).Where("harness_id = ? AND action_type = ?", harnessID, "ai.inference").Count(&acts)
	if usage == 0 {
		t.Error("expected metered usage for the resolved harness")
	}
	if acts == 0 {
		t.Error("expected provenance action for the resolved harness")
	}
}

// TestGovernInference_FailsClosedWithoutLease proves governance is enforced:
// an enrolled harness with NO active capability lease is denied (not silently
// forwarded).
func TestGovernInference_FailsClosedWithoutLease(t *testing.T) {
	db := setupGovernedTestDB(t)
	db.Create(&models.Harness{OrganizationID: "org-x", HarnessID: "hrn-nolease", Status: "enrolled"})

	svc, err := New(db, "http://localhost:8080", "relay-test")
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}
	forwarded := false
	svc.SetForwarder(func(ctx context.Context, req InferenceRequest, _ string) (*InferenceResponse, error) {
		forwarded = true
		return &InferenceResponse{}, nil
	})

	_, _, err = svc.GovernInference(context.Background(), GovernRequest{HarnessID: "hrn-nolease", Model: "any"})
	if err == nil {
		t.Fatal("expected denial for harness without active lease")
	}
	if forwarded {
		t.Fatal("PIA must not be forwarded for a denied (ungoverned) request")
	}
}

// TestAuthorizePeer_EnforcesStatus proves the connect-time gate ties fleet
// state to the live path: enrolled harnesses pass; revoked/quarantined/unknown
// are rejected.
func TestAuthorizePeer_EnforcesStatus(t *testing.T) {
	db := setupGovernedTestDB(t)
	db.Create(&models.Harness{OrganizationID: "org-a", HarnessID: "hrn-ok", Status: "enrolled"})
	db.Create(&models.Harness{OrganizationID: "org-a", HarnessID: "hrn-rev", Status: "revoked"})
	db.Create(&models.Harness{OrganizationID: "org-a", HarnessID: "hrn-qua", Status: "quarantined"})

	svc, err := New(db, "", "r")
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}
	if org, err := svc.AuthorizePeer("hrn-ok"); err != nil || org != "org-a" {
		t.Fatalf("enrolled harness should pass, got org=%s err=%v", org, err)
	}
	for _, id := range []string{"hrn-rev", "hrn-qua", "hrn-unknown"} {
		if _, err := svc.AuthorizePeer(id); err == nil {
			t.Errorf("harness %s should be rejected", id)
		}
	}
}

// TestGovernInference_BlocksOnSecurityDeny proves the inline DLP/PII scan on the
// live path: a request containing a Korean RRN (critical) is denied, the finding
// is recorded, and the PIA is NOT forwarded.
func TestGovernInference_BlocksOnSecurityDeny(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID     = "org-sec-1"
		userID    = "user-sec-1"
		harnessID = "hrn-sec-1"
		sessionID = "ses-sec-1"
		leaseID   = "lease-sec-1"
		epochID   = "epoch-sec-1"
		modelPkg  = "pmp-sec-1"
		modelID   = "patty-sec"
		endpoint  = "ep-sec-1"
		epLease   = "eplease-sec-1"
	)
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	db.Create(&models.Harness{OrganizationID: orgID, HarnessID: harnessID, Status: "enrolled"})
	db.Create(&models.CapabilityLease{OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID, UserID: userID, SessionID: sessionID, PolicyEpochID: epochID, NotBefore: past, NotAfter: future, Status: "active"})
	allowed, _ := json.Marshal([]string{modelPkg})
	db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: epochID, AllowedModelsJSON: string(allowed), Status: "active"})
	db.Create(&models.ModelPackage{PackageID: modelPkg, ModelID: modelID, Name: "Sec", State: "published"})
	db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: endpoint, ModelPackageID: modelPkg, Status: "active"})
	db.Create(&models.EndpointLease{EndpointID: endpoint, OrganizationID: orgID, ModelPackageID: modelPkg, LeaseID: epLease, NotAfter: future, Status: "active", IssuedAt: past})

	svc, err := New(db, "", "r")
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}
	forwarded := false
	svc.SetForwarder(func(ctx context.Context, req InferenceRequest, _ string) (*InferenceResponse, error) {
		forwarded = true
		return &InferenceResponse{}, nil
	})

	// Request containing a Korean RRN (critical severity → DENY).
	_, _, err = svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: modelID,
		Messages: []map[string]string{{"role": "user", "content": "주민번호 901225-1234567 확인해줘"}},
	})
	if err == nil {
		t.Fatal("expected security denial for RRN in request")
	}
	if forwarded {
		t.Fatal("PIA must not be forwarded for a DENY verdict")
	}
	var findingCount int64
	db.Model(&models.SecurityFinding{}).Where("organization_id = ?", orgID).Count(&findingCount)
	if findingCount == 0 {
		t.Error("expected security finding recorded from the blocked request")
	}
}
