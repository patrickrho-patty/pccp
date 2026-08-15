package dari

import (
	"testing"
	"time"
)

func TestPeerCredentialSignAndVerify(t *testing.T) {
	issuer, err := NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatal(err)
	}

	pub, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	cred, err := issuer.Issue(IssueRequest{
		SubjectPeerID:           "hrn_test_001",
		Organization:            "org_test",
		Profile:                 ProfileHarness,
		PublicKey:               pub,
		Validity:                24 * time.Hour,
		RevocationAuthority:     "test-ca",
		AllowedProtocolVersions: []uint8{1},
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}

	// Sign the credential
	sigHex, err := cred.SignWith(issuer.PrivateKey)
	if err != nil {
		t.Fatalf("sign credential: %v", err)
	}
	if sigHex == "" {
		t.Fatal("expected non-empty signature")
	}

	// Verify with correct issuer key
	err = cred.VerifySignature(issuer.PublicKey, sigHex)
	if err != nil {
		t.Fatalf("verify with correct key failed: %v", err)
	}

	// Verify with wrong key should fail
	wrongPub, _, _ := GenerateKeyPair()
	err = cred.VerifySignature(wrongPub, sigHex)
	if err == nil {
		t.Fatal("verification should fail with wrong key")
	}

	// SigningBytes should return non-nil
	sb := cred.SigningBytes()
	if sb == nil {
		t.Fatal("SigningBytes should return non-nil")
	}
}
