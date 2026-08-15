package dari

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// COSEAlgorithm identifies the signature algorithm in a COSE-Sign1 structure.
type COSEAlgorithm int

const (
	COSEAlgEdDSA COSEAlgorithm = -8 // Ed25519
	COSEAlgES256 COSEAlgorithm = -7 // ECDSA P-256
)

// COSEKeyType identifies the key type in a COSE key structure.
type COSEKeyType int

const (
	COSEKeyTypeOKP COSEKeyType = 1 // Octet Key Pair (Ed25519)
	COSEKeyTypeEC2 COSEKeyType = 2 // Elliptic Curve
)

// COSEHeader is the protected/unprotected header map.
type COSEHeader struct {
	Alg   COSEAlgorithm       `cbor:"1,keyasint,omitempty"`
	KID   []byte              `cbor:"4,keyasint,omitempty"`
	Other map[int]interface{} `cbor:"-9999,toarray,omitempty"`
}

// COSESign1 is a COSE-Sign1 signed data structure (RFC 8152 §4.2).
type COSESign1 struct {
	Protected   []byte     `cbor:"0,keyasint"` // encoded protected header
	Unprotected COSEHeader `cbor:"1,keyasint"` // unprotected header
	Payload     []byte     `cbor:"2,keyasint"` // the signed content
	Signature   []byte     `cbor:"3,keyasint"` // signature over Sig_structure
}

// SigStructure is the structure that gets signed (RFC 8152 §4.4).
type SigStructure struct {
	Context       string `cbor:"0,keyasint"` // "Signature1"
	BodyProtected []byte `cbor:"1,keyasint"`
	ExternalAAD   []byte `cbor:"2,keyasint"` // empty in our case
	Payload       []byte `cbor:"3,keyasint"`
}

// CreateCOSESign1 creates a COSE-Sign1 structure over the given payload.
func CreateCOSESign1(payload []byte, privKey ed25519.PrivateKey, keyID []byte) (*COSESign1, error) {
	if len(privKey) == 0 {
		return nil, errors.New("dari: empty private key")
	}

	// Build protected header
	protectedHeader := COSEHeader{
		Alg: COSEAlgEdDSA,
		KID: keyID,
	}
	protectedBytes, err := MarshalCBOR(protectedHeader)
	if err != nil {
		return nil, fmt.Errorf("dari: marshal protected header: %w", err)
	}

	// Build Sig_structure as the RFC 8152 §4.4 ARRAY form:
	// ["Signature1", body_protected, external_aad, payload]. The
	// connector recomputes exactly this array; a map-form encoding
	// would never verify cross-repo.
	sigInput := []interface{}{
		"Signature1",
		protectedBytes,
		[]byte{},
		payload,
	}
	sigBytes, err := MarshalCBOR(sigInput)
	if err != nil {
		return nil, fmt.Errorf("dari: marshal sig structure: %w", err)
	}

	// Sign with Ed25519
	signature := ed25519.Sign(privKey, sigBytes)

	return &COSESign1{
		Protected:   protectedBytes,
		Unprotected: COSEHeader{},
		Payload:     payload,
		Signature:   signature,
	}, nil
}

// VerifyCOSESign1 verifies a COSE-Sign1 structure using the given public key.
func VerifyCOSESign1(sign1 *COSESign1, pubKey ed25519.PublicKey) error {
	if sign1 == nil {
		return errors.New("dari: nil COSE-Sign1")
	}
	if len(pubKey) == 0 {
		return errors.New("dari: empty public key")
	}

	// Reconstruct the RFC 8152 array-form Sig_structure (must equal
	// the connector's recomputation byte-for-byte).
	sigInput := []interface{}{
		"Signature1",
		sign1.Protected,
		[]byte{},
		sign1.Payload,
	}
	sigBytes, err := MarshalCBOR(sigInput)
	if err != nil {
		return fmt.Errorf("dari: marshal sig structure for verify: %w", err)
	}

	// Verify Ed25519 signature
	if !ed25519.Verify(pubKey, sigBytes, sign1.Signature) {
		return errors.New("dari: COSE-Sign1 signature verification failed")
	}

	return nil
}

// EncodeCOSESign1 encodes a COSE-Sign1 structure to CBOR bytes.
func EncodeCOSESign1(sign1 *COSESign1) ([]byte, error) {
	return MarshalCBOR(sign1)
}

// DecodeCOSESign1 decodes CBOR bytes to a COSE-Sign1 structure.
func DecodeCOSESign1(data []byte) (*COSESign1, error) {
	var sign1 COSESign1
	if err := UnmarshalCBOR(data, &sign1); err != nil {
		return nil, fmt.Errorf("dari: decode COSE-Sign1: %w", err)
	}
	return &sign1, nil
}

// COSESign1Hex is a convenience function that creates a COSE-Sign1 and
// returns it as a hex-encoded string (for storage in database text fields).
func COSESign1Hex(payload []byte, privKey ed25519.PrivateKey, keyID []byte) (string, error) {
	sign1, err := CreateCOSESign1(payload, privKey, keyID)
	if err != nil {
		return "", err
	}
	encoded, err := EncodeCOSESign1(sign1)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(encoded), nil
}

// VerifyCOSESign1Hex verifies a hex-encoded COSE-Sign1.
func VerifyCOSESign1Hex(hexStr string, pubKey ed25519.PublicKey) error {
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return fmt.Errorf("dari: decode hex: %w", err)
	}
	sign1, err := DecodeCOSESign1(data)
	if err != nil {
		return err
	}
	return VerifyCOSESign1(sign1, pubKey)
}

// PayloadDigest computes the SHA-256 digest of a COSE-Sign1 payload.
func PayloadDigest(payload []byte) string {
	h := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(h[:])
}
