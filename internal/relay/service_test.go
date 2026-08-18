package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupGovernedTestDB opens an in-memory SQLite DB with all models migrated.
// govDBSeq distinguishes in-memory DBs across test invocations.
var govDBSeq atomic.Int64

func setupGovernedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// One shared-cache DB per test-invocation: a process-global counter
	// separates -count>1 reruns (name-keyed fixtures previously hit
	// UNIQUE constraints on the second run). Tests re-opening the
	// helper within one invocation receive independent DBs, which is
	// the isolation they assume anyway.
	dsn := fmt.Sprintf("file:governed_exchange_test_%d?mode=memory&cache=shared",
		govDBSeq.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}
	return db
}

func seedGovernedSession(t *testing.T, db *gorm.DB, orgID, userID, harnessID, sessionID string) {
	t.Helper()
	if err := db.FirstOrCreate(&models.User{AuditBase: models.AuditBase{Base: models.Base{ID: userID}, OrganizationID: orgID}, Email: userID + "@example.test", Name: userID, Status: "active"}, "organization_id = ? AND email = ?", orgID, userID+"@example.test").Error; err != nil {
		t.Fatalf("seed governed user: %v", err)
	}
	if err := db.FirstOrCreate(&models.Harness{OrganizationID: orgID, HarnessID: harnessID, Status: "enrolled"}, "organization_id = ? AND harness_id = ?", orgID, harnessID).Error; err != nil {
		t.Fatalf("seed governed harness: %v", err)
	}
	if err := db.FirstOrCreate(&models.Session{AuditBase: models.AuditBase{OrganizationID: orgID}, HarnessID: harnessID, UserID: userID, SessionID: sessionID, Status: "active"}, "organization_id = ? AND session_id = ?", orgID, sessionID).Error; err != nil {
		t.Fatalf("seed governed session: %v", err)
	}
}

func TestGovernInferenceRejectsDurableFleetRestrictionBeforeCachedAuthority(t *testing.T) {
	db := setupGovernedTestDB(t)
	const orgID, userID, harnessID, sessionID = "org-desired", "user-desired", "harness-desired", "session-desired"
	seedGovernedSession(t, db, orgID, userID, harnessID, sessionID)
	if err := db.Create(&models.FleetDesiredState{
		OrganizationID: orgID, HarnessID: harnessID, Action: models.FleetStateSuspendModel,
		Status: "active", Reason: "security review", SetAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc, err := New(db, "http://localhost:8080", "relay-desired-state-test")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: "cached-model",
	})
	if err == nil || !strings.Contains(err.Error(), "admission blocked by "+models.FleetStateSuspendModel) {
		t.Fatalf("durable fleet restriction did not fail closed: %v", err)
	}
}

