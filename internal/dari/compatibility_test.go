package dari_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// compatibility_test.go pins the legacy `paper/1` compatibility
// surface byte-for-byte (master plan Task 5 + Task 22). If anyone
// changes the preface bytes, the legacy ALPN literal, the golden
// HELLO/record encodings, or the message-type allocation, this file
// fails: these values are protocol-frozen compatibility artifacts.

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
	got, err := dari.MarshalCBOR(dari.HelloMessage{
		CoreVersions: []uint8{1},
		PeerProfile:  dari.ProfileHarness,
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
	err := dari.EncodeRecord(&got, &dari.Record{
		Kind:        dari.KindControl,
		MessageType: uint16(dari.MsgHello),
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

func TestLegacyPaper1PrefaceFrozen(t *testing.T) {
	want := []byte{0x50, 0x41, 0x50, 0x45, 0x52, 0x00, 0x01, 0x0A}
	if !bytes.Equal(dari.LegacyPaper1Preface, want) {
		t.Fatalf("legacy preface bytes changed: got %x want %x", dari.LegacyPaper1Preface, want)
	}
}

func TestLegacyPaper1ALPNLiteral(t *testing.T) {
	if dari.LegacyPaper1ALPN != "paper/1" {
		t.Fatalf("legacy ALPN literal changed: %q", dari.LegacyPaper1ALPN)
	}
}

func TestALPNPreferenceDARIFirst(t *testing.T) {
	protos := dari.DARIProtocols()
	if len(protos) != 2 || protos[0] != dari.DARIProtocol || protos[1] != dari.LegacyPaper1ALPN {
		t.Fatalf("ALPN preference must be [dari/1, paper/1], got %v", protos)
	}
}

// TestCoreMessageAllocationFrozen pins the core message registry the
// legacy profile freezes numerically (compat map §6 rule 3): HELLO
// 0x0001 … PONG 0x0004, DRAIN 0x0005, CLOSE 0x0006. A renumber here
// silently breaks every deployed peer.
func TestCoreMessageAllocationFrozen(t *testing.T) {
	cases := map[string]struct {
		got  dari.MessageType
		want uint16
	}{
		"HELLO":    {dari.MsgHello, 0x0001},
		"HELLOACK": {dari.MsgHelloAck, 0x0002},
		"PING":     {dari.MsgPing, 0x0003},
		"PONG":     {dari.MsgPong, 0x0004},
		"DRAIN":    {dari.MsgDrain, 0x0005},
		"CLOSE":    {dari.MsgClose, 0x0006},
	}
	for name, c := range cases {
		if uint16(c.got) != c.want {
			t.Errorf("%s renumbered: got 0x%04x want 0x%04x", name, uint16(c.got), c.want)
		}
	}
}
