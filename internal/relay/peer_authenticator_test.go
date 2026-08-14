package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testRevocationEpoch uint64 = 7

type signedProofTestFixture struct {
	proof *dari.AuthProofMessage
	trust TrustBundle
	cred  *dari.PeerCredential
}

func signedProofFixture(t *testing.T, transcript []byte) (*dari.AuthProofMessage, TrustBundle) {
	t.Helper()
	fixture := newSignedProofTestFixture(t, transcript, time.Hour)
	return fixture.proof, fixture.trust
}

func newSignedProofTestFixture(t *testing.T, transcript []byte, validity time.Duration) signedProofTestFixture {
	t.Helper()

	issuer, err := dari.NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatalf("create issuer: %v", err)
	}
	subjectPublic, subjectPrivate, err := dari.GenerateKeyPair()
	if err != nil {
		t.Fatalf("create subject key: %v", err)
	}
	cred, err := issuer.Issue(dari.IssueRequest{
		SubjectPeerID:           "hrn-auth-test",
		Organization:            "org-auth-test",
		Profile:                 dari.ProfileHarness,
		PublicKey:               subjectPublic,
		Validity:                validity,
		RevocationAuthority:     "test-ca",
		AllowedProtocolVersions: []uint8{1},
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	if validity <= 0 {
		cred.NotBefore = time.Now().Add(-2 * time.Hour).UnixMilli()
		cred.NotAfter = time.Now().Add(-time.Hour).UnixMilli()
	}

	credentialHex, err := cred.SignWith(issuer.PrivateKey)
	if err != nil {
		t.Fatalf("sign credential: %v", err)
	}
	credential, err := hex.DecodeString(credentialHex)
	if err != nil {
		t.Fatalf("decode signed credential: %v", err)
	}
	challengeID := []byte("challenge-auth-test")
	proof := &dari.AuthProofMessage{
		Credential:         credential,
		KeyAlgorithm:       dari.COSEAlgEdDSA,
		ChallengeID:        challengeID,
		RevocationEvidence: dari.EncodeRevocationEpoch(testRevocationEpoch),
	}
	proof.Signature = ed25519.Sign(subjectPrivate, dari.PeerProofSigningBytes(
		transcript,
		proof.ChallengeID,
		testRevocationEpoch,
	))

	return signedProofTestFixture{
		proof: proof,
		trust: TrustBundle{
			Issuers:         map[string]ed25519.PublicKey{"test-ca": issuer.PublicKey},
			ProtocolVersion: 1,
			RevocationEpoch: testRevocationEpoch,
			RevokedSerials:  map[string]uint64{},
		},
		cred: cred,
	}
}

func TestPeerCredentialVerifySignatureRejectsDifferentSignedPayload(t *testing.T) {
	issuer, err := dari.NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := dari.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	issued, err := issuer.Issue(dari.IssueRequest{
		SubjectPeerID:           "hrn-signed",
		Organization:            "org-test",
		Profile:                 dari.ProfileHarness,
		PublicKey:               pub,
		Validity:                time.Hour,
		RevocationAuthority:     "test-ca",
		AllowedProtocolVersions: []uint8{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	signature, err := issued.SignWith(issuer.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	presented := *issued
	presented.SubjectPeerID = "hrn-attacker"
	if err := presented.VerifySignature(issuer.PublicKey, signature); err == nil {
		t.Fatal("valid signature over a different credential payload accepted")
	}
}

func TestPeerAuthenticatorAcceptsValidProof(t *testing.T) {
	transcript := []byte("complete negotiated transcript")
	proof, trust := signedProofFixture(t, transcript)
	verifier := NewPeerAuthenticator(trust)

	cred, err := verifier.VerifyPeerProof(context.Background(), transcript, proof)
	if err != nil {
		t.Fatalf("valid proof rejected: %v", err)
	}
	if cred.SubjectPeerID != "hrn-auth-test" {
		t.Fatalf("authenticated peer = %q, want hrn-auth-test", cred.SubjectPeerID)
	}
}

func TestPeerAuthenticatorRejectsTamperedProof(t *testing.T) {
	transcript := []byte("complete negotiated transcript")
	proof, trust := signedProofFixture(t, transcript)
	proof.Signature[0] ^= 0xff

	if _, err := NewPeerAuthenticator(trust).VerifyPeerProof(context.Background(), transcript, proof); err == nil {
		t.Fatal("tampered proof accepted")
	}
}

func TestPeerAuthenticatorRejectsAnotherTranscript(t *testing.T) {
	proof, trust := signedProofFixture(t, []byte("transcript-a"))
	verifier := NewPeerAuthenticator(trust)
	if _, err := verifier.VerifyPeerProof(context.Background(), []byte("transcript-b"), proof); err == nil {
		t.Fatal("replayed proof accepted")
	}
}

func TestPeerAuthenticatorRejectsExpiredCredential(t *testing.T) {
	transcript := []byte("expired credential transcript")
	fixture := newSignedProofTestFixture(t, transcript, -time.Hour)

	if _, err := NewPeerAuthenticator(fixture.trust).VerifyPeerProof(context.Background(), transcript, fixture.proof); err == nil {
		t.Fatal("expired credential accepted")
	}
}

func TestPeerAuthenticatorRejectsRevokedSerial(t *testing.T) {
	transcript := []byte("revoked credential transcript")
	fixture := newSignedProofTestFixture(t, transcript, time.Hour)
	fixture.trust.RevokedSerials[fixture.cred.Serial] = testRevocationEpoch

	if _, err := NewPeerAuthenticator(fixture.trust).VerifyPeerProof(context.Background(), transcript, fixture.proof); err == nil {
		t.Fatal("revoked credential accepted")
	}
}

func TestPeerAuthenticatorRejectsStaleRevocationEpoch(t *testing.T) {
	transcript := []byte("stale epoch transcript")
	proof, trust := signedProofFixture(t, transcript)
	trust.RevocationEpoch++

	if _, err := NewPeerAuthenticator(trust).VerifyPeerProof(context.Background(), transcript, proof); err == nil {
		t.Fatal("proof from stale revocation epoch accepted")
	}
}

func TestValidatePeerProfileRejectsNegotiatedProfileMismatch(t *testing.T) {
	credential := &dari.PeerCredential{PeerProfile: dari.ProfileInference}
	if err := validatePeerProfile(dari.ProfileHarness, credential); err == nil {
		t.Fatal("credential profile different from negotiated HELLO profile accepted")
	}
}

type recordingCredentialConn struct {
	closed chan struct{}
}

func (c *recordingCredentialConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func TestDARIListenerTerminatesConnectionForRevokedSerial(t *testing.T) {
	listener := NewDARIListener(nil, nil, TrustBundle{RevocationEpoch: testRevocationEpoch})
	conn := &recordingCredentialConn{closed: make(chan struct{})}
	listener.trackAuthenticatedConnection("conn-1", "serial-1", conn)

	listener.RevokeCredential("serial-1", testRevocationEpoch+1)

	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("revoked credential connection was not closed")
	}
	if got := listener.ActiveConnections(); got != 0 {
		t.Fatalf("active connections = %d, want 0", got)
	}
}

func TestDARIListenerRejectsConnectionRevokedBeforeRegistration(t *testing.T) {
	listener := NewDARIListener(nil, nil, TrustBundle{RevocationEpoch: testRevocationEpoch})
	listener.RevokeCredential("serial-before-track", testRevocationEpoch+1)
	conn := &recordingCredentialConn{closed: make(chan struct{})}

	if listener.trackAuthenticatedConnection("conn-late", "serial-before-track", conn) {
		t.Fatal("connection registered after its credential was revoked")
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("connection rejected during registration was not closed")
	}
}

func TestIdentityRevocationRecordsSerialAndTerminatesSessions(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "identity-revocation.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.Harness{}, &models.Session{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	issuer, err := dari.NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	subjectPublic, _, err := dari.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	credential, err := issuer.Issue(dari.IssueRequest{
		SubjectPeerID:           "hrn-revoke",
		Organization:            "org-revoke",
		Profile:                 dari.ProfileHarness,
		PublicKey:               subjectPublic,
		Validity:                time.Hour,
		RevocationAuthority:     "test-ca",
		AllowedProtocolVersions: []uint8{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.Harness{
		HarnessID:      "hrn-revoke",
		OrganizationID: "org-revoke",
		Status:         "enrolled",
		PublicKey:      hex.EncodeToString(subjectPublic),
		CredentialJSON: hex.EncodeToString(credential.SignedCredential),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.Session{
		SessionID: "ses-revoke",
		HarnessID: "hrn-revoke",
		Status:    "active",
	}).Error; err != nil {
		t.Fatal(err)
	}

	service, err := identity.New(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeHarness("org-revoke", "hrn-revoke", "test"); err != nil {
		t.Fatalf("revoke harness: %v", err)
	}
	epoch, revoked := service.RevocationSnapshot()
	if epoch == 0 || revoked[credential.Serial] != epoch {
		t.Fatalf("revocation snapshot epoch=%d serials=%v", epoch, revoked)
	}
	var session models.Session
	if err := database.Where("session_id = ?", "ses-revoke").First(&session).Error; err != nil {
		t.Fatal(err)
	}
	if session.Status != "terminated" {
		t.Fatalf("session status = %q, want terminated", session.Status)
	}
}
