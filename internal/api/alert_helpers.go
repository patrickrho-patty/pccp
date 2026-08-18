package api

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
)

// isAcceptableAlertTarget enforces the SSRF-safe URL contract for
// alert endpoints. PAT-1502 PR 2.
func isAcceptableAlertTarget(providerType, raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if err := assertPublicHost(raw); err != nil {
		return false
	}
	return true
}

// assertPublicHost refuses loopback, private, link-local, and
// unspecified addresses. Both literal IPs and resolved names are
// checked. PAT-1502 PR 2.
func assertPublicHost(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	// If the host is an IP, check directly. Otherwise resolve and
	// check every returned address.
	if ip := net.ParseIP(host); ip != nil {
		return rejectIfNonPublic(ip)
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("could not resolve host")
	}
	for _, ip := range addrs {
		if err := rejectIfNonPublic(ip); err != nil {
			return err
		}
	}
	return nil
}

func rejectIfNonPublic(ip net.IP) error {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("target host is not publicly routable")
	}
	return nil
}

// mustResolveTarget is the variant used inside handlers that need a
// stable credential_id for an audit row even when decryption fails;
// in that case we fall back to the legacy plaintext column so the
// audit row still correlates to the stored secret.
func mustResolveTarget(provider keymgmt.KeyProvider, enc, kekID, fallback string) string {
	if v, err := ResolveTarget(provider, enc, kekID, fallback); err == nil {
		return v
	}
	if fallback != "" {
		return fallback
	}
	return ""
}

// alertTestCooldown is the per-endpoint test cooldown (PAT-1502 PR 2).
const alertTestCooldown = time.Minute

// testAlertState tracks last-test timestamps for rate limiting.
// Tests inject their own clock via Server.testAlertNow; the default
// is time.Now. PAT-1502 PR 2.
type testAlertState struct {
	mu     sync.Mutex
	lastAt map[string]time.Time
	now    func() time.Time
}

func newTestAlertState() *testAlertState {
	return &testAlertState{lastAt: make(map[string]time.Time), now: time.Now}
}

// testAlertRateLimit returns true when the test is allowed. PAT-1502 PR 2.
func (s *Server) testAlertRateLimit(id string, now time.Time) bool {
	if s.testAlert == nil {
		s.testAlert = newTestAlertState()
	}
	s.testAlert.mu.Lock()
	defer s.testAlert.mu.Unlock()
	last, ok := s.testAlert.lastAt[id]
	if ok && now.Sub(last) < alertTestCooldown {
		return false
	}
	s.testAlert.lastAt[id] = now
	return true
}

// ctxWithTimeout is a tiny wrapper so callers can be concise. Kept
// here so the import lives in this file. PAT-1502 PR 2.
func ctxWithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}

// trimSpace is a tiny alias used by callers above to keep noise low.
func trimSpace(s string) string { return strings.TrimSpace(s) }
