package dari

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// HashProfile identifies the hash algorithm used for content addressing.
// Baseline interoperable hash per DARI §37.1 is SHA-256.
const HashProfile = "DARI-BASE-1"

// ObjectType identifies a registered DARI object type for domain-separated
// content addressing (DARI §32).
type ObjectType uint16

// Object types used in the provenance spine and evidence chain.
const (
	ObjTypePeerCredential  ObjectType = 0x0100
	ObjTypeCapabilityLease ObjectType = 0x0200
	ObjTypePolicyEpoch     ObjectType = 0x0201
	ObjTypeGovernanceEnv   ObjectType = 0x0300
	ObjTypeRelayVerdict    ObjectType = 0x0301
	ObjTypeEvidenceReceipt ObjectType = 0x0302
	ObjTypeProvenanceNode  ObjectType = 0x0700
	ObjTypeChangeSet       ObjectType = 0x0701
	ObjTypeActionEnvelope  ObjectType = 0x0702
	ObjTypeModelPackage    ObjectType = 0x0500
	ObjTypeEndpointLease   ObjectType = 0x0501
	ObjTypeEndpointAttest  ObjectType = 0x0502
	ObjTypeRepoBaseline    ObjectType = 0x0503
)

// Digest is a SHA-256 content digest.
type Digest [32]byte

// String returns the hex encoding of the digest.
func (d Digest) String() string {
	return fmt.Sprintf("sha256:%x", d[:])
}

// Bytes returns the raw bytes.
func (d Digest) Bytes() []byte {
	return d[:]
}

// IsZero reports whether the digest is all zeros.
func (d Digest) IsZero() bool {
	for _, b := range d {
		if b != 0 {
			return false
		}
	}
	return true
}

// ComputeObjectDigest computes the content-addressed digest of a registered
// DARI object per §32:
//
//	digest = HASH("DARI-OBJ-v1\0" || uint16(T) || deterministic_cbor(O))
//
// The input must already be the deterministic CBOR encoding of the object
// *without* any embedded digest field.
func ComputeObjectDigest(objType ObjectType, canonicalCBOR []byte) Digest {
	h := shaa256New()
	h.Write([]byte("DARI-OBJ-v1\x00"))
	h.Write([]byte{0})
	var typeBytes [2]byte
	binary.BigEndian.PutUint16(typeBytes[:], uint16(objType))
	h.Write(typeBytes[:])
	h.Write(canonicalCBOR)
	return shaa256Sum(h)
}

// ComputeChunkDigest computes the content-addressed digest of a payload chunk
// per §32.
func ComputeChunkDigest(exchangeID, laneID, laneSeq, payload []byte) Digest {
	h := sha256.New()
	h.Write([]byte("DARI-CHUNK-v1\x00"))
	h.Write(exchangeID)
	h.Write(laneID)
	h.Write(laneSeq)
	h.Write(payload)
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

// Evidence chain domain separation strings (DARI §34).
var (
	evidenceStartPrefix = []byte("DARI-EVIDENCE-START-v1\x00")
	evidenceEventPrefix = []byte("DARI-EVIDENCE-EVENT-v1\x00")
)

// EvidenceChainStart computes R0 for an evidence chain.
func EvidenceChainStart(exchangeOpenDigest []byte) Digest {
	h := sha256.New()
	h.Write(evidenceStartPrefix)
	h.Write(exchangeOpenDigest)
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

// EvidenceChainNext computes R(i) from R(i-1) and an event digest.
func EvidenceChainNext(prev Digest, eventDigest []byte) Digest {
	h := sha256.New()
	h.Write(evidenceEventPrefix)
	h.Write(prev[:])
	h.Write(eventDigest)
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

// AuthContext computes the DARI authentication context hash (§18.2).
//
//	auth_context = HASH(
//	  "DARI-AUTH-v1" || canonical(HELLO) || canonical(HELLO_ACK) ||
//	  client_nonce || server_nonce || channel_binding || peer_credential_digest
//	)
func AuthContext(helloCBOR, helloAckCBOR, clientNonce, serverNonce, channelBinding, peerCredDigest []byte) Digest {
	h := sha256.New()
	h.Write([]byte("DARI-AUTH-v1"))
	h.Write(helloCBOR)
	h.Write(helloAckCBOR)
	h.Write(clientNonce)
	h.Write(serverNonce)
	h.Write(channelBinding)
	h.Write(peerCredDigest)
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

// SignWithEd25519 signs a message digest with an Ed25519 private key.
func SignWithEd25519(priv ed25519.PrivateKey, message []byte) ([]byte, error) {
	if len(priv) == 0 {
		return nil, errors.New("dari: empty private key")
	}
	return ed25519.Sign(priv, message), nil
}

// VerifyEd25519 verifies an Ed25519 signature.
func VerifyEd25519(pub ed25519.PublicKey, message, sig []byte) bool {
	if len(pub) == 0 || len(sig) == 0 {
		return false
	}
	return ed25519.Verify(pub, message, sig)
}

// GenerateKeyPair generates a new Ed25519 key pair.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(nil)
}

// internal helpers (avoid importing crypto/sha256 in the public API surface
// name) — these are thin wrappers that make the file self-documenting.
type shaHasher = interface {
	Write([]byte) (int, error)
	Sum([]byte) []byte
}

func shaa256New() shaHasher {
	return sha256.New()
}

func shaa256Sum(h shaHasher) Digest {
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}
