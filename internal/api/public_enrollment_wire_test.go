package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
)

func TestPublicHarnessEnrollmentWireV1Golden(t *testing.T) {
	fixtureBytes, err := os.ReadFile("testdata/public_harness_enrollment_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if digest := sha256.Sum256(fixtureBytes); hex.EncodeToString(digest[:]) != "8ab4ee2ab79dabea857e3d175484b30f59323f16bc9c2ba9b8f0722b532baa9c" {
		t.Fatalf("public enrollment v1 fixture digest = %x", digest)
	}
	var fixture struct {
		GrantRequest        json.RawMessage `json:"grant_request"`
		GrantResponse       json.RawMessage `json:"grant_response"`
		EnrollmentRequest   json.RawMessage `json:"enrollment_request"`
		EnrollmentResponse  json.RawMessage `json:"enrollment_response"`
		RenewalRequest      json.RawMessage `json:"renewal_request"`
		RenewalResponse     json.RawMessage `json:"renewal_response"`
		RenewalSigningBytes string          `json:"renewal_signing_bytes_hex"`
	}
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatal(err)
	}

	const (
		publicKeyHex  = "0000000000000000000000000000000000000000000000000000000000000000"
		credentialHex = "1111111111111111111111111111111111111111111111111111111111111111"
		signedAt      = "2026-08-23T04:14:15.123456789Z"
		signatureHex  = "abababababababababababababababababababababababababababababababababababababababababababababababababababababababababababababababab"
	)
	cases := []struct {
		name  string
		want  json.RawMessage
		value any
	}{
		{"grant request", fixture.GrantRequest, publicHarnessEnrollmentGrantRequestV1{HarnessID: "hrn_fixture_001", PublicKeyHex: publicKeyHex, Organization: "acme"}},
		{"grant response", fixture.GrantResponse, publicHarnessEnrollmentGrantResponseV1{EnrollmentCode: "grant_fixture_001", OrganizationID: "org_fixture_001", UserID: "user_fixture_001", Plan: "enterprise", ExpiresAt: "2026-08-23T04:24:15Z"}},
		{"enrollment request", fixture.EnrollmentRequest, publicHarnessEnrollmentRequestV1{
			OrganizationID: "org_fixture_001", UserID: "user_fixture_001", EnrollmentCode: "grant_fixture_001",
			HarnessID: "hrn_fixture_001", PublicKeyHex: publicKeyHex, BinaryVersion: "1.2.3", BinaryHash: "sha256:fixture",
			ExtensionVersion: "1.2.3", CLIVersion: "1.2.3", DeviceHostname: "fixture-host", DeviceOS: "darwin",
			DeviceOSVersion: "15.0", DeviceArch: "arm64",
		}},
		{"enrollment response", fixture.EnrollmentResponse, publicHarnessEnrollmentResponseV1{Credential: publicHarnessCredentialV1{SignedCredential: []byte("signed-ppc-v1")}}},
		{"renewal request", fixture.RenewalRequest, publicHarnessRenewalRequestV1{HarnessID: "hrn_fixture_001", CredentialHash: credentialHex, SignedAt: signedAt, SignatureHex: signatureHex}},
		{"renewal response", fixture.RenewalResponse, publicHarnessRenewalResponseV1{Credential: publicHarnessCredentialV1{SignedCredential: []byte("renewed-ppc-v1")}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var want bytes.Buffer
			if err := json.Compact(&want, tc.want); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want.Bytes()) {
				t.Fatalf("wire JSON = %s, want %s", got, want.Bytes())
			}
		})
	}

	gotSigningBytes := hex.EncodeToString(identity.HarnessRenewalSigningBytes("hrn_fixture_001", credentialHex, signedAt))
	if gotSigningBytes != fixture.RenewalSigningBytes {
		t.Fatalf("renewal signing bytes = %s, want %s", gotSigningBytes, fixture.RenewalSigningBytes)
	}
}
