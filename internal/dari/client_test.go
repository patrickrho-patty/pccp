package dari

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func TestDARIClientConfig(t *testing.T) {
	pub, priv, _ := GenerateKeyPair()

	// Create a PPC for the client
	issuer, _ := NewPeerCredentialIssuer("test-ca")
	cred, _ := issuer.Issue(IssueRequest{
		SubjectPeerID:           "hrn_client_test",
		Organization:            "org_test",
		Profile:                 ProfileHarness,
		PublicKey:               pub,
		Validity:                time.Hour,
		RevocationAuthority:     "test-ca",
		AllowedProtocolVersions: []uint8{1},
	})

	cfg := ClientConfig{
		PeerID:         "hrn_client_test",
		OrganizationID: "org_test",
		PrivateKey:     priv,
		Credential:     cred,
		Profile:        ProfileHarness,
	}

	// Verify the client config
	if cfg.PeerID != "hrn_client_test" {
		t.Fatal("peer ID mismatch")
	}
	if cfg.Profile != ProfileHarness {
		t.Fatal("profile mismatch")
	}

	// Verify credential signing works for the client
	sig, err := cred.SignWith(issuer.PrivateKey)
	if err != nil {
		t.Fatalf("sign credential: %v", err)
	}
	err = cred.VerifySignature(issuer.PublicKey, sig)
	if err != nil {
		t.Fatalf("verify credential: %v", err)
	}
}

func TestDARIClientPublicKey(t *testing.T) {
	pub, priv, _ := GenerateKeyPair()
	_ = pub

	// Simulate what the client does
	pubFromPriv := priv.Public().(ed25519.PublicKey)
	pubHex := hexEncode(pubFromPriv)
	if len(pubHex) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64 hex chars, got %d", len(pubHex))
	}
}
