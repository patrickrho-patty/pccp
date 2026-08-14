package dari

import (
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// cborEncoderMode is the deterministic encoding mode for DARI objects.
// Per DARI §10.1, objects that are hashed, signed, or content-addressed MUST
// use deterministic encoding.
var cborEncoderMode func(opts cbor.EncOptions) (cbor.EncMode, error)

func init() {
	opts := cbor.EncOptions{
		Sort:        cbor.SortCoreDeterministic, // RFC 8949 Core Deterministic
		IndefLength: cbor.IndefLengthForbidden,
		Time:        cbor.TimeUnix,
	}
	mode, err := opts.EncMode()
	if err != nil {
		panic(fmt.Sprintf("dari: init cbor encoder mode: %v", err))
	}
	defaultEncMode = mode
}

var defaultEncMode cbor.EncMode

// MarshalCBOR encodes a value to deterministic CBOR bytes.
func MarshalCBOR(v interface{}) ([]byte, error) {
	return defaultEncMode.Marshal(v)
}

// UnmarshalCBOR decodes CBOR bytes into a value.
func UnmarshalCBOR(data []byte, v interface{}) error {
	return cbor.Unmarshal(data, v)
}

// CanonicalCBOR returns the deterministic CBOR encoding of a map.
// Used for content addressing where the canonical bytes matter.
func CanonicalCBOR(v interface{}) ([]byte, error) {
	return MarshalCBOR(v)
}

// CBOREncode encodes any value to CBOR (alias for MarshalCBOR).
func CBOREncode(v interface{}) ([]byte, error) {
	return MarshalCBOR(v)
}

// CBORDecode decodes CBOR bytes (alias for UnmarshalCBOR).
func CBORDecode(data []byte, v interface{}) error {
	return UnmarshalCBOR(data, v)
}
