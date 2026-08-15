package connectors

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dispatcher_test.go implements the Task 17 Step-2 vectors: dispatch
// only through stored scoped credentials, bounded retry ladder with
// 4xx no-retry, event-spine accounting, webhook signature validation.

func dispStack(t *testing.T, handler http.HandlerFunc) (*Dispatcher, *Connector) {
	t.Helper()
	svc := New()
	conn, err := svc.Register(Connector{
		Type:           TypeSlack,
		Name:           "ops-channel",
		OrganizationID: "org-x",
		BaseURL:        "https://hooks.example/x",
		AuthToken:      "stored-scoped-token",
		Status:         "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	conn.BaseURL = srv.URL
	d := NewDispatcher(svc)
	return d, conn
}

func TestDispatchRequiresStoredCredential(t *testing.T) {
	svc := New()
	conn, _ := svc.Register(Connector{Type: TypeSlack, Name: "no-cred", OrganizationID: "o", Status: "active"})
	d := NewDispatcher(svc)
	_, err := d.Dispatch(context.Background(), DispatchRequest{ConnectorID: conn.ID, Action: "send_message"})
	if !errors.Is(err, ErrScopedCredentialRequired) {
		t.Fatalf("expected scoped-credential refusal, got %v", err)
	}
}

func TestDispatchSuccessNormalizes(t *testing.T) {
	d, conn := dispStack(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer stored-scoped-token" {
			http.Error(w, "bad token", 401)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"ts":"123"}`))
	})
	out, err := d.Dispatch(context.Background(), DispatchRequest{
		ConnectorID: conn.ID, Action: "send_message", Payload: json.RawMessage(`{"text":"hi"}`),
	})
	if err != nil || !out.OK || out.Attempts != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if out.EventSpineRef == "" || !strings.HasPrefix(out.EventSpineRef, "sha256:") {
		t.Fatalf("spine ref = %q", out.EventSpineRef)
	}
	if len(d.Spine()) != 1 {
		t.Fatal("outcome not recorded to the spine")
	}
}

func TestDispatchRetriesTransientOnly(t *testing.T) {
	calls := 0
	d, conn := dispStack(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			// 5xx is transient → retry.
			http.Error(w, "boom", 500)
			return
		}
		_, _ = w.Write(nil)
	})
	out, err := d.Dispatch(context.Background(), DispatchRequest{
		ConnectorID: conn.ID, Action: "send_message", Payload: json.RawMessage(`{}`),
	})
	if err != nil || !out.OK {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if out.Attempts != 3 || calls != 3 {
		t.Fatalf("attempts=%d calls=%d", out.Attempts, calls)
	}
}

func TestDispatchDoesNotRetry4xx(t *testing.T) {
	calls := 0
	d, conn := dispStack(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "forbidden", 403)
	})
	out, err := d.Dispatch(context.Background(), DispatchRequest{
		ConnectorID: conn.ID, Action: "send_message", Payload: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("4xx must fail")
	}
	if out.Attempts != 1 || calls != 1 {
		t.Fatalf("4xx must not retry: attempts=%d calls=%d", out.Attempts, calls)
	}
	if !strings.Contains(out.Error, "403") {
		t.Fatalf("error = %q", out.Error)
	}
}

func TestDispatchDisabledConnector(t *testing.T) {
	d, conn := dispStack(t, func(w http.ResponseWriter, r *http.Request) {})
	_ = d.svc.Disable(conn.ID)
	_, err := d.Dispatch(context.Background(), DispatchRequest{ConnectorID: conn.ID, Action: "send_message"})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled refusal, got %v", err)
	}
}

func TestWebhookSignature(t *testing.T) {
	body := []byte(`{"event":"push"}`)
	mac := "not-the-mac"
	_ = mac
	valid := VerifyWebhookSignature("whsec", body, hmacSHA256("whsec", body))
	if !valid {
		t.Fatal("valid signature rejected")
	}
	if VerifyWebhookSignature("whsec", body, hmacSHA256("other", body)) {
		t.Fatal("forged signature accepted")
	}
	if VerifyWebhookSignature("whsec", []byte("tampered"), hmacSHA256("whsec", body)) {
		t.Fatal("tampered body accepted")
	}
}

func hmacSHA256(secret string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return mac.Sum(nil)
}
