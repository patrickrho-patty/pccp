package scheduler

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// CardDomain is the domain-separation prefix for the capability-card
// canonical signing body (mirrors the policy/lease canonical pattern).
const CardDomain = "DARI-WORKER-CARD-v1\x00"

// WorkerCard is the capability card a PIA publishes at registration and
// re-signs on every heartbeat (DARI scheduler §6). All fields are bound by
// the canonical signature; every field the scheduler consumes must be a
// member — an unsigned field is a forgeable field.
type WorkerCard struct {
	CardVersion         uint32   `json:"card_version"`
	WorkerID            string   `json:"worker_id"`
	EnrollmentID        string   `json:"enrollment_id"`
	NodeID              string   `json:"node_id"`
	Hostname            string   `json:"hostname"`
	IP                  string   `json:"ip"`
	Region              string   `json:"region,omitempty"`
	Zone                string   `json:"zone,omitempty"`
	EngineKind          string   `json:"engine_kind"`
	EngineVersion       string   `json:"engine_version"`
	EngineURL           string   `json:"engine_url"`
	ReachabilityMode    string   `json:"reachability_mode"`
	MeasuredGrade       string   `json:"measured_grade"`
	ModelName           string   `json:"model_name"`
	ModelVersion        string   `json:"model_version"`
	Precision           string   `json:"precision"`
	ContextLength       uint64   `json:"context_length"`
	MaxConcurrentSeqs   uint64   `json:"max_concurrent_seqs"`
	Modalities          []string `json:"modalities"`
	TP                  uint32   `json:"tp"`
	DP                  uint32   `json:"dp"`
	EP                  uint32   `json:"ep"`
	AcceleratorFamily   string   `json:"accelerator_family"`
	GPUSKU              string   `json:"gpu_sku"`
	GPUCount            uint32   `json:"gpu_count"`
	HBMGB               uint32   `json:"hbm_gb"`
	Status              string   `json:"status"`
	LastHeartbeatUnixMs int64    `json:"last_heartbeat_unix_ms"`
	LeaseExpiryUnixMs   int64    `json:"lease_expiry_unix_ms"`
	SignatureHex        string   `json:"signature_hex,omitempty"`
}

// SigningBytes renders the canonical byte string bound by the card
// signature. Field order and length prefixes are pinned; do not change
// without a schema version bump.
func (c *WorkerCard) SigningBytes() []byte {
	dst := make([]byte, 0, 512)
	dst = append(dst, CardDomain...)
	dst = lpU32(dst, c.CardVersion)
	dst = lpString(dst, c.WorkerID)
	dst = lpString(dst, c.EnrollmentID)
	dst = lpString(dst, c.NodeID)
	dst = lpString(dst, c.Hostname)
	dst = lpString(dst, c.IP)
	dst = lpString(dst, c.Region)
	dst = lpString(dst, c.Zone)
	dst = lpString(dst, c.EngineKind)
	dst = lpString(dst, c.EngineVersion)
	dst = lpString(dst, c.EngineURL)
	dst = lpString(dst, c.ReachabilityMode)
	dst = lpString(dst, c.MeasuredGrade)
	dst = lpString(dst, c.ModelName)
	dst = lpString(dst, c.ModelVersion)
	dst = lpString(dst, c.Precision)
	dst = lpU64(dst, c.ContextLength)
	dst = lpU64(dst, c.MaxConcurrentSeqs)
	dst = lpStringSlice(dst, c.Modalities)
	dst = lpU32(dst, c.TP)
	dst = lpU32(dst, c.DP)
	dst = lpU32(dst, c.EP)
	dst = lpString(dst, c.AcceleratorFamily)
	dst = lpString(dst, c.GPUSKU)
	dst = lpU32(dst, c.GPUCount)
	dst = lpU32(dst, c.HBMGB)
	dst = lpString(dst, c.Status)
	dst = lpU64(dst, uint64(c.LastHeartbeatUnixMs))
	dst = lpU64(dst, uint64(c.LeaseExpiryUnixMs))
	return dst
}

// Sign binds the canonical card body with the worker's PPC keypair.
func (c *WorkerCard) Sign(priv ed25519.PrivateKey) error {
	sign1, err := dari.CreateCOSESign1(c.SigningBytes(), priv, []byte(c.WorkerID))
	if err != nil {
		return fmt.Errorf("scheduler: sign card: %w", err)
	}
	encoded, err := dari.EncodeCOSESign1(sign1)
	if err != nil {
		return fmt.Errorf("scheduler: encode card signature: %w", err)
	}
	c.SignatureHex = hex.EncodeToString(encoded)
	return nil
}

// Verify checks the card signature against the worker's public key.
func (c *WorkerCard) Verify(pub ed25519.PublicKey) error {
	if c.SignatureHex == "" {
		return fmt.Errorf("scheduler: card has no signature")
	}
	raw, err := hex.DecodeString(c.SignatureHex)
	if err != nil {
		return fmt.Errorf("scheduler: decode card signature: %w", err)
	}
	sign1, err := dari.DecodeCOSESign1(raw)
	if err != nil {
		return fmt.Errorf("scheduler: decode card COSE: %w", err)
	}
	if !bytes.Equal(sign1.Payload, c.SigningBytes()) {
		return fmt.Errorf("scheduler: card signature payload mismatch")
	}
	return dari.VerifyCOSESign1(sign1, pub)
}

func lpString(dst []byte, v string) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(v)))
	dst = append(dst, lenBuf[:]...)
	return append(dst, v...)
}

func lpStringSlice(dst []byte, values []string) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(values)))
	dst = append(dst, lenBuf[:]...)
	for _, v := range values {
		dst = lpString(dst, v)
	}
	return dst
}

func lpU32(dst []byte, value uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	return append(dst, buf[:]...)
}

func lpU64(dst []byte, value uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], value)
	return append(dst, buf[:]...)
}
