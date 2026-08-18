package keymgmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProviderFromEnvironmentUsesSharedFileConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alert.kek")
	fingerprintPath := filepath.Join(t.TempDir(), "alert.fingerprint")
	if err := os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fingerprintPath, []byte("fedcba9876543210fedcba9876543210"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		AlertKEKFileEnv:            path,
		AlertKEKIDEnv:              "alert-prod-v1",
		AlertFingerprintKeyFileEnv: fingerprintPath,
	}
	provider, configured, err := LoadProviderFromEnvironment(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !configured || provider == nil || provider.KEKID() != "alert-prod-v1" {
		t.Fatalf("unexpected provider: configured=%v provider=%v", configured, provider)
	}
}

func TestLoadProviderFromEnvironmentDistinguishesMissingAndInvalid(t *testing.T) {
	if provider, configured, err := LoadProviderFromEnvironment(func(string) string { return "" }); err != nil || configured || provider != nil {
		t.Fatalf("missing config should be non-error disabled state: provider=%v configured=%v err=%v", provider, configured, err)
	}
	env := map[string]string{AlertKEKFileEnv: "/does/not/exist", AlertKEKIDEnv: "id", AlertFingerprintKeyFileEnv: "/also/missing"}
	if _, configured, err := LoadProviderFromEnvironment(func(key string) string { return env[key] }); err == nil || !configured {
		t.Fatalf("invalid configured provider must fail: configured=%v err=%v", configured, err)
	}
}

func TestLoadProviderRequiresDedicatedFingerprintKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alert.kek")
	if err := os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{AlertKEKFileEnv: path, AlertKEKIDEnv: "alert-prod-v1"}
	if _, configured, err := LoadProviderFromEnvironment(func(key string) string { return env[key] }); err == nil || !configured {
		t.Fatalf("configured alert encryption must require a stable fingerprint key: configured=%v err=%v", configured, err)
	}
}

func TestCredentialIDUsesStableDedicatedKeyAcrossKEKRotation(t *testing.T) {
	oldProvider, _ := NewLocalProvider([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), "old")
	newProvider, _ := NewLocalProvider([]byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), "new")
	fingerprintKey := []byte("cccccccccccccccccccccccccccccccc")
	oldRing, err := NewProviderRingWithFingerprintKey(oldProvider, fingerprintKey)
	if err != nil {
		t.Fatal(err)
	}
	newRing, err := NewProviderRingWithFingerprintKey(newProvider, fingerprintKey, oldProvider)
	if err != nil {
		t.Fatal(err)
	}
	oldID, err := CredentialID(oldRing, "https://example.com/private-token")
	if err != nil {
		t.Fatal(err)
	}
	newID, err := CredentialID(newRing, "https://example.com/private-token")
	if err != nil {
		t.Fatal(err)
	}
	if oldID != newID || !strings.HasPrefix(oldID, "hm:") {
		t.Fatalf("credential id must be keyed and stable across KEK rotation: %q %q", oldID, newID)
	}
	otherRing, _ := NewProviderRingWithFingerprintKey(newProvider, []byte("dddddddddddddddddddddddddddddddd"))
	otherID, _ := CredentialID(otherRing, "https://example.com/private-token")
	if otherID == oldID {
		t.Fatal("different fingerprint keys must not produce the same credential id")
	}
}

func TestProviderRingDecryptsRetainedKeyAndEncryptsWithPrimary(t *testing.T) {
	oldProvider, _ := NewLocalProvider([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), "old")
	newProvider, _ := NewLocalProvider([]byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), "new")
	ring, err := NewProviderRing(newProvider, oldProvider)
	if err != nil {
		t.Fatal(err)
	}
	encoded, oldID, err := SealEncoded(oldProvider, "secret")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenEncoded(ring, encoded, oldID)
	if err != nil || opened != "secret" {
		t.Fatalf("retained KEK unavailable: opened=%q err=%v", opened, err)
	}
	_, currentID, err := SealEncoded(ring, "new-secret")
	if err != nil || currentID != "new" {
		t.Fatalf("new writes must use primary KEK: id=%q err=%v", currentID, err)
	}
}