func TestGovernInferenceRejectsDurableProjectLockdown(t *testing.T) {
	db := setupGovernedTestDB(t)
	const orgID, userID, harnessID, sessionID, projectID = "org-lockdown", "user-lockdown", "harness-lockdown", "session-lockdown", "project-lockdown"
	seedGovernedSession(t, db, orgID, userID, harnessID, sessionID)
	if err := db.Model(&models.Session{}).Where("organization_id = ? AND session_id = ?", orgID, sessionID).Update("project_id", projectID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SecurityLockdown{
		OrganizationID: orgID, Scope: "project", ProjectID: projectID, Status: "active",
		Reason: "incident", ActivatedBy: "admin", ActivatedAt: time.Now().UTC().Format(time.RFC3339),
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc, err := New(db, "http://localhost:8080", "relay-lockdown-test")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: "cached-model",
	})
	if err == nil || !strings.Contains(err.Error(), "security lockdown is active") {
		t.Fatalf("durable project lockdown did not fail closed: %v", err)
	}
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
	seedGovernedSession(t, db, orgID, userID, harnessID, sessionID)

	if err := db.Create(&models.CapabilityLease{
		OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID,
		UserID: userID, SessionID: sessionID, PolicyEpochID: epochID,
		AllowedModelPackages: `["` + modelPkgID + `"]`,
		NotBefore:            past, NotAfter: future, Status: "active",
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

func TestOpenExchangeRejectsLeaseAuthorityContextMismatch(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID      = "org-authority"
		userID     = "user-authority"
		harnessID  = "hrn-authority"
		sessionID  = "ses-authority"
		leaseID    = "lease-authority"
		epochID    = "epoch-authority"
		modelPkgID = "pmp-authority"
	)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	seedGovernedSession(t, db, orgID, userID, harnessID, sessionID)
	if err := db.Create(&models.CapabilityLease{
		OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID,
		UserID: userID, SessionID: sessionID, PolicyEpochID: epochID,
		AllowedModelPackages: `["` + modelPkgID + `"]`, NotBefore: past, NotAfter: future, Status: "active",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: epochID, AllowedModelsJSON: `["` + modelPkgID + `"]`, Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModelPackage{PackageID: modelPkgID, ModelID: "model-authority", Name: "Authority", State: "published"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: "ep-authority", ModelPackageID: modelPkgID, Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.EndpointLease{OrganizationID: orgID, EndpointID: "ep-authority", ModelPackageID: modelPkgID, LeaseID: "epl-authority", NotAfter: future, Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	svc, err := New(db, "", "relay-authority-test")
	if err != nil {
		t.Fatal(err)
	}
	base := OpenExchangeRequest{
		OrganizationID: orgID, SessionID: sessionID, UserID: userID, HarnessID: harnessID,
		LeaseID: leaseID, PolicyEpochID: epochID, ModelPackageID: modelPkgID,
	}
	if _, verdict, err := svc.OpenExchange(context.Background(), base); err != nil || verdict != dari.VerdictAllow {
		t.Fatalf("valid authority context rejected: verdict=%s err=%v", verdict, err)
	}

	tests := []struct {
		name   string
		mutate func(OpenExchangeRequest) OpenExchangeRequest
	}{
		{"organization", func(req OpenExchangeRequest) OpenExchangeRequest { req.OrganizationID = "org-other"; return req }},
		{"harness", func(req OpenExchangeRequest) OpenExchangeRequest { req.HarnessID = "hrn-other"; return req }},
		{"session", func(req OpenExchangeRequest) OpenExchangeRequest { req.SessionID = "ses-other"; return req }},
		{"user", func(req OpenExchangeRequest) OpenExchangeRequest { req.UserID = "user-other"; return req }},
		{"epoch", func(req OpenExchangeRequest) OpenExchangeRequest { req.PolicyEpochID = "epoch-other"; return req }},
		{"model", func(req OpenExchangeRequest) OpenExchangeRequest { req.ModelPackageID = "pmp-other"; return req }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, verdict, err := svc.OpenExchange(context.Background(), tc.mutate(base)); err == nil || verdict != dari.VerdictDeny {
				t.Fatalf("mismatched %s authority was not denied: verdict=%s err=%v", tc.name, verdict, err)
			}
		})
	}

	if err := db.Model(&models.CapabilityLease{}).Where("lease_id = ?", leaseID).
		Update("allowed_model_packages", `["pmp-other"]`).Error; err != nil {
		t.Fatal(err)
	}
	if _, verdict, err := svc.OpenExchange(context.Background(), base); err == nil || verdict != dari.VerdictDeny {
		t.Fatalf("out-of-scope model was not denied: verdict=%s err=%v", verdict, err)
	}
}

func TestRecordUsagePreservesReportedZeroAndNormalizesProviderTokenKeys(t *testing.T) {
	db := setupGovernedTestDB(t)
	seedGovernedSession(t, db, "org-meter", "user-meter", "harness-meter", "session-meter")
	if err := db.Create(&models.ModelPackage{
		PackageID: "model-meter", ModelID: "meter", Name: "Meter", State: "published",
		PriceInputMicrosPer1K: 1_500_000, PriceOutputMicrosPer1K: 0,
		PriceInputConfigured: true, PriceOutputConfigured: true,
		PriceVersion: "catalog-v3", PriceSource: "model-catalog",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc, err := New(db, "", "relay-meter")
	if err != nil {
		t.Fatal(err)
	}
	exchange := &Exchange{ID: "exchange-meter", OrganizationID: "org-meter", SessionID: "session-meter", UserID: "user-meter", HarnessID: "harness-meter", ModelPackageID: "model-meter"}
	response := &InferenceResponse{Usage: map[string]int{"prompt_tokens": 11, "completion_tokens": 0, "input_tokens": 99, "output_tokens": 99}}
	disconnected, disconnect := context.WithCancel(context.Background())
	disconnect()
	if err := svc.recordUsageContext(disconnected, exchange, response); err != nil {
		t.Fatal(err)
	}

	var rows []models.UsageRecord
	if err := db.Where("exchange_id = ?", exchange.ID).Order("metric_type ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("reported input/output meters = %d, want 2 (including explicit zero output)", len(rows))
	}
	if rows[0].MetricType != "tokens_in" || rows[0].Quantity != 11 || rows[1].MetricType != "tokens_out" || rows[1].Quantity != 0 {
		t.Fatalf("provider token keys were not normalized once: %+v", rows)
	}
	if rows[0].PricingState != models.UsagePricingPriced || rows[0].CostMicros != 16_500 || rows[1].PricingState != models.UsagePricingPriced || rows[1].CostMicros != 0 {
		t.Fatalf("configured exact/free prices were not applied: %+v", rows)
	}
	for _, row := range rows {
		if row.Currency != "KRW" || row.AppliedPriceVersion != "catalog-v3" || row.AppliedPriceSource != "model-catalog" {
			t.Fatalf("applied pricing provenance missing: %+v", row)
		}
	}
	if got := strings.Join(exchange.EvidenceChain, "\n"); !strings.Contains(got, "in=11 out=0") {
		t.Fatalf("evidence disagrees with canonical meter values: %q", got)
	}
}

func TestCanonicalTokenUsageRejectsNegativeAndDistinguishesAbsentFromZero(t *testing.T) {
	usage, err := extractCanonicalTokenUsage(map[string]int{"prompt_tokens": 0, "input_tokens": 41, "output_tokens": 7})
	if err != nil {
		t.Fatal(err)
	}
	if !usage.InputReported || usage.Input != 0 || !usage.OutputReported || usage.Output != 7 {
		t.Fatalf("canonical provider precedence/presence = %+v", usage)
	}
	if _, err := extractCanonicalTokenUsage(map[string]int{"completion_tokens": -1}); err == nil {
		t.Fatal("negative provider usage was accepted")
	}
}

func TestRecordUsageDoesNotSilentlyDowngradePricingOnCatalogFailure(t *testing.T) {
	db := setupGovernedTestDB(t)
	seedGovernedSession(t, db, "org-meter-failure", "user-meter-failure", "harness-meter-failure", "session-meter-failure")
	svc, err := New(db, "", "relay-meter-failure")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&models.ModelPackage{}); err != nil {
		t.Fatal(err)
	}
	exchange := &Exchange{
		ID: "exchange-meter-failure", OrganizationID: "org-meter-failure", SessionID: "session-meter-failure",
		UserID: "user-meter-failure", HarnessID: "harness-meter-failure", ModelPackageID: "model-meter-failure",
	}
	err = svc.recordUsage(exchange, &InferenceResponse{Usage: map[string]int{"prompt_tokens": 1}})
	if err == nil {
		t.Fatal("catalog read failure silently produced an unpriced usage row")
	}
	var count int64
	if err := db.Model(&models.UsageRecord{}).Where("exchange_id = ?", exchange.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("catalog read failure persisted %d downgraded usage rows", count)
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
	seedGovernedSession(t, db, orgID, userID, harnessID, sessionID)
	db.Create(&models.CapabilityLease{
		OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID,
		UserID: userID, SessionID: sessionID, PolicyEpochID: epochID,
		AllowedModelPackages: `["` + modelPkg + `"]`,
		NotBefore:            past, NotAfter: future, Status: "active",
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
	seedGovernedSession(t, db, orgID, userID, harnessID, sessionID)
	allowed, _ := json.Marshal([]string{modelPkg})
	db.Create(&models.CapabilityLease{OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID, UserID: userID, SessionID: sessionID, PolicyEpochID: epochID, AllowedModelPackages: string(allowed), NotBefore: past, NotAfter: future, Status: "active"})
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

func TestRouteInferenceRejectsExchangeBindingMismatchBeforeForwarding(t *testing.T) {
	t.Setenv("PCCP_PIA_URL", "")
	t.Setenv("YOLO_AUTO_ENDPOINT", "")
	db := setupGovernedTestDB(t)
	const (
		orgID     = "org-bound"
		userID    = "user-bound"
		harnessID = "hrn-bound"
		sessionID = "ses-bound"
		modelPkg  = "pmp-bound"
	)
	seedGovernedSession(t, db, orgID, userID, harnessID, sessionID)
	db.Create(&models.ModelPackage{PackageID: modelPkg, ModelID: "model-bound", Name: "Bound", State: "published"})

	for _, stream := range []bool{false, true} {
		stream := stream
		for _, tc := range []struct {
			name   string
			mutate func(*InferenceRequest)
		}{
			{"organization", func(req *InferenceRequest) { req.OrganizationID = "org-other" }},
			{"session", func(req *InferenceRequest) { req.SessionID = "ses-other" }},
			{"model_package", func(req *InferenceRequest) { req.ModelPackageID = "pmp-other" }},
		} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, tc.name), func(t *testing.T) {
				svc, err := New(db, "", "relay-bound")
				if err != nil {
					t.Fatal(err)
				}
				forwarded := false
				svc.SetForwarder(func(context.Context, InferenceRequest, string) (*InferenceResponse, error) {
					forwarded = true
					return &InferenceResponse{Usage: map[string]int{"prompt_tokens": 1}}, nil
				})
				exchange := &Exchange{
					ID: "ex-bound-" + tc.name, OrganizationID: orgID, SessionID: sessionID,
					UserID: userID, HarnessID: harnessID, ModelPackageID: modelPkg,
					EndpointID: "ep-bound", State: dari.ExchangeAuthorized,
				}
				svc.exchanges[exchange.ID] = exchange
				req := InferenceRequest{
					ExchangeID: exchange.ID, OrganizationID: orgID, SessionID: sessionID,
					ModelPackageID: modelPkg, Model: "model-bound",
				}
				tc.mutate(&req)
				if stream {
					_, err = svc.RouteInferenceStream(context.Background(), req, nil)
				} else {
					_, err = svc.RouteInference(context.Background(), req)
				}
				if err == nil {
					t.Fatal("mismatched request must be rejected")
				}
				if forwarded {
					t.Fatal("mismatched request reached the inference forwarder")
				}
				var count int64
				db.Model(&models.UsageRecord{}).Where("exchange_id = ?", exchange.ID).Count(&count)
				if count != 0 {
					t.Fatalf("mismatched request wrote %d usage records", count)
				}
			})
		}
	}
}
