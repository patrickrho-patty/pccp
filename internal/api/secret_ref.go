package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
)

// SecretRef is the persisted shape of an envelope-encrypted secret.
// The Encoded column is base64(JSON(Envelope)) so the column type
// stays plain TEXT and SQL dumps remain portable. PAT-1502 PR 2.
type SecretRef struct {
	Encoded string `json:"encoded"`
	KEKID   string `json:"kek_id"`
}

// ErrNoKeyProvider is returned when a write path requires envelope
// encryption but the server has no KeyProvider configured. Production
// callers must inject a provider via Server.SetKeyProvider before
// serving create/rotate traffic. PAT-1502 PR 2 — fail closed.
var ErrNoKeyProvider = errors.New("api: secret reference requires a configured KeyProvider")

// SealTarget encrypts a webhook URL via the provider's envelope seal
// and returns the persisted SecretRef. Returns ErrNoKeyProvider when
// the provider is nil. PAT-1502 PR 2.
func SealTarget(provider keymgmt.KeyProvider, raw string) (SecretRef, error) {
	if provider == nil {
		return SecretRef{}, ErrNoKeyProvider
	}
	env, err := keymgmt.Seal(provider, []byte(raw))
	if err != nil {
		return SecretRef{}, fmt.Errorf("api: seal target: %w", err)
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return SecretRef{}, fmt.Errorf("api: marshal envelope: %w", err)
	}
	return SecretRef{
		Encoded: base64.StdEncoding.EncodeToString(envJSON),
		KEKID:   env.KEKID,
	}, nil
}

// OpenTarget reverses SealTarget. Returns ErrNoKeyProvider when no
// provider is configured. PAT-1502 PR 2.
func OpenTarget(provider keymgmt.KeyProvider, ref SecretRef) (string, error) {
	if provider == nil {
		return "", ErrNoKeyProvider
	}
	if ref.Encoded == "" {
		return "", errors.New("api: empty secret reference")
	}
	envJSON, err := base64.StdEncoding.DecodeString(ref.Encoded)
	if err != nil {
		return "", fmt.Errorf("api: decode envelope: %w", err)
	}
	var env keymgmt.Envelope
	if err := json.Unmarshal(envJSON, &env); err != nil {
		return "", fmt.Errorf("api: parse envelope: %w", err)
	}
	plain, err := keymgmt.Open(provider, &env)
	if err != nil {
		return "", fmt.Errorf("api: open envelope: %w", err)
	}
	return string(plain), nil
}

// ResolveTarget returns the URL the alert dispatcher must POST to.
// It prefers the encrypted envelope and falls back to the legacy
// plaintext column when TargetEnc is empty (dual-read window).
// Returns ErrNoKeyProvider only when the encrypted path is the only
// one available AND no provider is configured. PAT-1502 PR 2.
func ResolveTarget(provider keymgmt.KeyProvider, epTargetEnc, epTargetKEKID, epTarget string) (string, error) {
	if epTargetEnc != "" {
		if provider == nil {
			return "", ErrNoKeyProvider
		}
		return OpenTarget(provider, SecretRef{Encoded: epTargetEnc, KEKID: epTargetKEKID})
	}
	if epTarget != "" {
		// Legacy dual-read path. The plaintext column will be
		// dropped in a follow-up PR after the backfill is complete
		// and every running instance understands the encrypted
		// column.
		return epTarget, nil
	}
	return "", nil
}

// PersistTarget columns for the encrypted envelope. Returns empty
// values when sealing fails so callers can decide whether to fail
// closed or surface the error.
func PersistTarget(provider keymgmt.KeyProvider, raw string) (enc string, kekID string, err error) {
	ref, err := SealTarget(provider, raw)
	if err != nil {
		return "", "", err
	}
	return ref.Encoded, ref.KEKID, nil
}
