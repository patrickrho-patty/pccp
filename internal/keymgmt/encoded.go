package keymgmt

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SealEncoded stores an envelope as base64(JSON) for portable TEXT columns.
func SealEncoded(provider KeyProvider, plaintext string) (encoded, kekID string, err error) {
	if provider == nil {
		return "", "", errors.New("keymgmt: provider_not_configured")
	}
	env, err := Seal(provider, []byte(plaintext))
	if err != nil {
		return "", "", err
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", "", fmt.Errorf("keymgmt: encode envelope: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), env.KEKID, nil
}

// SealEncodedWithAAD stores a TEXT-safe envelope bound to immutable row
// context. Moving the ciphertext to a different tenant or secret name fails.
func SealEncodedWithAAD(provider KeyProvider, plaintext string, aad []byte) (encoded, kekID string, err error) {
	if provider == nil {
		return "", "", errors.New("keymgmt: provider_not_configured")
	}
	env, err := SealWithAAD(provider, []byte(plaintext), aad)
	if err != nil {
		return "", "", err
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", "", fmt.Errorf("keymgmt: encode envelope: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), env.KEKID, nil
}

// OpenEncoded opens a base64(JSON) envelope. kekID is checked against both the
// stored envelope and provider so a mismatched metadata row fails closed.
func OpenEncoded(provider KeyProvider, encoded, kekID string) (string, error) {
	return openEncoded(provider, encoded, kekID, nil)
}

// OpenEncodedWithAAD opens a TEXT-safe envelope using the immutable context
// supplied when it was sealed.
func OpenEncodedWithAAD(provider KeyProvider, encoded, kekID string, aad []byte) (string, error) {
	return openEncoded(provider, encoded, kekID, aad)
}

func openEncoded(provider KeyProvider, encoded, kekID string, aad []byte) (string, error) {
	if provider == nil {
		return "", errors.New("keymgmt: provider_not_configured")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("keymgmt: invalid_envelope_encoding")
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", errors.New("keymgmt: invalid_envelope")
	}
	if kekID != "" && env.KEKID != kekID {
		return "", errors.New("keymgmt: envelope_metadata_mismatch")
	}
	resolved, err := resolveProvider(provider, env.KEKID)
	if err != nil {
		return "", err
	}
	plaintext, err := OpenWithAAD(resolved, &env, aad)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

type credentialFingerprinter interface {
	CredentialID(string) (string, error)
}

// CredentialID is a keyed, stable correlation identifier. Production provider
// rings use a dedicated fingerprint key so IDs neither expose an offline URL
// oracle nor change during envelope-key rotation.
func CredentialID(provider KeyProvider, plaintext string) (string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return "", nil
	}
	if fingerprinter, ok := provider.(credentialFingerprinter); ok {
		return fingerprinter.CredentialID(plaintext)
	}
	key, err := derivedFingerprintKey(provider)
	if err != nil {
		return "", err
	}
	defer clear(key)
	return hmacCredentialID(key, plaintext), nil
}

// DomainFingerprint is the shared collision-resistant audit fingerprint
// primitive. Callers use distinct domains; display layers may truncate it.
func DomainFingerprint(domain, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(domain + "\x00" + value))
	return "h:" + hex.EncodeToString(sum[:])
}

type providerResolver interface {
	ProviderForKEK(string) (KeyProvider, error)
}

func resolveProvider(provider KeyProvider, kekID string) (KeyProvider, error) {
	if provider == nil {
		return nil, errors.New("keymgmt: provider_not_configured")
	}
	if provider.KEKID() == kekID {
		return provider, nil
	}
	if resolver, ok := provider.(providerResolver); ok {
		return resolver.ProviderForKEK(kekID)
	}
	return nil, errors.New("keymgmt: provider_for_kek_not_configured")
}
