package keymgmt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	AlertKEKFileEnv            = "PCCP_ALERT_KEK_FILE"
	AlertKEKIDEnv              = "PCCP_ALERT_KEK_ID"
	AlertOldKEKsEnv            = "PCCP_ALERT_OLD_KEKS"
	AlertFingerprintKeyFileEnv = "PCCP_ALERT_FINGERPRINT_KEY_FILE"
)

// ProviderRing encrypts with the primary provider and decrypts envelopes made
// with any retained provider. This permits zero-downtime KEK rotation.
type ProviderRing struct {
	primary        KeyProvider
	byID           map[string]KeyProvider
	fingerprintKey []byte
}

func NewProviderRing(primary KeyProvider, retained ...KeyProvider) (*ProviderRing, error) {
	key, err := derivedFingerprintKey(primary)
	if err != nil {
		return nil, err
	}
	return newProviderRing(primary, key, retained...)
}

// NewProviderRingWithFingerprintKey keeps alert credential correlation stable
// across KEK rotation. The fingerprint key is independent from envelope keys
// and must be retained for the lifetime of stored alert endpoints.
func NewProviderRingWithFingerprintKey(primary KeyProvider, fingerprintKey []byte, retained ...KeyProvider) (*ProviderRing, error) {
	if len(fingerprintKey) != 32 {
		return nil, fmt.Errorf("keymgmt: alert fingerprint key must be 32 bytes")
	}
	return newProviderRing(primary, fingerprintKey, retained...)
}

func newProviderRing(primary KeyProvider, fingerprintKey []byte, retained ...KeyProvider) (*ProviderRing, error) {
	if primary == nil {
		return nil, fmt.Errorf("keymgmt: primary provider is required")
	}
	ring := &ProviderRing{primary: primary, byID: map[string]KeyProvider{primary.KEKID(): primary}, fingerprintKey: append([]byte(nil), fingerprintKey...)}
	for _, provider := range retained {
		if provider == nil || strings.TrimSpace(provider.KEKID()) == "" {
			return nil, fmt.Errorf("keymgmt: invalid retained provider")
		}
		if _, exists := ring.byID[provider.KEKID()]; exists {
			return nil, fmt.Errorf("keymgmt: duplicate KEK id %q", provider.KEKID())
		}
		ring.byID[provider.KEKID()] = provider
	}
	return ring, nil
}

func (r *ProviderRing) KEKID() string                         { return r.primary.KEKID() }
func (r *ProviderRing) GetKEK() ([]byte, error)               { return r.primary.GetKEK() }
func (r *ProviderRing) WrapKey(dek []byte) ([]byte, error)    { return r.primary.WrapKey(dek) }
func (r *ProviderRing) UnwrapKey(data []byte) ([]byte, error) { return r.primary.UnwrapKey(data) }
func (r *ProviderRing) ProviderForKEK(id string) (KeyProvider, error) {
	provider, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("keymgmt: provider for KEK %q is not configured", id)
	}
	return provider, nil
}

func (r *ProviderRing) CredentialID(plaintext string) (string, error) {
	return hmacCredentialID(r.fingerprintKey, plaintext), nil
}

func derivedFingerprintKey(provider KeyProvider) ([]byte, error) {
	if provider == nil {
		return nil, fmt.Errorf("keymgmt: primary provider is required")
	}
	kek, err := provider.GetKEK()
	if err != nil {
		return nil, fmt.Errorf("keymgmt: derive local fingerprint key: %w", err)
	}
	defer clear(kek)
	mac := hmac.New(sha256.New, kek)
	_, _ = mac.Write([]byte("DARI-ALERT-FINGERPRINT-KEY-v1"))
	return mac.Sum(nil), nil
}

func hmacCredentialID(key []byte, plaintext string) string {
	value := strings.TrimSpace(plaintext)
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("DARI-ALERT-CREDENTIAL-v2\x00"))
	_, _ = mac.Write([]byte(value))
	return "hm:" + hex.EncodeToString(mac.Sum(nil))
}

// LoadProviderFromEnvironment constructs the alert-secret provider used by
// both the API process and the backfill command. The boolean distinguishes an
// intentionally unconfigured deployment from a configured provider that
// failed to initialize; callers must not silently downgrade the latter.
func LoadProviderFromEnvironment(getenv func(string) string) (KeyProvider, bool, error) {
	path := strings.TrimSpace(getenv(AlertKEKFileEnv))
	kekID := strings.TrimSpace(getenv(AlertKEKIDEnv))
	fingerprintPath := strings.TrimSpace(getenv(AlertFingerprintKeyFileEnv))
	if path == "" && kekID == "" && fingerprintPath == "" {
		return nil, false, nil
	}
	if path == "" || kekID == "" || fingerprintPath == "" {
		return nil, true, fmt.Errorf("keymgmt: %s, %s, and %s must be configured together", AlertKEKFileEnv, AlertKEKIDEnv, AlertFingerprintKeyFileEnv)
	}
	provider, err := LoadLocalProviderFile(path, kekID)
	if err != nil {
		return nil, true, err
	}
	fingerprintKey, err := loadOwnerOnlyKeyFile(fingerprintPath, "fingerprint")
	if err != nil {
		return nil, true, err
	}
	defer clear(fingerprintKey)
	retained, err := loadRetainedProviders(getenv(AlertOldKEKsEnv))
	if err != nil {
		return nil, true, err
	}
	ring, err := NewProviderRingWithFingerprintKey(provider, fingerprintKey, retained...)
	return ring, true, err
}

// PCCP_ALERT_OLD_KEKS is a comma-separated id=/absolute/key/file list.
func loadRetainedProviders(raw string) ([]KeyProvider, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var providers []KeyProvider
	for _, entry := range strings.Split(raw, ",") {
		id, path, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok || strings.TrimSpace(id) == "" || !filepath.IsAbs(strings.TrimSpace(path)) {
			return nil, fmt.Errorf("keymgmt: %s entries must be id=/absolute/path", AlertOldKEKsEnv)
		}
		provider, err := LoadLocalProviderFile(strings.TrimSpace(path), strings.TrimSpace(id))
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

// LoadLocalProviderFile loads a local AES-256 KEK without retaining the file
// buffer after the provider has copied it. The file-backed implementation is
// the currently shipped KeyProvider; external KMS/HSM implementations can be
// selected here without changing API or migration callers.
func LoadLocalProviderFile(path, kekID string) (KeyProvider, error) {
	kek, err := loadOwnerOnlyKeyFile(path, "KEK")
	if err != nil {
		return nil, err
	}
	defer clear(kek)
	provider, err := NewLocalProvider(kek, kekID)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

func loadOwnerOnlyKeyFile(path, label string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("keymgmt: inspect %s file: %w", label, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("keymgmt: %s file must be regular and owner-only", label)
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("keymgmt: read %s file: %w", label, err)
	}
	if len(key) != 32 {
		clear(key)
		return nil, fmt.Errorf("keymgmt: %s file must contain exactly 32 bytes", label)
	}
	return key, nil
}
