package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// fakeCredConn records Close calls for revocation-propagation tests.
type fakeCredConn struct {
	closed chan struct{}
	once   chan struct{}
}

func newFakeCredConn() *fakeCredConn {
	return &fakeCredConn{closed: make(chan struct{}, 1), once: make(chan struct{}, 1)}
}

func (f *fakeCredConn) Close() error {
	select {
	case f.closed <- struct{}{}:
	default:
	}
	return nil
}

// TestRevokeHarnessTerminatesActiveStreams is Task 6 Step 3's required
// evidence: revoking a harness in the control plane propagates to the
// attached listener and terminates the revoked credential's active
// transport; a different credential's stream survives.
func TestRevokeHarnessTerminatesActiveStreams(t *testing.T) {
	db := setupGovernedTestDB(t)
	svc, err := New(db, "", "relay-revoke-test")
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}
	const (
		orgID     = "org-rev-1"
		harnessID = "hrn-rev-1"
	)

	// Enroll the harness so a revocable credential serial exists.
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Identity().EnrollHarness(identity.EnrollHarnessRequest{
		OrganizationID: orgID,
		HarnessID:      harnessID,
		PublicKeyHex:   hex.EncodeToString(pub),
		BinaryVersion:  "test",
		BinaryHash:     "test",
	})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	_ = priv
	credHex, err := svc.Identity().CredentialHexForHarness(harnessID)
	if err != nil {
		t.Fatal(err)
	}
	serial := credentialSerialOf(t, credHex)

	listener := NewDARIListener(svc, nil, TrustBundle{RevokedSerials: map[string]uint64{}})
	svc.AttachDARIListener(listener)

	// Two authenticated connections: the revoked one and an innocent one.
	victim := newFakeCredConn()
	other := newFakeCredConn()
	listener.trackAuthenticatedConnection("conn-victim", serial, victim)
	listener.trackAuthenticatedConnection("conn-other", "serial-other", other)

	if err := svc.RevokeHarness(orgID, harnessID, "test revocation"); err != nil {
		t.Fatalf("RevokeHarness: %v", err)
	}

	select {
	case <-victim.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("revoked credential's stream was not terminated")
	}
	select {
	case <-other.closed:
		t.Fatal("unrelated stream must NOT be terminated")
	default:
	}

	// The revocation view now rejects the serial at connect time.
	if !listener.authenticator.isRevoked(serial) {
		t.Fatal("serial must be revoked in the listener's view")
	}
	epoch, serials := svc.Identity().RevocationSnapshot()
	if serials[serial] == 0 {
		t.Fatal("identity snapshot must carry the revoked serial")
	}
	if listener.authenticator.RevocationEpoch() < epoch {
		t.Fatal("listener epoch must advance to the authoritative snapshot")
	}
}

// TestRevokeHarnessUnknownHarnessFailsClosed covers the error path.
func TestRevokeHarnessUnknownHarnessFailsClosed(t *testing.T) {
	db := setupGovernedTestDB(t)
	svc, err := New(db, "", "relay-revoke-neg")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeHarness("org-none", "hrn-none", "x"); err == nil {
		t.Fatal("revoking an unknown harness must fail")
	}
	_ = context.Background()
	_ = models.Harness{}
}

// credentialSerialOf extracts the credential serial the way the
// identity service does (decode COSE → read the serial field).
func credentialSerialOf(t *testing.T, credHex string) string {
	t.Helper()
	raw, err := hex.DecodeString(credHex)
	if err != nil || len(raw) == 0 {
		t.Fatalf("bad credential hex: %v", err)
	}
	sign1, err := dari.DecodeCOSESign1(raw)
	if err != nil {
		t.Fatalf("decode COSE: %v", err)
	}
	cred, err := dari.DecodePeerCredential(sign1.Payload)
	if err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	return cred.Serial
}
