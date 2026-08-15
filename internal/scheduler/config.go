package scheduler

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// BuildTrust assembles the scheduler's offline trust bundle from hex-encoded
// public keys: the PPC issuer (CP CA) key and the CP config-signing key.
func BuildTrust(caPubKeyHex, configPubKeyHex, issuerID string) (Trust, error) {
	caBytes, err := hex.DecodeString(caPubKeyHex)
	if err != nil || len(caBytes) != ed25519.PublicKeySize {
		return Trust{}, fmt.Errorf("scheduler: invalid CA public key hex")
	}
	cfgBytes, err := hex.DecodeString(configPubKeyHex)
	if err != nil || len(cfgBytes) != ed25519.PublicKeySize {
		return Trust{}, fmt.Errorf("scheduler: invalid config public key hex")
	}
	return Trust{
		Issuers:      map[string]ed25519.PublicKey{issuerID: ed25519.PublicKey(caBytes)},
		ConfigPubKey: ed25519.PublicKey(cfgBytes),
		Now:          time.Now,
	}, nil
}

// FilePolicy is a static PolicySource loaded from a JSON file mapping tenant
// IDs to minimum reachability grades. The CP-backed policy source replaces
// this in S1.6+ deployments.
type FilePolicy map[string]string

// MinReachability implements PolicySource.
func (p FilePolicy) MinReachability(tenantID string) (string, bool) {
	grade, ok := p[tenantID]
	return grade, ok
}

// LoadPolicyFile loads a tenant reachability policy from JSON.
func LoadPolicyFile(path string) (FilePolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scheduler: read policy file: %w", err)
	}
	var policy FilePolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("scheduler: decode policy file: %w", err)
	}
	return policy, nil
}
