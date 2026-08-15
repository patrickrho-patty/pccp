package dari

import (
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

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

	// Strict decoding (F.2: finite depth/item-count limits; duplicate
	// map keys rejected — a required F.14-1 negative case).
	decMode, err := cbor.DecOptions{
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		MaxNestedLevels:  32,
		MaxArrayElements: 1 << 20,
		MaxMapPairs:      1 << 20,
		IndefLength:      cbor.IndefLengthForbidden,
	}.DecMode()
	if err != nil {
		panic(fmt.Sprintf("dari: init cbor decoder mode: %v", err))
	}
	defaultDecMode = decMode
}

var (
	defaultEncMode cbor.EncMode
	defaultDecMode cbor.DecMode
)

// MarshalCBOR encodes a value to deterministic CBOR bytes.
func MarshalCBOR(v interface{}) ([]byte, error) {
	return defaultEncMode.Marshal(v)
}

// UnmarshalCBOR decodes CBOR bytes into a value under the strict
// decoder (duplicate keys and indefinite lengths rejected).
func UnmarshalCBOR(data []byte, v interface{}) error {
	return defaultDecMode.Unmarshal(data, v)
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
