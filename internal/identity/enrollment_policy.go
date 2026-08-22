package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/config"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

const EnrollmentPolicySettingKey = "harness.enrollment_policy"

var ErrEnrollmentPolicyDenied = errors.New("identity: enrollment policy denied")

// EnrollmentPolicy is the tenant-owned gate evaluated before a device, PPC,
// or harness row is issued. Empty enterprise policy remains backwards
// compatible; sovereign defaults are fail-closed until signing trust exists.
type EnrollmentPolicy struct {
	RequireAdminApproval   bool     `json:"require_admin_approval"`
	RequireMDM             bool     `json:"require_mdm"`
	RequiredMDMPosture     []string `json:"required_mdm_posture,omitempty"`
	RequireAttestation     bool     `json:"require_attestation"`
	AttestationPublicKeys  []string `json:"attestation_public_keys,omitempty"`
	RequireNetworkZone     bool     `json:"require_network_zone"`
	AllowedNetworkZones    []string `json:"allowed_network_zones,omitempty"`
	BuildSigningPublicKeys []string `json:"build_signing_public_keys,omitempty"`
}

// HarnessHeartbeatProof is the complete signed control-plane heartbeat. Every
// mutable fact is bound to the enrolled harness key by SigningBytes.
type HarnessHeartbeatProof struct {
	OrganizationID  string `json:"organization_id,omitempty"`
	HarnessID       string `json:"harness_id"`
	SignedAt        string `json:"signed_at"`
	Signature       string `json:"signature"`
	BinaryVersion   string `json:"binary_version,omitempty"`
	DeviceHostname  string `json:"device_hostname,omitempty"`
	DeviceOS        string `json:"device_os,omitempty"`
	DeviceOSVersion string `json:"device_os_version,omitempty"`
	DeviceArch      string `json:"device_arch,omitempty"`
	IPAddress       string `json:"ip_address,omitempty"`
	Attestation     string `json:"attestation,omitempty"`
	AttestedAt      string `json:"attested_at,omitempty"`
}

func (p HarnessHeartbeatProof) SigningBytes() []byte {
	p.Signature = ""
	payload, _ := json.Marshal(p)
	return append([]byte("PCCP-HARNESS-HEARTBEAT-v1\x00"), payload...)
}

func (p HarnessHeartbeatProof) AttestationSigningBytes() []byte {
	return []byte(strings.Join([]string{
		"PCCP-HARNESS-HEARTBEAT-ATTESTATION-v1", strings.TrimSpace(p.OrganizationID), strings.TrimSpace(p.HarnessID),
		strings.TrimSpace(p.BinaryVersion), strings.TrimSpace(p.DeviceHostname), strings.TrimSpace(p.DeviceOS),
		strings.TrimSpace(p.DeviceOSVersion), strings.TrimSpace(p.DeviceArch), strings.TrimSpace(p.IPAddress), strings.TrimSpace(p.AttestedAt),
	}, "\x00"))
}

func ValidateHeartbeatAttestationAt(policy EnrollmentPolicy, proof HarnessHeartbeatProof, now time.Time) error {
	requiresProof := policy.RequireAttestation || len(policy.AttestationPublicKeys) > 0
	if !requiresProof {
		if strings.TrimSpace(proof.Attestation) != "" {
			return denyEnrollment("heartbeat attestation trust is not configured")
		}
		return nil
	}
	if _, err := ParseFreshSignedAt(proof.AttestedAt, now); err != nil {
		return denyEnrollment("fresh heartbeat attestation timestamp required")
	}
	signature, err := DecodeEd25519Signature(proof.Attestation)
	if err != nil {
		return denyEnrollment("heartbeat attestation signature required")
	}
	keys, err := decodeEd25519PublicKeys(policy.AttestationPublicKeys)
	if err != nil || !verifyAnyEd25519(keys, proof.AttestationSigningBytes(), signature) {
		return denyEnrollment("heartbeat attestation signature verification failed")
	}
	return nil
}

