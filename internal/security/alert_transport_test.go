package security

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

type dialFunc func(context.Context, string, string) (net.Conn, error)

func (f dialFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

func TestAlertHTTPClientRejectsPrivateAddressAtDialTime(t *testing.T) {
	dialed := false
	client := newAlertHTTPClient(2*time.Second,
		resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}),
		dialFunc(func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("must not dial")
		}),
	)

	_, err := client.Get("http://rebind.example/alert")
	if err == nil || !strings.Contains(err.Error(), "target_not_public") {
		t.Fatalf("private resolution must fail with a scrubbed reason, got %v", err)
	}
	if dialed {
		t.Fatal("client dialed an address that resolved private")
	}
}

func TestAlertHTTPClientPinsValidatedAddress(t *testing.T) {
	var dialAddress string
	client := newAlertHTTPClient(2*time.Second,
		resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		}),
		dialFunc(func(_ context.Context, _, address string) (net.Conn, error) {
			dialAddress = address
			clientSide, serverSide := net.Pipe()
			go func() {
				defer serverSide.Close()
				buf := make([]byte, 4096)
				_, _ = serverSide.Read(buf)
				_, _ = serverSide.Write([]byte("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n"))
			}()
			return clientSide, nil
		}),
	)

	resp, err := client.Get("http://alerts.example:8080/test")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if dialAddress != "93.184.216.34:8080" {
		t.Fatalf("connection was not pinned to the validated IP: %q", dialAddress)
	}
}

func TestAlertClientRefusesEveryRedirect(t *testing.T) {
	client := NewAlertHTTPClient(time.Second)
	req, err := http.NewRequest(http.MethodGet, "https://redirect.example/next", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(req, []*http.Request{{}}); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect must be refused without replaying webhook credentials: %v", err)
	}
}

func TestValidateAlertTargetEnforcesProviderFormat(t *testing.T) {
	if err := ValidateAlertTarget("slack", "https://example.com/services/a/b/c"); err == nil {
		t.Fatal("Slack endpoint on a non-Slack host must be rejected")
	}
	if err := ValidateAlertTarget("slack", "https://hooks.slack.com/services/a/b/c"); err != nil {
		t.Fatalf("valid Slack webhook rejected: %v", err)
	}
	if err := ValidateAlertTarget("webhook", "http://127.0.0.1/private"); err == nil {
		t.Fatal("literal loopback target must be rejected")
	}
	if err := ValidateAlertTarget("webhook", "http://example.com/hook"); err == nil {
		t.Fatal("unencrypted generic webhook must be rejected")
	}
}
