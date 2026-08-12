package sovereign

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestTrustBundleImport(t *testing.T) {
	svc := New()

	pub, _, _ := ed25519.GenerateKey(nil)
	bundle, err := svc.ImportTrustBundle(TrustBundle{
		OrganizationID:   "org-1",
		LocalCAIdentity:  "local-ca",
		LocalCAPublicKey: hex.EncodeToString(pub),
		ModelSigningKeys: []string{hex.EncodeToString(pub)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ImportedAt == "" {
		t.Fatal("expected imported_at")
	}
}

func TestOfflineUpdateImport(t *testing.T) {
	svc := New()
	update, err := svc.ImportUpdate(OfflineUpdate{
		Version: "1.0.1",
		Type:    "server",
		Hash:    "sha256:abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if update.Status != "imported" {
		t.Fatal("expected imported status")
	}
}

func TestOfflineUpdateApply(t *testing.T) {
	svc := New()
	update, _ := svc.ImportUpdate(OfflineUpdate{
		Version: "1.0.2",
		Type:    "pia",
		Hash:    "sha256:def456",
	})

	err := svc.ApplyUpdate(update.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Re-apply should fail
	err = svc.ApplyUpdate(update.ID)
	if err == nil {
		t.Fatal("re-applying should fail")
	}
}

func TestTimeProofGeneration(t *testing.T) {
	svc := New()
	proof := svc.GenerateTimeProof("org-1")
	if proof.Hash == "" {
		t.Fatal("expected hash")
	}

	// Verify
	if !svc.VerifyTimeProof(proof, "org-1") {
		t.Fatal("proof verification failed")
	}

	// Tampered proof should fail
	proof.Counter++
	if svc.VerifyTimeProof(proof, "org-1") {
		t.Fatal("tampered proof should fail")
	}
}

func TestModelSignatureVerification(t *testing.T) {
	svc := New()

	pub, priv, _ := ed25519.GenerateKey(nil)
	pubHex := hex.EncodeToString(pub)

	svc.ImportTrustBundle(TrustBundle{
		OrganizationID:   "org-1",
		LocalCAPublicKey: pubHex,
		ModelSigningKeys: []string{pubHex},
	})

	digest := "sha256:model_hash_123"
	sig := ed25519.Sign(priv, []byte(digest))

	// Valid signature
	err := svc.VerifyModelSignature("org-1", digest, sig)
	if err != nil {
		t.Fatalf("expected valid: %v", err)
	}

	// Invalid signature
	err = svc.VerifyModelSignature("org-1", "different_digest", sig)
	if err == nil {
		t.Fatal("expected verification failure")
	}
}

func TestRevocationCheck(t *testing.T) {
	svc := New()
	pub, _, _ := ed25519.GenerateKey(nil)
	svc.ImportTrustBundle(TrustBundle{
		OrganizationID:   "org-1",
		LocalCAPublicKey: hex.EncodeToString(pub),
		RevocationList:   []string{"key-revoked-1", "key-revoked-2"},
	})

	revoked, _ := svc.CheckRevocation("org-1", "key-revoked-1")
	if !revoked {
		t.Fatal("expected key to be revoked")
	}

	revoked, _ = svc.CheckRevocation("org-1", "key-valid-1")
	if revoked {
		t.Fatal("expected key to be valid")
	}
}
