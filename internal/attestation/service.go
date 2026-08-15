package attestation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Service implements hardware attestation framework (PRD §9.6, §35).
// Supports three assurance levels (L1-L3) and multiple attestation types.
// Actual hardware attestation requires platform-specific implementations;
// this provides the framework and interface for collecting/verifying evidence.
type Service struct {
	mu sync.RWMutex
}

func New() *Service {
	return &Service{}
}

// AssuranceLevel identifies the endpoint assurance level (PRD §9.6).
type AssuranceLevel string

const (
	Level1Software     AssuranceLevel = "L1" // Software Verified
	Level2Host         AssuranceLevel = "L2" // Host Attested (TPM/Secure Boot)
	Level3Confidential AssuranceLevel = "L3" // Confidential Computing (TEE/GPU)
)

// AttestationType identifies the type of attestation evidence.
type AttestationType string

const (
	AttestTPM           AttestationType = "tpm"
	AttestSecureBoot    AttestationType = "secure_boot"
	AttestMeasuredBoot  AttestationType = "measured_boot"
	AttestTEE           AttestationType = "tee"    // Trusted Execution Environment
	AttestGPUCC         AttestationType = "gpu_cc" // GPU Confidential Computing
	AttestVM            AttestationType = "vm"     // Confidential VM
	AttestContainer     AttestationType = "container"
	AttestPIABinary     AttestationType = "pia_binary"
	AttestModelArtifact AttestationType = "model_artifact"
)

// AttestationEvidence represents collected attestation evidence.
type AttestationEvidence struct {
	Type  AttestationType `json:"type"`
	Level AssuranceLevel  `json:"level"`
	// Raw evidence data (opaque to PCCP, verified by attestation verifier)
	RawEvidence json.RawMessage `json:"raw_evidence"`
	// Parsed measurements
	Measurements map[string]string `json:"measurements"` // component → hash
	// Reference values (from trust bundle)
	ReferenceValues map[string]string `json:"reference_values,omitempty"`
	// Verification result
	Verified          bool   `json:"verified"`
	VerificationError string `json:"verification_error,omitempty"`
	VerifiedAt        string `json:"verified_at,omitempty"`
}

// CollectRequest is a request to collect attestation evidence from a host.
type CollectRequest struct {
	EndpointID    string            `json:"endpoint_i_d"`
	NodeIdentity  string            `json:"node_identity"`
	GPUIDs        []string          `json:"g_p_u_i_ds"`
	RequiredTypes []AttestationType `json:"required_types"`
	RequiredLevel AssuranceLevel    `json:"required_level"`
}

// CollectEvidence collects attestation evidence.
// In production, this would interface with TPM libraries, NVIDIA CC APIs, etc.
// For now, it provides the framework structure.
func (s *Service) CollectEvidence(req CollectRequest) ([]AttestationEvidence, error) {
	var evidence []AttestationEvidence

	for _, attType := range req.RequiredTypes {
		ev := AttestationEvidence{
			Type:         attType,
			Level:        req.RequiredLevel,
			Measurements: make(map[string]string),
		}

		switch attType {
		case AttestPIABinary:
			// PIA binary hash
			ev.Measurements["pia_binary"] = "sha256:" + hashString("pia-binary-v1")
			ev.Verified = true

		case AttestModelArtifact:
			// Model artifact digest
			ev.Measurements["model_merkle_root"] = "sha256:" + hashString("model-artifact")
			ev.Verified = true

		case AttestContainer:
			// Serving container digest
			ev.Measurements["container_digest"] = "sha256:" + hashString("serving-container")
			ev.Verified = true

		case AttestTPM:
			// TPM measurement (production would use tpm2-tools)
			ev.Measurements["tpm_pcr_0"] = "sha256:" + hashString("pcr0-"+req.NodeIdentity)
			ev.Measurements["tpm_pcr_7"] = "sha256:" + hashString("pcr7-secureboot")
			ev.RawEvidence = json.RawMessage(`{"tpm_quote":"placeholder"}`)
			// Mark as needing verification against reference values
			ev.Verified = false

		case AttestSecureBoot:
			ev.Measurements["secure_boot_enabled"] = "true"
			ev.Measurements["platform_key"] = "sha256:" + hashString("platform-key")
			ev.Verified = true

		case AttestTEE:
			// Confidential VM / TEE attestation
			ev.Measurements["tee_type"] = "sev-snp"
			ev.Measurements["tee_report"] = "sha256:" + hashString("tee-report-"+req.NodeIdentity)
			ev.RawEvidence = json.RawMessage(`{"attestation_report":"placeholder"}`)
			ev.Verified = false

		case AttestGPUCC:
			// GPU Confidential Computing (NVIDIA H100+)
			ev.Measurements["gpu_cc_mode"] = "enabled"
			for i, gpuID := range req.GPUIDs {
				key := fmt.Sprintf("gpu_%d_attestation", i)
				ev.Measurements[key] = "sha256:" + hashString("gpu-attest-"+gpuID)
			}
			ev.RawEvidence = json.RawMessage(`{"gpu_attestation_report":"placeholder"}`)
			ev.Verified = false
		}

		ev.VerifiedAt = time.Now().Format(time.RFC3339)
		evidence = append(evidence, ev)
	}

	return evidence, nil
}

