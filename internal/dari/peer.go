package dari

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// PeerCredential is a CP/organization-authority-signed credential binding a
// peer ID and profile to a public key (DARI §16).  Encoded as a COSE-signed
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
	// AllowedProtocolVersions lists allowed DARI major versions.
	AllowedProtocolVersions []uint8 `cbor:"11"`
	// BuildChannel is an optional build channel/version policy.
	BuildChannel string `cbor:"12,omitempty"`
	// DeploymentZone is an optional deployment zone.
	DeploymentZone string `cbor:"13,omitempty"`
	// SignedCredential is the complete COSE-Sign1 credential presented on the
	// wire. It is excluded from the signed credential body.
	SignedCredential []byte `cbor:"-" json:"signed_credential,omitempty"`
}

type peerCredentialSigningBody struct {
	CredentialVersion       uint16      `cbor:"credential_version"`
	Issuer                  string      `cbor:"issuer"`
	SubjectPeerID           string      `cbor:"subject_peer_id"`
	Organization            string      `cbor:"organization"`
	PeerProfile             PeerProfile `cbor:"peer_profile"`
	PublicKey               []byte      `cbor:"public_key"`
	NotBefore               int64       `cbor:"not_before"`
	NotAfter                int64       `cbor:"not_after"`
	Serial                  string      `cbor:"serial"`
	RevocationAuthority     string      `cbor:"revocation_authority"`
	AllowedProtocolVersions []uint8     `cbor:"protocol_versions"`
	BuildChannel            string      `cbor:"build_channel,omitempty"`
	DeploymentZone          string      `cbor:"deployment_zone,omitempty"`
}

// PeerCredentialIssuer creates and signs PPCs for a trust domain.
type PeerCredentialIssuer struct {
	// IssuerID is the identity of this issuer (e.g. "pccp-ca").
	IssuerID string `json:"issuer_i_d"`
	// PrivateKey is the Ed25519 CA signing key.
	PrivateKey ed25519.PrivateKey `json:"private_key"`
	// PublicKey is the corresponding CA public key.
	PublicKey ed25519.PublicKey `json:"public_key"`
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
	SubjectPeerID           string            `json:"subject_peer_i_d"`
	Organization            string            `json:"organization"`
	Profile                 PeerProfile       `json:"profile"`
	PublicKey               ed25519.PublicKey `json:"public_key"`
	Validity                time.Duration     `json:"validity"`
	RevocationAuthority     string            `json:"revocation_authority"`
	AllowedProtocolVersions []uint8           `json:"allowed_protocol_versions"`
	BuildChannel            string            `json:"build_channel"`
	DeploymentZone          string            `json:"deployment_zone"`
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
	if _, err := cred.SignWith(i.PrivateKey); err != nil {
		return nil, err
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
// Per DARI §16: "The baseline credential encoding is a COSE-signed
// canonical CBOR object."
func (c *PeerCredential) SigningBytes() []byte {
	data, err := MarshalCBOR(peerCredentialSigningBody{
		CredentialVersion:       c.CredentialVersion,
		Issuer:                  c.Issuer,
		SubjectPeerID:           c.SubjectPeerID,
		Organization:            c.Organization,
		PeerProfile:             c.PeerProfile,
		PublicKey:               c.PublicKey,
		NotBefore:               c.NotBefore,
		NotAfter:                c.NotAfter,
		Serial:                  c.Serial,
		RevocationAuthority:     c.RevocationAuthority,
		AllowedProtocolVersions: c.AllowedProtocolVersions,
		BuildChannel:            c.BuildChannel,
		DeploymentZone:          c.DeploymentZone,
	})
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
		return "", errors.New("dari: failed to encode credential for signing")
	}
	sign1, err := CreateCOSESign1(signingBytes, priv, []byte(c.Serial))
	if err != nil {
		return "", fmt.Errorf("dari: sign credential: %w", err)
	}
	encoded, err := EncodeCOSESign1(sign1)
	if err != nil {
		return "", fmt.Errorf("dari: encode credential signature: %w", err)
	}
	c.SignedCredential = append(c.SignedCredential[:0], encoded...)
	return hex.EncodeToString(encoded), nil
}

// VerifySignature verifies the credential's COSE-Sign1 signature using
// the issuer's public key.
func (c *PeerCredential) VerifySignature(pub ed25519.PublicKey, signatureHex string) error {
	signingBytes := c.SigningBytes()
	if signingBytes == nil {
		return errors.New("dari: failed to encode credential for verification")
	}
	encoded, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("dari: decode credential signature: %w", err)
	}
	sign1, err := DecodeCOSESign1(encoded)
	if err != nil {
		return err
	}
	if err := VerifyCOSESign1(sign1, pub); err != nil {
		return err
	}
	if !bytes.Equal(sign1.Payload, signingBytes) {
		return errors.New("dari: signed credential payload does not match presented credential")
	}
	return nil
}

// DecodePeerCredential decodes the canonical credential body carried as the
// payload of a COSE-Sign1 credential.
func DecodePeerCredential(payload []byte) (*PeerCredential, error) {
	var body peerCredentialSigningBody
	if err := UnmarshalCBOR(payload, &body); err != nil {
		return nil, fmt.Errorf("dari: decode peer credential: %w", err)
	}
	return &PeerCredential{
		CredentialVersion:       body.CredentialVersion,
		Issuer:                  body.Issuer,
		SubjectPeerID:           body.SubjectPeerID,
		Organization:            body.Organization,
		PeerProfile:             body.PeerProfile,
		PublicKey:               body.PublicKey,
		NotBefore:               body.NotBefore,
		NotAfter:                body.NotAfter,
		Serial:                  body.Serial,
		RevocationAuthority:     body.RevocationAuthority,
		AllowedProtocolVersions: body.AllowedProtocolVersions,
		BuildChannel:            body.BuildChannel,
		DeploymentZone:          body.DeploymentZone,
	}, nil
}

// PeerProofSigningBytes returns the deterministic, domain-separated bytes
// signed by the credential subject. The transcript argument is the complete
// negotiated authentication-context hash assembled by the transport.
func PeerProofSigningBytes(transcript, challengeID []byte, revocationEpoch uint64) []byte {
	h := sha256.New()
	h.Write([]byte("DARI-AUTH-PROOF-v1\x00"))
	writeLengthPrefixed(h, transcript)
	writeLengthPrefixed(h, challengeID)
	var epoch [8]byte
	binary.BigEndian.PutUint64(epoch[:], revocationEpoch)
	h.Write(epoch[:])
	return h.Sum(nil)
}

// EncodeRevocationEpoch encodes the revocation checkpoint carried in the
// AUTH_PROOF revocation-evidence field.
func EncodeRevocationEpoch(epoch uint64) []byte {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, epoch)
	return encoded
}

// DecodeRevocationEpoch decodes the fixed-width AUTH_PROOF revocation epoch.
func DecodeRevocationEpoch(evidence []byte) (uint64, error) {
	if len(evidence) != 8 {
		return 0, fmt.Errorf("dari: revocation evidence must be 8 bytes, got %d", len(evidence))
	}
	return binary.BigEndian.Uint64(evidence), nil
}

func writeLengthPrefixed(h interface{ Write([]byte) (int, error) }, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	h.Write(length[:])
	h.Write(value)
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

// DecodeRevocationEpochOrZero is the non-erroring variant used by
// debug logging.
func DecodeRevocationEpochOrZero(evidence []byte) uint64 {
	v, err := DecodeRevocationEpoch(evidence)
	if err != nil {
		return 0
	}
	return v
}
