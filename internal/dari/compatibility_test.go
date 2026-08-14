package dari

import (
	"bytes"
	"testing"
)

// compatibility_test.go pins the legacy `paper/1` compatibility
// surface byte-for-byte (master plan Task 22). These values are
// protocol-frozen artifacts; changing them breaks every deployed
// transport.

func TestLegacyPaper1PrefaceFrozen(t *testing.T) {
	want := []byte{0x50, 0x41, 0x50, 0x45, 0x52, 0x00, 0x01, 0x0A}
	if !bytes.Equal(LegacyPaper1Preface, want) {
		t.Fatalf("legacy preface bytes changed: got %x want %x", LegacyPaper1Preface, want)
	}
}

func TestLegacyPaper1ALPNLiteral(t *testing.T) {
	if LegacyPaper1ALPN != "paper/1" {
		t.Fatalf("legacy ALPN literal changed: %q", LegacyPaper1ALPN)
	}
}

func TestALPNPreferenceDARIFirst(t *testing.T) {
	protos := DARIProtocols()
	if len(protos) != 2 || protos[0] != DARIProtocol || protos[1] != LegacyPaper1ALPN {
		t.Fatalf("ALPN preference must be [dari/1, paper/1], got %v", protos)
	}
}
