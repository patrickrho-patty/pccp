package scheduler

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildTrustFromHex(t *testing.T) {
	_, caPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	caPub := caPriv.Public().(ed25519.PublicKey)
	_, cfgPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	cfgPub := cfgPriv.Public().(ed25519.PublicKey)

	trust, err := BuildTrust(hex.EncodeToString(caPub), hex.EncodeToString(cfgPub), "test-ca")
	if err != nil {
		t.Fatalf("build trust: %v", err)
	}
	issuer, ok := trust.Issuers["test-ca"]
	if !ok || !issuer.Equal(caPub) {
		t.Fatal("CA issuer key not in trust bundle")
	}
	if !trust.ConfigPubKey.Equal(cfgPub) {
		t.Fatal("config pubkey mismatch")
	}
	if trust.Now == nil {
		t.Fatal("Now must default")
	}
}

func TestBuildTrustRejectsBadHex(t *testing.T) {
	if _, err := BuildTrust("not-hex", "00", "test-ca"); err == nil {
		t.Fatal("expected error for invalid CA hex")
	}
}

func TestFilePolicyLoadsAndAnswers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	data, err := json.Marshal(map[string]string{"tenant-a": "localhost", "tenant-b": "private"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	policy, err := LoadPolicyFile(path)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	if grade, ok := policy.MinReachability("tenant-a"); !ok || grade != "localhost" {
		t.Fatalf("tenant-a policy %q/%v", grade, ok)
	}
	if _, ok := policy.MinReachability("unknown"); ok {
		t.Fatal("unknown tenant must have no policy")
	}
}

func TestFilePolicyMissingFile(t *testing.T) {
	if _, err := LoadPolicyFile(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing policy file")
	}
}
