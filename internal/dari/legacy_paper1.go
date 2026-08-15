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

// Frozen `paper/1` cryptographic domain strings (spec legacy body +
// compat map §6 rule 2). The ACTIVE dari/1 kernel uses the DARI-*
// domains defined normatively in Appendix F (see crypto.go); these
// constants document the legacy values a paper/1 peer computes. A
// pre-rename peer interoperating on the legacy profile MUST be
// reached with THESE bytes; current builds compute the DARI domains
// (both endpoints deploy together and re-issue credentials on
// connect). Dual-kernel crypto selection by negotiated ALPN lands
// with the profile-negotiation work.
const (
	LegacyPaper1AuthDomain    = "PAPER-AUTH-v1"    // no trailing NUL
	LegacyPaper1ObjDomain     = "PAPER-OBJ-v1\x00" // incl. reserved zero byte
	LegacyPaper1ChunkDomain   = "PAPER-CHUNK-v1\x00"
	LegacyPaper1EvidenceStart = "PAPER-EVIDENCE-START-v1\x00"
	LegacyPaper1EvidenceEvent = "PAPER-EVIDENCE-EVENT-v1\x00"
)