func ValidateEnrollmentPolicyConfiguration(profile string, policy EnrollmentPolicy) error {
	for _, zone := range policy.AllowedNetworkZones {
		if strings.TrimSpace(zone) == "" {
			return fmt.Errorf("identity: enrollment policy contains an empty network zone")
		}
	}
	if _, err := decodeEd25519PublicKeys(policy.BuildSigningPublicKeys); err != nil {
		return fmt.Errorf("identity: enrollment policy contains an invalid Ed25519 build key")
	}
	if _, err := decodeEd25519PublicKeys(policy.AttestationPublicKeys); err != nil {
		return fmt.Errorf("identity: enrollment policy contains an invalid Ed25519 attestation key")
	}
	if (policy.RequireMDM || len(policy.RequiredMDMPosture) > 0 || policy.RequireNetworkZone || len(policy.AllowedNetworkZones) > 0) && !policy.RequireAttestation {
		return fmt.Errorf("identity: MDM posture and network gates require signed device attestation")
	}
	if policy.RequireAttestation && len(policy.AttestationPublicKeys) == 0 {
		return fmt.Errorf("identity: attestation policy requires attestation signing trust")
	}
	if config.GetProfile(profile).Name == "sovereign" && len(policy.BuildSigningPublicKeys) == 0 {
		return fmt.Errorf("identity: sovereign enrollment policy requires build signing trust")
	}
	return nil
}

func defaultEnrollmentPolicy(profile string) EnrollmentPolicy {
	deployment := config.GetProfile(profile)
	policy := EnrollmentPolicy{
		RequireMDM: deployment.RequireMDM, RequireAttestation: deployment.RequireAttestation,
		RequireNetworkZone: deployment.RequireVPN,
	}
	if deployment.Name == "sovereign" {
		policy.RequireAdminApproval = true
		policy.RequiredMDMPosture = []string{"disk_encryption", "screen_lock"}
	}
	return policy
}

// LoadEnrollmentPolicy reads the durable policy inside the enrollment
// transaction. A malformed policy denies enrollment rather than falling back.
func LoadEnrollmentPolicy(tx *gorm.DB, org models.Organization) (EnrollmentPolicy, error) {
	policy := defaultEnrollmentPolicy(org.Profile)
	var setting models.OrgSetting
	err := tx.Where("organization_id = ? AND key = ?", org.ID, EnrollmentPolicySettingKey).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return policy, nil
	}
	if err != nil {
		return EnrollmentPolicy{}, fmt.Errorf("identity: load enrollment policy: %w", err)
	}
	if err := json.Unmarshal([]byte(setting.Value), &policy); err != nil {
		return EnrollmentPolicy{}, fmt.Errorf("%w: malformed organization policy", ErrEnrollmentPolicyDenied)
	}
	if err := ValidateEnrollmentPolicyConfiguration(org.Profile, policy); err != nil {
		return EnrollmentPolicy{}, fmt.Errorf("%w: %v", ErrEnrollmentPolicyDenied, err)
	}
	return policy, nil
}

