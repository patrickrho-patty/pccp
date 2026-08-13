package paper_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/paper"
)

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func legacyPaper1Hello(t *testing.T) []byte {
	t.Helper()
	got, err := paper.MarshalCBOR(paper.HelloMessage{
		CoreVersions: []uint8{1},
		PeerProfile:  paper.ProfileHarness,
		ClientNonce:  make([]byte, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestLegacyPaper1HelloGoldenVector(t *testing.T) {
	gotHex := hex.EncodeToString(legacyPaper1Hello(t))
	wantHex := strings.TrimSpace(readFixture(t, "../../conformance/testdata/paper1/hello.cbor.hex"))
	if gotHex != wantHex {
		t.Fatal("legacy HELLO changed")
	}
}

func TestLegacyPaper1RecordGoldenVector(t *testing.T) {
	var got bytes.Buffer
	err := paper.EncodeRecord(&got, &paper.Record{
		Kind:        paper.KindControl,
		MessageType: uint16(paper.MsgHello),
		Payload:     legacyPaper1Hello(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	gotHex := hex.EncodeToString(got.Bytes())
	wantHex := strings.TrimSpace(readFixture(t, "../../conformance/testdata/paper1/record.bin.hex"))
	if gotHex != wantHex {
		t.Fatal("legacy HELLO record changed")
	}
}
