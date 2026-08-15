package relay

import (
	"context"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// TestGovernedExchangeIssuesSignedDecision (Task 9 live wiring): every
// governed exchange carries an F.6 Authorization Decision — ALLOW on
// the authorized path, DENY with a reason code on denial.
func TestGovernedExchangeIssuesSignedDecision(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID     = "org-dt"
		harnessID = "hrn-dt-decision"
		sessionID = "sess-dt"
		leaseID   = "lease-dt"
		epochID   = "epoch-dt"
		modelPkg  = "pmp-dt"
		modelID   = "patty-dt"
		endpoint  = "ep-dt"
		epLease   = "epl-dt"
	)
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	db.Create(&models.Harness{OrganizationID: orgID, HarnessID: harnessID, Status: "enrolled"})
	db.Create(&models.CapabilityLease{OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID,
		UserID: "u", SessionID: sessionID, PolicyEpochID: epochID,
		AllowedModelPackages: `["` + modelPkg + `"]`, NotBefore: past, NotAfter: future, Status: "active"})
	db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: epochID, AllowedModelsJSON: `["` + modelPkg + `"]`, Status: "active"})
	db.Create(&models.ModelPackage{PackageID: modelPkg, ModelID: modelID, Name: "DT", State: "published"})
	db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: endpoint, ModelPackageID: modelPkg, Status: "active"})
	db.Create(&models.EndpointLease{EndpointID: endpoint, OrganizationID: orgID, ModelPackageID: modelPkg, LeaseID: epLease, NotAfter: future, Status: "active", IssuedAt: past})

	svc, err := New(db, "", "relay-decision-test")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetForwarder(func(ctx context.Context, req InferenceRequest, _ string) (*InferenceResponse, error) {
		return &InferenceResponse{Model: req.Model, Usage: map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}, nil
	})

	resp, _, err := svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: modelID,
		Messages: []map[string]string{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp

	// The decision log must carry a signed ALLOW decision that decodes
	// under the policy key and validates (exchanges are removed at
	// close; decisions persist).
	var allowEnv *dari.DecisionEnvelope
	for _, env := range svc.RecentDecisions() {
		if env.Body.Outcome == dari.DecisionAllow {
			allowEnv = env
		}
	}
	if allowEnv == nil {
		t.Fatal("governed exchange did not issue an ALLOW decision")
	}
	decoded, derr := dari.DecodeAuthorizationDecision(allowEnv.COSEBytes, svc.Policy().SigningPublicKey())
	if derr != nil {
		t.Fatalf("decision decode: %v", derr)
	}
	if derr := dari.ValidateDecision(decoded.Body, time.Now().UnixMilli()); derr != nil {
		t.Fatalf("decision validate: %v", derr)
	}
	if decoded.Body.ExchangeID == "" {
		t.Fatal("decision not bound to its exchange")
	}

	// A denied exchange (model not allowed under epoch) must carry a
	// signed DENY decision with a stable reason code.
	db.Create(&models.CapabilityLease{OrganizationID: orgID, LeaseID: leaseID + "-2", SubjectPeerID: harnessID + "-2",
		UserID: "u", SessionID: sessionID + "-2", PolicyEpochID: epochID,
		AllowedModelPackages: `["` + modelPkg + `"]`, NotBefore: past, NotAfter: future, Status: "active"})
	db.Create(&models.Harness{OrganizationID: orgID, HarnessID: harnessID + "-2", Status: "enrolled"})
	_, _, err = svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID + "-2", SessionID: sessionID + "-2", Model: "unauthorized-model",
		Messages: []map[string]string{{"role": "user", "content": "hi"}},
	})
	if err == nil {
		t.Fatal("unauthorized model must be denied")
	}
	var denyEnv *dari.DecisionEnvelope
	for _, env := range svc.RecentDecisions() {
		if env.Body.Outcome == dari.DecisionDeny {
			denyEnv = env
		}
	}
	if denyEnv == nil {
		t.Fatal("denied exchange did not issue a DENY decision")
	}
	if len(denyEnv.Body.ReasonCodes) == 0 {
		t.Fatal("DENY decision must carry a reason code")
	}
}
