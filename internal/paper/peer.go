package paper

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// PeerCredential is a CP/organization-authority-signed credential binding a
// peer ID and profile to a public key (PAPER §16).  Encoded as a COSE-signed
// canonical CBOR object on the wire.
type PeerCredential struct {
	// CredentialVersion is the PPC schema version.
	CredentialVersion uint16 `cbor:"1"`
	// Issuer is the credential issuer (CP or org authority).
	Issuer string `cbor:"2"`
	// SubjectPeerID is the unique peer identifier.
	SubjectPeerID string `cbor:"3"`
	// Organization is the trust domain / organization ID.
	Organization string `cbor:"4"`
	// PeerProfile is the peer's role (HARNESS, INFERENCE, RELAY, CONTROL).
	PeerProfile PeerProfile `cbor:"5"`
	// PublicKey is the Ed25519 public key bytes.
	PublicKey []byte `cbor:"6"`
	// NotBefore is the credential validity start time (Unix ms).
	NotBefore int64 `cbor:"7"`
	// NotAfter is the credential expiry time (Unix ms).
	NotAfter int64 `cbor:"8"`
	// Serial is the credential serial number.
	Serial string `cbor:"9"`
	// RevocationAuthority identifies who can revoke this credential.
	RevocationAuthority string `cbor:"10"`
	// AllowedProtocolVersions lists allowed PAPER major versions.
	AllowedProtocolVersions []uint8 `cbor:"11"`
	// BuildChannel is an optional build channel/version policy.
	BuildChannel string `cbor:"12,omitempty"`
	// DeploymentZone is an optional deployment zone.
	DeploymentZone string `cbor:"13,omitempty"`
}

// PeerCredentialIssuer creates and signs PPCs for a trust domain.
type PeerCredentialIssuer struct {
	// IssuerID is the identity of this issuer (e.g. "pccp-ca").
	IssuerID string
	// PrivateKey is the Ed25519 CA signing key.
	PrivateKey ed25519.PrivateKey
	// PublicKey is the corresponding CA public key.
	PublicKey ed25519.PublicKey
}

// NewPeerCredentialIssuer creates a new self-signed PPC issuer.
func NewPeerCredentialIssuer(issuerID string) (*PeerCredentialIssuer, error) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	return &PeerCredentialIssuer{
		IssuerID:   issuerID,
		PrivateKey: priv,
		PublicKey:  pub,
	}, nil
}

// IssueRequest is the input to issue a new PPC.
type IssueRequest struct {
	SubjectPeerID          string
	Organization           string
	Profile                PeerProfile
	PublicKey              ed25519.PublicKey
	Validity               time.Duration
	RevocationAuthority    string
	AllowedProtocolVersions []uint8
	BuildChannel           string
	DeploymentZone         string
}

// Issue creates and signs a new PeerCredential.
func (i *PeerCredentialIssuer) Issue(req IssueRequest) (*PeerCredential, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	cred := &PeerCredential{
		CredentialVersion:       1,
		Issuer:                  i.IssuerID,
		SubjectPeerID:           req.SubjectPeerID,
		Organization:            req.Organization,
		PeerProfile:             req.Profile,
		PublicKey:               req.PublicKey,
		NotBefore:               now,
		NotAfter:                now + req.Validity.Milliseconds(),
		Serial:                  serial,
		RevocationAuthority:     req.RevocationAuthority,
		AllowedProtocolVersions: req.AllowedProtocolVersions,
		BuildChannel:            req.BuildChannel,
		DeploymentZone:          req.DeploymentZone,
	}
	return cred, nil
}

// Verify checks a PPC signature using the issuer's public key.
// In production, the signature is a COSE-Sign1 over the canonical CBOR of the
// credential body.  For Phase 0 the caller is expected to wire COSE signing.
func (i *PeerCredentialIssuer) Verify(cred *PeerCredential, signature []byte) bool {
	return VerifyEd25519(i.PublicKey, cred.SigningBytes(), signature)
}

// SigningBytes returns the canonical CBOR encoding of the credential body
// (without signature fields). This is what gets wrapped in COSE-Sign1.
// Per PAPER §16: "The baseline credential encoding is a COSE-signed
// canonical CBOR object."
func (c *PeerCredential) SigningBytes() []byte {
	// Build a map with only the signing-relevant fields (excluding any signature)
	signingMap := map[string]interface{}{
		"credential_version": c.CredentialVersion,
		"issuer":             c.Issuer,
		"subject_peer_id":    c.SubjectPeerID,
		"organization":       c.Organization,
		"peer_profile":       string(c.PeerProfile),
		"public_key":         c.PublicKey,
		"not_before":         c.NotBefore,
		"not_after":          c.NotAfter,
		"serial":             c.Serial,
		"revocation_authority": c.RevocationAuthority,
		"protocol_versions":  c.AllowedProtocolVersions,
	}
	if c.BuildChannel != "" {
		signingMap["build_channel"] = c.BuildChannel
	}
	if c.DeploymentZone != "" {
		signingMap["deployment_zone"] = c.DeploymentZone
	}
	data, err := MarshalCBOR(signingMap)
	if err != nil {
		return nil
	}
	return data
}

// SignWith signs the credential using the provided Ed25519 private key.
// Returns a COSE-Sign1 hex-encoded string for storage.
func (c *PeerCredential) SignWith(priv ed25519.PrivateKey) (string, error) {
	signingBytes := c.SigningBytes()
	if signingBytes == nil {
		return "", errors.New("paper: failed to encode credential for signing")
	}
	sign1, err := CreateCOSESign1(signingBytes, priv, []byte(c.Serial))
	if err != nil {
		return "", fmt.Errorf("paper: sign credential: %w", err)
	}
	encoded, err := EncodeCOSESign1(sign1)
	if err != nil {
		return "", fmt.Errorf("paper: encode credential signature: %w", err)
	}
	return hex.EncodeToString(encoded), nil
}

// VerifySignature verifies the credential's COSE-Sign1 signature using
// the issuer's public key.
func (c *PeerCredential) VerifySignature(pub ed25519.PublicKey, signatureHex string) error {
	signingBytes := c.SigningBytes()
	if signingBytes == nil {
		return errors.New("paper: failed to encode credential for verification")
	}
	return VerifyCOSESign1Hex(signatureHex, pub)
}

// IsValidAt reports whether the credential is valid at the given time.
func (c *PeerCredential) IsValidAt(t time.Time) bool {
	ms := t.UnixMilli()
	return ms >= c.NotBefore && ms < c.NotAfter
}

// IsExpired reports whether the credential has expired.
func (c *PeerCredential) IsExpired() bool {
	return !c.IsValidAt(time.Now())
}

func randomSerial() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hexEncode(b), nil
}

func hexEncode(b []byte) string {
	const hexchars = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[2*i] = hexchars[v>>4]
		out[2*i+1] = hexchars[v&0xf]
	}
	return string(out)
}