func EnrollmentPolicyFingerprint(profile string, policy EnrollmentPolicy) ([32]byte, error) {
	encoded, err := json.Marshal(struct {
		Profile string           `json:"profile"`
		Policy  EnrollmentPolicy `json:"policy"`
	}{Profile: config.GetProfile(profile).Name, Policy: policy})
	if err != nil {
		return [32]byte{}, fmt.Errorf("identity: fingerprint enrollment policy: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

// ParseFreshSignedAt applies the one control-plane freshness window shared by
// enrollment and heartbeat proofs.
func ParseFreshSignedAt(value string, now time.Time) (time.Time, error) {
	signedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil || signedAt.Before(now.Add(-5*time.Minute)) || signedAt.After(now.Add(time.Minute)) {
		return time.Time{}, fmt.Errorf("identity: signed timestamp is outside the freshness window")
	}
	return signedAt, nil
}

// DecodeEd25519Signature decodes the canonical hex signature representation.
func DecodeEd25519Signature(value string) ([]byte, error) {
	signature, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("identity: valid Ed25519 signature required")
	}
	return signature, nil
}

// BuildSigningBytes is the portable organization build-attestation contract.
// A signature can be produced once by the release pipeline and presented by
// every installation without binding it to a mutable hostname or harness ID.
func BuildSigningBytes(orgID, binaryHash string) []byte {
	return []byte("PCCP-HARNESS-BUILD-v1\x00" + strings.TrimSpace(orgID) + "\x00" + strings.TrimSpace(binaryHash))
}

func AttestationSigningBytes(req EnrollHarnessRequest) []byte {
	fields := []string{
		"PCCP-DEVICE-ATTESTATION-v1", strings.TrimSpace(req.OrganizationID), strings.TrimSpace(req.UserID),
		strings.TrimSpace(req.HarnessID), strings.TrimSpace(req.PublicKeyHex), strings.TrimSpace(req.BinaryHash),
		strings.TrimSpace(req.MDMPosture), strings.TrimSpace(req.NetworkZone), strings.TrimSpace(req.AttestedAt),
	}
	return []byte(strings.Join(fields, "\x00"))
}

func denyEnrollment(reason string) error {
	return fmt.Errorf("%w: %s", ErrEnrollmentPolicyDenied, reason)
}

func decodeEd25519PublicKeys(encoded []string) ([]ed25519.PublicKey, error) {
	keys := make([]ed25519.PublicKey, 0, len(encoded))
	for _, value := range encoded {
		publicKey, err := hex.DecodeString(strings.TrimSpace(value))
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid Ed25519 public key")
		}
		keys = append(keys, ed25519.PublicKey(publicKey))
	}
	return keys, nil
}

func verifyAnyEd25519(keys []ed25519.PublicKey, message, signature []byte) bool {
	for _, publicKey := range keys {
		if ed25519.Verify(publicKey, message, signature) {
			return true
		}
	}
	return false
}

// ValidateEnrollmentPolicy proves every configured gate before enrollment.
func ValidateEnrollmentPolicyAt(policy EnrollmentPolicy, req EnrollHarnessRequest, now time.Time) error {
	if policy.RequireAdminApproval && strings.TrimSpace(req.EnrollmentCode) == "" {
		return denyEnrollment("administrator approval code required")
	}
	if policy.RequireMDM {
		if !req.MDMEnrolled {
			return denyEnrollment("MDM enrollment required")
		}
		var posture map[string]interface{}
		if err := json.Unmarshal([]byte(req.MDMPosture), &posture); err != nil {
			return denyEnrollment("valid MDM posture proof required")
		}
		for _, key := range policy.RequiredMDMPosture {
			value, ok := posture[key].(bool)
			if !ok || !value {
				return denyEnrollment("MDM posture requirement not satisfied: " + key)
			}
		}
	}
	if policy.RequireAttestation {
		if _, err := ParseFreshSignedAt(req.AttestedAt, now); err != nil {
			return denyEnrollment("fresh device attestation timestamp required")
		}
		signature, err := DecodeEd25519Signature(req.Attestation)
		if err != nil {
			return denyEnrollment("device attestation signature required")
		}
		keys, keyErr := decodeEd25519PublicKeys(policy.AttestationPublicKeys)
		if keyErr != nil || !verifyAnyEd25519(keys, AttestationSigningBytes(req), signature) {
			return denyEnrollment("device attestation signature verification failed")
		}
	}
	zone := strings.TrimSpace(req.NetworkZone)
	if policy.RequireNetworkZone && zone == "" {
		return denyEnrollment("managed network zone required")
	}
	if len(policy.AllowedNetworkZones) > 0 {
		allowed := false
		for _, candidate := range policy.AllowedNetworkZones {
			if strings.EqualFold(strings.TrimSpace(candidate), zone) {
				allowed = true
				break
			}
		}
		if !allowed {
			return denyEnrollment("network zone is not allowed")
		}
	}
	if len(policy.BuildSigningPublicKeys) > 0 {
		signature, err := hex.DecodeString(strings.TrimSpace(req.BuildSignature))
		if err != nil || len(signature) != ed25519.SignatureSize {
			return denyEnrollment("organization build signature required")
		}
		keys, keyErr := decodeEd25519PublicKeys(policy.BuildSigningPublicKeys)
		if keyErr != nil || !verifyAnyEd25519(keys, BuildSigningBytes(req.OrganizationID, req.BinaryHash), signature) {
			return denyEnrollment("organization build signature verification failed")
		}
	} else if config.GetProfile(req.DeploymentProfile).Name == "sovereign" {
		return denyEnrollment("sovereign build signing trust is not configured")
	}
	return nil
}

func ValidateEnrollmentPolicy(policy EnrollmentPolicy, req EnrollHarnessRequest) error {
	return ValidateEnrollmentPolicyAt(policy, req, time.Now().UTC())
}