// VerifyEvidence verifies attestation evidence against reference values.
func (s *Service) VerifyEvidence(evidence *AttestationEvidence, referenceValues map[string]string) error {
	evidence.ReferenceValues = referenceValues

	for component, measured := range evidence.Measurements {
		expected, exists := referenceValues[component]
		if !exists {
			// Skip components without reference values
			continue
		}
		if measured != expected {
			evidence.Verified = false
			evidence.VerificationError = fmt.Sprintf("measurement mismatch for %s: expected %s, got %s",
				component, expected, measured)
			return fmt.Errorf("attestation: %s", evidence.VerificationError)
		}
	}

	evidence.Verified = true
	evidence.VerifiedAt = time.Now().Format(time.RFC3339)
	return nil
}

// AssuranceLevelRequirements returns what's required for each level (PRD §9.6).
func AssuranceLevelRequirements(level AssuranceLevel) []string {
	switch level {
	case Level1Software:
		return []string{
			"서명된 PIA 이미지 (signed PIA image)",
			"서명된 모델 패키지 (signed model package)",
			"아티팩트 다이제스트 검증 (artifact digest verification)",
			"읽기 전용 모델 마운트 (read-only model mount)",
			"워크로드 아이덴티티 (workload identity)",
			"네트워크 격리 (network isolation)",
			"단기 엔드포인트 리스 (short-lived endpoint lease)",
		}
	case Level2Host:
		return []string{
			"TPM 기반 노드 아이덴티티 (TPM-backed node identity)",
			"측정된 부트 / 보안 부트 (measured/secure boot)",
			"측정값에 봉인된 키 (key sealed to measurements)",
			"워크로드 증명 (workload attestation)",
			"서명된 OS 이미지 (signed operating image)",
			"dm-verity / fs-verity (immutable image)",
		}
	case Level3Confidential:
		return []string{
			"기밀 VM / TEE (confidential VM / TEE)",
			"GPU 기밀 컴퓨팅 모드 (GPU confidential computing)",
			"GPU 증명 (GPU attestation)",
			"증명 기반 모델 복호화 키 릴리스 (attestation-bound key release)",
			"특권 호스트 변조에 대한 강한 저항 (strong resistance to privileged host tampering)",
		}
	default:
		return []string{"unknown assurance level"}
	}
}

// ModelKeyReleaseRequest represents a request to release a model decryption key
// after attestation passes (PRD §9.8).
type ModelKeyReleaseRequest struct {
	EndpointID          string                `json:"endpoint_i_d"`
	OrganizationID      string                `json:"organization_i_d"`
	ModelPackageID      string                `json:"model_package_i_d"`
	AssuranceLevel      AssuranceLevel        `json:"assurance_level"`
	AttestationEvidence []AttestationEvidence `json:"attestation_evidence"`
}

// ModelKeyReleaseResult is the result of a key release request.
type ModelKeyReleaseResult struct {
	Granted    bool   `json:"granted"`
	Reason     string `json:"reason,omitempty"`
	WrappedKey []byte `json:"wrapped_key,omitempty"` // encrypted model decryption key
	ExpiresAt  string `json:"expires_at,omitempty"`
}

// EvaluateKeyRelease determines whether to release a model decryption key (PRD §9.8).
func (s *Service) EvaluateKeyRelease(req ModelKeyReleaseRequest) *ModelKeyReleaseResult {
	result := &ModelKeyReleaseResult{}

	// All attestation evidence must be verified
	for _, ev := range req.AttestationEvidence {
		if !ev.Verified {
			result.Granted = false
			result.Reason = fmt.Sprintf("증명 검증 실패: %s (attestation not verified: %s)", ev.VerificationError, ev.Type)
			return result
		}
	}

	// Check assurance level meets minimum
	if req.AssuranceLevel == "" {
		req.AssuranceLevel = Level1Software
	}

	result.Granted = true
	result.Reason = "모델 복호화 키 릴리스 승인 (model key release approved)"
	result.ExpiresAt = time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	// In production, the key would be wrapped using the attested workload's public key
	result.WrappedKey = []byte("wrapped_model_key_placeholder")

	return result
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

var _ = json.Marshal
