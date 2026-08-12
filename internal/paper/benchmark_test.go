package paper

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"testing"
)

func BenchmarkRecordRoundTrip(b *testing.B) {
	for _, payloadSize := range []int{1024, 64 * 1024, 1024 * 1024} {
		b.Run(fmt.Sprintf("payload_%d", payloadSize), func(b *testing.B) {
			record := &Record{
				Kind:         KindData,
				Flags:        FlagFinal,
				Header:       []byte("benchmark-header"),
				Payload:      bytes.Repeat([]byte{0xA5}, payloadSize),
				LaneID:       1,
				LaneSequence: 1,
			}

			b.ReportAllocs()
			b.SetBytes(int64(payloadSize))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var encoded bytes.Buffer
				encoded.Grow(PrefaceSize + len(record.Header) + len(record.Payload))
				if err := EncodeRecord(&encoded, record); err != nil {
					b.Fatal(err)
				}
				if _, err := DecodeRecord(&encoded); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCanonicalCBOR(b *testing.B) {
	message := HelloMessage{
		CoreVersions:   []uint8{1},
		PeerProfile:    ProfileHarness,
		CryptoProfiles: []string{"PAPER-BASE-1"},
		ClientNonce:    bytes.Repeat([]byte{0xAB}, 32),
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		encoded, err := MarshalCBOR(message)
		if err != nil {
			b.Fatal(err)
		}
		var decoded HelloMessage
		if err := UnmarshalCBOR(encoded, &decoded); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvidenceChainNext(b *testing.B) {
	root := EvidenceChainStart([]byte("exchange-open-digest"))
	event := bytes.Repeat([]byte{0x5A}, 32)

	b.ReportAllocs()
	b.SetBytes(int64(len(root) + len(event)))
	for i := 0; i < b.N; i++ {
		root = EvidenceChainNext(root, event)
	}
}

func BenchmarkObjectDigest(b *testing.B) {
	payload := bytes.Repeat([]byte{0xC3}, 1024)

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for i := 0; i < b.N; i++ {
		_ = ComputeObjectDigest(ObjTypeProvenanceNode, payload)
	}
}

func BenchmarkEd25519SignVerify(b *testing.B) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	message := bytes.Repeat([]byte{0x3C}, 32)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		signature, err := SignWithEd25519(privateKey, message)
		if err != nil {
			b.Fatal(err)
		}
		if !VerifyEd25519(publicKey, message, signature) {
			b.Fatal("signature verification failed")
		}
	}
}
