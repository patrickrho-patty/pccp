package relay

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/provenance"
)

func issueReqFor(t *testing.T) provenance.IssueReceiptRequest {
	t.Helper()
	return provenance.IssueReceiptRequest{
		OrganizationID: "org-c", ExchangeID: "exch-c", SessionID: "s",
		FinalState: "ACTIVE", LastEventSeq: 2,
		ChainRoot:     hex.EncodeToString(make([]byte, 32)),
		PolicyEpochID: "ep-c", ModelPackageID: "pmp-c", EndpointID: "e",
	}
}

// TestReceiptCloseExchangeVerifiesConnectorSide: the REAL close path
// (issue → wire) verified with an inline copy of the connector's
// verifier (nested provenancewire.VerifyEvidenceReceiptSignature).
func TestReceiptCloseExchangeVerifiesConnectorSide(t *testing.T) {
	db := setupGovernedTestDB(t)
	const (
		orgID    = "org-cv"
		harness  = "h-cv"
		sess     = "sess-cv"
		leaseID  = "lease-cv"
		epochID  = "epoch-cv"
		modelPkg = "pmp-cv"
		endpoint = "ep-cv"
	)
	future := timeNowRFCPlus(time.Hour)
	past := timeNowRFCPlus(-time.Hour)
	db.Create(&models.Harness{OrganizationID: orgID, HarnessID: harness, Status: "enrolled"})
	db.Create(&models.CapabilityLease{OrganizationID: orgID, LeaseID: leaseID, SubjectPeerID: harness,
		UserID: "u", SessionID: sess, PolicyEpochID: epochID,
		AllowedModelPackages: `["` + modelPkg + `"]`, NotBefore: past, NotAfter: future, Status: "active"})
	db.Create(&models.PolicyEpoch{OrganizationID: orgID, EpochID: epochID, AllowedModelsJSON: `["` + modelPkg + `"]`, Status: "active"})
	db.Create(&models.ModelPackage{PackageID: modelPkg, ModelID: "m-cv", Name: "CV", State: "published"})
	db.Create(&models.InferenceEndpoint{OrganizationID: orgID, EndpointID: endpoint, ModelPackageID: modelPkg, Status: "active"})
	db.Create(&models.EndpointLease{EndpointID: endpoint, OrganizationID: orgID, ModelPackageID: modelPkg, LeaseID: "epl-cv", NotAfter: future, Status: "active", IssuedAt: past})

	svc, err := New(db, "", "relay-cv")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ex, _, err := svc.OpenExchange(ctx, OpenExchangeRequest{
		OrganizationID: orgID, SessionID: sess, UserID: "u", HarnessID: harness,
		LeaseID: leaseID, PolicyEpochID: epochID, ModelPackageID: modelPkg,
	})
	if err != nil {
		t.Fatal(err)
	}
	appendEvidence(ex, "decision|x")
	receipt, err := svc.CloseExchange(ctx, ex.ID)
	if err != nil {
		t.Fatal(err)
	}

	wire := buildWireEvidenceReceipt(receipt)
	// Inline copy of the connector verifier (COSE envelope + payload
	// binding + Sig_structure verification).
	raw, _ := hex.DecodeString(wire.Signature)
	sign1, err := dari.DecodeCOSESign1(raw)
	if err != nil {
		t.Fatalf("decode COSE: %v", err)
	}
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d",
		wire.ExchangeID, wire.FinalState, hex.EncodeToString(wire.ChainRoot[:]),
		wire.RelayIdentity, wire.PolicyEpochID, wire.ModelPackageID, wire.IssuedAtUnixMs)
	if string(sign1.Payload) != data {
		t.Fatalf("COSE payload not bound to receipt fields:\npayload=%q\ndata   =%q", sign1.Payload, data)
	}
	if err := dari.VerifyCOSESign1(sign1, svc.Provenance().SigningPublicKey()); err != nil {
		t.Fatalf("connector verification failed: %v", err)
	}
	t.Log("close-path receipt verifies connector-side")
}

func timeNowRFCPlus(d time.Duration) string { return time.Now().Add(d).Format(time.RFC3339) }
