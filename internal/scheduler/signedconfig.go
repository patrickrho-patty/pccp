package scheduler

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// ConfigDomain is the domain-separation prefix for the signed PIA worker
// config (DARI scheduler §8).
const ConfigDomain = "DARI-PIA-CONFIG-v1\x00"

// ConfigKeyID is the COSE key identifier for config signatures. It matches
// the `config` key domain in internal/keymgmt.
const ConfigKeyID = "pccp-config"

// PIAWorkerConfig is the authorization object a PIA carries: which models it
// may serve, how it may reach its engine, and which tenant/pool it binds to.
// The Control Plane signs it; the scheduler verifies it at admission.
type PIAWorkerConfig struct {
	WorkerID      string   `json:"worker_id"`
	TenantID      string   `json:"tenant_id"`
	BackendMode   string   `json:"backend_mode"` // localhost | private | mtls
	EngineURL     string   `json:"engine_url"`
	AllowedModels []string `json:"allowed_models"`
}

// SignedConfig is the distributed envelope: canonical-JSON config + COSE-Sign1
// signature under the CP config key.
type SignedConfig struct {
	Config    PIAWorkerConfig `json:"config"`
	Signature string          `json:"signature"`
	CPKeyID   string          `json:"cp_key_id"`
}

func configSigningBytes(cfg PIAWorkerConfig) ([]byte, error) {
	body, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("scheduler: marshal worker config: %w", err)
	}
	dst := make([]byte, 0, len(ConfigDomain)+len(body))
	dst = append(dst, ConfigDomain...)
	return append(dst, body...), nil
}

// SignConfig signs a PIA worker config with the CP config key.
func SignConfig(priv ed25519.PrivateKey, cfg PIAWorkerConfig) (*SignedConfig, error) {
	canonical, err := configSigningBytes(cfg)
	if err != nil {
		return nil, err
	}
	sign1, err := dari.CreateCOSESign1(canonical, priv, []byte(ConfigKeyID))
	if err != nil {
		return nil, fmt.Errorf("scheduler: sign worker config: %w", err)
	}
	encoded, err := dari.EncodeCOSESign1(sign1)
	if err != nil {
		return nil, fmt.Errorf("scheduler: encode worker config signature: %w", err)
	}
	return &SignedConfig{
		Config:    cfg,
		Signature: hex.EncodeToString(encoded),
		CPKeyID:   ConfigKeyID,
	}, nil
}

// Verify checks the envelope signature against the CP config public key.
func (s *SignedConfig) Verify(pub ed25519.PublicKey) error {
	if s == nil {
		return fmt.Errorf("scheduler: missing signed config")
	}
	canonical, err := configSigningBytes(s.Config)
	if err != nil {
		return err
	}
	raw, err := hex.DecodeString(s.Signature)
	if err != nil {
		return fmt.Errorf("scheduler: decode config signature: %w", err)
	}
	sign1, err := dari.DecodeCOSESign1(raw)
	if err != nil {
		return fmt.Errorf("scheduler: decode config COSE: %w", err)
	}
	if !bytes.Equal(sign1.Payload, canonical) {
		return fmt.Errorf("scheduler: config signature payload mismatch")
	}
	return dari.VerifyCOSESign1(sign1, pub)
}

// Authorizes checks that the config permits the card's deployment: identity
// binding, backend reachability mode, and model allow-list.
func (s *SignedConfig) Authorizes(card WorkerCard) error {
	if s.Config.WorkerID != card.WorkerID {
		return fmt.Errorf("config worker_id %q does not match card %q", s.Config.WorkerID, card.WorkerID)
	}
	if s.Config.BackendMode != card.ReachabilityMode {
		return fmt.Errorf("config backend mode %q does not match card %q", s.Config.BackendMode, card.ReachabilityMode)
	}
	for _, m := range s.Config.AllowedModels {
		if m == card.ModelName {
			return nil
		}
	}
	return fmt.Errorf("model %q not in config allow-list", card.ModelName)
}
