package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// TrustBundle is the relay's local authentication trust and revocation view.
// RevokedSerials maps a credential serial to the epoch in which it was revoked.
type TrustBundle struct {
	Issuers         map[string]ed25519.PublicKey
	ProtocolVersion uint8
	AllowedProfiles map[dari.PeerProfile]bool
	RevocationEpoch uint64
	RevokedSerials  map[string]uint64
	Now             func() time.Time
}

// PeerAuthenticator validates signed credentials and transcript-bound proofs.
type PeerAuthenticator struct {
	mu    sync.RWMutex
	trust TrustBundle
}

// NewPeerAuthenticator creates a verifier with an isolated copy of trust data.
func NewPeerAuthenticator(trust TrustBundle) *PeerAuthenticator {
	if trust.ProtocolVersion == 0 {
		trust.ProtocolVersion = 1
	}
	if trust.Now == nil {
		trust.Now = time.Now
	}
	trust.Issuers = cloneIssuerKeys(trust.Issuers)
	trust.AllowedProfiles = cloneProfiles(trust.AllowedProfiles)
	trust.RevokedSerials = cloneRevokedSerials(trust.RevokedSerials)
	return &PeerAuthenticator{trust: trust}
}

// VerifyPeerProof verifies the issuer-signed credential and the subject's
// proof of possession over the supplied transport transcript and challenge.
func (a *PeerAuthenticator) VerifyPeerProof(ctx context.Context, transcript []byte, proof *dari.AuthProofMessage) (*dari.PeerCredential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if proof == nil {
		return nil, errors.New("relay: nil peer authentication proof")
	}
	if len(transcript) == 0 || len(proof.ChallengeID) == 0 {
		return nil, errors.New("relay: incomplete peer authentication transcript")
	}
	if proof.KeyAlgorithm != dari.COSEAlgEdDSA {
		return nil, fmt.Errorf("relay: unsupported peer proof algorithm %d", proof.KeyAlgorithm)
	}
	proofEpoch, err := dari.DecodeRevocationEpoch(proof.RevocationEvidence)
	if err != nil {
		return nil, fmt.Errorf("relay: decode peer revocation evidence: %w", err)
	}

	sign1, err := dari.DecodeCOSESign1(proof.Credential)
	if err != nil {
		return nil, fmt.Errorf("relay: decode peer credential: %w", err)
	}
	cred, err := dari.DecodePeerCredential(sign1.Payload)
	if err != nil {
		return nil, err
	}

	a.mu.RLock()
	issuerKey, issuerTrusted := a.trust.Issuers[cred.Issuer]
	protocolVersion := a.trust.ProtocolVersion
	profileAllowed := len(a.trust.AllowedProfiles) == 0 || a.trust.AllowedProfiles[cred.PeerProfile]
	revocationEpoch := a.trust.RevocationEpoch
	_, serialRevoked := a.trust.RevokedSerials[cred.Serial]
	now := a.trust.Now()
	a.mu.RUnlock()

	if !issuerTrusted {
		return nil, fmt.Errorf("relay: untrusted peer credential issuer %q", cred.Issuer)
	}
	if err := cred.VerifySignature(issuerKey, hex.EncodeToString(proof.Credential)); err != nil {
		return nil, fmt.Errorf("relay: verify peer credential: %w", err)
	}
	if cred.CredentialVersion != 1 {
		return nil, fmt.Errorf("relay: unsupported peer credential version %d", cred.CredentialVersion)
	}
	if cred.SubjectPeerID == "" || cred.Organization == "" || cred.Serial == "" || len(cred.PublicKey) != ed25519.PublicKeySize {
		return nil, errors.New("relay: incomplete peer credential")
	}
	if !knownPeerProfile(cred.PeerProfile) || !profileAllowed {
		return nil, fmt.Errorf("relay: peer profile %q is not trusted", cred.PeerProfile)
	}
	if !cred.IsValidAt(now) {
		return nil, errors.New("relay: peer credential is outside its validity window")
	}
	if !containsProtocolVersion(cred.AllowedProtocolVersions, protocolVersion) {
		return nil, fmt.Errorf("relay: peer credential does not allow protocol version %d", protocolVersion)
	}
	if proofEpoch != revocationEpoch {
		return nil, fmt.Errorf("relay: stale peer revocation epoch %d (current %d)", proofEpoch, revocationEpoch)
	}
	if serialRevoked {
		return nil, fmt.Errorf("relay: peer credential serial %s is revoked", cred.Serial)
	}

	signingBytes := dari.PeerProofSigningBytes(transcript, proof.ChallengeID, proofEpoch)
	if !dari.VerifyEd25519(ed25519.PublicKey(cred.PublicKey), signingBytes, proof.Signature) {
		return nil, errors.New("relay: peer proof-of-possession verification failed")
	}
	cred.SignedCredential = append([]byte(nil), proof.Credential...)
	return cred, nil
}

// Revoke records a credential revocation and advances the verifier's epoch.
func (a *PeerAuthenticator) Revoke(serial string, epoch uint64) {
	if serial == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if epoch > a.trust.RevocationEpoch {
		a.trust.RevocationEpoch = epoch
	}
	if a.trust.RevokedSerials == nil {
		a.trust.RevokedSerials = make(map[string]uint64)
	}
	a.trust.RevokedSerials[serial] = epoch
}

// AdvanceEpoch moves the verifier's revocation epoch forward without
// adding a serial. New connections must then present revocation
// evidence at or above the new epoch.
func (a *PeerAuthenticator) AdvanceEpoch(epoch uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if epoch > a.trust.RevocationEpoch {
		a.trust.RevocationEpoch = epoch
	}
}

// RevocationEpoch returns the verifier's current revocation epoch.
func (a *PeerAuthenticator) RevocationEpoch() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.trust.RevocationEpoch
}

func (a *PeerAuthenticator) isRevoked(serial string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, revoked := a.trust.RevokedSerials[serial]
	return revoked
}

func containsProtocolVersion(versions []uint8, wanted uint8) bool {
	for _, version := range versions {
		if version == wanted {
			return true
		}
	}
	return false
}

func knownPeerProfile(profile dari.PeerProfile) bool {
	switch profile {
	case dari.ProfileHarness, dari.ProfileInference, dari.ProfileRelay, dari.ProfileControl:
		return true
	default:
		return false
	}
}

func cloneIssuerKeys(src map[string]ed25519.PublicKey) map[string]ed25519.PublicKey {
	dst := make(map[string]ed25519.PublicKey, len(src))
	for issuer, key := range src {
		dst[issuer] = append(ed25519.PublicKey(nil), key...)
	}
	return dst
}

func cloneProfiles(src map[dari.PeerProfile]bool) map[dari.PeerProfile]bool {
	dst := make(map[dari.PeerProfile]bool, len(src))
	for profile, allowed := range src {
		dst[profile] = allowed
	}
	return dst
}

func cloneRevokedSerials(src map[string]uint64) map[string]uint64 {
	dst := make(map[string]uint64, len(src))
	for serial, epoch := range src {
		dst[serial] = epoch
	}
	return dst
}
