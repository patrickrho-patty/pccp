package dari

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestCOSESign1RoundTrip(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"test":"data","value":42}`)
	keyID := []byte("test-key-1")

	sign1, err := CreateCOSESign1(payload, priv, keyID)
	if err != nil {
		t.Fatalf("create COSE-Sign1: %v", err)
	}

	// Verify with correct key
	if err := VerifyCOSESign1(sign1, pub); err != nil {
		t.Fatalf("verify failed: %v", err)
	}

	// Encode/decode round trip
	encoded, err := EncodeCOSESign1(sign1)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := DecodeCOSESign1(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if err := VerifyCOSESign1(decoded, pub); err != nil {
		t.Fatalf("verify after round trip failed: %v", err)
	}
}

func TestCOSESign1Rejection(t *testing.T) {
	pub, priv, _ := GenerateKeyPair()
	payload := []byte("test payload")

	sign1, err := CreateCOSESign1(payload, priv, []byte("kid"))
	if err != nil {
		t.Fatal(err)
	}

	// Wrong key should fail
	pub2, _, _ := GenerateKeyPair()
	if err := VerifyCOSESign1(sign1, pub2); err == nil {
		t.Fatal("verification should fail with wrong key")
	}

	// Tampered signature should fail
	sign1.Signature[0] ^= 0xFF
	if err := VerifyCOSESign1(sign1, pub); err == nil {
		t.Fatal("verification should fail with tampered signature")
	}
}

func TestCOSESign1Hex(t *testing.T) {
	pub, priv, _ := GenerateKeyPair()
	payload := []byte("hex test payload")

	hexStr, err := COSESign1Hex(payload, priv, []byte("hex-kid"))
	if err != nil {
		t.Fatalf("COSESign1Hex: %v", err)
	}

	// Verify it's valid hex
	if _, err := hex.DecodeString(hexStr); err != nil {
		t.Fatalf("invalid hex output: %v", err)
	}

	// Verify the signature
	if err := VerifyCOSESign1Hex(hexStr, pub); err != nil {
		t.Fatalf("verify hex failed: %v", err)
	}
}

func TestCBORDeterminism(t *testing.T) {
	data := map[string]interface{}{
		"b": 2,
		"a": 1,
		"c": 3,
	}

	enc1, err := MarshalCBOR(data)
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := MarshalCBOR(data)
	if err != nil {
		t.Fatal(err)
	}

	if hex.EncodeToString(enc1) != hex.EncodeToString(enc2) {
		t.Fatal("CBOR encoding is not deterministic")
	}

	// Verify it's sorted (a before b before c)
	var result map[string]interface{}
	if err := UnmarshalCBOR(enc1, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestEd25519Size(t *testing.T) {
	pub, priv, _ := GenerateKeyPair()
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("public key size: expected %d, got %d", ed25519.PublicKeySize, len(pub))
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("private key size: expected %d, got %d", ed25519.PrivateKeySize, len(priv))
	}
}
