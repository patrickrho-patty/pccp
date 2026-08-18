package relay

import (
	"context"
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// grant_test.go validates the Task 7 live wiring: session-grant
// issuance from a lease, legacy-lease adaptation, and GovernInference's
// grant verification.

func grantTestStack(t *testing.T) (*Service, *models.CapabilityLease, ed25519.PublicKey) {
	t.Helper()
	db := setupGovernedTestDB(t)
	svc, err := New(db, "", "relay-grant-test")
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	// The helper DB is shared in-memory — unique IDs per test.
	suffix := strings.NewReplacer("/", "_", "-", "_").Replace(t.Name())
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-time.Minute).Format(time.RFC3339)
	lease := &models.CapabilityLease{
		OrganizationID: "org-g-" + suffix, LeaseID: "lease-g-" + suffix, SubjectPeerID: "hrn-g-" + suffix,
		UserID: "u-g", SessionID: "sess-g-" + suffix, PolicyEpochID: "epoch-g-" + suffix,
		AllowedModelPackages: `["model-x","model-y"]`,
		NotBefore:            past, NotAfter: future, LeaseSequence: 1, Status: "active",
	}
	if err := db.Create(lease).Error; err != nil {
		t.Fatal(err)
	}
	return svc, lease, pub
}

func TestIssueSessionGrantProducesValidRoot(t *testing.T) {
	svc, lease, pub := grantTestStack(t)
	harnessKey := pub
	env, err := svc.IssueSessionGrant(lease, harnessKey)
	if err != nil {
		t.Fatal(err)
	}
	// Decodes + verifies under the policy key; subject bound to the
	// harness thumbprint.
	decoded, err := dari.DecodeAuthorizationGrant(env.COSEBytes, svc.Policy().SigningPublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Body.SubjectPeerID != lease.SubjectPeerID || decoded.Body.SessionID != lease.SessionID {
		t.Fatalf("bad binding: %+v", decoded.Body)
	}
	if decoded.Body.SubjectKeyThumbprint != dari.SubjectKeyThumbprint(harnessKey) {
		t.Fatal("subject key thumbprint not bound")
	}
	if decoded.Body.DelegationDepth != 3 {
		t.Fatalf("session grant must be delegable, depth %d", decoded.Body.DelegationDepth)
	}
	// Chain-validates as a root.
	if err := dari.ValidateDelegationChain([]*dari.GrantEnvelope{decoded}, dari.ChainContext{NowMs: time.Now().UnixMilli()}); err != nil {
		t.Fatalf("root chain: %v", err)
	}
}

func TestVerifySessionGrantEnforcesBinding(t *testing.T) {
	svc, lease, pub := grantTestStack(t)
	env, err := svc.IssueSessionGrant(lease, pub)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if err := svc.VerifySessionGrant(env, lease.SubjectPeerID, lease.SessionID, "model-x", now); err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}
	// Wrong harness.
	if err := svc.VerifySessionGrant(env, "hrn-other", lease.SessionID, "model-x", now); err == nil {
		t.Fatal("subject mismatch accepted")
	}
	// Wrong session.
	if err := svc.VerifySessionGrant(env, lease.SubjectPeerID, "sess-other", "model-x", now); err == nil {
		t.Fatal("session mismatch accepted")
	}
	// Unauthorized model.
	if err := svc.VerifySessionGrant(env, lease.SubjectPeerID, lease.SessionID, "model-z", now); err == nil {
		t.Fatal("unauthorized model accepted")
	}
	// Outside validity.
	if err := svc.VerifySessionGrant(env, lease.SubjectPeerID, lease.SessionID, "model-x", now+int64(time.Hour)*2); err == nil {
		t.Fatal("expired grant accepted")
	}
}

func TestGovernInferenceVerifiesPresentedGrant(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID     = "org-gov2"
		userID    = "u2"
		harnessID = "hrn-gov2"
		sessionID = "sess-gov2"
		leaseID   = "lease-gov2"
		epochID   = "epoch-gov2"
		modelPkg  = "pmp-gov2"
		modelID   = "patty-gov2"
		endpoint  = "ep-gov2"
		epLease   = "epl-gov2"
	)
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)

	db.Create(&models.Harness{OrganizationID: orgID, HarnessID: harnessID, Status: "enrolled"})
	seedGovernedSession(t, db, orgID, userID, harnessID, sessionID)
	lease := &models.CapabilityLease{
		OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID,
		UserID: userID, SessionID: sessionID, PolicyEpochID: epochID,
		AllowedModelPackages: `["` + modelPkg + `"]`,
		NotBefore:            past, NotAfter: future, LeaseSequence: 1, Status: "active",
	}
	db.Create(lease)
	allowedJSON := `["` + modelPkg + `"]`
	db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: epochID, AllowedModelsJSON: allowedJSON, Status: "active"})
	db.Create(&models.ModelPackage{PackageID: modelPkg, ModelID: modelID, Name: "G2", State: "published"})
	db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: endpoint, ModelPackageID: modelPkg, Status: "active"})
	db.Create(&models.EndpointLease{EndpointID: endpoint, OrganizationID: orgID, ModelPackageID: modelPkg, LeaseID: epLease, NotAfter: future, Status: "active", IssuedAt: past})

	svc, err := New(db, "", "relay-gov2")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetForwarder(func(ctx context.Context, req InferenceRequest, _ string) (*InferenceResponse, error) {
		return &InferenceResponse{Model: req.Model, Usage: map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}, nil
	})

	// Legacy path (no grant): adapter converts the lease; the exchange proceeds.
	if _, _, err := svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: modelID,
		Messages: []map[string]string{{"role": "user", "content": "hi"}},
	}); err != nil {
		t.Fatalf("legacy-adapter path failed: %v", err)
	}

	// Present a VALID grant for the same session.
	harnessPub, _, _ := ed25519.GenerateKey(nil)
	grantEnv, err := svc.IssueSessionGrant(lease, harnessPub)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: modelID, Grant: grantEnv,
		Messages: []map[string]string{{"role": "user", "content": "hi"}},
	}); err != nil {
		t.Fatalf("grant path failed: %v", err)
	}

	// Present a TAMPERED grant — must fail closed.
	tampered := *grantEnv
	tampered.COSEBytes = append([]byte(nil), grantEnv.COSEBytes...)
	tampered.COSEBytes[len(tampered.COSEBytes)-1] ^= 0xFF
	if _, _, err := svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: sessionID, Model: modelID, Grant: &tampered,
		Messages: []map[string]string{{"role": "user", "content": "hi"}},
	}); err == nil {
		t.Fatal("tampered grant accepted")
	}
}

func TestDecodeLegacyCapabilityLeaseNonDelegable(t *testing.T) {
	_, lease, _ := grantTestStack(t)
	body, err := DecodeLegacyCapabilityLease(lease)
	if err != nil {
		t.Fatal(err)
	}
	if body.DelegationDepth != 0 {
		t.Fatal("legacy lease view must be non-delegable")
	}
	if body.SubjectPeerID != lease.SubjectPeerID {
		t.Fatalf("bad subject: %s", body.SubjectPeerID)
	}
}
