package dari

// legacy_paper1.go is the only home of the legacy `paper/1`
// compatibility surface on the relay side (master plan Task 22). The
// relay negotiates `dari/1` first and still accepts `paper/1` from a
// not-yet-migrated connector. The 8-byte connection preface is a
// frozen wire artifact; compatibility_test.go pins its bytes.

// LegacyPaper1ALPN is the historical ALPN identifier, still accepted.
const LegacyPaper1ALPN = "paper/1"

// DARIProtocol is the canonical ALPN identifier for DARI.
const DARIProtocol = "dari/1"

// LegacyPaper1Preface is the frozen 8-byte connection preface every
// DARI and legacy transport writes and expects ("P-A-P-E-R", version
// 1, frame-kind 0x0A). Historical bytes kept for wire compatibility.
var LegacyPaper1Preface = []byte{0x50, 0x41, 0x50, 0x45, 0x52, 0x00, 0x01, 0x0A}

// DARIProtocols returns the server's ALPN preference order: DARI
// first, legacy `paper/1` accepted for compatibility.
func DARIProtocols() []string { return []string{DARIProtocol, LegacyPaper1ALPN} }

// LegacyPaper1RelayAddrEnv is the pre-migration listen-address
// environment variable, still honored for compatibility.
const LegacyPaper1RelayAddrEnv = "PCCP_RELAY_PAPER_ADDR"
