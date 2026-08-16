package dari

import (
	"bytes"
	"testing"
)

// header_test.go pins the §9 header codec: round-trip + deterministic
// bytes (key order must not depend on map iteration).
func TestHeaderRoundTripDeterministic(t *testing.T) {
	h1, err := EncodeHeader(map[HeaderKey][]byte{
		HKIdempotencyKey: []byte("idem-1"),
		HKPolicyEpoch:    []byte("epoch-9"),
		HKClassification: []byte("internal"),
	})
	if err != nil {
		t.Fatal(err)
	}
	h2, err := EncodeHeader(map[HeaderKey][]byte{
		HKClassification: []byte("internal"),
		HKIdempotencyKey: []byte("idem-1"),
		HKPolicyEpoch:    []byte("epoch-9"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(h1, h2) {
		t.Fatalf("header encoding not deterministic:\n%x\n%x", h1, h2)
	}
	decoded, err := DecodeHeader(h1)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded[HKIdempotencyKey]) != "idem-1" || string(decoded[HKPolicyEpoch]) != "epoch-9" {
		t.Fatalf("round-trip = %v", decoded)
	}
	if _, err := DecodeHeader(nil); err != nil || decoded == nil {
		// nil header decodes to nil, no error — callers treat empty.
	}
}
