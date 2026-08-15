// Package webbinding implements the dari.web/1 browser runtime
// profile (spec Appendix F.13 §10, master plan Task 13).
//
// The browser binding carries the SAME canonical DARI envelope as the
// native transport over two carriers: WebTransport over HTTP/3
// (primary) and a constrained WebSocket fallback. Authorization comes
// ONLY from the browser-held subject key proof of possession bound to
// the exact origin and channel — cookies, bearer tokens, and origin
// headers alone never authenticate (map §10).
package webbinding

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// Origin is a normalized web origin.
type Origin struct {
	Scheme string
	Host   string
	Port   string
}

// String renders scheme://host[:port] (port omitted when default).
func (o Origin) String() string {
	if o.Port == "" || (o.Scheme == "https" && o.Port == "443") {
		return o.Scheme + "://" + o.Host
	}
	return o.Scheme + "://" + o.Host + ":" + o.Port
}

// TopLevelSite approximates the top-level site (eTLD+1 without a PSL
// dependency: last two labels of a multi-label host; the host itself
// when it has fewer).
func (o Origin) TopLevelSite() string {
	labels := strings.Split(o.Host, ".")
	if len(labels) <= 2 {
		return o.Host
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

// NormalizeOrigin parses and validates an origin. Browser origins are
// https (http allowed only for loopback development); userinfo, path,
// query, and fragment are rejected.
func NormalizeOrigin(raw string) (Origin, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Origin{}, fmt.Errorf("webbinding: invalid origin: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return Origin{}, errors.New("webbinding: origin must be absolute")
	}
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return Origin{}, errors.New("webbinding: origin must not carry userinfo/path/query/fragment")
	}
	host := u.Hostname()
	port := u.Port()
	switch u.Scheme {
	case "https":
		if port == "" {
			port = "443"
		}
	case "http":
		if host != "localhost" && !strings.HasPrefix(host, "127.") {
			return Origin{}, errors.New("webbinding: http origin allowed only for loopback")
		}
		if port == "" {
			port = "80"
		}
	default:
		return Origin{}, errors.New("webbinding: origin scheme must be https (or loopback http)")
	}
	return Origin{Scheme: u.Scheme, Host: host, Port: port}, nil
}

// ---------------------------------------------------------------------------
// Channel binding (F.3: SHA-256("DARI-CHANNEL-BINDING-v1\0" || value)).
// ---------------------------------------------------------------------------

// ChannelBinding derives the binding value from the carrier's
// exported session material: the WebTransport session ID or the
// WebSocket handshake accept token.
func ChannelBinding(carrierValue []byte) [32]byte {
	return dari.KernelObjectDigestRaw("DARI-CHANNEL-BINDING-v1\x00", carrierValue)
}

// ---------------------------------------------------------------------------
// Browser proof of possession.
// ---------------------------------------------------------------------------

// ProofDomain separates browser proofs from every other signature.
const ProofDomain = "DARI-WEB-PROOF-v1\x00"

// Challenge is a one-use server-issued challenge.
type Challenge struct {
	ID        string
	Nonce     [32]byte
	Origin    string
	ExpiresAt time.Time
	used      bool
}

// BrowserProof is the proof-of-possession presented at Open/Reconnect.
type BrowserProof struct {
	Origin               string
	ReconnectSessionID   string // empty on first open
	SubjectKeyThumbprint [32]byte
	ChallengeID          string
	ChannelBinding       [32]byte
	Signature            []byte // ed25519 by the browser subject key
}

// ProofSigningBytes derives the canonical signed bytes:
// domain || origin || session || thumbprint || challenge || binding.
func (p *BrowserProof) ProofSigningBytes(challengeNonce [32]byte) []byte {
	h := sha256.New()
	h.Write([]byte(ProofDomain))
	lp := func(b []byte) {
		var l [4]byte
		l[0] = byte(len(b) >> 24)
		l[1] = byte(len(b) >> 16)
		l[2] = byte(len(b) >> 8)
		l[3] = byte(len(b))
		h.Write(l[:])
		h.Write(b)
	}
	lp([]byte(p.Origin))
	lp([]byte(p.ReconnectSessionID))
	lp(p.SubjectKeyThumbprint[:])
	lp([]byte(p.ChallengeID))
	lp(challengeNonce[:])
	lp(p.ChannelBinding[:])
	return h.Sum(nil)
}

// VerifyBrowserProof checks the proof under the subject key against
// the expected origin and channel binding and the one-use challenge.
func VerifyBrowserProof(p *BrowserProof, subjectKey ed25519.PublicKey, expectOrigin string, expectBinding [32]byte, challenge *Challenge) error {
	if challenge == nil {
		return errors.New("webbinding: unknown challenge")
	}
	if challenge.used {
		return errors.New("webbinding: challenge already used")
	}
	if time.Now().After(challenge.ExpiresAt) {
		return errors.New("webbinding: challenge expired")
	}
	if p.Origin != expectOrigin || p.Origin != challenge.Origin {
		return errors.New("webbinding: proof origin mismatch")
	}
	if p.ChallengeID != challenge.ID {
		return errors.New("webbinding: proof challenge mismatch")
	}
	if p.ChannelBinding != expectBinding {
		return errors.New("webbinding: proof channel binding mismatch")
	}
	thumb := SubjectThumbprint(subjectKey)
	if p.SubjectKeyThumbprint != thumb {
		return errors.New("webbinding: subject key thumbprint mismatch")
	}
	if !ed25519.Verify(subjectKey, p.ProofSigningBytes(challenge.Nonce), p.Signature) {
		return errors.New("webbinding: proof signature invalid")
	}
	return nil
}

// SubjectThumbprint mirrors dari.SubjectKeyThumbprint.
func SubjectThumbprint(pub ed25519.PublicKey) [32]byte {
	return dari.SubjectKeyThumbprint(pub)
}

// ErrMissingProofOfPossession is the cookie/bearer-only rejection.
var ErrMissingProofOfPossession = errors.New("webbinding: missing proof of possession")

// ParsePort extracts a numeric port for rate-limit keys.
func ParsePort(o Origin) (int, error) {
	return strconv.Atoi(o.Port)
}

// ed25519PubKey aliases the concrete key type used by carriers.
type ed25519PubKey = ed25519.PublicKey

func ed25519Pub(raw []byte) ed25519PubKey { return ed25519.PublicKey(raw) }
