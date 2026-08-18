package relay

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// TestGovernInferenceRefusesClosedSession is the web/02 B3 gate: a
// session the control plane closed/paused must not keep exchanging,
// even with an enrolled harness and a live lease.
func TestGovernInferenceRefusesClosedSession(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID     = "org-sess-1"
		userID    = "u-sess-1"
		harnessID = "hrn-sess-1"
		leaseID   = "lease-sess-1"
		epochID   = "epoch-sess-1"
		modelPkg  = "pmp-sess-1"
		modelID   = "patty-sess"
		endpoint  = "ep-sess-1"
		epLease   = "eplease-sess-1"
	)
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	db.Create(&models.Harness{OrganizationID: orgID, HarnessID: harnessID, Status: "enrolled"})
	db.Create(&models.CapabilityLease{
		OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harnessID,
		UserID: userID, SessionID: "sess-closed", PolicyEpochID: epochID,
		NotBefore: past, NotAfter: future, Status: "active",
	})
	allowedJSON, _ := json.Marshal([]string{modelPkg})
	db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: epochID, AllowedModelsJSON: string(allowedJSON), Status: "active"})
	db.Create(&models.ModelPackage{PackageID: modelPkg, ModelID: modelID, Name: "S", State: "published"})
	db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: endpoint, ModelPackageID: modelPkg, Status: "active"})
	db.Create(&models.EndpointLease{EndpointID: endpoint, OrganizationID: orgID, ModelPackageID: modelPkg, LeaseID: epLease, NotAfter: future, Status: "active", IssuedAt: past})

	// The CP closed the session.
	db.Create(&models.Session{
		AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: orgID},
		HarnessID: harnessID, UserID: userID, SessionID: "sess-closed", Status: "closed",
	})
	// And paused another.
	db.Create(&models.Session{
		AuditBase: models.AuditBase{Base: models.Base{}, OrganizationID: orgID},
		HarnessID: harnessID, UserID: userID, SessionID: "sess-paused", Status: "paused",
	})

	svc, err := New(db, "", "relay-sess-test")
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}

	_, _, err = svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: "sess-closed", Model: modelID,
		Messages: []map[string]string{{"role": "user", "content": "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed session must refuse inference naming its status, got %v", err)
	}

	_, _, err = svc.GovernInference(context.Background(), GovernRequest{
		HarnessID: harnessID, SessionID: "sess-paused", Model: modelID,
		Messages: []map[string]string{{"role": "user", "content": "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("paused session must refuse inference naming its status, got %v", err)
	}
}

func TestGovernInferenceRequiresCanonicalActiveSessionBinding(t *testing.T) {
	db := setupGovernedTestDB(t)
	db.Create(&models.Harness{OrganizationID: "org-a", HarnessID: "hrn-a", Status: "enrolled"})
	db.Create(&models.Harness{OrganizationID: "org-a", HarnessID: "hrn-b", Status: "enrolled"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-a"}, HarnessID: "hrn-b", UserID: "user", SessionID: "wrong-harness", Status: "active"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-b"}, HarnessID: "hrn-a", UserID: "user", SessionID: "foreign-org", Status: "active"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-a"}, HarnessID: "hrn-a", UserID: "user", SessionID: "pending", Status: "pending"})

	svc, err := New(db, "", "relay-session-binding-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		session string
		want    string
	}{
		{"missing", "not registered for organization"},
		{"foreign-org", "not registered for organization"},
		{"wrong-harness", "not bound to harness"},
		{"pending", "pending"},
	} {
		_, _, err := svc.GovernInference(context.Background(), GovernRequest{HarnessID: "hrn-a", SessionID: tc.session, Model: "model"})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("session %q error = %v, want %q", tc.session, err, tc.want)
		}
	}
}
