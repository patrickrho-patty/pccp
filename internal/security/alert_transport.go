package security

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/netpolicy"
)

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type contextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// HTTPDoer is the narrow request seam used by API tests and the background
// dispatcher. Production callers use NewAlertHTTPClient.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// NewAlertHTTPClient returns an outbound client that resolves at dial time,
// rejects every non-public result, and connects to the validated IP rather
// than resolving the hostname a second time. Redirects are rejected.
func NewAlertHTTPClient(timeout time.Duration) *http.Client {
	return newAlertHTTPClient(timeout, net.DefaultResolver, &net.Dialer{Timeout: timeout})
}

func newAlertHTTPClient(timeout time.Duration, resolver ipResolver, dialer contextDialer) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.MaxConnsPerHost = 20
	transport.MaxIdleConnsPerHost = 10
	transport.MaxIdleConns = 100
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("alert_transport: invalid_address")
		}
		ips, err := resolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("alert_transport: dns_resolution_failed")
		}
		validated := make([]netip.Addr, 0, len(ips))
		for _, candidate := range ips {
			addr, ok := netip.AddrFromSlice(candidate.IP)
			if !ok || !netpolicy.IsPublicAddress(addr.Unmap()) {
				return nil, errors.New("alert_transport: target_not_public")
			}
			validated = append(validated, addr.Unmap())
		}
		var lastErr error
		for _, addr := range validated {
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, errors.New("alert_transport: connection_failed")
		}
		return nil, errors.New("alert_transport: no_valid_address")
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// ValidateAlertTarget validates provider-specific syntax without trusting a
// DNS result. DNS is checked again—and the accepted IP pinned—by the transport
// at the actual connection boundary.
func ValidateAlertTarget(providerType, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return errors.New("alert_target: invalid_url")
	}
	if err := validateAlertURL(u); err != nil {
		return err
	}
	if strings.EqualFold(providerType, "slack") {
		if u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "hooks.slack.com") || !strings.HasPrefix(u.EscapedPath(), "/services/") {
			return errors.New("alert_target: invalid_slack_webhook")
		}
	}
	return nil
}

func validateAlertURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.Hostname() == "" {
		return errors.New("alert_target: https_required")
	}
	if u.User != nil {
		return errors.New("alert_target: embedded_credentials_forbidden")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok || !netpolicy.IsPublicAddress(addr.Unmap()) {
			return errors.New("alert_transport: target_not_public")
		}
	}
	return nil
}

// AlertDeliveryErrorClass maps transport details to a bounded reason code for
// API responses, logs, and audit rows. It deliberately never returns raw URL,
// DNS, TLS, proxy, or provider error text.
func AlertDeliveryErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	msg := err.Error()
	for _, code := range []string{"target_not_public", "dns_resolution_failed", "invalid_address", "embedded_credentials_forbidden", "https_required"} {
		if strings.Contains(msg, code) {
			return code
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "timeout") {
		return "timeout"
	}
	return "delivery_failed"
}
