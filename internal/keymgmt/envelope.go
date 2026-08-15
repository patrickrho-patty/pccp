package keymgmt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// envelope.go implements the KMS/HSM seam (master plan Task 16):
// envelope encryption where data keys wrap payloads under a KEK
// supplied by a KeyProvider — a local provider for dev, an external
// KMS/HSM (AWS KMS, HashiCorp Vault, Luna, etc.) in production. The
// seam is the contract; providers plug in without touching callers.

// KeyProvider supplies key-encryption keys. An HSM provider returns a
// KEK that never leaves the module (Wrap/Unwrap via the provider); the
// local provider returns the raw key bytes for in-process wrapping.
type KeyProvider interface {
	// KEKID identifies the key-encryption key.
	KEKID() string
	// GetKEK returns the KEK bytes (local) or an error for
	// provider-managed wrap/unwrap implementations.
	GetKEK() ([]byte, error)
	// WrapKey encrypts a data key under the KEK (provider-side for
	// KMS/HSM implementations; default = AES-GCM wrap).
	WrapKey(dek []byte) ([]byte, error)
	// UnwrapKey decrypts a wrapped data key.
	UnwrapKey(wrapped []byte) ([]byte, error)
}

// LocalProvider is the dev/local KEK provider (AES-256 KEK in-process).
type LocalProvider struct {
	kek   []byte
	kekID string
}

// NewLocalProvider builds a local provider from raw KEK bytes.
func NewLocalProvider(kek []byte, kekID string) (*LocalProvider, error) {
	if len(kek) != 32 {
		return nil, errors.New("keymgmt: local KEK must be 32 bytes (AES-256)")
	}
	return &LocalProvider{kek: append([]byte(nil), kek...), kekID: kekID}, nil
}

// KEKID implements KeyProvider.
func (l *LocalProvider) KEKID() string { return l.kekID }

// GetKEK implements KeyProvider.
func (l *LocalProvider) GetKEK() ([]byte, error) { return append([]byte(nil), l.kek...), nil }

// WrapKey implements KeyProvider (AES-GCM).
func (l *LocalProvider) WrapKey(dek []byte) ([]byte, error) {
	return wrap(l.kek, dek)
}

// UnwrapKey implements KeyProvider.
func (l *LocalProvider) UnwrapKey(wrapped []byte) ([]byte, error) {
	return unwrap(l.kek, wrapped)
}

func wrap(kek, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func unwrap(kek, sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("keymgmt: wrapped key too short")
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// Envelope is one encrypted payload: the wrapped DEK + the ciphertext.
type Envelope struct {
	KEKID      string `json:"kek_id"`
	WrappedDEK []byte `json:"wrapped_dek"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

// Seal performs envelope encryption: a fresh 256-bit data key
// AES-GCM-encrypts the payload; the DEK is wrapped under the
// provider's KEK. The DEK is zeroed after use.
func Seal(provider KeyProvider, plaintext []byte) (*Envelope, error) {
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, err
	}
	defer zero(dek)

	wrapped, err := provider.WrapKey(dek)
	if err != nil {
		return nil, fmt.Errorf("keymgmt: wrap DEK: %w", err)
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return &Envelope{
		KEKID:      provider.KEKID(),
		WrappedDEK: wrapped,
		Nonce:      nonce,
		Ciphertext: gcm.Seal(nil, nonce, plaintext, nil),
	}, nil
}

// Open decrypts an envelope: the DEK is unwrapped by the provider and
// the payload decrypted. Fails closed on any tampering (GCM tag).
func Open(provider KeyProvider, env *Envelope) ([]byte, error) {
	if env == nil {
		return nil, errors.New("keymgmt: nil envelope")
	}
	if env.KEKID != provider.KEKID() {
		return nil, fmt.Errorf("keymgmt: envelope KEK %q does not match provider %q", env.KEKID, provider.KEKID())
	}
	dek, err := provider.UnwrapKey(env.WrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("keymgmt: unwrap DEK: %w", err)
	}
	defer zero(dek)
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, env.Nonce, env.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("keymgmt: decrypt (tampered or wrong key): %w", err)
	}
	return plaintext, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
