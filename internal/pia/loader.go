package pia

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

// LoadWorkerCredential loads the PIA's PPC from a hex-encoded COSE-Sign1
// credential file (issued by the CP at enrollment).
func LoadWorkerCredential(path string) (*dari.PeerCredential, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pia: read credential file: %w", err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("pia: decode credential hex: %w", err)
	}
	sign1, err := dari.DecodeCOSESign1(raw)
	if err != nil {
		return nil, fmt.Errorf("pia: decode credential COSE: %w", err)
	}
	cred, err := dari.DecodePeerCredential(sign1.Payload)
	if err != nil {
		return nil, fmt.Errorf("pia: decode credential body: %w", err)
	}
	cred.SignedCredential = append([]byte(nil), raw...)
	return cred, nil
}

// LoadSubjectKey loads the PIA's Ed25519 subject key (hex) matching the PPC.
func LoadSubjectKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pia: read subject key file: %w", err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("pia: decode subject key hex: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("pia: subject key must be %d bytes", ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(raw), nil
}

// LoadSignedConfig loads the CP-signed worker config envelope. When
// requireSignature is true (production profile) the envelope must verify
// against the CP config public key — fail closed.
func LoadSignedConfig(path string, configPubKey ed25519.PublicKey, requireSignature bool) (*scheduler.SignedConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !requireSignature {
			// Dev profile: run without an envelope (worker mode then
			// stays unregistered).
			return nil, nil
		}
		return nil, fmt.Errorf("pia: config envelope missing (production requires a signed config): %w", err)
	}
	var envelope scheduler.SignedConfig
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("pia: decode config envelope: %w", err)
	}
	if requireSignature {
		if err := envelope.Verify(configPubKey); err != nil {
			return nil, fmt.Errorf("pia: config envelope verification failed: %w", err)
		}
	}
	return &envelope, nil
}
