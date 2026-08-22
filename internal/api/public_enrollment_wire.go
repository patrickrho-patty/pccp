package api

// The v1 public Harness enrollment HTTP contract is intentionally mirrored in
// Patty Code. Keep the field names and JSON tags in sync with Patty Code's
// internal/device/public_enrollment_wire.go; golden vectors in both
// repositories prevent either side from changing the contract independently.
type publicHarnessEnrollmentGrantRequestV1 struct {
	HarnessID    string `json:"harness_id"`
	PublicKeyHex string `json:"public_key_hex"`
	Organization string `json:"organization,omitempty"`
}

type publicHarnessEnrollmentGrantResponseV1 struct {
	EnrollmentCode string `json:"enrollment_code"`
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
	Plan           string `json:"plan"`
	ExpiresAt      string `json:"expires_at"`
}

type publicHarnessEnrollmentRequestV1 struct {
	OrganizationID   string `json:"organization_id"`
	UserID           string `json:"user_id"`
	EnrollmentCode   string `json:"enrollment_code"`
	HarnessID        string `json:"harness_id"`
	PublicKeyHex     string `json:"public_key_hex"`
	BinaryVersion    string `json:"binary_version"`
	BinaryHash       string `json:"binary_hash,omitempty"`
	ExtensionVersion string `json:"extension_version,omitempty"`
	CLIVersion       string `json:"cli_version,omitempty"`
	DeviceHostname   string `json:"device_hostname,omitempty"`
	DeviceOS         string `json:"device_os,omitempty"`
	DeviceOSVersion  string `json:"device_os_version,omitempty"`
	DeviceArch       string `json:"device_arch,omitempty"`
}

type publicHarnessCredentialV1 struct {
	SignedCredential []byte `json:"signed_credential"`
}

type publicHarnessEnrollmentResponseV1 struct {
	Credential publicHarnessCredentialV1 `json:"credential"`
}

type publicHarnessRenewalRequestV1 struct {
	HarnessID      string `json:"harness_id"`
	CredentialHash string `json:"credential_sha256"`
	SignedAt       string `json:"signed_at"`
	SignatureHex   string `json:"signature_hex"`
}

type publicHarnessRenewalResponseV1 struct {
	Credential publicHarnessCredentialV1 `json:"credential"`
}

// publicHarnessEnrollmentResponseV1Key marks the public endpoint's response
// projection while it reuses the existing transactional enrollment handler.
// The authenticated console endpoint continues returning its richer response.
type publicHarnessEnrollmentResponseV1Key struct{}
